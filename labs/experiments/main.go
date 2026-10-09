package main

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	mathrand "math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"d7024e/kademlia"
)

const (
	rpcFindNode  = "FIND_NODE"
	rpcFindValue = "FIND_VALUE"
)

type rpcMessage struct {
	Type          string `json:"type"`
	SenderAddress string `json:"sender_address"`
	TargetID      string `json:"target_id,omitempty"`
	Key           string `json:"key,omitempty"`
}

type options struct {
	nodeCounts  []int
	alphas      []int
	kValues     []int
	seeds       []int64
	lookups     int
	valueBytes  int
	timeout     time.Duration
	rawPath     string
	summaryPath string
	reportPath  string
}

type experimentNode struct {
	node    *kademlia.Kademlia
	contact kademlia.Contact
	network kademlia.Node
}

type observedNode struct {
	inner    kademlia.Node
	recorder *lookupRecorder
}

func (node *observedNode) Listen(address string) error {
	return node.inner.Listen(address)
}

func (node *observedNode) Close() error {
	return node.inner.Close()
}

func (node *observedNode) Receive() (kademlia.Message, error) {
	return node.inner.Receive()
}

func (node *observedNode) SendData(address string, data []byte) error {
	node.recorder.recordSend(address, data)
	return node.inner.SendData(address, data)
}

type lookupRecorder struct {
	mu      sync.Mutex
	file    *os.File
	writer  *csv.Writer
	active  *activeLookup
	counter int
}

type activeLookup struct {
	configID   string
	seed       int64
	nodeCount  int
	alpha      int
	kValue     int
	runIndex   int
	id         string
	lookupType string
	target     string
	probes     int
}

type lookupMetric struct {
	ConfigID   string
	Seed       int64
	NodeCount  int
	Alpha      int
	KValue     int
	RunIndex   int
	LookupType string
	Target     string
	Success    bool
	Error      string
	Probes     int
	Hops       int
}

func newLookupRecorder(path string) (*lookupRecorder, error) {
	if err := ensureParent(path); err != nil {
		return nil, err
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	writer := csv.NewWriter(file)
	if err := writer.Write([]string{
		"timestamp",
		"config_id",
		"seed",
		"node_count",
		"alpha",
		"k",
		"run_index",
		"lookup_id",
		"lookup_type",
		"target",
		"hop",
		"event",
		"from",
		"to",
		"rpc",
		"success",
		"error",
		"metric_count",
	}); err != nil {
		file.Close()
		return nil, err
	}
	writer.Flush()

	return &lookupRecorder{file: file, writer: writer}, nil
}

func (recorder *lookupRecorder) close() error {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()

	recorder.writer.Flush()
	if err := recorder.writer.Error(); err != nil {
		_ = recorder.file.Close()
		return err
	}
	return recorder.file.Close()
}

func (recorder *lookupRecorder) startLookup(configID string, seed int64, nodeCount int, alpha int, kValue int, runIndex int, lookupType string, target string) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()

	recorder.counter++
	recorder.active = &activeLookup{
		configID:   configID,
		seed:       seed,
		nodeCount:  nodeCount,
		alpha:      alpha,
		kValue:     kValue,
		runIndex:   runIndex,
		id:         fmt.Sprintf("%s-%05d", lookupType, recorder.counter),
		lookupType: lookupType,
		target:     target,
	}
}

func (recorder *lookupRecorder) finishLookup(success bool, errText string) lookupMetric {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()

	if recorder.active == nil {
		return lookupMetric{}
	}

	active := *recorder.active
	hops := hopsFor(active.probes, active.alpha)
	recorder.writeLocked(active, hops, "result", "", "", "", success, errText, active.probes)
	recorder.active = nil
	recorder.writer.Flush()

	return lookupMetric{
		ConfigID:   active.configID,
		Seed:       active.seed,
		NodeCount:  active.nodeCount,
		Alpha:      active.alpha,
		KValue:     active.kValue,
		RunIndex:   active.runIndex,
		LookupType: active.lookupType,
		Target:     active.target,
		Success:    success,
		Error:      errText,
		Probes:     active.probes,
		Hops:       hops,
	}
}

func (recorder *lookupRecorder) recordSend(to string, data []byte) {
	var message rpcMessage
	if err := json.Unmarshal(data, &message); err != nil {
		return
	}
	if message.Type != rpcFindNode && message.Type != rpcFindValue {
		return
	}

	recorder.mu.Lock()
	defer recorder.mu.Unlock()

	if recorder.active == nil {
		return
	}

	expectedType := "node"
	target := message.TargetID
	if message.Type == rpcFindValue {
		expectedType = "value"
		target = message.Key
	}
	if recorder.active.lookupType != expectedType || recorder.active.target != target {
		return
	}

	recorder.active.probes++
	hop := hopsFor(recorder.active.probes, recorder.active.alpha)
	recorder.writeLocked(*recorder.active, hop, "probe", message.SenderAddress, to, message.Type, true, "", recorder.active.probes)
	recorder.writer.Flush()
}

