package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"d7024e/kademlia"
)

var packetLossMode = flag.Bool("packet-loss", false, "run Experiment 2: node/value lookup reliability versus packet loss")
var packetLossOutput = flag.String("packet-loss-out", "experiments/packet_loss", "Experiment 2 output directory")
var packetLossResume = flag.Bool("packet-loss-resume", false, "reuse matching completed Experiment 2 checkpoints")

type lossAttempt struct {
	from, to, rpc                                          string
	requestDropped, replyAttempted, replyDropped, received bool
	started                                                time.Time
	rtt                                                    time.Duration
	onTime                                                 bool
}

type lossTrace struct {
	seed                 int64
	trial                int
	loss                 float64
	source, target, kind string
	attempts             []*lossAttempt
	latency              time.Duration
	timing               bool
	timeout              time.Duration
}

type lossPending struct {
	trace   *lossTrace
	attempt *lossAttempt
}
type packetLossState struct {
	mu      sync.Mutex
	active  *lossTrace
	pending map[string]lossPending
}

// Each distinct lookup/destination/direction has a seeded pseudorandom draw.
// Excluding crypto-random RPC IDs makes masks independent of goroutine order.
// Reusing draws across loss levels gives matched, nested loss masks.
func packetDrop(seed int64, trial int, kind, from, to, direction string, loss float64) bool {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%s|%s|%s|%s", seed, trial, kind, from, to, direction)))
	draw := float64(binary.BigEndian.Uint64(digest[:8])>>11) / float64(uint64(1)<<53)
	return draw < loss
}

type packetLossNode struct {
	kademlia.Node
	state *packetLossState
}

func (n *packetLossNode) SendData(to string, payload []byte) error {
	var message struct {
		Type          string `json:"type"`
		RequestID     string `json:"request_id"`
		SenderAddress string `json:"sender_address"`
		TargetID      string `json:"target_id"`
		Key           string `json:"key"`
	}
	if json.Unmarshal(payload, &message) != nil {
		return n.Node.SendData(to, payload)
	}
	drop := false
	var delay time.Duration
	n.state.mu.Lock()
	if message.Type == rpcFindNode || message.Type == rpcFindValue {
		kind, target := "node", message.TargetID
		if message.Type == rpcFindValue {
			kind, target = "value", message.Key
		}
		trace := n.state.active
		if trace != nil && trace.source == message.SenderAddress && trace.target == target && trace.kind == kind {
			attempt := &lossAttempt{from: message.SenderAddress, to: to, rpc: message.Type}
			if trace.timing {
				attempt.started = time.Now()
				delay = trace.latency
			}
			attempt.requestDropped = packetDrop(trace.seed, trace.trial, kind, attempt.from, to, "request", trace.loss)
			trace.attempts = append(trace.attempts, attempt)
			n.state.pending[message.RequestID] = lossPending{trace, attempt}
			drop = attempt.requestDropped
		}
	} else if strings.HasSuffix(message.Type, "_REPLY") {
		if pending, ok := n.state.pending[message.RequestID]; ok {
			if pending.trace.timing {
				delay = pending.trace.latency
			}
			pending.attempt.replyAttempted = true
			pending.attempt.replyDropped = packetDrop(pending.trace.seed, pending.trace.trial, pending.trace.kind, pending.attempt.from, pending.attempt.to, "reply", pending.trace.loss)
			drop = pending.attempt.replyDropped
		}
	}
	n.state.mu.Unlock()
	if drop {
		return nil
	}
	if delay > 0 {
		payload = append([]byte(nil), payload...)
		time.AfterFunc(delay, func() { _ = n.Node.SendData(to, payload) })
		return nil
	}
	return n.Node.SendData(to, payload)
}

func (n *packetLossNode) Receive() (kademlia.Message, error) {
	message, err := n.Node.Receive()
	if err != nil {
		return message, err
	}
	receivedAt := time.Now()
	var reply struct {
		Type string `json:"type"`
		ID   string `json:"request_id"`
	}
	if json.Unmarshal(message.Data, &reply) == nil && strings.HasSuffix(reply.Type, "_REPLY") {
		n.state.mu.Lock()
		if pending, ok := n.state.pending[reply.ID]; ok {
			pending.attempt.received = true
			if pending.trace.timing {
				pending.attempt.rtt = receivedAt.Sub(pending.attempt.started)
				pending.attempt.onTime = pending.attempt.rtt <= pending.trace.timeout
			}
		}
		n.state.mu.Unlock()
	}
	return message, nil
}

