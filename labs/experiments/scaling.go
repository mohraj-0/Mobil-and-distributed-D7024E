package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	mathrand "math/rand"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"d7024e/kademlia"
)

// A trace belongs to one source and target. Batch selection happens before
// goroutines launch; each selected destination is assigned its actual round.
// The buffered notification lets finish wait for sends from an early-returning
// value lookup without delaying the algorithm itself.
type scalingTrace struct {
	mu                       sync.Mutex
	source, target, kind     string
	rounds, expected, probes int
	destinations             map[string]int
	seen                     map[string]bool
	rows                     [][]string
	sent                     chan struct{}
}

func newScalingTrace(source, target, kind string) *scalingTrace {
	return &scalingTrace{source: source, target: target, kind: kind,
		destinations: make(map[string]int), seen: make(map[string]bool), sent: make(chan struct{}, 1)}
}

func (t *scalingTrace) batch(contacts []kademlia.Contact) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rounds++
	t.expected += len(contacts)
	for _, contact := range contacts {
		t.destinations[contact.Address] = t.rounds
	}
}

func (t *scalingTrace) send(to string, payload []byte) {
	var message rpcMessage
	if json.Unmarshal(payload, &message) != nil || message.SenderAddress != t.source {
		return
	}
	kind, target := "node", message.TargetID
	if message.Type == rpcFindValue {
		kind, target = "value", message.Key
	} else if message.Type != rpcFindNode {
		return
	}
	if kind != t.kind || target != t.target {
		return
	}
	t.mu.Lock()
	round, selected := t.destinations[to]
	if !selected || t.seen[to] {
		t.mu.Unlock()
		return
	}
	t.seen[to] = true
	t.probes++
	t.rows = append(t.rows, []string{strconv.Itoa(round), t.source, to, message.Type})
	t.mu.Unlock()
	select {
	case t.sent <- struct{}{}:
	default:
	}
}

func (t *scalingTrace) finish(timeout time.Duration) error {
	t.mu.Lock()
	expected := t.expected
	t.mu.Unlock()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		t.mu.Lock()
		complete := t.probes == expected
		t.mu.Unlock()
		if complete {
			return nil
		}
		select {
		case <-t.sent:
		case <-timer.C:
			return fmt.Errorf("probe accounting incomplete: expected %d sends", expected)
		}
	}
}

type scalingTransport struct {
	kademlia.Node
	mu    sync.RWMutex
	trace *scalingTrace
}

func (n *scalingTransport) SendData(address string, payload []byte) error {
	n.mu.RLock()
	trace := n.trace
	n.mu.RUnlock()
	err := n.Node.SendData(address, payload)
	if trace != nil {
		trace.send(address, payload)
	}
	return err
}

func (n *scalingTransport) setTrace(trace *scalingTrace) { n.mu.Lock(); n.trace = trace; n.mu.Unlock() }

