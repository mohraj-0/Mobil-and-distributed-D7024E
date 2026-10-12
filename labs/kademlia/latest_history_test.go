package kademlia

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"d7024e/metadata"
)

type latestFixture struct {
	n           *Kademlia
	sk          ed25519.PrivateKey
	pk          ed25519.PublicKey
	first, head LatestPointer
}

func newLatestFixture(t *testing.T) latestFixture {
	t.Helper()
	pk, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	transport := NewSimulatedNodeWithNetwork(NewSimulatedNetwork())
	if err := transport.Listen("127.0.0.1:18600"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = transport.Close() })
	n := &Kademlia{Network: transport, RoutingTable: NewRoutingTable(NewContact(testID("11"), "127.0.0.1:18600")), K: 1,
		OwnershipVerifier: DNSOwnershipVerifier{Resolver: NewFakeTXTResolver(map[string][]string{"example.com": {ownershipTXTPrefix + hex.EncodeToString(pk)}})}}
	f := latestFixture{n: n, sk: sk, pk: pk}
	f.first = f.record(t, 1, metadata.ZeroHash, "first", nil)
	f.accept(t, f.first)
	f.head = f.record(t, 5, f.first.VersionRecordHash, "head", nil)
	f.accept(t, f.head)
	return f
}

func (f latestFixture) record(t *testing.T, version uint64, previous, blob string, change func(*VersionRecord)) LatestPointer {
	t.Helper()
	r := VersionRecord{Tag: VersionRecordTag, Domain: "example.com", Package: "demo", Version: version, BlobHash: hashValue([]byte(blob)), PreviousVersionRecord: previous}
	if change != nil {
		change(&r)
	}
	if err := SignVersionRecord(&r, f.sk); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := f.n.Store(data)
	if err != nil {
		t.Fatal(err)
	}
	p := LatestPointer{Tag: LatestPointerTag, Domain: "example.com", Package: "demo", Version: version, VersionRecordHash: hash}
	return f.sign(t, p, f.sk)
}

func (f latestFixture) sign(t *testing.T, p LatestPointer, sk ed25519.PrivateKey) LatestPointer {
	t.Helper()
	if err := SignLatestPointer(&p, sk); err != nil {
		t.Fatal(err)
	}
	return p
}

