package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRequestTimingAppliesBothDirectionsAndObservesEarlyValueReplies(t *testing.T) {
	opts := options{lookups: 2, valueBytes: 32, timeout: 100 * time.Millisecond}
	results, err := packetLossTrialsWithLatency(opts, 12, 3, 3, 7, 0, 3*time.Millisecond, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if !r.success {
			t.Fatalf("healthy timing lookup failed: %s", r.kind)
		}
		if len(r.attempts) != r.probes || r.probes == 0 {
			t.Fatal("unaccounted or local timing read")
		}
		for _, a := range r.attempts {
			if !a.received || !a.onTime {
				t.Fatal("pending reply was not observed after lookup return")
			}
			if a.rtt < 6*time.Millisecond {
				t.Fatalf("RTT %s is less than twice one-way delay", a.rtt)
			}
			if a.started.IsZero() {
				t.Fatal("request start missing")
			}
		}
	}
}

func TestRequestTimingMissingRepliesAreNotZeroRTTs(t *testing.T) {
	opts := options{nodeCounts: []int{12}, alphas: []int{3}, kValues: []int{3}, seeds: []int64{1}, lookups: 2, valueBytes: 32, timeout: 5 * time.Millisecond}
	results, err := packetLossTrialsWithLatency(opts, 12, 3, 3, 1, 1, time.Millisecond, true)
	if err != nil {
		t.Fatal(err)
	}
	var samples []timingSample
	for _, r := range results {
		samples = append(samples, timingSample{"loss", r})
	}
	dir := t.TempDir()
	if err := writeRequestTiming(opts, samples, dir); err != nil {
		t.Fatal(err)
	}
	requests := readCSV(t, filepath.Join(dir, "requests.csv"))
	for _, row := range requests[1:] {
		if row[12] != "false" || row[13] != "false" || row[14] != "" || row[15] != "5.0000" {
			t.Fatalf("missing response logged incorrectly: %v", row)
		}
	}
	summary := readCSV(t, filepath.Join(dir, "summary.csv"))
	for _, row := range summary[1:] {
		if row[16] != "" || row[17] != "" || row[19] != "5.0000" || row[23] != "" || row[24] != "5.0000" {
			t.Fatalf("missing RTT averaged as zero: %v", row)
		}
	}
}

func TestRequestTimingLateResponsesExcludedAndCheckpointed(t *testing.T) {
	now := time.Now()
	r := lossResult{n: 12, alpha: 3, k: 3, kind: "node", seed: 1, latency: 3 * time.Millisecond, attempts: []lossAttempt{
		{from: "a", to: "b", rpc: rpcFindNode, started: now, received: true, rtt: 6 * time.Millisecond, onTime: true},
		{from: "a", to: "c", rpc: rpcFindNode, started: now, received: true, rtt: 12 * time.Millisecond, onTime: false},
	}}
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")
	if err := writeLossCheckpoint(path, []lossResult{r}); err != nil {
		t.Fatal(err)
	}
	loaded, err := readLossCheckpoint(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded[0].latency != r.latency || !loaded[0].attempts[0].started.Equal(now) || loaded[0].attempts[1].rtt != 12*time.Millisecond {
		t.Fatal("timing checkpoint lost timestamps or RTT")
	}
	opts := options{nodeCounts: []int{12}, alphas: []int{3}, kValues: []int{3}, seeds: []int64{1}, lookups: 1, timeout: 10 * time.Millisecond}
	if err := writeRequestTiming(opts, []timingSample{{"latency", loaded[0]}}, dir); err != nil {
		t.Fatal(err)
	}
	row := readCSV(t, filepath.Join(dir, "summary.csv"))[1]
	if row[9] != "1" || row[11] != "1" || row[16] != "6.0000" || row[19] != "8.0000" {
		t.Fatalf("late response biased averages: %v", row)
	}
}

func TestTimingSeedSEAccountsForUnequalReplyCounts(t *testing.T) {
	if got := timingSeedSE(map[int64][]float64{1: {1, 3}, 2: {9}}); got != 3.5 {
		t.Fatalf("cluster SE=%v, want 3.5", got)
	}
}
