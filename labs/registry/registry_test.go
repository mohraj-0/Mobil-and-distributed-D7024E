package registry

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"d7024e/kademlia"
)

type memoryStore map[string][]byte

func TestHistoryCycleGuard(t *testing.T) {
	visited := map[string]bool{}
	hash := strings.Repeat("a", 64)
	if err := visitHistoryHash(visited, hash, 2); err != nil {
		t.Fatal(err)
	}
	if err := visitHistoryHash(visited, hash, 2); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle guard: %v", err)
	}
	if err := visitHistoryHash(visited, strings.Repeat("b", 64), 1); err == nil {
		t.Fatal("unbounded traversal")
	}
}

func TestBlobTamperingAndInvalidConfiguration(t *testing.T) {
	r, store, sk := fixture(t)
	hash, err := r.StoreBlob([]byte("original"))
	if err != nil {
		t.Fatal(err)
	}
	store[hash] = []byte("tampered")
	if _, err := r.LookupBlob(hash); err == nil {
		t.Fatal("accepted corrupted blob")
	}
	record := signedRecord(t, sk, 1, hash, ZeroHash)
	r.MaxHistory = -1
	if _, err := r.StoreVersionRecord(&record); err == nil {
		t.Fatal("accepted negative history limit")
	}
	r.MaxHistory = 0
	r.OwnershipVerifier = nil
	if _, err := r.StoreVersionRecord(&record); err == nil {
		t.Fatal("accepted record without ownership verifier")
	}
	r.Store = nil
	if _, err := r.StoreBlob([]byte("blob")); err == nil {
		t.Fatal("accepted missing content store")
	}
}

func (m memoryStore) Store(data []byte) (string, error) {
	hash := contentHash(data)
	m[hash] = append([]byte(nil), data...)
	return hash, nil
}
func (m memoryStore) LookupData(hash string) ([]byte, error) {
	data, ok := m[hash]
	if !ok {
		return nil, fmt.Errorf("missing record")
	}
	return append([]byte(nil), data...), nil
}

func fixture(t *testing.T) (*Registry, memoryStore, ed25519.PrivateKey) {
	t.Helper()
	pk, sk := testKeyPair(t)
	dns := NewFakeTXTResolver(nil)
	if err := ConfigureTestDomainOwner(dns, "example.com", pk); err != nil {
		t.Fatal(err)
	}
	if err := ConfigureTestDomainOwner(dns, "other.com", pk); err != nil {
		t.Fatal(err)
	}
	store := memoryStore{}
	return &Registry{Store: store, OwnershipVerifier: DNSOwnershipVerifier{Resolver: dns}}, store, sk
}

func signedRecord(t *testing.T, sk ed25519.PrivateKey, version uint64, blob, prev string) VersionRecord {
	t.Helper()
	record := VersionRecord{Tag: VersionRecordTag, Domain: "example.com", Package: "mypackage", Version: version, BlobHash: blob, PreviousVersionRecord: prev}
	if err := SignVersionRecord(&record, sk); err != nil {
		t.Fatal(err)
	}
	return record
}

func rawRecord(t *testing.T, store memoryStore, record VersionRecord) string {
	t.Helper()
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := store.Store(data)
	return hash
}

func TestHistoryAndHistoricalBinaryAvailability(t *testing.T) {
	r, store, sk := fixture(t)
	original := []byte{0, 255, 128, 1, 0}
	blob, err := r.StoreBlob(original)
	if err != nil {
		t.Fatal(err)
	}
	original[0] = 99
	v1 := signedRecord(t, sk, 1, blob, ZeroHash)
	first, err := r.StoreVersionRecord(&v1)
	if err != nil {
		t.Fatal(err)
	}
	firstBytes := append([]byte(nil), store[first]...)
	blob2, _ := r.StoreBlob([]byte("new release"))
	v2 := signedRecord(t, sk, 7, blob2, first)
	head, err := r.StoreVersionRecord(&v2)
	if err != nil {
		t.Fatal(err)
	}
	history, err := r.TraverseHistory(head, "example.com", "mypackage")
	if err != nil || len(history) != 2 || history[0].Record.Version != 7 || history[1].Hash != first {
		t.Fatalf("history: %v %v", history, err)
	}
	for _, hash := range []string{first, head} {
		data, err := r.DownloadVersion(hash, "example.com", "mypackage")
		if err != nil {
			t.Fatal(err)
		}
		expected := []byte("new release")
		if hash == first {
			expected = []byte{0, 255, 128, 1, 0}
		}
		if !bytes.Equal(data, expected) {
			t.Fatalf("download %s: %v", hash, data)
		}
	}
	if !bytes.Equal(firstBytes, store[first]) {
		t.Fatal("old record overwritten")
	}
}