type lossResult struct {
	n, alpha, k, trial, replicas, probes, rounds int
	seed                                         int64
	loss, expected, elapsedMS                    float64
	kind, source, target                         string
	success, returnedContact                     bool
	attempts                                     []lossAttempt
	latency                                      time.Duration
}

func expectedLossSuccess(kind string, loss float64, replicas int) float64 {
	q := (1 - loss) * (1 - loss)
	if kind == "node" {
		return q
	}
	return 1 - math.Pow(1-q, float64(replicas))
}

func packetLossTrials(opts options, n, alpha, k int, seed int64, loss float64) ([]lossResult, error) {
	return packetLossTrialsWithLatency(opts, n, alpha, k, seed, loss, 0, false)
}

func packetLossTrialsWithLatency(opts options, n, alpha, k int, seed int64, loss float64, latency time.Duration, timing bool) ([]lossResult, error) {
	state := &packetLossState{pending: map[string]lossPending{}}
	nodes, err := scalingNetworkWithTransport(n, alpha, k, opts.timeout, seed, func(node kademlia.Node) kademlia.Node { return &packetLossNode{Node: node, state: state} })
	if err != nil {
		return nil, err
	}
	defer closeNodes(nodes)
	rng := rand.New(rand.NewSource(seed ^ 0x2deece66d))
	values := make([][]byte, opts.lookups)
	keys := make([]string, opts.lookups)
	for trial := range values {
		values[trial] = randomBytes(rng, opts.valueBytes)
		keys[trial], err = nodes[rng.Intn(n)].node.Store(values[trial])
		if err != nil {
			return nil, fmt.Errorf("healthy publishing: %w", err)
		}
	}
	// Publishing is synchronous and complete. There are no later writes to the
	// stores; inspect replica membership only now and choose remote-only sources.
	sources, targets, replicas := make([]int, opts.lookups), make([]int, opts.lookups), make([]int, opts.lookups)
	for trial := range values {
		var candidates []int
		for i, node := range nodes {
			if stored, ok := node.node.DataStore[keys[trial]]; ok && bytes.Equal(stored, values[trial]) {
				replicas[trial]++
			} else {
				candidates = append(candidates, i)
			}
		}
		want := k
		if want > n {
			want = n
		}
		if replicas[trial] != want {
			return nil, fmt.Errorf("healthy replica count %d, want %d", replicas[trial], want)
		}
		if len(candidates) == 0 {
			return nil, fmt.Errorf("no remote source: node-counts must exceed k")
		}
		sources[trial] = candidates[rng.Intn(len(candidates))]
		targets[trial] = rng.Intn(n - 1)
		if targets[trial] >= sources[trial] {
			targets[trial]++
		}
	}
	var results []lossResult
	for _, kind := range []string{"node", "value"} {
		for trial := 0; trial < opts.lookups; trial++ {
			source := sources[trial]
			target := keys[trial]
			if kind == "node" {
				target = nodes[targets[trial]].contact.ID.String()
			}
			trace := &lossTrace{seed: seed, trial: trial, loss: loss, source: nodes[source].contact.Address, target: target, kind: kind, latency: latency, timing: timing, timeout: opts.timeout}
			state.mu.Lock()
			state.active = trace
			state.mu.Unlock()
			returned := false
			started := time.Now()
			counts, success, err := measureScaling(nodes, source, target, kind, opts.timeout, func() bool {
				if kind == "node" {
					returned = contactFound(nodes[source].node.LookupContact(&nodes[targets[trial]].contact), nodes[targets[trial]].contact)
					state.mu.Lock()
					defer state.mu.Unlock()
					for _, a := range trace.attempts {
						if a.to == nodes[targets[trial]].contact.Address && a.received && (!timing || a.onTime) {
							return returned
						}
					}
					return false
				}
				got, err := nodes[source].node.LookupData(keys[trial])
				return err == nil && bytes.Equal(got, values[trial])
			})
			elapsed := float64(time.Since(started)) / float64(time.Millisecond)
			if err != nil {
				return nil, err
			}
			if timing {
				observeRequestDeadlines(state, trace)
			}
			r := lossResult{n: n, alpha: alpha, k: k, trial: trial, seed: seed, loss: loss, replicas: replicas[trial], kind: kind, source: trace.source, target: target, success: success, returnedContact: returned, probes: counts.probes, rounds: counts.rounds, elapsedMS: elapsed, expected: expectedLossSuccess(kind, loss, replicas[trial])}
			r.latency = latency
			state.mu.Lock()
			for _, a := range trace.attempts {
				r.attempts = append(r.attempts, *a)
			}
			state.active = nil
			state.mu.Unlock()
			if len(r.attempts) != r.probes {
				return nil, fmt.Errorf("packet/probe accounting mismatch")
			}
			results = append(results, r)
		}
	}
	return results, nil
}

