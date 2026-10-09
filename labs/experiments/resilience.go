package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"d7024e/kademlia"
)

var (
	resilienceMode   = flag.Bool("resilience", false, "run latency, loss, alpha, churn, and replication sweeps")
	lossLevels       = flag.String("loss-rates", "0,0.1,0.3", "per-packet loss probabilities")
	latencyLevels    = flag.String("latencies", "0ms,5ms,20ms", "one-way packet delays")
	churnLevels      = flag.String("churn-rates", "0,0.1,0.3", "per-node departures per simulated second")
	churnWindow      = flag.Duration("churn-window", time.Second, "simulated exposure between storage and lookup")
	resilienceOutput = flag.String("resilience-out", "experiments/resilience", "output directory")
)

// Conditions are shared by all transports, including the response path.
type conditions struct {
	mu       sync.Mutex
	rng      *rand.Rand
	loss     float64
	latency  time.Duration
	requests map[string]*requestTiming
}

type requestTiming struct {
	rpc       string
	to        string
	started   time.Time
	elapsed   time.Duration
	completed bool
}

type impairedNode struct {
	kademlia.Node
	conditions *conditions
}

func (node *impairedNode) SendData(to string, data []byte) error {
	c := node.conditions
	c.mu.Lock()
	var message struct {
		Type string `json:"type"`
		ID   string `json:"request_id"`
	}
	if c.requests != nil && json.Unmarshal(data, &message) == nil && (message.Type == rpcFindNode || message.Type == rpcFindValue) {
		c.requests[message.ID] = &requestTiming{rpc: message.Type, to: to, started: time.Now()}
	}
	drop := c.rng.Float64() < c.loss
	delay := c.latency
	c.mu.Unlock()
	if drop {
		return nil // UDP packet loss is silent; the RPC must time out.
	}
	if delay == 0 {
		return node.Node.SendData(to, data)
	}
	payload := append([]byte(nil), data...)
	time.AfterFunc(delay, func() { _ = node.Node.SendData(to, payload) })
	return nil
}

func (node *impairedNode) Receive() (kademlia.Message, error) {
	msg, err := node.Node.Receive()
	if err != nil {
		return msg, err
	}
	var reply struct {
		Type string `json:"type"`
		ID   string `json:"request_id"`
	}
	if json.Unmarshal(msg.Data, &reply) == nil && strings.HasSuffix(reply.Type, "_REPLY") {
		c := node.conditions
		c.mu.Lock()
		if pending := c.requests[reply.ID]; pending != nil && !pending.completed {
			pending.elapsed, pending.completed = time.Since(pending.started), true
		}
		c.mu.Unlock()
	}
	return msg, nil
}

type scenario struct {
	axis     string
	loss     float64
	latency  time.Duration
	churn    float64
	alpha, k int
}

type trialResult struct {
	metric            lookupMetric
	elapsed           time.Duration
	departures, joins int
	storeError        string
	requests          []requestTiming
}

// Churn is a discrete exposure model: each original node departs independently
// with probability 1-exp(-rate*window), then a fresh node joins in its place.
func departureProbability(rate float64, window time.Duration) float64 {
	return 1 - math.Exp(-rate*window.Seconds())
}

