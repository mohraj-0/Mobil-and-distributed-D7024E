package kademlia

import (
	"crypto/ed25519"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

// Start disconnected replicas so publications can be missed before rejoining.
func catchupReplicas(t *testing.T) (latestFixture, *Kademlia) {
	t.Helper()
	f := newLatestFixture(t)
	shared := NewSimulatedNetwork()
	b := &Kademlia{K: 1, OwnershipVerifier: f.n.OwnershipVerifier}
	for i, node := range []*Kademlia{f.n, b} {
		transport := NewSimulatedNodeWithNetwork(shared)
		address := []string{"127.0.0.1:18701", "127.0.0.1:18702"}[i]
		if err := transport.Listen(address); err != nil {
			t.Fatal(err)
		}
		node.Network = transport
		node.RoutingTable = NewRoutingTable(NewContact(testID([]string{"11", "ff"}[i]), address))
		node.RPCRetries, node.RPCTimeout = 1, 100*time.Millisecond
		done := make(chan struct{})
		go func() { node.ListenForRPC(); close(done) }()
		t.Cleanup(func() { _ = transport.Close(); <-done })
	}
	b.ensureDataStore()
	for key, data := range f.n.DataSnapshot() {
		b.DataStore[key] = data
	}
	return f, b
}

func reconnectLatest(a, b *Kademlia) {
	a.K, b.K = 2, 2
	a.addContact(b.me())
	b.addContact(a.me())
}

func assertLatestHead(t *testing.T, n *Kademlia, p LatestPointer) {
	t.Helper()
	if string(n.DataSnapshot()[LatestKey(p.Domain, p.Package)]) != string(latestBytes(t, p)) {
		t.Fatalf("head did not become version %d", p.Version)
	}
}

func TestLatestRejoiningReplicaCatchesUpAndRepairs(t *testing.T) {
	for _, readerIsStale := range []bool{true, false} {
		t.Run(map[bool]string{true: "stale response first", false: "new response first"}[readerIsStale], func(t *testing.T) {
			f, departed := catchupReplicas(t)
			// The departed replica misses three publications and their records.
			newest := f.head
			for _, version := range []uint64{8, 13, 21} {
				newest = f.record(t, version, newest.VersionRecordHash, "publication", nil)
				f.accept(t, newest)
			}
			if _, ok := departed.DataSnapshot()[newest.VersionRecordHash]; ok {
				t.Fatal("departed node unexpectedly received publications")
			}
			reconnectLatest(f.n, departed)
			reader := f.n
			if readerIsStale {
				reader = departed
			}
			got, err := reader.LookupLatest(newest.Domain, newest.Package)
			if err != nil || got == nil || *got != newest {
				t.Fatalf("catch-up lookup: %v %v", got, err)
			}
			assertLatestHead(t, f.n, newest)
			assertLatestHead(t, departed, newest)
			// Delayed repair of an older signed pointer must not roll back.
			_, err = f.n.latestRPC(departed.me(), "UPDATE_LATEST", LatestKey(newest.Domain, newest.Package), latestBytes(t, f.head))
			var failure *LatestError
			if !errors.As(err, &failure) || failure.Status != "conflict" {
				t.Fatalf("delayed repair accepted: %v", err)
			}
			assertLatestHead(t, departed, newest)
		})
	}
}

func TestLatestCatchupMissingIntermediateIsAtomic(t *testing.T) {
	f, stale := catchupReplicas(t)
	middle := f.record(t, 8, f.head.VersionRecordHash, "middle", nil)
	newest := f.record(t, 13, middle.VersionRecordHash, "newest", nil)
	f.accept(t, newest)
	f.n.dataMu.Lock()
	delete(f.n.DataStore, middle.VersionRecordHash)
	f.n.dataMu.Unlock()
	reconnectLatest(f.n, stale)
	before := stale.DataSnapshot()
	_, err := f.n.latestRPC(stale.me(), "UPDATE_LATEST", LatestKey(newest.Domain, newest.Package), latestBytes(t, newest))
	var failure *LatestError
	if !errors.As(err, &failure) || failure.Status != "missing_data" {
		t.Fatalf("missing intermediate: %v", err)
	}
	if !reflect.DeepEqual(before, stale.DataSnapshot()) {
		t.Fatal("failed catch-up partially changed state")
	}
}

func TestLatestLocalHeadConstrainsLookupOutsideReplicaSet(t *testing.T) {
	f, remote := catchupReplicas(t)
	reconnectLatest(f.n, remote)
	// Make the remote the sole selected replica by placing its ID at the key.
	key := LatestKey(f.head.Domain, f.head.Package)
	remote.RoutingTable = NewRoutingTable(NewContact(NewKademliaID(key), remote.me().Address))
	f.n.RoutingTable = NewRoutingTable(f.n.me())
	f.n.K = 1
	f.n.addContact(remote.me())
	newest := f.record(t, 8, f.head.VersionRecordHash, "newest", nil)
	f.accept(t, newest)
	got, err := f.n.LookupLatest(newest.Domain, newest.Package)
	if err != nil || got == nil || *got != newest {
		t.Fatalf("local accepted history ignored: %v %v", got, err)
	}
	assertLatestHead(t, remote, newest)
}

func TestLatestRepairRechecksConcurrentPublication(t *testing.T) {
	f, replica := catchupReplicas(t)
	middle := f.record(t, 8, f.head.VersionRecordHash, "middle", nil)
	newest := f.record(t, 13, middle.VersionRecordHash, "newest", nil)
	reconnectLatest(f.n, replica)
	ready, release := make(chan struct{}), make(chan struct{})
	var blocked atomic.Bool
	replica.OwnershipVerifier = latestKeyResolver(func(string) (ed25519.PublicKey, error) {
		if blocked.CompareAndSwap(false, true) {
			close(ready)
			<-release
		}
		return f.pk, nil
	})
	// Hold repair in authentication while a newer compatible update commits.
	result := make(chan error, 1)
	go func() {
		result <- replica.acceptLatest(LatestKey(middle.Domain, middle.Package), latestBytes(t, middle))
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("repair did not reach authentication")
	}
	err := replica.acceptLatest(LatestKey(newest.Domain, newest.Package), latestBytes(t, newest))
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	var failure *LatestError
	if err := <-result; !errors.As(err, &failure) || failure.Status != "conflict" {
		t.Fatalf("concurrent repair overwrote publication: %v", err)
	}
	assertLatestHead(t, replica, newest)
}

func TestLatestDelayedReadPreservesConcurrentNewerHead(t *testing.T) {
	f, reader := catchupReplicas(t)
	middle := f.record(t, 8, f.head.VersionRecordHash, "middle", nil)
	newest := f.record(t, 13, middle.VersionRecordHash, "newest", nil)
	f.accept(t, middle)
	reconnectLatest(f.n, reader)
	ready, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	reader.OwnershipVerifier = latestKeyResolver(func(string) (ed25519.PublicKey, error) {
		// The local pointer and its two records consume the first three calls.
		// Hold authentication of the remote pointer until a newer head commits.
		if calls.Add(1) == 4 {
			close(ready)
			<-release
		}
		return f.pk, nil
	})
	type result struct {
		p   *LatestPointer
		err error
	}
	results := make(chan result, 1)
	go func() {
		p, err := reader.LookupLatest(middle.Domain, middle.Package)
		results <- result{p, err}
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("read did not reach remote authentication")
	}
	err := reader.acceptLatest(LatestKey(newest.Domain, newest.Package), latestBytes(t, newest))
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	r := <-results
	if r.err != nil || r.p == nil || *r.p != newest {
		t.Fatalf("delayed read failed to preserve newer publication: %v %v", r.p, r.err)
	}
	assertLatestHead(t, reader, newest)
	assertLatestHead(t, f.n, newest)
}
