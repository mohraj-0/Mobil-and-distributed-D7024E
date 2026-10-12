package kademlia

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"d7024e/metadata"
)

// Observe the real transport, never synthesize replies or share node stores.
type registryWire struct {
	mu        sync.Mutex
	blocked   map[string]bool
	delivered map[string]int
}

type observedRegistryNode struct {
	Node
	address string
	wire    *registryWire
}

func (n *observedRegistryNode) SendData(address string, data []byte) error {
	n.wire.mu.Lock()
	blocked := n.wire.blocked[n.address] || n.wire.blocked[address]
	n.wire.mu.Unlock()
	if blocked {
		return fmt.Errorf("test partition: %s -> %s", n.address, address)
	}
	if err := n.Node.SendData(address, data); err != nil {
		return err
	}
	var rpc rpcMessage
	if err := json.Unmarshal(data, &rpc); err != nil {
		return err
	}
	n.wire.mu.Lock()
	n.wire.delivered[n.address+"|"+address+"|"+rpc.Type]++
	n.wire.mu.Unlock()
	return nil
}

func (w *registryWire) count(from, to, kind string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.delivered[from+"|"+to+"|"+kind]
}

func registryE2ENodes(t *testing.T, kind string) (latestFixture, []*Kademlia, *registryWire) {
	t.Helper()
	pk, sk, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	verifier := DNSOwnershipVerifier{Resolver: NewFakeTXTResolver(map[string][]string{"example.com": {"d7024e-ed25519=" + hex.EncodeToString(pk)}})}
	network := NewSimulatedNetwork()
	wire := &registryWire{blocked: map[string]bool{}, delivered: map[string]int{}}
	nodes := make([]*Kademlia, 3)
	for i := range nodes {
		var transport Node = NewSimulatedNodeWithNetwork(network)
		address := fmt.Sprintf("127.0.0.1:%d", 18800+i)
		if kind == "udp" {
			transport = NewUDPNode()
			address = "127.0.0.1:0"
		}
		if err := transport.Listen(address); err != nil {
			t.Fatal(err)
		}
		if udp, ok := transport.(*UDPNode); ok {
			address = udp.address
		}
		node := &Kademlia{Network: &observedRegistryNode{transport, address, wire}, RoutingTable: NewRoutingTable(NewContact(testID([]string{"11", "88", "ff"}[i]), address)), K: 3, Alpha: 3, RPCRetries: 1, RPCTimeout: time.Second, OwnershipVerifier: verifier}
		nodes[i] = node
		done := make(chan struct{})
		go func() { node.ListenForRPC(); close(done) }()
		t.Cleanup(func() { _ = transport.Close(); <-done })
	}
	for _, n := range nodes {
		for _, peer := range nodes {
			if peer != n {
				n.addContact(peer.me())
			}
		}
	}
	return latestFixture{n: nodes[0], pk: pk, sk: sk}, nodes, wire
}