func resilienceTrial(opts options, recorder *lookupRecorder, config string, s scenario, count int, seed int64, run int) (trialResult, error) {
	rng := rand.New(rand.NewSource(seed + int64(run)*100003))
	c := &conditions{rng: rand.New(rand.NewSource(seed + int64(run)*100019))}
	simnet := kademlia.NewSimulatedNetwork()
	var nodes []experimentNode
	defer func() { closeNodes(nodes) }()
	newNode := func(address string) (experimentNode, error) {
		transport := &observedNode{inner: &impairedNode{Node: kademlia.NewSimulatedNodeWithNetwork(simnet), conditions: c}, recorder: recorder}
		if err := transport.Listen(address); err != nil {
			return experimentNode{}, err
		}
		contact := kademlia.NewContact(hashID(address), address)
		node := &kademlia.Kademlia{RoutingTable: kademlia.NewRoutingTable(contact), Network: transport, Alpha: s.alpha, K: s.k, RPCTimeout: opts.timeout}
		return experimentNode{node: node, contact: contact, network: transport}, nil
	}
	for _, address := range randomAddresses(rng, count) {
		node, err := newNode(address)
		if err != nil {
			return trialResult{}, err
		}
		nodes = append(nodes, node)
	}
	for i := range nodes {
		for j := range nodes {
			if i != j {
				nodes[i].node.RoutingTable.AddContact(nodes[j].contact)
			}
		}
		go nodes[i].node.ListenForRPC()
	}
	value := randomBytes(rng, opts.valueBytes)
	key, storeErr := nodes[0].node.Store(value)
	result := trialResult{}
	if storeErr != nil {
		result.storeError = storeErr.Error()
	}
	p := departureProbability(s.churn, *churnWindow)
	for i := range nodes {
		if rng.Float64() >= p {
			continue
		}
		old := nodes[i]
		if err := old.network.Close(); err != nil {
			return result, err
		}
		result.departures++
		address := fmt.Sprintf("172.16.%d.%d:%d", run/250%250, i/250%250, 10000+i)
		fresh, err := newNode(address)
		if err != nil {
			return result, err
		}
		nodes[i] = fresh
		result.joins++
	}
	// Seed replacement routing tables from current membership. Survivors retain
	// stale contacts; new nodes have no values. No background repair is simulated.
	if result.joins > 0 {
		for i := range nodes {
			if !strings.HasPrefix(nodes[i].contact.Address, "172.16.") {
				continue
			}
			for j := range nodes {
				if i != j {
					nodes[i].node.RoutingTable.AddContact(nodes[j].contact)
				}
			}
		}
		// Existing listeners stopped on Close; only replacements need new loops.
		// Identify replacements by their 172.16 prefix.
		for i := range nodes {
			if strings.HasPrefix(nodes[i].contact.Address, "172.16.") {
				go nodes[i].node.ListenForRPC()
			}
		}
	}
	c.mu.Lock()
	c.loss, c.latency = s.loss, s.latency
	c.requests = make(map[string]*requestTiming)
	c.mu.Unlock()
	lookupType, target := "value", key
	if s.axis == "alpha" || s.axis == "latency_loss" {
		lookupType, target = "node", nodes[0].contact.ID.String()
	}
	recorder.startLookup(config, seed, count, s.alpha, s.k, run, lookupType, target)
	started := time.Now()
	success, errText := false, ""
	if lookupType == "node" {
		success = contactFound(nodes[1].node.LookupContact(&nodes[0].contact), nodes[0].contact)
		c.mu.Lock()
		responded := false
		for _, request := range c.requests {
			if request.to == nodes[0].contact.Address && request.completed {
				responded = true
			}
		}
		c.mu.Unlock()
		success = success && responded
		if !success {
			errText = "target not found or did not respond"
		}
	} else if storeErr != nil {
		errText = "store failed: " + storeErr.Error()
	} else {
		got, lookupErr := nodes[1].node.LookupData(key)
		success = lookupErr == nil && bytes.Equal(got, value)
		if lookupErr != nil {
			errText = lookupErr.Error()
		} else if !success {
			errText = "value mismatch"
		}
	}
	result.elapsed = time.Since(started)
	result.metric = recorder.finishLookup(success, errText)
	c.mu.Lock()
	for _, request := range c.requests {
		result.requests = append(result.requests, *request)
	}
	c.requests = nil
	c.mu.Unlock()
	return result, nil
}

func parseRates(text string, probability bool) ([]float64, error) {
	var values []float64
	for _, part := range strings.Split(text, ",") {
		v, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || probability && v > 1 {
			return nil, fmt.Errorf("invalid rate %q", part)
		}
		values = append(values, v)
	}
	return values, nil
}