func (recorder *lookupRecorder) writeLocked(active activeLookup, hop int, event string, from string, to string, rpc string, success bool, errText string, metricCount int) {
	_ = recorder.writer.Write([]string{
		time.Now().Format(time.RFC3339Nano),
		active.configID,
		strconv.FormatInt(active.seed, 10),
		strconv.Itoa(active.nodeCount),
		strconv.Itoa(active.alpha),
		strconv.Itoa(active.kValue),
		strconv.Itoa(active.runIndex),
		active.id,
		active.lookupType,
		active.target,
		strconv.Itoa(hop),
		event,
		from,
		to,
		rpc,
		strconv.FormatBool(success),
		errText,
		strconv.Itoa(metricCount),
	})
}

type summaryKey struct {
	ConfigID   string
	NodeCount  int
	Alpha      int
	KValue     int
	LookupType string
}

type stats struct {
	Count     int
	Successes int
	ProbeMean float64
	ProbeM2   float64
	HopMean   float64
	HopM2     float64
}

func (s *stats) add(metric lookupMetric) {
	s.Count++
	if metric.Success {
		s.Successes++
	}
	addSample(&s.ProbeMean, &s.ProbeM2, s.Count, float64(metric.Probes))
	addSample(&s.HopMean, &s.HopM2, s.Count, float64(metric.Hops))
}

func addSample(mean *float64, m2 *float64, count int, value float64) {
	delta := value - *mean
	*mean += delta / float64(count)
	*m2 += delta * (value - *mean)
}

func (s stats) probeVariance() float64 {
	if s.Count < 2 {
		return 0
	}
	return s.ProbeM2 / float64(s.Count-1)
}

func (s stats) hopVariance() float64 {
	if s.Count < 2 {
		return 0
	}
	return s.HopM2 / float64(s.Count-1)
}