func TestRejectInvalidHistory(t *testing.T) {
	for _, kind := range []string{"tampered bytes", "tampered signature", "missing predecessor", "cross package", "cross domain", "rollback", "invalid reference", "bounded"} {
		t.Run(kind, func(t *testing.T) {
			r, store, sk := fixture(t)
			blob, _ := r.StoreBlob([]byte("blob"))
			prev := signedRecord(t, sk, 1, blob, ZeroHash)
			if kind == "cross package" {
				prev.Package = "other"
				_ = SignVersionRecord(&prev, sk)
			}
			if kind == "cross domain" {
				prev.Domain = "other.com"
				_ = SignVersionRecord(&prev, sk)
			}
			first := rawRecord(t, store, prev)
			next := signedRecord(t, sk, 2, blob, first)
			if kind == "rollback" {
				next.Version = 1
				_ = SignVersionRecord(&next, sk)
			}
			if kind == "missing predecessor" {
				delete(store, first)
			}
			if kind == "invalid reference" {
				next.PreviousVersionRecord = "bad"
				_ = SignVersionRecord(&next, sk)
			}
			if kind == "tampered signature" {
				next.Version = 3
			}
			if kind == "bounded" {
				r.MaxHistory = 1
			}
			head := rawRecord(t, store, next)
			if kind == "tampered bytes" {
				store[head][0] ^= 1
			}
			history, err := r.TraverseHistory(head, "example.com", "mypackage")
			if err == nil || history != nil {
				t.Fatalf("accepted %s: %v", kind, history)
			}
		})
	}
}

func TestStoreLimitsAndPredecessorValidation(t *testing.T) {
	r, _, sk := fixture(t)
	for _, size := range []int{0, 1, 255, 32768, 32769} {
		hash, err := r.StoreBlob(make([]byte, size))
		if (err == nil) != (size > 0 && size <= kademlia.MaxValueSize) {
			t.Fatalf("size %d: %v", size, err)
		}
		if err == nil {
			data, err := r.LookupBlob(hash)
			if err != nil || len(data) != size {
				t.Fatalf("round trip %d: %v", size, err)
			}
		}
	}
	record := signedRecord(t, sk, 1, contentHash([]byte("blob")), ZeroHash)
	record.Package = strings.Repeat("x", kademlia.MaxValueSize)
	_ = SignVersionRecord(&record, sk)
	if _, err := r.StoreVersionRecord(&record); err == nil || !strings.Contains(err.Error(), "encoded version record size") {
		t.Fatalf("oversized record: %v", err)
	}
	record = signedRecord(t, sk, 2, contentHash([]byte("blob")), strings.Repeat("a", 64))
	if _, err := r.StoreVersionRecord(&record); err == nil {
		t.Fatal("accepted missing predecessor")
	}
	record.PreviousVersionRecord = ZeroHash
	record.Signature = strings.Repeat("0", 128)
	if _, err := r.StoreVersionRecord(&record); err == nil {
		t.Fatal("accepted unauthorized record")
	}
}

func TestRegistryAcrossSimulatedDHT(t *testing.T) {
	sim := kademlia.NewSimulatedNetwork()
	nodes := make([]*kademlia.Kademlia, 2)
	for i := range nodes {
		address := fmt.Sprintf("127.0.0.1:%d", 19000+i)
		transport := kademlia.NewSimulatedNodeWithNetwork(sim)
		if err := transport.Listen(address); err != nil {
			t.Fatal(err)
		}
		contact := kademlia.NewContact(kademlia.NewKademliaID(contentHash([]byte(address))), address)
		nodes[i] = &kademlia.Kademlia{Network: transport, RoutingTable: kademlia.NewRoutingTable(contact), K: 1, RPCTimeout: time.Second}
		done := make(chan struct{})
		node := nodes[i]
		go func() { node.ListenForRPC(); close(done) }()
		t.Cleanup(func() { _ = transport.Close(); <-done })
	}
	if err := nodes[1].Join(nodes[0].RoutingTable.Me()); err != nil {
		t.Fatal(err)
	}
	r, _, sk := fixture(t)
	r.Store = nodes[0]
	blob, err := r.StoreBlob([]byte{0, 255, 1, 128})
	if err != nil {
		t.Fatal(err)
	}
	record := signedRecord(t, sk, 1, blob, ZeroHash)
	head, err := r.StoreVersionRecord(&record)
	if err != nil {
		t.Fatal(err)
	}
	r.Store = nodes[1]
	data, err := r.DownloadVersion(head, "example.com", "mypackage")
	if err != nil || !bytes.Equal(data, []byte{0, 255, 1, 128}) {
		t.Fatalf("DHT download: %v %v", data, err)
	}
}
