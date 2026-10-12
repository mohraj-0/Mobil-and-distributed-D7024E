package metadata

import (
	"errors"
	"strings"
	"testing"
)

func TestHistoryCycleAndBoundGuards(t *testing.T) {
	a, b := strings.Repeat("a", 64), strings.Repeat("b", 64)
	for _, chain := range [][]string{{a, a}, {a, b, a}} {
		visited := map[string]bool{}
		for i, hash := range chain {
			err := VisitHistoryHash(visited, hash, 3)
			if i < len(chain)-1 {
				if err != nil {
					t.Fatal(err)
				}
				continue
			}
			var failure *HistoryError
			if !errors.As(err, &failure) || failure.Status != "conflict" || !strings.Contains(err.Error(), "cycle") {
				t.Fatalf("cycle accepted: %v", err)
			}
			if len(visited) != len(chain)-1 {
				t.Fatal("cycle changed visited state")
			}
		}
	}
	visited := map[string]bool{a: true}
	if err := VisitHistoryHash(visited, b, 1); err == nil {
		t.Fatal("traversal limit ignored")
	}
	if visited[b] {
		t.Fatal("limit rejection changed visited state")
	}
}