func latestBytes(t *testing.T, p LatestPointer) []byte {
	t.Helper()
	value, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func (f latestFixture) accept(t *testing.T, p LatestPointer) {
	t.Helper()
	if err := f.n.acceptLatest(LatestKey(p.Domain, p.Package), latestBytes(t, p)); err != nil {
		t.Fatal(err)
	}
}

func TestLatestHistoryRejectionsAreAtomic(t *testing.T) {
	for _, name := range []string{"non-owner pointer", "non-owner record", "rollback", "same-version replacement", "fork", "wrong domain", "wrong package", "cross-package ancestor", "pointer version mismatch", "equal predecessor version", "decreasing successor version", "missing skipped ancestry", "invalid predecessor", "invalid genesis", "zero blob", "zero head", "cyclic untrusted storage"} {
		t.Run(name, func(t *testing.T) {
			f := newLatestFixture(t)
			p := f.head
			status := "conflict"
			switch name {
			case "non-owner pointer":
				_, other, err := ed25519.GenerateKey(rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				p = f.sign(t, p, other)
				status = "authentication"
			case "non-owner record":
				_, other, err := ed25519.GenerateKey(rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				outsider := f
				outsider.sk = other
				p = outsider.record(t, 6, f.head.VersionRecordHash, "outsider", nil)
				p = f.sign(t, p, f.sk)
				status = "authentication"
			case "rollback":
				p = f.first
			case "same-version replacement":
				p = f.record(t, 5, f.first.VersionRecordHash, "replacement", nil)
			case "fork":
				p = f.record(t, 7, metadata.ZeroHash, "other genesis", nil)
			case "wrong domain":
				p = f.record(t, 6, f.head.VersionRecordHash, "wrong", func(r *VersionRecord) { r.Domain = "other.com" })
			case "wrong package":
				p = f.record(t, 6, f.head.VersionRecordHash, "wrong", func(r *VersionRecord) { r.Package = "other" })
			case "cross-package ancestor":
				ancestor := f.record(t, 6, f.head.VersionRecordHash, "wrong", func(r *VersionRecord) { r.Package = "other" })
				p = f.record(t, 8, ancestor.VersionRecordHash, "successor", nil)
			case "pointer version mismatch":
				p = f.record(t, 6, f.head.VersionRecordHash, "successor", nil)
				p.Version = 7
				p = f.sign(t, p, f.sk)
			case "equal predecessor version":
				p = f.record(t, 5, f.head.VersionRecordHash, "equal", nil)
			case "decreasing successor version":
				p = f.record(t, 4, f.head.VersionRecordHash, "decrease", nil)
			case "missing skipped ancestry":
				p = f.record(t, 9, strings.Repeat("a", 64), "missing", nil)
				status = "missing_data"
			case "invalid predecessor":
				p = f.record(t, 6, "bad", "invalid", nil)
			case "invalid genesis":
				p = f.record(t, 7, strings.Repeat("a", 64), "not genesis", nil)
				status = "missing_data"
			case "zero blob":
				p = f.record(t, 6, f.head.VersionRecordHash, "zero", func(r *VersionRecord) { r.BlobHash = metadata.ZeroHash })
			case "zero head":
				p.VersionRecordHash = metadata.ZeroHash
				p = f.sign(t, p, f.sk)
			case "cyclic untrusted storage":
				// Real SHA-256 addressed cycles require hash fixed points. Model
				// malicious cyclic storage; it must fail the immutable hash gate.
				a, b := strings.Repeat("a", 64), strings.Repeat("b", 64)
				for hash, prev := range map[string]string{a: b, b: a} {
					r := VersionRecord{Tag: VersionRecordTag, Domain: p.Domain, Package: p.Package, Version: 6, BlobHash: hashValue([]byte("cycle")), PreviousVersionRecord: prev}
					if err := SignVersionRecord(&r, f.sk); err != nil {
						t.Fatal(err)
					}
					data, err := json.Marshal(r)
					if err != nil {
						t.Fatal(err)
					}
					f.n.dataMu.Lock()
					f.n.DataStore[hash] = data
					f.n.dataMu.Unlock()
				}
				p.Version, p.VersionRecordHash = 6, a
				p = f.sign(t, p, f.sk)
				status = "missing_data"
			}
			before := f.n.DataSnapshot()
			err := f.n.acceptLatest(LatestKey(p.Domain, p.Package), latestBytes(t, p))
			var failure *LatestError
			if !errors.As(err, &failure) || failure.Status != status {
				t.Fatalf("want %s rejection, got %v", status, err)
			}
			if !reflect.DeepEqual(before, f.n.DataSnapshot()) {
				t.Fatal("rejection changed stored state")
			}
		})
	}
}

func TestLatestSkippedVersionsAndReplay(t *testing.T) {
	f := newLatestFixture(t)
	middle := f.record(t, 8, f.head.VersionRecordHash, "intermediate", nil)
	skipped := f.record(t, 20, middle.VersionRecordHash, "newest", nil)
	f.accept(t, skipped)
	before := f.n.DataSnapshot()
	f.accept(t, skipped)
	if !reflect.DeepEqual(before, f.n.DataSnapshot()) {
		t.Fatal("identical replay changed state")
	}
	if string(before[LatestKey(skipped.Domain, skipped.Package)]) != string(latestBytes(t, skipped)) {
		t.Fatal("skipped successor was not committed")
	}
}

type latestKeyResolver func(string) (ed25519.PublicKey, error)

func (r latestKeyResolver) ResolvePublicKey(domain string) (ed25519.PublicKey, error) {
	return r(domain)
}

func TestLatestConcurrentSiblings(t *testing.T) {
	for _, sameVersion := range []bool{false, true} {
		for attempt := 0; attempt < 20; attempt++ {
			f := newLatestFixture(t)
			versions := []uint64{6, 7}
			if sameVersion {
				versions[1] = 6
			}
			candidates := []LatestPointer{f.record(t, versions[0], f.head.VersionRecordHash, "left", nil), f.record(t, versions[1], f.head.VersionRecordHash, "right", nil)}
			values := [][]byte{latestBytes(t, candidates[0]), latestBytes(t, candidates[1])}
			ready, release := make(chan struct{}, 2), make(chan struct{})
			var calls atomic.Int32
			f.n.OwnershipVerifier = latestKeyResolver(func(string) (ed25519.PublicKey, error) {
				if calls.Add(1) <= 2 {
					ready <- struct{}{}
					<-release
				}
				return f.pk, nil
			})
			type result struct {
				index int
				err   error
			}
			results := make(chan result, 2)
			key := LatestKey(f.head.Domain, f.head.Package)
			for i := range candidates {
				go func(i int) { results <- result{i, f.n.acceptLatest(key, values[i])} }(i)
			}
			for i := 0; i < 2; i++ {
				select {
				case <-ready:
				case <-time.After(5 * time.Second):
					close(release)
					t.Fatal("siblings did not validate concurrently")
				}
			}
			close(release)
			winner, loser := -1, -1
			for i := 0; i < 2; i++ {
				r := <-results
				if r.err == nil {
					if winner != -1 {
						t.Fatal("both siblings accepted")
					}
					winner = r.index
				} else {
					var failure *LatestError
					if !errors.As(r.err, &failure) || failure.Status != "conflict" {
						t.Fatalf("sibling rejection: %v", r.err)
					}
					loser = r.index
				}
			}
			if winner == -1 || loser == -1 {
				t.Fatal("expected exactly one winner and one rejection")
			}
			before := f.n.DataSnapshot()
			if string(before[key]) != string(values[winner]) {
				t.Fatal("stored head differs from winner")
			}
			if err := f.n.acceptLatest(key, values[loser]); err == nil {
				t.Fatal("loser replay accepted")
			}
			if !reflect.DeepEqual(before, f.n.DataSnapshot()) {
				t.Fatal("loser rejection changed state")
			}
			f.n.pointerMu.Lock()
			remaining := len(f.n.pointerLocks)
			f.n.pointerMu.Unlock()
			if remaining != 0 {
				t.Fatal("package locks retained after callers finished")
			}
		}
	}
}

func TestLatestRechecksHeadAfterValidation(t *testing.T) {
	f := newLatestFixture(t)
	a := f.record(t, 6, f.head.VersionRecordHash, "left", nil)
	b := f.record(t, 7, f.head.VersionRecordHash, "right", nil)
	key := LatestKey(a.Domain, a.Package)
	value := latestBytes(t, b)
	p, history, err := f.n.verifyLatest(key, value)
	if err != nil {
		t.Fatal(err)
	}
	f.accept(t, a)
	before := f.n.DataSnapshot()
	if err := f.n.commitLatest(key, value, p, history); err == nil {
		t.Fatal("stale validation committed sibling")
	}
	if !reflect.DeepEqual(before, f.n.DataSnapshot()) {
		t.Fatal("stale validation rejection changed head")
	}
}

func TestLatestPackageLocksAreIndependent(t *testing.T) {
	f := newLatestFixture(t)
	p := f.record(t, 1, metadata.ZeroHash, "other package", func(r *VersionRecord) { r.Package = "other" })
	p.Package = "other"
	p = f.sign(t, p, f.sk)
	value := latestBytes(t, p)
	unlock := f.n.lockLatestPackage(LatestKey(f.head.Domain, f.head.Package))
	result := make(chan error, 1)
	go func() { result <- f.n.acceptLatest(LatestKey(p.Domain, p.Package), value) }()
	select {
	case err := <-result:
		unlock()
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		unlock()
		<-result
		t.Fatal("one package's commit lock blocked another package")
	}
}

func TestLatestPartitionedOwnerEquivocation(t *testing.T) {
	for _, version := range []uint64{5, 9} {
		f := newLatestFixture(t)
		// Both branches are signed by the trusted owner. Before the replicas
		// meet, neither can know that the owner published another genesis.
		fork := f.record(t, version, metadata.ZeroHash, "partition branch", nil)
		shared := NewSimulatedNetwork()
		nodes := []*Kademlia{f.n, {K: 1, OwnershipVerifier: f.n.OwnershipVerifier}}
		for i, node := range nodes {
			transport := NewSimulatedNodeWithNetwork(shared)
			address := []string{"127.0.0.1:18601", "127.0.0.1:18602"}[i]
			if err := transport.Listen(address); err != nil {
				t.Fatal(err)
			}
			node.Network = transport
			node.RoutingTable = NewRoutingTable(NewContact(testID([]string{"11", "ff"}[i]), address))
			done := make(chan struct{})
			go func() { node.ListenForRPC(); close(done) }()
			t.Cleanup(func() { _ = transport.Close(); <-done })
		}
		key := LatestKey(f.head.Domain, f.head.Package)
		for hash, value := range f.n.DataSnapshot() {
			if hash != key {
				nodes[1].ensureDataStore()
				nodes[1].dataMu.Lock()
				nodes[1].DataStore[hash] = value
				nodes[1].dataMu.Unlock()
			}
		}
		if err := nodes[1].acceptLatest(key, latestBytes(t, fork)); err != nil {
			t.Fatal(err)
		}
		for _, node := range nodes {
			node.K = 2
			node.RPCTimeout = time.Second
			node.RPCRetries = 1
		}
		nodes[0].addContact(nodes[1].me())
		nodes[1].addContact(nodes[0].me())
		before := []map[string][]byte{nodes[0].DataSnapshot(), nodes[1].DataSnapshot()}
		for i, node := range nodes {
			got, err := node.LookupLatest(f.head.Domain, f.head.Package)
			var failure *LatestError
			if got != nil || !errors.As(err, &failure) || failure.Status != "conflict" {
				t.Fatalf("replica %d silently resolved branches: %v %v", i, got, err)
			}
			if !reflect.DeepEqual(before[i], node.DataSnapshot()) {
				t.Fatal("conflicting read changed stored state")
			}
		}
	}
}
