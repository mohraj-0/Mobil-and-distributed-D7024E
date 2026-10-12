package kademlia

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestRoutingTreeAllBranchParameters(t *testing.T) {
	for _, b := range []int{1, 2, 3, 8, 64, 255, 256} {
		t.Run(fmt.Sprint(b), func(t *testing.T) {
			me := NewContact(testID("ff"), "me")
			rt := NewRoutingTableWithBranchBits(me, b)
			rt.SetCapacity(2)
			for i := 0; i < 12; i++ {
				rt.AddContact(NewContact(testRoutingTreeID(byte(i*16), byte(i+1)), fmt.Sprint(i)))
			}
			if rt.BranchBits() != b {
				t.Fatal("branch parameter changed")
			}
			var walk func(*routingTreeNode)
			walk = func(n *routingTreeNode) {
				if n.isLeaf() {
					if n.bucket.Len() > 2 {
						t.Fatal("bucket exceeds k")
					}
					return
				}
				if len(n.children) != 2 || n.children[0].depth != n.depth+1 {
					t.Fatal("splits must remain binary")
				}
				if !n.containsMe && n.depth%b == 0 {
					t.Fatalf("invalid non-local split at depth %d for b=%d", n.depth, b)
				}
				for _, child := range n.children {
					walk(child)
				}
			}
			walk(rt.root)
		})
	}
}

func TestFullBucketProbesOldestAndPreservesLiveContact(t *testing.T) {
	for _, alive := range []bool{false, true} {
		t.Run(fmt.Sprint(alive), func(t *testing.T) {
			transport := &callbackNode{}
			n := newTestNode("ff", "me", transport)
			n.K = 1
			n.RPCRetries = 1
			n.RPCTimeout = time.Millisecond
			old := NewContact(testID("00"), "old")
			fresh := NewContact(testID("10"), "fresh")
			n.addContact(old)
			probed := make(chan string, 1)
			transport.send = func(address string, data []byte) error {
				var r rpcMessage
				if err := json.Unmarshal(data, &r); err != nil {
					return err
				}
				if r.Type != "PING" {
					return fmt.Errorf("expected PING")
				}
				probed <- address
				if alive {
					n.handlePingReply(rpcMessage{RequestID: r.RequestID, SenderID: old.ID.String(), SenderAddress: old.Address})
				}
				return nil
			}
			n.addContact(fresh)
			select {
			case address := <-probed:
				if address != "old" {
					t.Fatal(address)
				}
			case <-time.After(time.Second):
				t.Fatal("no liveness probe")
			}
			deadline := time.Now().Add(time.Second)
			for {
				n.evictionMu.Lock()
				pending := len(n.evictions)
				n.evictionMu.Unlock()
				if pending == 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("probe did not finish")
				}
				time.Sleep(time.Millisecond)
			}
			contacts := n.RoutingTable.FindClosestContacts(old.ID, 2)
			want := fresh
			if alive {
				want = old
			}
			if len(contacts) != 1 || !contacts[0].ID.Equals(want.ID) {
				t.Fatalf("contacts=%v want=%v", contacts, want)
			}
		})
	}
}

func TestLookupExcludesDeadContactAndUsesFallback(t *testing.T) {
	transport := &callbackNode{}
	n := newTestNode("ff", "me", transport)
	n.K = 1
	n.RPCRetries = 1
	dead := NewContact(testID("00"), "dead")
	live := NewContact(testID("80"), "live")
	n.RoutingTable.SetCapacity(1)
	n.RoutingTable.AddContact(dead)
	n.RoutingTable.AddContact(live)
	transport.send = func(address string, data []byte) error {
		if address == "dead" {
			return fmt.Errorf("offline")
		}
		var r rpcMessage
		_ = json.Unmarshal(data, &r)
		n.handleFindNodeReplyRPC(rpcMessage{RequestID: r.RequestID, SenderID: live.ID.String(), SenderAddress: live.Address})
		return nil
	}
	got := n.LookupContact(&dead)
	if len(got) != 1 || !got[0].ID.Equals(live.ID) {
		t.Fatalf("got %v, want responsive fallback", got)
	}
}