func main() {
	opts, err := parseOptions()
	if err == nil {
		if *packetLossMode {
			err = runPacketLoss(opts)
		} else if *resilienceMode {
			err = runResilience(opts)
		} else {
			err = runScaling(opts)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func scalingNetwork(count, alpha, k int, timeout time.Duration, seed int64) ([]experimentNode, error) {
	return scalingNetworkWithTransport(count, alpha, k, timeout, seed, nil)
}

func scalingNetworkWithTransport(count, alpha, k int, timeout time.Duration, seed int64, wrap func(kademlia.Node) kademlia.Node) ([]experimentNode, error) {
	rng := mathrand.New(mathrand.NewSource(seed))
	addresses := randomAddresses(rng, count)
	simnet := kademlia.NewSimulatedNetwork()
	nodes := make([]experimentNode, 0, count)
	for _, address := range addresses {
		var inner kademlia.Node = kademlia.NewSimulatedNodeWithNetwork(simnet)
		if wrap != nil {
			inner = wrap(inner)
		}
		transport := &scalingTransport{Node: inner}
		if err := transport.Listen(address); err != nil {
			closeNodes(nodes)
			return nil, err
		}
		host, port, _ := net.SplitHostPort(address)
		contact := kademlia.NewContact(hashID(host+"|"+port), address)
		node := &kademlia.Kademlia{RoutingTable: kademlia.NewRoutingTable(contact), Network: transport, Alpha: alpha, K: k, RPCTimeout: timeout}
		nodes = append(nodes, experimentNode{node: node, contact: contact, network: transport})
	}
	// Independently shuffled insertion avoids giving every table the same oldest
	// contacts. Real bucket limits apply: this is a populated, bounded overlay,
	// not an all-to-all routing table or a test of the joining procedure.
	for i := range nodes {
		for _, j := range rng.Perm(count) {
			if i != j {
				nodes[i].node.RoutingTable.AddContact(nodes[j].contact)
			}
		}
	}
	for i := range nodes {
		go nodes[i].node.ListenForRPC()
	}
	return nodes, nil
}

func closestOracle(nodes []experimentNode, source int, target *kademlia.KademliaID, k int) []kademlia.Contact {
	contacts := make([]kademlia.Contact, 0, len(nodes)-1)
	for i := range nodes {
		if i != source {
			c := nodes[i].contact
			c.CalcDistance(target)
			contacts = append(contacts, c)
		}
	}
	sort.Slice(contacts, func(i, j int) bool { return contacts[i].Less(&contacts[j]) })
	if k < len(contacts) {
		contacts = contacts[:k]
	}
	return contacts
}

func sameContacts(got, want []kademlia.Contact) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i].ID == nil || !got[i].ID.Equals(want[i].ID) {
			return false
		}
	}
	return true
}

type scalingSample struct {
	config, kind, target               string
	seed                               int64
	n, alpha, k, trial, probes, rounds int
	success, local                     bool
}

func measureScaling(nodes []experimentNode, source int, target, kind string, timeout time.Duration, lookup func() bool) (*scalingTrace, bool, error) {
	trace := newScalingTrace(nodes[source].contact.Address, target, kind)
	transport := nodes[source].network.(*scalingTransport)
	transport.setTrace(trace)
	nodes[source].node.LookupBatch = trace.batch
	success := lookup()
	err := trace.finish(timeout)
	nodes[source].node.LookupBatch = nil
	transport.setTrace(nil)
	return trace, success, err
}

func runScaling(opts options) error {
	for _, path := range []string{opts.rawPath, opts.summaryPath, opts.reportPath} {
		if err := ensureParent(path); err != nil {
			return err
		}
	}
	file, err := os.Create(opts.rawPath)
	if err != nil {
		return err
	}
	defer file.Close()
	raw := csv.NewWriter(file)
	if err := raw.Write([]string{"config_id", "seed", "node_count", "alpha", "k", "trial", "lookup_type", "target", "event", "round", "from", "to", "rpc", "success", "local_hit", "probes", "rounds"}); err != nil {
		return err
	}
	samples := []scalingSample{}
	for _, n := range opts.nodeCounts {
		for _, alpha := range opts.alphas {
			for _, k := range opts.kValues {
				for _, seed := range opts.seeds {
					fmt.Printf("lookup sweep n=%d alpha=%d k=%d seed=%d\n", n, alpha, k, seed)
					nodes, err := scalingNetwork(n, alpha, k, opts.timeout, seed)
					if err != nil {
						return err
					}
					err = func() error {
						defer closeNodes(nodes)
						// Separate RNG streams keep workloads identical across alpha/k and
						// prevent scheduling or initialization work from consuming trial RNG.
						rng := mathrand.New(mathrand.NewSource(seed ^ 0x5deece66d))
						values := make([][]byte, opts.lookups)
						keys := make([]string, opts.lookups)
						sources := make([]int, opts.lookups)
						targets := make([]*kademlia.KademliaID, opts.lookups)
						for trial := 0; trial < opts.lookups; trial++ {
							values[trial] = randomBytes(rng, opts.valueBytes)
							sources[trial] = rng.Intn(n)
							targets[trial] = hashID(hexString(randomBytes(rng, 32)))
							key, err := nodes[rng.Intn(n)].node.Store(values[trial])
							if err != nil {
								return fmt.Errorf("store: %w", err)
							}
							keys[trial] = key
						}
						for _, kind := range []string{"node", "value"} {
							for trial := 0; trial < opts.lookups; trial++ {
								source := sources[trial]
								target := targets[trial].String()
								if kind == "value" {
									target = keys[trial]
								}
								trace, success, err := measureScaling(nodes, source, target, kind, opts.timeout, func() bool {
									if kind == "node" {
										got := nodes[source].node.LookupContact(&kademlia.Contact{ID: targets[trial]})
										return sameContacts(got, closestOracle(nodes, source, targets[trial], k))
									}
									got, err := nodes[source].node.LookupData(keys[trial])
									return err == nil && bytes.Equal(got, values[trial])
								})
								if err != nil {
									return err
								}
								s := scalingSample{config: fmt.Sprintf("n%d-a%d-k%d", n, alpha, k), seed: seed, n: n, alpha: alpha, k: k, trial: trial, kind: kind, target: target, success: success, local: kind == "value" && trace.probes == 0 && success, probes: trace.probes, rounds: trace.rounds}
								samples = append(samples, s)
								prefix := []string{s.config, strconv.FormatInt(seed, 10), strconv.Itoa(n), strconv.Itoa(alpha), strconv.Itoa(k), strconv.Itoa(trial), kind, target}
								for _, row := range trace.rows {
									if err := raw.Write(append(append([]string{}, prefix...), append([]string{"probe"}, append(row, "", "", "", "")...)...)); err != nil {
										return err
									}
								}
								if err := raw.Write(append(prefix, "result", "", nodes[source].contact.Address, "", "", strconv.FormatBool(success), strconv.FormatBool(s.local), strconv.Itoa(s.probes), strconv.Itoa(s.rounds))); err != nil {
									return err
								}
							}
						}
						return nil
					}()
					if err != nil {
						return fmt.Errorf("n%d-a%d-k%d seed%d: %w", n, alpha, k, seed, err)
					}
				}
			}
		}
	}
	raw.Flush()
	if err := raw.Error(); err != nil {
		return err
	}
	return writeScalingResults(opts, samples)
}

