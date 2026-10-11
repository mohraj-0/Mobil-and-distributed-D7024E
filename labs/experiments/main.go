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

func parseOptions() (options, error) {
	nodeCountsText := flag.String("node-counts", "100,200,300,400,500,600,700,800,900,1000", "comma-separated network sizes")
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
