package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"d7024e/kademlia"
	"d7024e/metadata"
)

const ZeroHash = metadata.ZeroHash
const DefaultMaxHistory = metadata.DefaultMaxHistory

// ContentStore is implemented by *kademlia.Kademlia. Registry metadata remains
// separate from the transport and uses the existing content-addressed path.
type ContentStore interface {
	Store([]byte) (string, error)
	LookupData(string) ([]byte, error)
}

type Registry struct {
	Store             ContentStore
	OwnershipVerifier OwnershipVerifier
	// MaxHistory bounds record fetches per traversal. Zero uses DefaultMaxHistory.
	MaxHistory int
}

type HistoryEntry = metadata.HistoryEntry

func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validHash(hash string) bool {
	bytes, err := hex.DecodeString(hash)
	return err == nil && len(bytes) == sha256.Size && hex.EncodeToString(bytes) == hash
}

func checkSize(kind string, data []byte) error {
	if len(data) < 1 || len(data) > kademlia.MaxValueSize {
		return fmt.Errorf("%s size %d bytes outside supported range 1–%d", kind, len(data), kademlia.MaxValueSize)
	}
	return nil
}

func (r *Registry) storeContent(kind string, data []byte) (string, error) {
	if err := checkSize(kind, data); err != nil {
		return "", err
	}
	if r.Store == nil {
		return "", fmt.Errorf("content store is not configured")
	}
	hash, err := r.Store.Store(append([]byte(nil), data...))
	if err != nil {
		return "", fmt.Errorf("store %s: %w", kind, err)
	}
	if hash != contentHash(data) {
		return "", fmt.Errorf("store returned incorrect %s hash", kind)
	}
	return hash, nil
}

func (r *Registry) fetch(kind, hash string) ([]byte, error) {
	if !validHash(hash) || hash == ZeroHash {
		return nil, fmt.Errorf("invalid %s hash %q", kind, hash)
	}
	if r.Store == nil {
		return nil, fmt.Errorf("content store is not configured")
	}
	data, err := r.Store.LookupData(hash)
	if err != nil {
		return nil, fmt.Errorf("fetch %s %s: %w", kind, hash, err)
	}
	if err := checkSize(kind, data); err != nil {
		return nil, err
	}
	if contentHash(data) != hash {
		return nil, fmt.Errorf("%s hash mismatch for %s", kind, hash)
	}
	return append([]byte(nil), data...), nil
}

func (r *Registry) StoreBlob(data []byte) (string, error) {
	return r.storeContent("package blob", data)
}

func (r *Registry) LookupBlob(hash string) ([]byte, error) {
	return r.fetch("package blob", hash)
}

// StoreVersionRecord stores the complete signed record only after authenticating
// its predecessor history. It never changes a latest pointer or an older entry.
func (r *Registry) StoreVersionRecord(record *VersionRecord) (string, error) {
	if r.historyLimit() <= 0 {
		return "", fmt.Errorf("history limit must be positive")
	}
	if record == nil {
		return "", fmt.Errorf("version record is nil")
	}
	snapshot := *record
	data, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	if err := checkSize("encoded version record", data); err != nil {
		return "", err
	}
	if err := r.VerifyOwnedVersionRecord(&snapshot); err != nil {
		return "", err
	}
	if err := validateReferences(&snapshot); err != nil {
		return "", err
	}
	if snapshot.PreviousVersionRecord != ZeroHash {
		history, err := r.TraverseHistory(snapshot.PreviousVersionRecord, snapshot.Domain, snapshot.Package)
		if err != nil {
			return "", fmt.Errorf("invalid predecessor: %w", err)
		}
		if snapshot.Version <= history[0].Record.Version {
			return "", fmt.Errorf("version must increase beyond predecessor")
		}
		if len(history) >= r.historyLimit() {
			return "", fmt.Errorf("history exceeds traversal limit %d", r.historyLimit())
		}
	}
	return r.storeContent("encoded version record", data)
}

func validateReferences(record *VersionRecord) error {
	if !validHash(record.BlobHash) || record.BlobHash == ZeroHash {
		return fmt.Errorf("invalid blob hash")
	}
	if !validHash(record.PreviousVersionRecord) {
		return fmt.Errorf("invalid predecessor hash")
	}
	return nil
}

func (r *Registry) historyLimit() int {
	if r.MaxHistory == 0 {
		return DefaultMaxHistory
	}
	return r.MaxHistory
}

func visitHistoryHash(visited map[string]bool, hash string, limit int) error {
	return metadata.VisitHistoryHash(visited, hash, limit)
}

// TraverseHistory returns newest first, and returns no partial authenticated
// history on failure. Namespace checks precede ownership-key lookup.
func (r *Registry) TraverseHistory(head, domain, pkg string) ([]HistoryEntry, error) {
	if domain == "" || pkg == "" {
		return nil, fmt.Errorf("domain and package are required")
	}
	if r.historyLimit() <= 0 {
		return nil, fmt.Errorf("history limit must be positive")
	}
	if !validHash(head) || head == ZeroHash {
		return nil, fmt.Errorf("invalid history head")
	}
	return metadata.TraverseHistory(head, domain, pkg, r.historyLimit(), func(hash string) ([]byte, error) {
		return r.fetch("version record", hash)
	}, r.VerifyOwnedVersionRecord)
}

// ResolveVersionRecord authenticates the requested record and its entire ancestry.
func (r *Registry) ResolveVersionRecord(hash, domain, pkg string) (*VersionRecord, error) {
	history, err := r.TraverseHistory(hash, domain, pkg)
	if err != nil {
		return nil, err
	}
	record := history[0].Record
	return &record, nil
}

// DownloadVersion retrieves a historical package using its immutable record hash.
func (r *Registry) DownloadVersion(hash, domain, pkg string) ([]byte, error) {
	record, err := r.ResolveVersionRecord(hash, domain, pkg)
	if err != nil {
		return nil, err
	}
	return r.LookupBlob(record.BlobHash)
}
