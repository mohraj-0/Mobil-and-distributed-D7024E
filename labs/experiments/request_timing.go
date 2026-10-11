package main

import (
	"crypto/sha256"
	"encoding/csv"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"
)

var requestTimingMode = flag.Bool("request-timing", false, "run Experiment 3: request/response time versus loss and latency")
var requestTimingOutput = flag.String("request-timing-out", "experiments/request_response", "Experiment 3 output directory")
var requestTimingResume = flag.Bool("request-timing-resume", false, "reuse matching completed Experiment 3 checkpoints")
var timingLossLatency = flag.Duration("timing-loss-latency", 5*time.Millisecond, "one-way delay held fixed during Experiment 3 loss sweep")

// Observe each launched request through its own deadline, even when a value
// lookup returns early. Received replies have RTTs; missing replies have no RTT.
// This observation window is outside the measured lookup duration.
func observeRequestDeadlines(state *packetLossState, trace *lossTrace) {
	for {
		state.mu.Lock()
		complete := true
		now := time.Now()
		for _, a := range trace.attempts {
			if !a.received && now.Before(a.started.Add(trace.timeout)) {
				complete = false
				break
			}
		}
		state.mu.Unlock()
		if complete {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

type timingSample struct {
	axis   string
	result lossResult
}

func durationMS(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func runRequestTiming(opts options) error {
	if *packetLossMode || *resilienceMode {
		return fmt.Errorf("request-timing must be used as its own experiment")
	}
	if !flagExplicit("node-counts") {
		opts.nodeCounts = []int{1000}
	}
	if !flagExplicit("alphas") {
		opts.alphas = []int{3}
	}
	if !flagExplicit("lookups") {
		opts.lookups = 10
	}
	if !flagExplicit("timeout") {
		opts.timeout = 50 * time.Millisecond
	}
	if opts.timeout <= 0 || *timingLossLatency < 0 {
		return fmt.Errorf("timeout must be positive and latency nonnegative")
	}
	lossText := *lossLevels
	if !flagExplicit("loss-rates") {
		lossText = "0,0.1,0.2,0.3,0.4,0.5,0.6,0.7,0.8,0.9,1"
	}
	losses, err := parseRates(lossText, true)
	if err != nil {
		return err
	}
	latencyText := *latencyLevels
	if !flagExplicit("latencies") {
		latencyText = "0ms,2ms,5ms,10ms,20ms"
	}
	var delays []time.Duration
	for _, text := range strings.Split(latencyText, ",") {
		d, err := time.ParseDuration(strings.TrimSpace(text))
		if err != nil || d < 0 {
			return fmt.Errorf("invalid latency %q", text)
		}
		delays = append(delays, d)
	}
	for _, n := range opts.nodeCounts {
		for _, k := range opts.kValues {
			if n <= k {
				return fmt.Errorf("request-timing requires node count > k for remote reads")
			}
		}
	}
	var cases []struct {
		axis    string
		loss    float64
		latency time.Duration
	}
	for _, d := range delays {
		cases = append(cases, struct {
			axis    string
			loss    float64
			latency time.Duration
		}{"latency", 0, d})
	}
	for _, p := range losses {
		cases = append(cases, struct {
			axis    string
			loss    float64
			latency time.Duration
		}{"loss", p, *timingLossLatency})
	}
	var samples []timingSample
	for _, n := range opts.nodeCounts {
		for _, alpha := range opts.alphas {
			for _, k := range opts.kValues {
				for _, c := range cases {
					for _, seed := range opts.seeds {
						fmt.Printf("Experiment 3: axis=%s n=%d alpha=%d k=%d loss=%.1f latency=%s seed=%d\n", c.axis, n, alpha, k, c.loss, c.latency, seed)
						identity := fmt.Sprintf("timing-v1|%d|%d|%d|%d|%.17g|%d|%d|%d|%d", n, alpha, k, seed, c.loss, c.latency, opts.lookups, opts.valueBytes, opts.timeout)
						path := filepath.Join(*requestTimingOutput, ".checkpoints", fmt.Sprintf("%x.json", sha256.Sum256([]byte(identity))))
						var results []lossResult
						if *requestTimingResume {
							results, err = readLossCheckpoint(path)
							if err != nil && !os.IsNotExist(err) {
								return err
							}
						}
						if len(results) == 0 {
							results, err = packetLossTrialsWithLatency(opts, n, alpha, k, seed, c.loss, c.latency, true)
							if err != nil {
								return err
							}
							if err = writeLossCheckpoint(path, results); err != nil {
								return err
							}
							debug.FreeOSMemory()
						}
						if len(results) != opts.lookups*2 {
							return fmt.Errorf("incomplete timing checkpoint")
						}
						for _, r := range results {
							samples = append(samples, timingSample{c.axis, r})
						}
					}
				}
			}
		}
	}
	return writeRequestTiming(opts, samples, *requestTimingOutput)
}

type timingKey struct {
	axis, kind  string
	n, alpha, k int
	loss        float64
	latency     time.Duration
}

type requestTimingStats struct {
	rtt, wait, response, lookup []float64
	rttBySeed                   map[int64][]float64
	seedRates                   map[int64][]float64
	late                        int
}

// Cluster jackknife SE for a pooled mean with potentially unequal response
// counts per seed. No-reply seeds remain clusters with zero RTT samples.
func timingSeedSE(bySeed map[int64][]float64) float64 {
	if len(bySeed) < 2 {
		return 0
	}
	var total float64
	count := 0
	for _, values := range bySeed {
		for _, v := range values {
			total += v
			count++
		}
	}
	var means []float64
	for _, values := range bySeed {
		if count == len(values) {
			return math.NaN()
		}
		var removed float64
		for _, v := range values {
			removed += v
		}
		means = append(means, (total-removed)/float64(count-len(values)))
	}
	_, variance := meanVariance(means)
	s := float64(len(means))
	return math.Sqrt(variance * (s - 1) * (s - 1) / s)
}

func optionalTiming(values []float64) (string, string) {
	if len(values) == 0 {
		return "", ""
	}
	mean, variance := meanVariance(values)
	return formatFloat(mean), formatFloat(variance)
}

func writeRequestTiming(opts options, samples []timingSample, dir string) error {
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
		return writer.WriteAll(rows)
	}
	requests := [][]string{{"axis", "node_count", "alpha", "k", "loss", "latency_ms", "seed", "trial", "lookup_type", "from", "to", "request_started", "response_received", "response_on_time", "response_ms", "outcome_wait_ms", "request_dropped", "reply_dropped"}}
	trials := [][]string{{"axis", "node_count", "alpha", "k", "loss", "latency_ms", "seed", "trial", "lookup_type", "success", "probes", "rounds", "lookup_elapsed_ms"}}
	groups := map[timingKey]*requestTimingStats{}
	for _, sample := range samples {
		r := sample.result
		key := timingKey{sample.axis, r.kind, r.n, r.alpha, r.k, r.loss, r.latency}
		if groups[key] == nil {
			groups[key] = &requestTimingStats{rttBySeed: map[int64][]float64{}, seedRates: map[int64][]float64{}}
		}
		g := groups[key]
		g.lookup = append(g.lookup, r.elapsedMS)
		if _, ok := g.rttBySeed[r.seed]; !ok {
			g.rttBySeed[r.seed] = nil
		}
		prefix := []string{sample.axis, strconv.Itoa(r.n), strconv.Itoa(r.alpha), strconv.Itoa(r.k), formatFloat(r.loss), formatFloat(durationMS(r.latency)), strconv.FormatInt(r.seed, 10), strconv.Itoa(r.trial), r.kind}
		trials = append(trials, append(append([]string{}, prefix...), strconv.FormatBool(r.success), strconv.Itoa(r.probes), strconv.Itoa(r.rounds), formatFloat(r.elapsedMS)))
		for _, a := range r.attempts {
			rttText := ""
			wait := durationMS(opts.timeout)
			response := 0.0
			if a.received {
				rttText = formatFloat(durationMS(a.rtt))
				if !a.onTime {
					g.late++
				}
			}
			if a.received && a.onTime {
				wait = durationMS(a.rtt)
				response = 1
				g.rtt = append(g.rtt, wait)
				g.rttBySeed[r.seed] = append(g.rttBySeed[r.seed], wait)
			}
			g.wait = append(g.wait, wait)
			g.response = append(g.response, response)
			g.seedRates[r.seed] = append(g.seedRates[r.seed], response)
			requests = append(requests, append(append([]string{}, prefix...), a.from, a.to, a.started.UTC().Format(time.RFC3339Nano), strconv.FormatBool(a.received), strconv.FormatBool(a.onTime), rttText, formatFloat(wait), strconv.FormatBool(a.requestDropped), strconv.FormatBool(a.replyDropped)))
		}
	}
	if err := write("requests.csv", requests); err != nil {
		return err
	}
	if err := write("trials.csv", trials); err != nil {
		return err
	}
	summary := [][]string{{"axis", "node_count", "alpha", "k", "loss", "latency_ms", "lookup_type", "lookups", "requests", "on_time_responses", "missing_or_late", "late_observed", "seeds", "response_fraction", "response_variance", "seed_response_variance", "avg_response_ms", "response_ms_variance", "response_seed_se_ms", "avg_outcome_wait_ms", "outcome_wait_variance", "avg_lookup_ms", "lookup_variance", "expected_response_ms", "expected_outcome_wait_ms", "expected_response_fraction"}}
	keys := []timingKey{}
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.axis != b.axis {
			return a.axis < b.axis
		}
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
		if a.latency != b.latency {
			return a.latency < b.latency
		}
		return a.kind < b.kind
	})
	var report strings.Builder
	report.WriteString("# Experiment 3: Request–response time versus packet loss and latency\n\n## Setup and measurement\n\n")
	fmt.Fprintf(&report, "N=%s; alpha=%s; k=%s; seeds=%s; %d remote lookup trials per seed/type/condition; RPC timeout=%s. Latency means one-way delay, applied independently to the request and reply paths.\n\n", intListString(opts.nodeCounts), intListString(opts.alphas), intListString(opts.kValues), seedListString(opts.seeds), opts.lookups, opts.timeout)
	report.WriteString("The latency sweep holds packet loss at zero. The loss sweep holds the configured one-way latency fixed. Fresh seeded random IP:port networks and random values match Experiment 2, with verified healthy publishing and remote-only value sources. Each configuration is repeated across seeds. Publishing is outside the impairment window. The real algorithm is exercised, with no added retries, churn or changes to lookup decisions. Reusing matching checkpoints can share the common control point between sweeps.\n\nAt the outgoing request transport boundary, a monotonic timestamp is recorded; the reply is matched by its echoed RPC ID and timestamped at the receiving transport boundary. RTT is their difference and includes both simulated directions, processing, queueing and scheduling. After an early value return, observation continues until each launched request replies or reaches its own deadline. This observation window is excluded from lookup_elapsed_ms. Missing replies have a blank response_ms, never zero. Late replies are recorded separately and excluded from on-time RTT averages. Asynchronous timers model each one-way delay without blocking the lookup's parallel dispatch.\n\nMeans and unbiased sample variances are reported for on-time RTT, response fraction, capped request outcome wait and overall lookup duration. The outcome wait is RTT for an on-time reply, otherwise the configured deadline; it is a timeout-inclusive observation metric, not the duration of a successful reply or an actual early-returning value lookup. Seed-level response-rate variance and cluster-jackknife RTT standard errors account for trials on shared topologies.\n\n## Expectations\n\nWith one-way delay L, received RTT should be approximately 2L plus implementation overhead. Without retries, independent packet loss alone does not add propagation delay to a reply that arrives; it lowers the received-response fraction. One exchange survives with q=(1-p)^2. For 2L below timeout T, the ideal capped outcome-wait mean is q·2L+(1-q)·T. If 2L exceeds the deadline, the timely response reference is zero and the ideal outcome wait is T. Empty RTT means are undefined when no on-time replies exist. These are transport-model references, not precise predictions for scheduler jitter or end-to-end lookup duration.\n\n## Observed results\n\n| sweep | loss | one-way ms | type | requests | on-time replies | response fraction | RTT mean ms | RTT variance | expected RTT ms | outcome wait ms | expected wait ms | lookup mean ms |\n|---|---:|---:|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, key := range keys {
		g := groups[key]
		fraction, fv := meanVariance(g.response)
		wm, wv := meanVariance(g.wait)
		lm, lv := meanVariance(g.lookup)
		rttMean, rttVariance := optionalTiming(g.rtt)
		rttSE := ""
		if len(g.rtt) > 0 {
			se := timingSeedSE(g.rttBySeed)
			if !math.IsNaN(se) {
				rttSE = formatFloat(se)
			}
		}
		seedMeans := []float64{}
		for _, values := range g.seedRates {
			m, _ := meanVariance(values)
			seedMeans = append(seedMeans, m)
		}
		_, sv := meanVariance(seedMeans)
		q := (1 - key.loss) * (1 - key.loss)
		expectedRTT := ""
		expectedWait := durationMS(opts.timeout)
		if 2*key.latency < opts.timeout {
			expectedWait = q*2*durationMS(key.latency) + (1-q)*durationMS(opts.timeout)
			if q > 0 {
				expectedRTT = formatFloat(2 * durationMS(key.latency))
			}
		} else {
			q = 0
		}
		row := []string{key.axis, strconv.Itoa(key.n), strconv.Itoa(key.alpha), strconv.Itoa(key.k), formatFloat(key.loss), formatFloat(durationMS(key.latency)), key.kind, strconv.Itoa(len(g.lookup)), strconv.Itoa(len(g.wait)), strconv.Itoa(len(g.rtt)), strconv.Itoa(len(g.wait) - len(g.rtt)), strconv.Itoa(g.late), strconv.Itoa(len(g.seedRates)), formatFloat(fraction), formatFloat(fv), formatFloat(sv), rttMean, rttVariance, rttSE, formatFloat(wm), formatFloat(wv), formatFloat(lm), formatFloat(lv), expectedRTT, formatFloat(expectedWait), formatFloat(q)}
		summary = append(summary, row)
		fmt.Fprintf(&report, "| %s | %.2f | %.1f | %s | %d | %d | %.4f | %s | %s | %s | %.4f | %.4f | %.4f |\n", key.axis, key.loss, durationMS(key.latency), key.kind, len(g.wait), len(g.rtt), fraction, rttMean, rttVariance, expectedRTT, wm, expectedWait, lm)
	}
	if err := write("summary.csv", summary); err != nil {
		return err
	}
	report.WriteString("\n## Comparison and deviations\n\n")
	for _, row := range summary[1:] {
		if row[0] == "latency" || row[4] == "0.5000" || row[4] == "1.0000" {
			actual, expected := row[16]+" ms", row[23]+" ms"
			if row[16] == "" {
				actual = "undefined (no timely replies)"
			}
			if row[23] == "" {
				expected = "undefined"
			}
			fmt.Fprintf(&report, "- %s, %s: loss=%s, one-way delay=%s ms; measured on-time RTT=%s versus %s reference; response fraction=%s versus %s.\n", row[0], row[6], row[4], row[5], actual, expected, row[13], row[25])
		}
	}
	report.WriteString("\nRTT above 2L is expected from processing, timer precision and goroutine scheduling. Received-only RTT should remain broadly flat across packet loss at fixed delay, while missing replies and the timeout-inclusive wait increase. Lookup duration can grow more sharply because node lookup waits for whole batches and multiple rounds; values may terminate early. Deviations in response fraction can reflect finite sampling, delay near the deadline or scheduling. Inspect requests.csv for drop and late-response flags instead of substituting a zero time for absent replies.\n\n![Expected and actual request–response time](request_response_time.png)\n\n![Timeout-inclusive request outcome wait](request_wait_vs_loss.png)\n")
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte(report.String()), 0644); err != nil {
		return err
	}
	if err := appendTimerCalibration(dir); err != nil {
		return err
	}
	fmt.Printf("wrote %d Experiment 3 lookups and %d requests to %s\n", len(samples), len(requests)-1, dir)
	return nil
}
