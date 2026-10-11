package main

import (
	"bytes"
	"encoding/csv"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"d7024e/kademlia"
)

func TestTraceCountsActualPartialRoundsAndLateValueSends(t *testing.T) {
	trace := newScalingTrace("source", "key", "value")
	trace.batch([]kademlia.Contact{{Address: "a"}, {Address: "b"}})
	trace.send("a", []byte(`{"type":"FIND_VALUE","key":"key","sender_address":"source"}`))
	trace.send("b", []byte(`{"type":"FIND_VALUE","key":"key","sender_address":"unrelated"}`))
	trace.batch([]kademlia.Contact{{Address: "c"}})
	trace.send("c", []byte(`{"type":"FIND_VALUE","key":"key","sender_address":"source"}`))
	go trace.send("b", []byte(`{"type":"FIND_VALUE","key":"key","sender_address":"source"}`))
	if err := trace.finish(time.Second); err != nil {
		t.Fatal(err)
	}
	if trace.probes != 3 || trace.rounds != 2 || trace.destinations["c"] != 2 {
		t.Fatalf("measured %d probes, %d rounds; want 3 probes, 2 actual rounds", trace.probes, trace.rounds)
	}
}

func TestTraceRejectsIncompleteAccounting(t *testing.T) {
	trace := newScalingTrace("source", "target", "node")
	trace.batch([]kademlia.Contact{{Address: "missing"}})
	if err := trace.finish(time.Millisecond); err == nil {
		t.Fatal("missing selected send was not detected")
	}
}

func TestSeedLevelVariance(t *testing.T) {
	g := scalingGroup{samples: []scalingSample{
		{seed: 1, probes: 1, rounds: 1, success: true}, {seed: 1, probes: 3, rounds: 1, success: true},
		{seed: 2, probes: 3, rounds: 2, success: true}, {seed: 2, probes: 5, rounds: 2, success: true},
	}}
	pm, pv, rm, rv, sv, rsv, _, success, seeds := g.statistics()
	if pm != 3 || math.Abs(pv-8.0/3) > 1e-9 || rm != 1.5 || math.Abs(rv-1.0/3) > 1e-9 || sv != 2 || rsv != 0.5 || success != 4 || seeds != 2 {
		t.Fatalf("unexpected statistics: %v %v %v %v %v %v %v %v", pm, pv, rm, rv, sv, rsv, success, seeds)
	}
	pse, rse := g.seedStandardErrors()
	if pse != 1 || rse != 0.5 {
		t.Fatalf("cluster standard errors = %v, %v; want 1, 0.5", pse, rse)
	}
	unequal := scalingGroup{samples: []scalingSample{{seed: 1, probes: 1}, {seed: 1, probes: 3}, {seed: 2, probes: 9}}}
	pse, _ = unequal.seedStandardErrors()
	if pse != 3.5 {
		t.Fatalf("unequal-cluster standard error = %v, want 3.5", pse)
	}
}

func TestSeededTopologyRepeatableAcrossConfigurations(t *testing.T) {
	makeSnapshot := func(alpha int, seed int64) []string {
		nodes, err := scalingNetwork(40, alpha, 10, time.Second, seed)
		if err != nil {
			t.Fatal(err)
		}
		defer closeNodes(nodes)
		var snapshot []string
		for _, n := range nodes {
			snapshot = append(snapshot, n.contact.Address, n.contact.ID.String())
			for _, c := range n.node.RoutingTable.FindClosestContacts(n.contact.ID, 40) {
				snapshot = append(snapshot, c.ID.String())
			}
		}
		return snapshot
	}
	first := makeSnapshot(1, 7)
	if !reflect.DeepEqual(first, makeSnapshot(3, 7)) {
		t.Fatal("same seed changed topology across alpha")
	}
	if reflect.DeepEqual(first, makeSnapshot(1, 8)) {
		t.Fatal("different seed did not change topology")
	}
}

func TestScaling1000NodesMatchesGlobalOracleAndValues(t *testing.T) {
	nodes, err := scalingNetwork(1000, 3, 10, time.Second, 42)
	if err != nil {
		t.Fatal(err)
	}
	defer closeNodes(nodes)
	value := []byte("1000-node integration test value")
	key, err := nodes[123].node.Store(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []int{0, 333, 999} {
		target := hashID("oracle-target-" + strconv.Itoa(source))
		trace, success, err := measureScaling(nodes, source, target.String(), "node", time.Second, func() bool {
			return sameContacts(nodes[source].node.LookupContact(&kademlia.Contact{ID: target}), closestOracle(nodes, source, target, 10))
		})
		if err != nil || !success || trace.rounds == 0 || trace.probes < 10 {
			t.Fatalf("node source %d: success=%v err=%v probes=%d rounds=%d", source, success, err, trace.probes, trace.rounds)
		}
		_, success, err = measureScaling(nodes, source, key, "value", time.Second, func() bool {
			got, err := nodes[source].node.LookupData(key)
			return err == nil && bytes.Equal(got, value)
		})
		if err != nil || !success {
			t.Fatalf("value source %d: success=%v err=%v", source, success, err)
		}
	}
}

func TestScalingOutputsReconcileWithTransportProbes(t *testing.T) {
	dir := t.TempDir()
	opts := options{nodeCounts: []int{30}, alphas: []int{1, 3}, kValues: []int{10}, seeds: []int64{1, 2}, lookups: 4, valueBytes: 32, timeout: time.Second,
		rawPath: filepath.Join(dir, "raw.csv"), summaryPath: filepath.Join(dir, "summary.csv"), reportPath: filepath.Join(dir, "report.md")}
	if err := runScaling(opts); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(opts.rawPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	maxRound := map[string]int{}
	results := 0
	workloads := map[string]string{}
	for _, row := range rows[1:] {
		key := row[0] + "/" + row[1] + "/" + row[5] + "/" + row[6]
		if row[8] == "probe" {
			counts[key]++
			round, _ := strconv.Atoi(row[9])
			if round > maxRound[key] {
				maxRound[key] = round
			}
		} else {
			results++
			probes, _ := strconv.Atoi(row[15])
			rounds, _ := strconv.Atoi(row[16])
			if row[13] != "true" || counts[key] != probes || maxRound[key] != rounds {
				t.Fatalf("result does not reconcile: %v", row)
			}
			workloadKey := row[1] + "/" + row[5] + "/" + row[6]
			input := row[7] + "/" + row[10]
			if previous, ok := workloads[workloadKey]; ok && previous != input {
				t.Fatalf("seeded workload changed across alpha: %v", row)
			}
			workloads[workloadKey] = input
		}
	}
	if results != 32 {
		t.Fatalf("got %d results, want 32", results)
	}
}
