package main

import (
	"math"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPacketLossExpectedRequestAndReplyModel(t *testing.T) {
	if got := expectedLossSuccess("node", 0.5, 10); got != 0.25 {
		t.Fatalf("node reference=%v, want .25", got)
	}
	if got := expectedLossSuccess("value", 0.5, 2); got != 0.4375 {
		t.Fatalf("value reference=%v, want .4375", got)
	}
	for _, kind := range []string{"node", "value"} {
		if expectedLossSuccess(kind, 0, 10) != 1 || expectedLossSuccess(kind, 1, 10) != 0 {
			t.Fatal("incorrect loss endpoints")
		}
	}
}

func TestPacketLossMasksRepeatableAndNested(t *testing.T) {
	var before []bool
	for trial := 0; trial < 100; trial++ {
		before = append(before, packetDrop(7, trial, "value", "source", "target", "request", 0.3))
	}
	for trial := 99; trial >= 0; trial-- {
		if before[trial] != packetDrop(7, trial, "value", "source", "target", "request", 0.3) {
			t.Fatal("mask depends on evaluation order")
		}
		if before[trial] && !packetDrop(7, trial, "value", "source", "target", "request", 0.8) {
			t.Fatal("higher loss did not preserve earlier drop")
		}
	}
	// Different directions must use distinct deterministic draws.
	different := false
	for trial := 0; trial < 100; trial++ {
		if before[trial] != packetDrop(7, trial, "value", "source", "target", "reply", 0.3) {
			different = true
		}
	}
	if !different {
		t.Fatal("request/reply masks unexpectedly identical")
	}
}

func TestPacketLossRemoteLookupsAndTargetConfirmation(t *testing.T) {
	opts := options{lookups: 3, valueBytes: 32, timeout: 5 * time.Millisecond}
	for _, loss := range []float64{0, 1} {
		results, err := packetLossTrials(opts, 12, 3, 3, 7, loss)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 6 {
			t.Fatalf("got %d trials", len(results))
		}
		for _, r := range results {
			if r.success != (loss == 0) {
				t.Fatalf("type=%s loss=%v success=%v", r.kind, loss, r.success)
			}
			if r.probes == 0 || len(r.attempts) != r.probes || r.rounds == 0 {
				t.Fatalf("local or unaccounted lookup: %+v", r)
			}
			if r.replicas != 3 {
				t.Fatalf("replicas=%d, want 3", r.replicas)
			}
			if math.Abs(r.expected-expectedLossSuccess(r.kind, loss, 3)) > 1e-9 {
				t.Fatal("reference mismatch")
			}
			if loss == 1 {
				if r.kind == "node" && !r.returnedContact {
					t.Fatal("test requires a cached target contact to verify response confirmation")
				}
				for _, a := range r.attempts {
					if !a.requestDropped || a.received || a.replyAttempted {
						t.Fatalf("complete loss packet log: %+v", a)
					}
				}
			}
		}
	}
}

func TestPacketLossCSVReconciliation(t *testing.T) {
	opts := options{nodeCounts: []int{12}, alphas: []int{3}, kValues: []int{3}, seeds: []int64{1, 2}, lookups: 2, valueBytes: 32, timeout: 5 * time.Millisecond}
	losses := []float64{0, 0.5, 1}
	var results []lossResult
	for _, loss := range losses {
		for _, seed := range opts.seeds {
			rs, err := packetLossTrials(opts, 12, 3, 3, seed, loss)
			if err != nil {
				t.Fatal(err)
			}
			results = append(results, rs...)
		}
	}
	dir := t.TempDir()
	if err := writePacketLossResults(opts, losses, results, dir); err != nil {
		t.Fatal(err)
	}
	packets := readCSV(t, filepath.Join(dir, "packets.csv"))
	counts := map[string]int{}
	for _, row := range packets[1:] {
		counts[strings.Join(row[:7], "/")]++
	}
	trials := readCSV(t, filepath.Join(dir, "trials.csv"))
	if len(trials) != 25 {
		t.Fatalf("got %d trials, want 24", len(trials)-1)
	}
	for _, row := range trials[1:] {
		probes, err := strconv.Atoi(row[12])
		if err != nil {
			t.Fatal(err)
		}
		if probes != counts[strings.Join(row[:7], "/")] {
			t.Fatalf("packet count does not reconcile: %v", row)
		}
		if row[3] == "0.0000" && row[10] != "true" {
			t.Fatalf("healthy lookup failed: %v", row)
		}
		if row[3] == "1.0000" && row[10] != "false" {
			t.Fatalf("complete loss succeeded: %v", row)
		}
	}
	summary := readCSV(t, filepath.Join(dir, "summary.csv"))
	if len(summary) != 7 {
		t.Fatalf("got %d summary rows, want 6", len(summary)-1)
	}
	for _, row := range summary[1:] {
		if row[5] != "4" || row[6] != "2" {
			t.Fatalf("wrong sample/seed count: %v", row)
		}
	}
}

func TestPacketLossCheckpointPreservesMeasurements(t *testing.T) {
	results, err := packetLossTrials(options{lookups: 2, valueBytes: 32, timeout: 5 * time.Millisecond}, 12, 3, 3, 9, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "checkpoints", "completed.json")
	for i := 0; i < 2; i++ {
		if err := writeLossCheckpoint(path, results); err != nil {
			t.Fatal(err)
		}
		loaded, err := readLossCheckpoint(path)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(results, loaded) {
			t.Fatal("checkpoint altered lookup or packet measurements")
		}
	}
}

func TestPacketLossWorkloadMatchedAcrossLossLevels(t *testing.T) {
	opts := options{lookups: 3, valueBytes: 32, timeout: 5 * time.Millisecond}
	healthy, err := packetLossTrials(opts, 20, 3, 3, 11, 0)
	if err != nil {
		t.Fatal(err)
	}
	lost, err := packetLossTrials(opts, 20, 3, 3, 11, 1)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range healthy {
		if r.source != lost[i].source || r.target != lost[i].target || r.kind != lost[i].kind || r.replicas != lost[i].replicas {
			t.Fatal("workload changed with loss")
		}
	}
}
