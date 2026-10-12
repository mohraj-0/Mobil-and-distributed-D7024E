package metadata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const ZeroHash = "0000000000000000000000000000000000000000000000000000000000000000"
const DefaultMaxHistory = 1024

type HistoryEntry struct {
	Hash   string
	Record VersionRecord
}

// HistoryError retains fetch causes, including transport timeouts.
type HistoryError struct {
	Status string
	Err    error
}

func (e *HistoryError) Error() string { return e.Err.Error() }
func (e *HistoryError) Unwrap() error { return e.Err }
func historyError(status, detail string) error {
	return &HistoryError{Status: status, Err: fmt.Errorf("%s", detail)}
}
func validHistoryHash(hash string) bool {
	data, err := hex.DecodeString(hash)
	return err == nil && len(data) == sha256.Size && hex.EncodeToString(data) == hash
}

// VisitHistoryHash rejects cycles and bounds traversal.
func VisitHistoryHash(visited map[string]bool, hash string, limit int) error {
	if visited[hash] {
		return historyError("conflict", "cycle in history at "+hash)
	}
	if len(visited) >= limit {
		return historyError("conflict", fmt.Sprintf("history exceeds traversal limit %d", limit))
	}
	visited[hash] = true
	return nil
}

// TraverseHistory is the shared registry and pointer-acceptance policy. It
// authenticates every record, checks namespace and increasing successor
// versions, and requires a zero predecessor at genesis. No partial history is
// returned on failure, and this function never changes a stored head.
func TraverseHistory(head, domain, pkg string, limit int, fetch func(string) ([]byte, error), verify func(*VersionRecord) error) ([]HistoryEntry, error) {
	if domain == "" || pkg == "" || limit <= 0 || !validHistoryHash(head) || head == ZeroHash {
		return nil, historyError("conflict", "invalid history namespace, head or traversal limit")
	}
	visited := make(map[string]bool)
	history := []HistoryEntry{}
	for hash := head; hash != ZeroHash; {
		if !validHistoryHash(hash) {
			return nil, historyError("conflict", "invalid predecessor hash")
		}
		if err := VisitHistoryHash(visited, hash, limit); err != nil {
			return nil, err
		}
		data, err := fetch(hash)
		if err != nil {
			return nil, &HistoryError{Status: "missing_data", Err: err}
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != hash {
			return nil, historyError("authentication", "version record hash mismatch")
		}
		var record VersionRecord
		if json.Unmarshal(data, &record) != nil {
			return nil, historyError("malformed", "invalid version record")
		}
		if record.Domain != domain || record.Package != pkg {
			return nil, historyError("conflict", "cross-domain/package history link at "+hash)
		}
		if err := verify(&record); err != nil {
			return nil, &HistoryError{Status: "authentication", Err: err}
		}
		if !validHistoryHash(record.BlobHash) || record.BlobHash == ZeroHash || !validHistoryHash(record.PreviousVersionRecord) {
			return nil, historyError("conflict", "invalid blob or predecessor hash")
		}
		if visited[record.PreviousVersionRecord] {
			return nil, historyError("conflict", "cycle in history at "+record.PreviousVersionRecord)
		}
		if len(history) > 0 && record.Version >= history[len(history)-1].Record.Version {
			return nil, historyError("conflict", "history versions must strictly increase")
		}
		history = append(history, HistoryEntry{Hash: hash, Record: record})
		hash = record.PreviousVersionRecord
	}
	return history, nil
}