func hexString(data []byte) string { return fmt.Sprintf("%x", data) }

type scalingGroup struct{ samples []scalingSample }

// Cluster jackknife accounts for repeated trials on each seed's network and
// unequal remote-read sample counts without treating trials as independent seeds.
func (g scalingGroup) seedStandardErrors() (float64, float64) {
	type totals struct {
		count          int
		probes, rounds float64
	}
	bySeed := map[int64]totals{}
	var p, r float64
	for _, s := range g.samples {
		t := bySeed[s.seed]
		t.count++
		t.probes += float64(s.probes)
		t.rounds += float64(s.rounds)
		bySeed[s.seed] = t
		p += float64(s.probes)
		r += float64(s.rounds)
	}
	if len(bySeed) < 2 {
		return 0, 0
	}
	leaveP, leaveR := []float64{}, []float64{}
	for _, t := range bySeed {
		count := float64(len(g.samples) - t.count)
		leaveP = append(leaveP, (p-t.probes)/count)
		leaveR = append(leaveR, (r-t.rounds)/count)
	}
	_, pv := meanVariance(leaveP)
	_, rv := meanVariance(leaveR)
	s := float64(len(bySeed))
	return math.Sqrt(pv * (s - 1) * (s - 1) / s), math.Sqrt(rv * (s - 1) * (s - 1) / s)
}

func meanVariance(values []float64) (float64, float64) {
	var mean, m2 float64
	for i, value := range values {
		addSample(&mean, &m2, i+1, value)
	}
	if len(values) < 2 {
		return mean, 0
	}
	return mean, m2 / float64(len(values)-1)
}

func (g scalingGroup) statistics() (pm, pv, rm, rv, seedMeanVariance, roundSeedVariance float64, local, success, seeds int) {
	probes, rounds := []float64{}, []float64{}
	bySeed := map[int64][]scalingSample{}
	for _, s := range g.samples {
		probes = append(probes, float64(s.probes))
		rounds = append(rounds, float64(s.rounds))
		bySeed[s.seed] = append(bySeed[s.seed], s)
		if s.local {
			local++
		}
		if s.success {
			success++
		}
	}
	pm, pv = meanVariance(probes)
	rm, rv = meanVariance(rounds)
	seedMeans, roundMeans := []float64{}, []float64{}
	for _, ss := range bySeed {
		var p, r float64
		for _, s := range ss {
			p += float64(s.probes)
			r += float64(s.rounds)
		}
		seedMeans = append(seedMeans, p/float64(len(ss)))
		roundMeans = append(roundMeans, r/float64(len(ss)))
	}
	_, seedMeanVariance = meanVariance(seedMeans)
	_, roundSeedVariance = meanVariance(roundMeans)
	seeds = len(bySeed)
	return
}

