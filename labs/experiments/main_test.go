package main

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseLists(t *testing.T) {
	got, err := parseIntList(" 2, 10,50 ")
	if err != nil || !reflect.DeepEqual(got, []int{2, 10, 50}) {
		t.Fatalf("parseIntList = %v, %v", got, err)
	}
	seeds, err := parseSeedList("-1, 2147483648")
	if err != nil || !reflect.DeepEqual(seeds, []int64{-1, 2147483648}) {
		t.Fatalf("parseSeedList = %v, %v", seeds, err)
	}
	for _, input := range []string{"", "1,", "1,no", "1.5"} {
		if _, err := parseIntList(input); err == nil {
			t.Errorf("parseIntList(%q) accepted invalid input", input)
		}
		if _, err := parseSeedList(input); err == nil {
			t.Errorf("parseSeedList(%q) accepted invalid input", input)
		}
	}
}

func TestHopsFor(t *testing.T) {
	for _, tc := range []struct{ probes, alpha, want int }{
		{0, 3, 0}, {1, 3, 1}, {3, 3, 1}, {4, 3, 2}, {7, 3, 3}, {4, 1, 4},
	} {
		if got := hopsFor(tc.probes, tc.alpha); got != tc.want {
			t.Errorf("hopsFor(%d, %d) = %d, want %d", tc.probes, tc.alpha, got, tc.want)
		}
	}
}

func TestStatsAndSummary(t *testing.T) {
	var s stats
	if s.probeVariance() != 0 || s.hopVariance() != 0 {
		t.Fatal("empty sample variance must be zero")
	}
	s.add(lookupMetric{Success: true, Probes: 2, Hops: 1})
	if s.probeVariance() != 0 || s.hopVariance() != 0 {
		t.Fatal("single sample variance must be zero")
	}
	s.add(lookupMetric{Success: false, Probes: 4, Hops: 3})
	if s.Count != 2 || s.Successes != 1 || s.ProbeMean != 3 || s.HopMean != 2 || s.probeVariance() != 2 || s.hopVariance() != 2 {
		t.Fatalf("unexpected statistics: %+v", s)
	}
	path := filepath.Join(t.TempDir(), "nested", "summary.csv")
	key := summaryKey{ConfigID: "n8-a3-k4", NodeCount: 8, Alpha: 3, KValue: 4, LookupType: "node"}
	if err := writeSummary(path, map[summaryKey]*stats{key: &s}); err != nil {
		t.Fatal(err)
	}
	rows := readCSV(t, path)
	want := []string{"n8-a3-k4", "8", "3", "4", "node", "2", "0.5000", "3.0000", "2.0000", "2.0000", "2.0000"}
	if len(rows) != 2 || !reflect.DeepEqual(rows[1], want) {
		t.Fatalf("summary CSV = %v, want header and %v", rows, want)
	}
}

func TestLookupRecorderFiltersAndCountsProbes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "raw.csv")
	recorder, err := newLookupRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := recorder.close(); err != nil {
			t.Error(err)
		}
	})
	probe := []byte(`{"type":"FIND_NODE","target_id":"target","sender_address":"source"}`)
	recorder.recordSend("destination", probe) // No lookup is active yet.
	recorder.startLookup("config", 7, 8, 2, 4, 0, "node", "target")
	for _, ignored := range []string{
		`invalid JSON`,
		`{"type":"STORE"}`,
		`{"type":"FIND_NODE","target_id":"other"}`,
		`{"type":"FIND_VALUE","key":"target"}`,
	} {
		recorder.recordSend("destination", []byte(ignored))
	}
	for i := 0; i < 3; i++ {
		recorder.recordSend("destination", probe)
	}
	metric := recorder.finishLookup(false, "not found")
	if metric.Probes != 3 || metric.Hops != 2 || metric.Success || metric.Error != "not found" || metric.Seed != 7 || metric.Target != "target" {
		t.Fatalf("unexpected metric: %+v", metric)
	}
	recorder.recordSend("destination", probe) // Completed lookups must not count more probes.
	if got := recorder.finishLookup(true, ""); got != (lookupMetric{}) {
		t.Fatalf("inactive lookup returned %+v", got)
	}
	rows := readCSV(t, path)
	if len(rows) != 5 {
		t.Fatalf("CSV has %d rows, want header, 3 probes, and result", len(rows))
	}
	result := rows[4]
	if result[10] != "2" || result[11] != "result" || result[15] != "false" || result[16] != "not found" || result[17] != "3" {
		t.Fatalf("unexpected result row: %v", result)
	}
}

func readCSV(t *testing.T, path string) [][]string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}
