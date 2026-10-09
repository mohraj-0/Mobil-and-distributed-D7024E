package main

import (
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResilienceRateValidation(t *testing.T) {
	for _, text := range []string{"", "NaN", "+Inf", "-0.1", "bad", "1.1"} {
		if _, err := parseRates(text, true); err == nil {
			t.Errorf("accepted invalid loss probability %q", text)
		}
	}
	if values, err := parseRates("0, 0.5,1", true); err != nil || len(values) != 3 {
		t.Fatalf("valid loss probabilities: %v, %v", values, err)
	}
	if _, err := parseRates("2", false); err != nil {
		t.Fatal("churn rates may exceed one departure per node per second:", err)
	}
}

func TestChurnExposureProbability(t *testing.T) {
	if departureProbability(0, time.Second) != 0 {
		t.Fatal("zero churn must preserve nodes")
	}
	if got := departureProbability(1, time.Second); math.Abs(got-(1-math.Exp(-1))) > 1e-12 {
		t.Fatalf("departure probability = %v", got)
	}
	if departureProbability(1, 2*time.Second) <= departureProbability(1, time.Second) {
		t.Fatal("longer exposure must increase departure probability")
	}
}

func TestResilienceLookupLossLatencyAndChurn(t *testing.T) {
	for _, tc := range []struct {
		name       string
		s          scenario
		success    bool
		departures int
	}{
		{"healthy value", scenario{axis: "churn_replication", alpha: 2, k: 3}, true, 0},
		{"all replicas leave", scenario{axis: "churn_replication", alpha: 2, k: 3, churn: 1000}, false, 3},
		{"all packets lost", scenario{axis: "latency_loss", alpha: 2, k: 3, loss: 1}, false, 0},
		{"delayed responses", scenario{axis: "alpha", alpha: 2, k: 3, latency: 3 * time.Millisecond}, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder, err := newLookupRecorder(filepath.Join(t.TempDir(), "probes.csv"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := recorder.close(); err != nil {
					t.Error(err)
				}
			})
			r, err := resilienceTrial(options{valueBytes: 16, timeout: 100 * time.Millisecond}, recorder, "test", tc.s, 3, 1, 0)
			if err != nil {
				t.Fatal(err)
			}
			if r.storeError != "" {
				t.Fatalf("healthy storage failed: %s", r.storeError)
			}
			t.Logf("success=%t probes=%d duration=%s departures=%d joins=%d", r.metric.Success, r.metric.Probes, r.elapsed, r.departures, r.joins)
			if r.metric.Success != tc.success || r.departures != tc.departures || r.joins != tc.departures {
				t.Fatalf("unexpected trial: %+v", r)
			}
			if r.elapsed < 0 {
				t.Fatal("lookup duration must not be negative")
			}
			if tc.s.loss == 1 {
				if r.metric.Probes == 0 || len(r.requests) == 0 {
					t.Fatal("no lost probes recorded")
				}
				for _, request := range r.requests {
					if request.completed {
						t.Fatal("lost packet received a reply")
					}
				}
			}
			if tc.s.latency > 0 {
				completed := 0
				for _, request := range r.requests {
					if request.completed {
						completed++
						if request.elapsed < 2*tc.s.latency {
							t.Fatalf("RTT %v is less than twice one-way delay", request.elapsed)
						}
					}
				}
				if completed == 0 {
					t.Fatal("no response timing recorded")
				}
			}
		})
	}
}

func TestReplicationFactorControlsReplicaCount(t *testing.T) {
	for _, k := range []int{1, 2, 4} {
		recorder, err := newLookupRecorder(filepath.Join(t.TempDir(), "probes.csv"))
		if err != nil {
			t.Fatal(err)
		}
		nodes, err := buildRandomNetwork(4, 2, k, time.Second, recorder, rand.New(rand.NewSource(7)))
		if err != nil {
			t.Fatal(err)
		}
		key, err := nodes[0].node.Store([]byte("replication test"))
		if err != nil {
			t.Fatal(err)
		}
		replicas := 0
		for _, node := range nodes {
			if _, ok := node.node.DataStore[key]; ok {
				replicas++
			}
		}
		closeNodes(nodes)
		if err := recorder.close(); err != nil {
			t.Fatal(err)
		}
		if replicas != k {
			t.Errorf("k=%d produced %d replicas, want %d", k, replicas, k)
		}
		t.Logf("replication factor k=%d: stored replicas=%d", k, replicas)
	}
}

func TestAlphaChangesRounds(t *testing.T) {
	for _, alpha := range []int{1, 3} {
		recorder, err := newLookupRecorder(filepath.Join(t.TempDir(), "probes.csv"))
		if err != nil {
			t.Fatal(err)
		}
		r, err := resilienceTrial(options{valueBytes: 16, timeout: time.Second}, recorder, "alpha", scenario{axis: "alpha", alpha: alpha, k: 3, latency: time.Millisecond}, 4, 1, 0)
		if closeErr := recorder.close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		if err != nil {
			t.Fatal(err)
		}
		if !r.metric.Success || r.metric.Probes != 3 || r.metric.Hops != 3/alpha {
			t.Fatalf("alpha=%d: unexpected metric %+v", alpha, r.metric)
		}
		t.Logf("alpha=%d probes=%d estimated rounds=%d duration=%s", alpha, r.metric.Probes, r.metric.Hops, r.elapsed)
	}
}

func TestResilienceWritesLogsAndReport(t *testing.T) {
	oldLoss, oldLatency, oldChurn, oldWindow, oldOutput := *lossLevels, *latencyLevels, *churnLevels, *churnWindow, *resilienceOutput
	t.Cleanup(func() {
		*lossLevels, *latencyLevels, *churnLevels, *churnWindow, *resilienceOutput = oldLoss, oldLatency, oldChurn, oldWindow, oldOutput
	})
	*lossLevels, *latencyLevels, *churnLevels, *churnWindow, *resilienceOutput = "0", "1ms", "0", time.Second, t.TempDir()
	opts := options{nodeCounts: []int{3}, alphas: []int{1, 3}, kValues: []int{1, 3}, seeds: []int64{1}, lookups: 1, valueBytes: 16, timeout: time.Second}
	if err := runResilience(opts); err != nil {
		t.Fatal(err)
	}
	rows := readCSV(t, filepath.Join(*resilienceOutput, "trials.csv"))
	if len(rows) != 6 || rows[0][0] != "config_id" {
		t.Fatalf("unexpected trial log: %v", rows)
	}
	for _, row := range rows[1:] {
		if len(row) != 20 || row[12] != "true" {
			t.Fatalf("unexpected trial row: %v", row)
		}
	}
	requests := readCSV(t, filepath.Join(*resilienceOutput, "requests.csv"))
	if len(requests) < 2 {
		t.Fatal("request timings missing")
	}
	probes := readCSV(t, filepath.Join(*resilienceOutput, "probes.csv"))
	if len(probes) < 6 {
		t.Fatal("probe events missing")
	}
	report, err := os.ReadFile(filepath.Join(*resilienceOutput, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"latency_loss", "alpha", "churn_replication", "mean RTT ms", "Expected behavior"} {
		if !strings.Contains(string(report), expected) {
			t.Errorf("report missing %q", expected)
		}
	}
}