func flagExplicit(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func runPacketLoss(opts options) error {
	if *resilienceMode {
		return fmt.Errorf("choose packet-loss or resilience, not both")
	}
	if !flagExplicit("node-counts") {
		opts.nodeCounts = []int{1000}
	}
	if !flagExplicit("alphas") {
		opts.alphas = []int{3}
	}
	if !flagExplicit("timeout") {
		opts.timeout = 50 * time.Millisecond
	}
	if opts.timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	text := *lossLevels
	if !flagExplicit("loss-rates") {
		text = "0,0.1,0.2,0.3,0.4,0.5,0.6,0.7,0.8,0.9,1"
	}
	losses, err := parseRates(text, true)
	if err != nil {
		return err
	}
	for _, n := range opts.nodeCounts {
		for _, k := range opts.kValues {
			if n <= k {
				return fmt.Errorf("packet-loss requires node count > k for remote value reads")
			}
		}
	}
	var results []lossResult
	for _, n := range opts.nodeCounts {
		for _, alpha := range opts.alphas {
			for _, k := range opts.kValues {
				for _, loss := range losses {
					for _, seed := range opts.seeds {
						fmt.Printf("packet loss n=%d alpha=%d k=%d loss=%.1f seed=%d\n", n, alpha, k, loss, seed)
						identity := fmt.Sprintf("v1|%d|%d|%d|%d|%.17g|%d|%d|%d", n, alpha, k, seed, loss, opts.lookups, opts.valueBytes, opts.timeout)
						checkpoint := filepath.Join(*packetLossOutput, ".checkpoints", fmt.Sprintf("%x.json", sha256.Sum256([]byte(identity))))
						var r []lossResult
						if *packetLossResume {
							r, err = readLossCheckpoint(checkpoint)
							if err != nil && !os.IsNotExist(err) {
								return err
							}
						}
						if len(r) == 0 {
							r, err = packetLossTrials(opts, n, alpha, k, seed, loss)
							if err != nil {
								return err
							}
							if err = writeLossCheckpoint(checkpoint, r); err != nil {
								return err
							}
							debug.FreeOSMemory()
						}
						if err != nil {
							return err
						}
						results = append(results, r...)
					}
				}
			}
		}
	}
	return writePacketLossResults(opts, losses, results, *packetLossOutput)
}

func writePacketLossResults(opts options, losses []float64, results []lossResult, dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	write := func(name string, rows [][]string) error {
		file, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		defer file.Close()
		writer := csv.NewWriter(file)
		if err := writer.WriteAll(rows); err != nil {
			return err
		}
		return writer.Error()
	}
	trials := [][]string{{"node_count", "alpha", "k", "loss", "seed", "trial", "lookup_type", "source", "target", "replicas", "success", "returned_contact", "probes", "rounds", "elapsed_ms", "expected_success"}}
	packets := [][]string{{"node_count", "alpha", "k", "loss", "seed", "trial", "lookup_type", "from", "to", "rpc", "request_dropped", "reply_attempted", "reply_dropped", "response_received"}}
	groups := map[string][]lossResult{}
	for _, r := range results {
		prefix := []string{strconv.Itoa(r.n), strconv.Itoa(r.alpha), strconv.Itoa(r.k), formatFloat(r.loss), strconv.FormatInt(r.seed, 10), strconv.Itoa(r.trial), r.kind}
		trials = append(trials, append(append([]string{}, prefix...), r.source, r.target, strconv.Itoa(r.replicas), strconv.FormatBool(r.success), strconv.FormatBool(r.returnedContact), strconv.Itoa(r.probes), strconv.Itoa(r.rounds), formatFloat(r.elapsedMS), formatFloat(r.expected)))
		for _, a := range r.attempts {
			packets = append(packets, append(append([]string{}, prefix...), a.from, a.to, a.rpc, strconv.FormatBool(a.requestDropped), strconv.FormatBool(a.replyAttempted), strconv.FormatBool(a.replyDropped), strconv.FormatBool(a.received)))
		}
		group := fmt.Sprintf("n%d-a%d-k%d-p%.4f-%s", r.n, r.alpha, r.k, r.loss, r.kind)
		groups[group] = append(groups[group], r)
	}
	if err := write("trials.csv", trials); err != nil {
		return err
	}
	if err := write("packets.csv", packets); err != nil {
		return err
	}
	summary := [][]string{{"node_count", "alpha", "k", "loss", "lookup_type", "samples", "seeds", "success_rate", "success_variance", "seed_success_variance", "success_seed_se", "expected_success", "avg_probes", "probe_variance", "avg_rounds", "round_variance", "avg_elapsed_ms", "elapsed_variance"}}
	var report strings.Builder
	report.WriteString("# Experiment 2: Lookup reliability as a function of packet loss\n\n## Experimental setup\n\n")
	fmt.Fprintf(&report, "N=%s; alpha=%s; k=%s; seeds=%s; %d trials per seed/type/loss; loss levels=%v; RPC timeout=%s. No latency, churn, retries or background repair.\n\n", intListString(opts.nodeCounts), intListString(opts.alphas), intListString(opts.kValues), seedListString(opts.seeds), opts.lookups, losses, opts.timeout)
	report.WriteString("Each configuration/loss/seed starts a fresh populated bounded network with unique seeded random IP:port addresses, SHA-256(IP|port) IDs, independently shuffled peer insertion and the implementation's 20-contact buckets. Topologies and randomly generated value/source/target workloads are matched across loss levels. Actual Store runs without loss first; the replica count is checked against min(k,N). Sources are chosen outside the replica set, excluding local value hits. Routing learning remains enabled between trials; node trials precede value trials. This tests lookup loss tolerance, not publishing under loss or joining.\n\nPacket dropping wraps every node's transport, covering both requests and replies. Independent seeded pseudorandom draws for each trial/type/source/destination/direction apply loss p to each packet. Stable SHA-256 draws exclude crypto-random RPC IDs and do not depend on send ordering; the same draw is reused across loss levels for matched nested masks. Replies retain their originating trial's mask even if late. The algorithm's scheduling and learned routing state can still affect which contacts are queried.\n\n## Measurements and correctness\n\nNode success means the target contact was returned AND a FIND_NODE response from that exact target arrived. Returning a cached contact after its probe times out is reported separately and cannot count as confirmed success. Value success requires a remote LookupData result exactly equal to the original generated bytes. Each launched request is counted at the transport; real batch callbacks count rounds. Packet CSV rows identify request drops, reply attempts/drops and received responses. Early successful values still account for every launched probe. A reply that arrives after completion can remain absent in the completion snapshot. Trial duration includes the final accounting wait and timeout costs.\n\nMeans and unbiased sample variances are reported for binary success, probes, rounds and elapsed time. Independent-seed success-rate variance and its standard error describe between-topology variability; trials sharing a seed are not treated as independent networks. Packet logs can reconcile the measured probe totals and distinguish request loss from reply loss.\n\n## Expected behavior\n\nFor independent per-packet loss p, one request/reply exchange succeeds with q=(1-p)^2. The node reference is q: it assumes the target can be reached directly and is queried once. The value reference is 1-(1-q)^r for r verified replicas: it assumes all r can be queried without extra routing failures. These are ideal-reachability references, not exact predictions for multi-step lookup. They isolate the expected benefit of replication. Healthy remote lookups should succeed; complete packet loss should make both confirmed node and remote value lookups fail.\n\nActual reliability can fall below the reference because lost intermediate replies prevent discovering the target or replicas, fewer replicas are queried, timed-out contacts remain in the shortlist, or a response misses the timeout. Finite-sample rates can lie above an expectation. Higher loss should generally reduce reliability and increase timeout costs; individual measured points need not be monotonic. Alpha provides parallel opportunities, whereas k provides replica redundancy. The Kademlia protocol's parallel node search, value replication and early value termination motivate this distinction: [original paper, section 2.2](https://pdos.csail.mit.edu/~petar/papers/maymounkov-kademlia.pdf).\n\n## Observed results\n\n| N | alpha | k | loss | type | samples | success mean | success variance | seed-rate variance | expected | mean probes | probe variance | mean rounds | round variance |\n|---:|---:|---:|---:|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	keys := []string{}
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := groups[keys[i]][0], groups[keys[j]][0]
		if a.n != b.n {
			return a.n < b.n
		}
		if a.alpha != b.alpha {
			return a.alpha < b.alpha
		}
		if a.k != b.k {
			return a.k < b.k
		}
		if a.loss != b.loss {
			return a.loss < b.loss
		}
		return a.kind < b.kind
	})
	for _, key := range keys {
		rs := groups[key]
		r := rs[0]
		binaryValues, probes, rounds, elapsed := []float64{}, []float64{}, []float64{}, []float64{}
		bySeed := map[int64][]float64{}
		var expected float64
		for _, s := range rs {
			v := 0.0
			if s.success {
				v = 1
			}
			binaryValues = append(binaryValues, v)
			probes = append(probes, float64(s.probes))
			rounds = append(rounds, float64(s.rounds))
			elapsed = append(elapsed, s.elapsedMS)
			bySeed[s.seed] = append(bySeed[s.seed], v)
			expected += s.expected
		}
		mean, variance := meanVariance(binaryValues)
		pm, pv := meanVariance(probes)
		rm, rv := meanVariance(rounds)
		em, ev := meanVariance(elapsed)
		seedMeans := []float64{}
		for _, values := range bySeed {
			m, _ := meanVariance(values)
			seedMeans = append(seedMeans, m)
		}
		_, sv := meanVariance(seedMeans)
		expected /= float64(len(rs))
		summary = append(summary, []string{strconv.Itoa(r.n), strconv.Itoa(r.alpha), strconv.Itoa(r.k), formatFloat(r.loss), r.kind, strconv.Itoa(len(rs)), strconv.Itoa(len(bySeed)), formatFloat(mean), formatFloat(variance), formatFloat(sv), formatFloat(math.Sqrt(sv / float64(len(bySeed)))), formatFloat(expected), formatFloat(pm), formatFloat(pv), formatFloat(rm), formatFloat(rv), formatFloat(em), formatFloat(ev)})
		fmt.Fprintf(&report, "| %d | %d | %d | %.2f | %s | %d | %.4f | %.4f | %.4f | %.4f | %.4f | %.4f | %.4f | %.4f |\n", r.n, r.alpha, r.k, r.loss, r.kind, len(rs), mean, variance, sv, expected, pm, pv, rm, rv)
	}
	if err := write("summary.csv", summary); err != nil {
		return err
	}
	report.WriteString("\n## Comparison and deviations\n\n")
	for _, row := range summary[1:] {
		actual, _ := strconv.ParseFloat(row[7], 64)
		expected, _ := strconv.ParseFloat(row[11], 64)
		if row[3] == "0.0000" || row[3] == "0.5000" || row[3] == "1.0000" {
			fmt.Fprintf(&report, "- N=%s, alpha=%s, k=%s, %s at loss=%s: actual %.1f%%, ideal reference %.1f%% (gap %.1f percentage points).\n", row[0], row[1], row[2], row[4], row[3], 100*actual, 100*expected, 100*(actual-expected))
		}
	}
	report.WriteString("\nUse packets.csv to inspect failed target confirmation and the number of received responses. Low value reliability relative to the ideal replica reference can reflect routing/shortlist limitations as well as finite-sample variation. This experiment deliberately uses the existing algorithm, including its timeout and failed-contact handling. It does not change the algorithm to match the reference.\n\n![Expected and actual lookup reliability](lookup_reliability_vs_loss.png)\n")
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte(report.String()), 0644); err != nil {
		return err
	}
	fmt.Printf("wrote %d Experiment 2 lookup trials to %s\n", len(results), dir)
	return nil
}