func TestPeriodicReplicationRepairsReplicaAfterChurn(t *testing.T) {
	network := NewSimulatedNetwork()
	var nodes []*Kademlia
	var listeners sync.WaitGroup
	defer func() {
		for _, n := range nodes {
			_ = n.Network.Close()
		}
		for _, n := range nodes {
			n.StopMaintenance()
		}
		listeners.Wait()
	}()
	for i := 0; i < 3; i++ {
		address := fmt.Sprintf("127.0.0.1:%d", 24000+i)
		transport := NewSimulatedNodeWithNetwork(network)
		if err := transport.Listen(address); err != nil {
			t.Fatal(err)
		}
		n := &Kademlia{RoutingTable: NewRoutingTable(NewContact(testRoutingTreeID(byte(i*64), 0), address)), Network: transport, K: 2, RPCRetries: 1, RPCTimeout: 10 * time.Millisecond}
		nodes = append(nodes, n)
	}
	for _, n := range nodes {
		for _, peer := range nodes {
			n.addContact(peer.me())
		}
		listeners.Add(1)
		go func(n *Kademlia) { defer listeners.Done(); n.ListenForRPC() }(n)
	}
	value := []byte("replica repair after churn")
	key, err := nodes[0].Store(value)
	if err != nil {
		t.Fatal(err)
	}
	var survivor, departing, empty *Kademlia
	for _, n := range nodes {
		if _, ok := n.DataSnapshot()[key]; ok {
			if survivor == nil {
				survivor = n
			} else {
				departing = n
			}
		} else {
			empty = n
		}
	}
	if survivor == nil || departing == nil || empty == nil {
		t.Fatal("expected two replicas and one non-replica")
	}
	_ = departing.Network.Close()
	survivor.ReplicationPeriod = 5 * time.Millisecond
	survivor.RefreshPeriod = time.Hour
	survivor.StartMaintenance()
	deadline := time.Now().Add(2 * time.Second)
	for !bytes.Equal(empty.DataSnapshot()[key], value) {
		if time.Now().After(deadline) {
			t.Fatal("periodic replication failed to restore second replica")
		}
		time.Sleep(time.Millisecond)
	}
	got, source, err := empty.LookupDataFrom(key)
	if err != nil || !bytes.Equal(got, value) || source.Address != empty.me().Address {
		t.Fatalf("lookup source=%v error=%v", source, err)
	}
}

func TestUnsolicitedReplyDoesNotPoisonRoutingTable(t *testing.T) {
	n := newTestNode("ff", "me", &callbackNode{})
	n.handleFindNodeReplyRPC(rpcMessage{RequestID: "guessed", SenderID: testID("00").String(), SenderAddress: "attacker"})
	if len(n.RoutingTable.Snapshot()) != 0 {
		t.Fatal("unsolicited reply altered routing table")
	}
}

func TestRefreshTargetsStayInsideEveryLeaf(t *testing.T) {
	rt := NewRoutingTableWithBranchBits(NewContact(testID("ff"), "me"), 3)
	rt.SetCapacity(1)
	for i := 0; i < 9; i++ {
		rt.AddContact(NewContact(testRoutingTreeID(byte(i*24), byte(i)), fmt.Sprint(i)))
	}
	targets := rt.RefreshTargets(0)
	prefixes := collectLeafValues(rt, func(_ int, n *routingTreeNode, p KademliaID) (struct {
		depth  int
		prefix KademliaID
	}, bool) { return struct {
		depth  int
		prefix KademliaID
	}{n.depth, p}, true })
	if len(targets) != len(prefixes) {
		t.Fatal("refresh skipped leaves")
	}
	for i, target := range targets {
		for bit := 0; bit < prefixes[i].depth; bit++ {
			if bitAt(target, bit) != bitAt(&prefixes[i].prefix, bit) {
				t.Fatal("refresh target outside leaf")
			}
		}
	}
	if len(rt.RefreshTargets(time.Hour)) != 0 {
		t.Fatal("fresh buckets should not be refreshed")
	}
}

func TestValueSourceIsRemotePeer(t *testing.T) {
	transport := &callbackNode{}
	n := newTestNode("ff", "me", transport)
	peer := NewContact(testID("00"), "peer")
	n.addContact(peer)
	value := []byte("remote value")
	sum := KademliaID(sha256.Sum256(value))
	transport.send = func(_ string, data []byte) error {
		var r rpcMessage
		_ = json.Unmarshal(data, &r)
		n.handleFindValueReplyRPC(rpcMessage{RequestID: r.RequestID, SenderID: peer.ID.String(), SenderAddress: peer.Address, Found: true, Value: value})
		return nil
	}
	got, source, err := n.LookupDataFrom(sum.String())
	if err != nil || !bytes.Equal(got, value) || source.Address != peer.Address {
		t.Fatalf("source=%v error=%v", source, err)
	}
}