func main() {
	opts, err := parseOptions()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *resilienceMode {
		if err := runResilience(opts); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	recorder, err := newLookupRecorder(opts.rawPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer recorder.close()

	summaries := make(map[summaryKey]*stats)
	for _, nodeCount := range opts.nodeCounts {
		for _, alpha := range opts.alphas {
			for _, kValue := range opts.kValues {
				for _, seed := range opts.seeds {
					configID := fmt.Sprintf("n%d-a%d-k%d", nodeCount, alpha, kValue)
					metrics, err := runOneConfiguration(opts, recorder, configID, nodeCount, alpha, kValue, seed)
					if err != nil {
						fmt.Fprintf(os.Stderr, "config %s seed %d failed: %v\n", configID, seed, err)
						os.Exit(1)
					}
					for _, metric := range metrics {
						key := summaryKey{
							ConfigID:   metric.ConfigID,
							NodeCount:  metric.NodeCount,
							Alpha:      metric.Alpha,
							KValue:     metric.KValue,
							LookupType: metric.LookupType,
						}
						if summaries[key] == nil {
							summaries[key] = &stats{}
						}
						summaries[key].add(metric)
					}
				}
			}
		}
	}

	if err := writeSummary(opts.summaryPath, summaries); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := writeReport(opts.reportPath, opts, summaries); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Printf("wrote raw metrics to %s\n", opts.rawPath)
	fmt.Printf("wrote summary to %s\n", opts.summaryPath)
	fmt.Printf("wrote report to %s\n", opts.reportPath)
}

func parseOptions() (options, error) {
	nodeCountsText := flag.String("node-counts", "20,50", "comma-separated network sizes")
	alphasText := flag.String("alphas", "1,3,5", "comma-separated alpha values")
	kValuesText := flag.String("k-values", "10", "comma-separated k values")
	seedsText := flag.String("seeds", "1,2,3,4,5", "comma-separated RNG seeds")
	lookups := flag.Int("lookups", 20, "number of random key-value and node lookups per seed/config")
	valueBytes := flag.Int("value-bytes", 32, "random bytes per stored value")
	timeout := flag.Duration("timeout", 500*time.Millisecond, "RPC timeout")
	rawPath := flag.String("raw-out", "experiments/lookup_raw.csv", "raw CSV output path")
	summaryPath := flag.String("summary-out", "experiments/lookup_summary.csv", "summary CSV output path")
	reportPath := flag.String("report-out", "experiments/report.md", "markdown report output path")
	flag.Parse()

	nodeCounts, err := parseIntList(*nodeCountsText)
	if err != nil {
		return options{}, fmt.Errorf("node-counts: %w", err)
	}
	alphas, err := parseIntList(*alphasText)
	if err != nil {
		return options{}, fmt.Errorf("alphas: %w", err)
	}
	kValues, err := parseIntList(*kValuesText)
	if err != nil {
		return options{}, fmt.Errorf("k-values: %w", err)
	}
	seeds, err := parseSeedList(*seedsText)
	if err != nil {
		return options{}, fmt.Errorf("seeds: %w", err)
	}
	if *lookups <= 0 {
		return options{}, fmt.Errorf("lookups must be positive")
	}
	if *valueBytes <= 0 || *valueBytes > kademlia.MaxValueSize {
		return options{}, fmt.Errorf("value-bytes must be between 1 and %d", kademlia.MaxValueSize)
	}
	for _, nodeCount := range nodeCounts {
		if nodeCount < 2 {
			return options{}, fmt.Errorf("node counts must be at least 2")
		}
	}
	for _, alpha := range alphas {
		if alpha <= 0 {
			return options{}, fmt.Errorf("alpha values must be positive")
		}
	}
	for _, kValue := range kValues {
		if kValue <= 0 {
			return options{}, fmt.Errorf("k values must be positive")
		}
	}

	return options{
		nodeCounts:  nodeCounts,
		alphas:      alphas,
		kValues:     kValues,
		seeds:       seeds,
		lookups:     *lookups,
		valueBytes:  *valueBytes,
		timeout:     *timeout,
		rawPath:     *rawPath,
		summaryPath: *summaryPath,
		reportPath:  *reportPath,
	}, nil
}

func runOneConfiguration(opts options, recorder *lookupRecorder, configID string, nodeCount int, alpha int, kValue int, seed int64) ([]lookupMetric, error) {
	rng := mathrand.New(mathrand.NewSource(seed))
	nodes, err := buildRandomNetwork(nodeCount, alpha, kValue, opts.timeout, recorder, rng)
	if err != nil {
		return nil, err
	}
	defer closeNodes(nodes)

	metrics := make([]lookupMetric, 0, opts.lookups*2)
	for runIndex := 0; runIndex < opts.lookups; runIndex++ {
		storeIndex := rng.Intn(len(nodes))
		lookupIndex := rng.Intn(len(nodes))
		value := randomBytes(rng, opts.valueBytes)

		key, err := nodes[storeIndex].node.Store(value)
		if err != nil {
			return nil, fmt.Errorf("store value %d: %w", runIndex, err)
		}

		targetIndex := rng.Intn(len(nodes))
		for targetIndex == lookupIndex && len(nodes) > 1 {
			targetIndex = rng.Intn(len(nodes))
		}
		target := nodes[targetIndex].contact
		recorder.startLookup(configID, seed, nodeCount, alpha, kValue, runIndex, "node", target.ID.String())
		contacts := nodes[lookupIndex].node.LookupContact(&target)
		metrics = append(metrics, recorder.finishLookup(contactFound(contacts, target), ""))

		recorder.startLookup(configID, seed, nodeCount, alpha, kValue, runIndex, "value", key)
		_, err = nodes[lookupIndex].node.LookupData(key)
		if err != nil {
			metrics = append(metrics, recorder.finishLookup(false, err.Error()))
		} else {
			metrics = append(metrics, recorder.finishLookup(true, ""))
		}
	}

	return metrics, nil
}

func buildRandomNetwork(count int, alpha int, kValue int, timeout time.Duration, recorder *lookupRecorder, rng *mathrand.Rand) ([]experimentNode, error) {
	simnet := kademlia.NewSimulatedNetwork()
	nodes := make([]experimentNode, 0, count)
	addresses := randomAddresses(rng, count)

	for _, address := range addresses {
		transport := &observedNode{
			inner:    kademlia.NewSimulatedNodeWithNetwork(simnet),
			recorder: recorder,
		}
		if err := transport.Listen(address); err != nil {
			closeNodes(nodes)
			return nil, err
		}

		contact := kademlia.NewContact(hashID(address), address)
		node := &kademlia.Kademlia{
			RoutingTable: kademlia.NewRoutingTable(contact),
			Network:      transport,
			Alpha:        alpha,
			K:            kValue,
			RPCTimeout:   timeout,
		}
		nodes = append(nodes, experimentNode{
			node:    node,
			contact: contact,
			network: transport,
		})
	}

	for i := range nodes {
		for j := range nodes {
			if i != j {
				nodes[i].node.RoutingTable.AddContact(nodes[j].contact)
			}
		}
		go nodes[i].node.ListenForRPC()
	}

	return nodes, nil
}

func randomAddresses(rng *mathrand.Rand, count int) []string {
	addresses := make([]string, 0, count)
	seen := make(map[string]bool)
	for len(addresses) < count {
		address := fmt.Sprintf(
			"10.%d.%d.%d:%d",
			rng.Intn(256),
			rng.Intn(256),
			rng.Intn(256),
			10000+rng.Intn(50000),
		)
		if seen[address] {
			continue
		}
		seen[address] = true
		addresses = append(addresses, address)
	}
	return addresses
}

func randomBytes(rng *mathrand.Rand, count int) []byte {
	value := make([]byte, count)
	for i := range value {
		value[i] = byte(rng.Intn(256))
	}
	return value
}

func closeNodes(nodes []experimentNode) {
	for _, node := range nodes {
		_ = node.network.Close()
	}
}

func contactFound(contacts []kademlia.Contact, target kademlia.Contact) bool {
	for _, contact := range contacts {
		if contact.ID != nil && target.ID != nil && contact.ID.Equals(target.ID) {
			return true
		}
	}
	return false
}

func hashID(text string) *kademlia.KademliaID {
	sum := sha256.Sum256([]byte(text))
	return kademlia.NewKademliaID(hex.EncodeToString(sum[:]))
}

func hopsFor(probes int, alpha int) int {
	if probes <= 0 {
		return 0
	}
	return (probes + alpha - 1) / alpha
}

func parseIntList(text string) ([]int, error) {
	parts := strings.Split(text, ",")
	values := make([]int, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func parseSeedList(text string) ([]int64, error) {
	parts := strings.Split(text, ",")
	values := make([]int64, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func writeSummary(path string, summaries map[summaryKey]*stats) error {
	if err := ensureParent(path); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	if err := writer.Write([]string{
		"config_id",
		"node_count",
		"alpha",
		"k",
		"lookup_type",
		"samples",
		"success_rate",
		"avg_probes",
		"probe_variance",
		"avg_hops",
		"hop_variance",
	}); err != nil {
		return err
	}

	for _, key := range sortedSummaryKeys(summaries) {
		stat := summaries[key]
		if err := writer.Write([]string{
			key.ConfigID,
			strconv.Itoa(key.NodeCount),
			strconv.Itoa(key.Alpha),
			strconv.Itoa(key.KValue),
			key.LookupType,
			strconv.Itoa(stat.Count),
			formatFloat(float64(stat.Successes) / float64(stat.Count)),
			formatFloat(stat.ProbeMean),
			formatFloat(stat.probeVariance()),
			formatFloat(stat.HopMean),
			formatFloat(stat.hopVariance()),
		}); err != nil {
			return err
		}
	}
	return writer.Error()
}

func writeReport(path string, opts options, summaries map[summaryKey]*stats) error {
	if err := ensureParent(path); err != nil {
		return err
	}
	var builder strings.Builder
	builder.WriteString("# Lookup Experiment Report\n\n")
	builder.WriteString("## Experimental setup\n\n")
	builder.WriteString(fmt.Sprintf("- Node counts: `%s`\n", intListString(opts.nodeCounts)))
	builder.WriteString(fmt.Sprintf("- Alpha values: `%s`\n", intListString(opts.alphas)))
	builder.WriteString(fmt.Sprintf("- K values: `%s`\n", intListString(opts.kValues)))
	builder.WriteString(fmt.Sprintf("- Seeds: `%s`\n", seedListString(opts.seeds)))
	builder.WriteString(fmt.Sprintf("- Random key-value pairs per seed/configuration: `%d`\n", opts.lookups))
	builder.WriteString(fmt.Sprintf("- Value size: `%d` bytes\n", opts.valueBytes))
	builder.WriteString("\nFor each configuration and seed, the experiment builds a fresh simulated Kademlia network. Node addresses are generated from the seeded RNG as random `IP:port` strings, and node IDs are the SHA-256 based IDs already used by the project for contacts. Values are also generated from the same seeded RNG, making every run repeatable when the same flags are used.\n\n")
	builder.WriteString("## Measurement method\n\n")
	builder.WriteString("The Kademlia package is not instrumented directly. Instead, the experiment wraps each simulated network node and observes outbound JSON RPC messages. A `FIND_NODE` message is counted as one node lookup probe; a `FIND_VALUE` message is counted as one value lookup probe. The implementation sends probes in strict parallel batches of size `alpha`, so hop count is estimated as `ceil(probes / alpha)`. Final success is measured from the actual public lookup result: node lookup succeeds if the target contact is returned, and value lookup succeeds if `LookupData` returns the stored value without error.\n\n")
	builder.WriteString("These measurements are meaningful because they are taken at the transport boundary: every lookup RPC must pass through `SendData`, so counting observed `FIND_NODE` and `FIND_VALUE` messages directly measures lookup traffic rather than an internal approximation. The simulated network has no packet loss, so variance mainly reflects topology, key placement, source node choice, and parameter settings.\n\n")
	builder.WriteString("## Expected results\n\n")
	builder.WriteString("With larger networks, lookups may require more probes because there are more possible contacts to search. Increasing `alpha` should usually reduce measured hops because more contacts are queried per round, but it may not reduce total probes. In this codebase, routing tables are pre-populated with all contacts during the experiment setup, so node lookups often approach `k` probes: the lookup already knows many close contacts and then queries the closest shortlist. Value lookups should usually succeed in the lossless simulation because `Store` replicates to the closest known nodes.\n\n")
	builder.WriteString("## Observed summary\n\n")
	builder.WriteString("| config | type | samples | success rate | avg probes | probe variance | avg hops | hop variance |\n")
	builder.WriteString("| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, key := range sortedSummaryKeys(summaries) {
		stat := summaries[key]
		builder.WriteString(fmt.Sprintf(
			"| %s | %s | %d | %s | %s | %s | %s | %s |\n",
			key.ConfigID,
			key.LookupType,
			stat.Count,
			formatFloat(float64(stat.Successes)/float64(stat.Count)),
			formatFloat(stat.ProbeMean),
			formatFloat(stat.probeVariance()),
			formatFloat(stat.HopMean),
			formatFloat(stat.hopVariance()),
		))
	}
	builder.WriteString("\n## Discussion\n\n")
	builder.WriteString(observedDiscussion(summaries))

	return os.WriteFile(path, []byte(builder.String()), 0644)
}

func observedDiscussion(summaries map[summaryKey]*stats) string {
	var builder strings.Builder
	allSuccessful := true
	for _, stat := range summaries {
		if stat.Successes != stat.Count {
			allSuccessful = false
			break
		}
	}
	if allSuccessful {
		builder.WriteString("All lookups succeeded in this run. That matches the expectation for a lossless simulated network with pre-populated routing tables and replicated stored values.\n\n")
	} else {
		builder.WriteString("Some lookups failed. In this simulation that is not expected under healthy routing, so inspect the raw CSV for the failed configuration and check whether the lookup source sent `FIND_VALUE` probes to nodes that actually received the stored value.\n\n")
	}

	builder.WriteString("Observed node lookup probe counts are mostly controlled by `k`: because the experiment pre-populates every routing table with all other contacts, the lookup starts with a strong shortlist and mainly pays the cost of probing that shortlist. If average node probes stay flat as network size changes, that is therefore expected for this setup rather than evidence that network size has no effect in a sparse real deployment.\n\n")
	builder.WriteString("Increasing `alpha` should reduce hop count when probe count stays similar, because the same probes are grouped into wider parallel rounds. The summary table should show this as lower average hops for larger `alpha`; deviations usually come from value lookups that finish locally or find the value early, which can produce zero or very few probes.\n\n")
	builder.WriteString("Variance is meaningful here because each configuration is repeated across explicit seeds. Variation comes from seeded topology, random source/target choices, and random key placement. Low or zero variance means those choices did not materially change the lookup cost for that configuration, often because the fully populated routing tables make the shortlist deterministic in size.\n")
	return builder.String()
}

func sortedSummaryKeys(summaries map[summaryKey]*stats) []summaryKey {
	keys := make([]summaryKey, 0, len(summaries))
	for key := range summaries {
		keys = append(keys, key)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if summaryKeyLess(keys[j], keys[i]) {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

func summaryKeyLess(a summaryKey, b summaryKey) bool {
	if a.NodeCount != b.NodeCount {
		return a.NodeCount < b.NodeCount
	}
	if a.Alpha != b.Alpha {
		return a.Alpha < b.Alpha
	}
	if a.KValue != b.KValue {
		return a.KValue < b.KValue
	}
	return a.LookupType < b.LookupType
}

func ensureParent(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0755)
}

func formatFloat(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "0"
	}
	return strconv.FormatFloat(value, 'f', 4, 64)
}

func intListString(values []int) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.Itoa(value))
	}
	return strings.Join(parts, ",")
}

func seedListString(values []int64) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.FormatInt(value, 10))
	}
	return strings.Join(parts, ",")
}