func publishE2E(t *testing.T, f latestFixture, version uint64, prev, payload string) LatestPointer {
	t.Helper()
	if _, err := f.n.Store([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	p := f.record(t, version, prev, payload, nil)
	results, err := f.n.PublishLatest(p)
	if err != nil {
		t.Fatalf("publication %d: %v", version, err)
	}
	for address, err := range results {
		if err != nil {
			t.Fatalf("replica %s: %v", address, err)
		}
	}
	return p
}

func TestRegistryRemoteAuthenticityAndImmutableHistory(t *testing.T) {
	for _, kind := range []string{"simulated", "udp"} {
		t.Run(kind, func(t *testing.T) {
			f, nodes, wire := registryE2ENodes(t, kind)
			first := publishE2E(t, f, 1, metadata.ZeroHash, "first binary")
			immutable := nodes[1].DataSnapshot()
			head := publishE2E(t, f, 2, first.VersionRecordHash, "second binary")
			key := LatestKey(head.Domain, head.Package)
			for _, n := range nodes {
				assertLatestHead(t, n, head)
				for hash, value := range immutable {
					if hash != key && !bytes.Equal(n.DataSnapshot()[hash], value) {
						t.Fatalf("publication changed immutable %s", hash)
					}
				}
			}
			for _, name := range []string{"non-owner pointer", "non-owner ancestor", "fork", "rollback", "same version", "duplicate replay", "malformed ancestor", "missing ancestor", "cross-package ancestor", "decreasing ancestry", "cyclic storage"} {
				t.Run(name, func(t *testing.T) {
					p, status := head, "conflict"
					switch name {
					case "non-owner pointer":
						_, outsider, err := ed25519.GenerateKey(nil)
						if err != nil {
							t.Fatal(err)
						}
						p = f.sign(t, head, outsider)
						status = "authentication"
					case "non-owner ancestor":
						_, outsider, err := ed25519.GenerateKey(nil)
						if err != nil {
							t.Fatal(err)
						}
						other := f
						other.sk = outsider
						ancestor := other.record(t, 3, head.VersionRecordHash, "outsider", nil)
						p = f.record(t, 4, ancestor.VersionRecordHash, "successor", nil)
						status = "authentication"
					case "fork":
						p = f.record(t, 3, first.VersionRecordHash, "fork", nil)
					case "rollback":
						p = first
					case "same version":
						p = f.record(t, 2, first.VersionRecordHash, "replacement", nil)
					case "duplicate replay":
						status = ""
					case "malformed ancestor":
						hash, err := f.n.Store([]byte(`{"tag":"version-record","version":"invalid"}`))
						if err != nil {
							t.Fatal(err)
						}
						p = f.record(t, 3, hash, "malformed", nil)
						status = "malformed"
					case "missing ancestor":
						p = f.record(t, 3, strings.Repeat("a", 64), "missing", nil)
						status = "missing_data"
					case "cross-package ancestor":
						a := f.record(t, 3, head.VersionRecordHash, "cross-package", func(r *VersionRecord) { r.Package = "other" })
						p = f.record(t, 4, a.VersionRecordHash, "successor", nil)
					case "decreasing ancestry":
						a := f.record(t, 2, head.VersionRecordHash, "equal", nil)
						p = f.record(t, 4, a.VersionRecordHash, "successor", nil)
					case "cyclic storage":
						// Hash-consistent cycles require SHA-256 fixed points. Model
						// cyclic malicious storage without bypassing the hash gate.
						a, b := strings.Repeat("b", 64), strings.Repeat("c", 64)
						for hash, prev := range map[string]string{a: b, b: a} {
							r := VersionRecord{Tag: VersionRecordTag, Domain: head.Domain, Package: head.Package, Version: 3, BlobHash: hashValue([]byte("cycle")), PreviousVersionRecord: prev}
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
						p = f.record(t, 4, a, "cyclic successor", nil)
						status = "authentication"
					}
					before := make([]map[string][]byte, len(nodes))
					updates := make([]int, len(nodes))
					for i, n := range nodes {
						before[i] = n.DataSnapshot()
						updates[i] = wire.count(f.n.me().Address, n.me().Address, "UPDATE_LATEST")
					}
					results, err := f.n.PublishLatestForced(p)
					if (err == nil) != (status == "") || len(results) != 3 {
						t.Fatalf("forced publication: %v %v", results, err)
					}
					for i, n := range nodes {
						if status == "" {
							if results[n.me().Address] != nil {
								t.Fatal(results[n.me().Address])
							}
						} else {
							var failure *LatestError
							if !errors.As(results[n.me().Address], &failure) || failure.Status != status {
								t.Fatalf("receiver %d: want %s, got %v", i, status, results[n.me().Address])
							}
						}
						if !reflect.DeepEqual(before[i], n.DataSnapshot()) {
							t.Fatalf("receiver %d changed state on rejection/replay", i)
						}
						if i > 0 && wire.count(f.n.me().Address, n.me().Address, "UPDATE_LATEST") != updates[i]+1 {
							t.Fatalf("receiver %d was never sent update", i)
						}
					}
				})
			}
		})
	}
}

func TestRegistryPartitionCatchupUsesRemoteHistoryAndBlobs(t *testing.T) {
	for _, kind := range []string{"simulated", "udp"} {
		t.Run(kind, func(t *testing.T) {
			f, nodes, wire := registryE2ENodes(t, kind)
			first := publishE2E(t, f, 1, metadata.ZeroHash, "original release")
			stale := nodes[2]
			before := stale.DataSnapshot()
			wire.mu.Lock()
			wire.blocked[stale.me().Address] = true
			wire.mu.Unlock()
			head := first
			for _, version := range []uint64{4, 9, 15} {
				payload := fmt.Sprintf("release %d", version)
				if _, err := f.n.Store([]byte(payload)); err != nil {
					t.Fatal(err)
				}
				p := f.record(t, version, head.VersionRecordHash, payload, nil)
				results, err := f.n.PublishLatest(p)
				if err != nil || results[nodes[1].me().Address] != nil {
					t.Fatalf("live replicas failed publication: %v %v", results, err)
				}
				head = p
			}
			if !reflect.DeepEqual(before, stale.DataSnapshot()) {
				t.Fatal("partitioned node received missed publications")
			}
			wire.mu.Lock()
			wire.blocked[stale.me().Address] = false
			wire.mu.Unlock()
			reads := wire.count(stale.me().Address, f.n.me().Address, "FIND_VALUE")
			got, err := stale.LookupLatest(head.Domain, head.Package)
			if err != nil || got == nil || *got != head {
				t.Fatalf("catch-up: %v %v", got, err)
			}
			for _, n := range nodes {
				assertLatestHead(t, n, head)
			}
			if wire.count(stale.me().Address, f.n.me().Address, "FIND_VALUE") <= reads {
				t.Fatal("catch-up did not fetch remote predecessors")
			}
			for _, p := range []LatestPointer{first, head} {
				history, err := metadata.TraverseHistory(p.VersionRecordHash, p.Domain, p.Package, metadata.DefaultMaxHistory, stale.LookupData, stale.VerifyOwnedVersionRecord)
				if err != nil {
					t.Fatal(err)
				}
				blob, source, err := stale.LookupDataFrom(history[0].Record.BlobHash)
				want := "release 15"
				if p == first {
					want = "original release"
				}
				if err != nil || string(blob) != want {
					t.Fatalf("authenticated blob: %q %v", blob, err)
				}
				if p == head && source.ID.Equals(stale.me().ID) {
					t.Fatal("latest blob was not retrieved remotely")
				}
			}
		})
	}
}