func runResilience(opts options) error {
	losses, err := parseRates(*lossLevels, true)
	if err != nil {
		return err
	}
	churns, err := parseRates(*churnLevels, false)
	if err != nil {
		return err
	}
	if *churnWindow <= 0 || opts.timeout <= 0 {
		return fmt.Errorf("churn-window and timeout must be positive")
	}
	var delays []time.Duration
	for _, text := range strings.Split(*latencyLevels, ",") {
		d, err := time.ParseDuration(strings.TrimSpace(text))
		if err != nil || d < 0 {
			return fmt.Errorf("invalid latency %q", text)
		}
		delays = append(delays, d)
	}
	base := scenario{alpha: opts.alphas[0], k: opts.kValues[len(opts.kValues)-1]}
	var scenarios []scenario
	for _, loss := range losses {
		for _, delay := range delays {
			s := base
			s.axis, s.loss, s.latency = "latency_loss", loss, delay
			scenarios = append(scenarios, s)
		}
	}
	for _, alpha := range opts.alphas {
		s := base
		s.axis, s.alpha, s.latency = "alpha", alpha, delays[len(delays)-1]
		scenarios = append(scenarios, s)
	}
	for _, churn := range churns {
		for _, k := range opts.kValues {
			s := base
			s.axis, s.churn, s.k = "churn_replication", churn, k
			scenarios = append(scenarios, s)
		}
	}
	dir := *resilienceOutput
	recorder, err := newLookupRecorder(filepath.Join(dir, "probes.csv"))
	if err != nil {
		return err
	}
	defer recorder.close()
	file, err := os.Create(filepath.Join(dir, "trials.csv"))
	if err != nil {
		return err
	}
	defer file.Close()
	w := csv.NewWriter(file)
	defer w.Flush()
	rpcFile, err := os.Create(filepath.Join(dir, "requests.csv"))
	if err != nil {
		return err
	}
	defer rpcFile.Close()
	rpcCSV := csv.NewWriter(rpcFile)
	defer rpcCSV.Flush()
	if err := rpcCSV.Write([]string{"config_id", "seed", "trial", "rpc", "response_received", "response_ms"}); err != nil {
		return err
	}
	if err := w.Write([]string{"config_id", "axis", "nodes", "alpha", "k", "loss", "latency_ms", "churn_per_node_second", "exposure_seconds", "seed", "trial", "type", "success", "probes", "estimated_hops", "elapsed_ms", "departures", "joins", "store_error", "lookup_error"}); err != nil {
		return err
	}
	var report strings.Builder
	report.WriteString("# Resilience experiment results\n\nSee [experiment documentation](../README.md) for controls and limitations. Timing includes timeout waits. Each trial uses a fresh network; storage occurs before impairments. Churn is a discrete simulated exposure, not continuous changes during RPCs.\n\nExpected behavior: successful RPC time is roughly twice one-way latency; packet loss increases missing responses and timeout costs. Increasing alpha should reduce sequential rounds at a similar probe count, though wider batches can send more probes. Higher churn removes stored replicas; larger k improves replica survival but also changes routing shortlist size. Compare enough seeds and trials before drawing conclusions. Successful-response mean RTT excludes missing responses.\n\n| axis | nodes | alpha | k | loss | latency ms | churn /node/s | samples | success rate | mean probes | mean elapsed ms | response fraction | mean RTT ms |\n| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for index, s := range scenarios {
		for _, count := range opts.nodeCounts {
			var summary stats
			var elapsed float64
			var requestCount, responseCount int
			var responseMS float64
			config := fmt.Sprintf("s%d-n%d-a%d-k%d", index, count, s.alpha, s.k)
			for _, seed := range opts.seeds {
				for run := 0; run < opts.lookups; run++ {
					r, err := resilienceTrial(opts, recorder, config, s, count, seed, run)
					if err != nil {
						return err
					}
					summary.add(r.metric)
					ms := float64(r.elapsed) / float64(time.Millisecond)
					elapsed += ms
					row := []string{config, s.axis, strconv.Itoa(count), strconv.Itoa(s.alpha), strconv.Itoa(s.k), formatFloat(s.loss), formatFloat(float64(s.latency) / float64(time.Millisecond)), formatFloat(s.churn), formatFloat(churnWindow.Seconds()), strconv.FormatInt(seed, 10), strconv.Itoa(run), r.metric.LookupType, strconv.FormatBool(r.metric.Success), strconv.Itoa(r.metric.Probes), strconv.Itoa(r.metric.Hops), formatFloat(ms), strconv.Itoa(r.departures), strconv.Itoa(r.joins), r.storeError, r.metric.Error}
					if err := w.Write(row); err != nil {
						return err
					}
					for _, request := range r.requests {
						requestCount++
						ms := ""
						if request.completed {
							responseCount++
							responseMS += float64(request.elapsed) / float64(time.Millisecond)
							ms = formatFloat(float64(request.elapsed) / float64(time.Millisecond))
						}
						if err := rpcCSV.Write([]string{config, strconv.FormatInt(seed, 10), strconv.Itoa(run), request.rpc, strconv.FormatBool(request.completed), ms}); err != nil {
							return err
						}
					}
				}
			}
			responseFraction, meanRTT := "n/a", "n/a"
			if requestCount > 0 {
				responseFraction = formatFloat(float64(responseCount) / float64(requestCount))
			}
			if responseCount > 0 {
				meanRTT = formatFloat(responseMS / float64(responseCount))
			}
			fmt.Fprintf(&report, "| %s | %d | %d | %d | %.3f | %.3f | %.3f | %d | %.4f | %.4f | %.4f | %s | %s |\n", s.axis, count, s.alpha, s.k, s.loss, float64(s.latency)/float64(time.Millisecond), s.churn, summary.Count, float64(summary.Successes)/float64(summary.Count), summary.ProbeMean, elapsed/float64(summary.Count), responseFraction, meanRTT)
		}
	}
	w.Flush()
	rpcCSV.Flush()
	if err := rpcCSV.Error(); err != nil {
		return err
	}
	if err := w.Error(); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "report.md"), []byte(report.String()), 0644)
}