func writeScalingResults(opts options, samples []scalingSample) error {
	groups := map[string]*scalingGroup{}
	for _, s := range samples {
		for _, scope := range []string{"all", "remote"} {
			if scope == "remote" && (s.kind != "value" || s.local) {
				continue
			}
			key := s.config + "/" + s.kind + "/" + scope
			if groups[key] == nil {
				groups[key] = &scalingGroup{}
			}
			groups[key].samples = append(groups[key].samples, s)
		}
	}
	keys := []string{}
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := groups[keys[i]].samples[0], groups[keys[j]].samples[0]
		if a.n != b.n {
			return a.n < b.n
		}
		return keys[i] < keys[j]
	})
	file, err := os.Create(opts.summaryPath)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	if err := writer.Write([]string{"config_id", "node_count", "alpha", "k", "lookup_type", "scope", "samples", "seeds", "success_rate", "local_hits", "avg_probes", "probe_variance", "avg_rounds", "round_variance", "seed_mean_probe_variance", "seed_mean_round_variance", "probe_seed_se", "round_seed_se"}); err != nil {
		return err
	}
	var report strings.Builder
	report.WriteString("# Kademlia lookup scaling experiment\n\n## Setup\n\n")
	fmt.Fprintf(&report, "Network sizes: %s. Alpha: %s. Replication/shortlist k: %s. Seeds: %s. %d trials per seed and lookup type; %d-byte random values.\n\n", intListString(opts.nodeCounts), intListString(opts.alphas), intListString(opts.kValues), seedListString(opts.seeds), opts.lookups, opts.valueBytes)
	report.WriteString("Each configuration/seed gets a fresh lossless in-process network. Unique seeded random IP:port addresses produce SHA-256 IDs from IP|port, matching the simulator shell. Each routing table receives an independently shuffled peer list, subject to the implementation's 20-contact bucket limit and splitting rules. This models a populated healthy overlay, not network joining. It is not an unrestricted complete routing graph.\n\nSeparate seeded RNG streams generate workloads: random values with content-addressed SHA-256 keys, publishers, sources, and random target IDs. The same seed/N reuses the same addresses, insertion order and workload across alpha/k. Values are stored through the real Store method before measurements; all node trials precede value trials. Routing learning from publishing and earlier trials remains enabled. Seeds reproduce inputs, not goroutine scheduling or timestamps.\n\n## Measurements and correctness\n\nA probe is one outgoing FIND_NODE/FIND_VALUE request counted at SendData, filtered by source, type, target and selected destination. Replies and publishing traffic are excluded. A read-only batch observer records actual rounds and selected destinations before RPC goroutines start; it does not change routing decisions. Accounting waits for every launched request to reach the transport, including sends that follow an early successful value return. Measured rounds are sequential batches, not IP packet hops or an inferred ceil(probes/alpha).\n\nNode success requires the complete ordered k-closest result to match an independent global XOR-distance sort, excluding the initiating node as the implementation does. Value success requires exact equality with the original stored bytes. Local reads are identified and remote value results are also reported separately. Raw result/probe rows permit reconciliation of batch sizes and probe counts.\n\nSample variances use denominator m-1 across lookup trials. Seed-mean variances describe variation between independently generated networks; plot standard errors use a leave-one-seed-out cluster jackknife, avoiding treating all trials in one network as independent networks and accounting for unequal remote sample counts.\n\n## Expected behavior and comparison\n\nThe original Kademlia analysis and Cai–Devroye predict logarithmic routing growth under healthy bucket coverage/random-ID assumptions. Node lookup must discover and verify the closest set; this implementation keeps alpha-sized batches throughout completion, adding a baseline near k probes. Successful value lookup stops at the first replica, so generally needs fewer probes/rounds. Larger alpha may increase probes while reducing rounds. Local replicas yield zero probes and are separated from remote reads.\n\nThe plot overlays an empirical logarithmic-shape reference P(N)=P(N_min)+alpha*log2(N/N_min) and R(N)=R(N_min)+log2(N/N_min), one additional discovery batch per doubling. These are deliberately labelled anchored references, not exact theoretical predictions or proven bounds: bucket capacity, multiple contacts per reply, populated tables, termination, and asynchronous scheduling affect the coefficient and intercept. The plot also fits a+b*log2(N) descriptively; agreement over this finite range is not proof of asymptotic complexity.\n\nSources: [Maymounkov–Mazières, §§2.2–3](https://pdos.csail.mit.edu/~petar/papers/maymounkov-kademlia.pdf); [Cai–Devroye, random-ID analysis](https://arxiv.org/abs/1402.1191).\n\n## Observed averages and variance\n\n| config | type | scope | trials | seeds | success | probes mean | probes variance | rounds mean | rounds variance | seed-mean probe variance |\n|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, key := range keys {
		g := *groups[key]
		s := g.samples[0]
		scope := strings.Split(key, "/")[2]
		pm, pv, rm, rv, sv, rsv, local, success, seeds := g.statistics()
		pse, rse := g.seedStandardErrors()
		count := len(g.samples)
		row := []string{s.config, strconv.Itoa(s.n), strconv.Itoa(s.alpha), strconv.Itoa(s.k), s.kind, scope, strconv.Itoa(count), strconv.Itoa(seeds), formatFloat(float64(success) / float64(count)), strconv.Itoa(local), formatFloat(pm), formatFloat(pv), formatFloat(rm), formatFloat(rv), formatFloat(sv), formatFloat(rsv), formatFloat(pse), formatFloat(rse)}
		if err := writer.Write(row); err != nil {
			return err
		}
		fmt.Fprintf(&report, "| %s | %s | %s | %d | %d | %.4f | %.4f | %.4f | %.4f | %.4f | %.4f |\n", s.config, s.kind, scope, count, seeds, float64(success)/float64(count), pm, pv, rm, rv, sv)
	}
	report.WriteString("\n## Interpretation\n\n")
	for _, alpha := range opts.alphas {
		for _, k := range opts.kValues {
			for _, kind := range []string{"node", "value"} {
				var low, high *scalingGroup
				for _, key := range keys {
					g := groups[key]
					s := g.samples[0]
					if s.alpha != alpha || s.k != k || s.kind != kind || !strings.HasSuffix(key, "/all") {
						continue
					}
					if low == nil || s.n < low.samples[0].n {
						low = g
					}
					if high == nil || s.n > high.samples[0].n {
						high = g
					}
				}
				if low == nil {
					continue
				}
				lp, _, lr, _, _, _, _, _, _ := low.statistics()
				hp, _, hr, _, _, _, _, _, _ := high.statistics()
				fmt.Fprintf(&report, "- %s, alpha=%d, k=%d: N=%d→%d, probes %.3f→%.3f, measured rounds %.3f→%.3f.\n", kind, alpha, k, low.samples[0].n, high.samples[0].n, lp, hp, lr, hr)
			}
		}
	}
	failed := 0
	for _, s := range samples {
		if !s.success {
			failed++
		}
	}
	fmt.Fprintf(&report, "\n%d/%d lookup correctness checks failed. Failures must be investigated rather than interpreted as cheap successful lookups.\n\n", failed, len(samples))
	report.WriteString("Slow growth relative to the one-bit-per-round reference is compatible with populated bounded buckets and learning: a reply can skip several prefix levels. A near-k node baseline measures final shortlist confirmation, not ten discovery hops. Value deviations also reflect early termination and the changing fraction of local replicas. Small non-monotonic changes can arise from topology/workload variation; use seed-level uncertainty. Larger-alpha probe increases can come from speculative requests launched before a successful reply. Timing, churn, join quality, and production-network latency are not tested here. Bucket capacity remains 20 independently of k=10 and is explicitly a property of this implementation.\n")
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}
	if err := os.WriteFile(opts.reportPath, []byte(report.String()), 0644); err != nil {
		return err
	}
	fmt.Printf("wrote %d lookup results to %s, %s and %s\n", len(samples), opts.rawPath, opts.summaryPath, opts.reportPath)
	return nil
}
