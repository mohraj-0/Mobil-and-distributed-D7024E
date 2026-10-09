package kademlia

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func sizedValue(size int) []byte {
	value := make([]byte, size)
	for i := range value {
		value[i] = byte(i)
	}
	return value
}

func TestStoreValueSizeBoundaries(t *testing.T) {
	for _, size := range []int{0, 1, 255, 256, 1024, MaxValueSize - 1, MaxValueSize, MaxValueSize + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			value := sizedValue(size)
			sends := 0
			node := newTestNode("ff", "node-a", &callbackNode{send: func(string, []byte) error { sends++; return nil }})
			key, err := node.Store(value)
			if size == 0 || size > MaxValueSize {
				if err == nil || key != "" || len(node.DataStore) != 0 || sends != 0 {
					t.Fatalf("unsupported size %d: key=%q error=%v stored=%d sends=%d", size, key, err, len(node.DataStore), sends)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := node.LookupData(key)
			if err != nil || !bytes.Equal(got, value) {
				t.Fatalf("%d-byte round trip failed: %v", size, err)
			}
			t.Logf("stored and retrieved %d raw bytes", size)
		})
	}
}

func TestIncomingStoreValueSizeBoundaries(t *testing.T) {
	for _, size := range []int{0, 1, MaxValueSize, MaxValueSize + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			value := sizedValue(size)
			sum := sha256.Sum256(value)
			key := KademliaID(sum)
			var reply rpcMessage
			node := newTestNode("ff", "node-a", &callbackNode{send: func(_ string, data []byte) error { return json.Unmarshal(data, &reply) }})
			node.handleStoreRPC(rpcMessage{Type: rpcStore, RequestID: "size-test", SenderID: testID("11").String(), SenderAddress: "node-b", Key: key.String(), Value: value})
			want := size > 0 && size <= MaxValueSize
			_, stored := node.DataStore[key.String()]
			if reply.Type != rpcStoreReply || reply.Stored != want || stored != want {
				t.Fatalf("size=%d: reply=%+v stored=%t, want %t", size, reply, stored, want)
			}
		})
	}
}

func TestValueSizeRemoteRoundTrip(t *testing.T) {
	for _, kind := range []string{"simulated", "udp"} {
		for _, size := range []int{1, 255, 256, 1024, MaxValueSize - 1, MaxValueSize} {
			t.Run(fmt.Sprintf("%s/%d", kind, size), func(t *testing.T) {
				value := sizedValue(size)
				sum := sha256.Sum256(value)
				remoteID := KademliaID(sum) // Ensure the remote peer is the closest replica.
				localID := remoteID
				for i := range localID {
					localID[i] ^= 0xff
				}
				var transports [2]Node
				addresses := [2]string{"127.0.0.1:18000", "127.0.0.1:18001"}
				if kind == "simulated" {
					network := NewSimulatedNetwork()
					transports = [2]Node{NewSimulatedNodeWithNetwork(network), NewSimulatedNodeWithNetwork(network)}
				} else {
					transports = [2]Node{NewUDPNode(), NewUDPNode()}
					addresses = [2]string{"127.0.0.1:0", "127.0.0.1:0"}
				}
				for i, transport := range transports {
					if err := transport.Listen(addresses[i]); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = transport.Close() })
					if udp, ok := transport.(*UDPNode); ok {
						addresses[i] = udp.address
					}
				}
				localContact, remoteContact := NewContact(&localID, addresses[0]), NewContact(&remoteID, addresses[1])
				local := &Kademlia{RoutingTable: NewRoutingTable(localContact), Network: transports[0], K: 1, RPCTimeout: 2 * time.Second}
				remote := &Kademlia{RoutingTable: NewRoutingTable(remoteContact), Network: transports[1], K: 1, RPCTimeout: 2 * time.Second}
				local.RoutingTable.AddContact(remoteContact)
				remote.RoutingTable.AddContact(localContact)
				go local.ListenForRPC()
				go remote.ListenForRPC()
				key, err := local.Store(value)
				if err != nil {
					t.Fatal(err)
				}
				if _, exists := local.DataStore[key]; exists {
					t.Fatal("test must retrieve remotely, not use a local replica")
				}
				got, err := local.LookupData(key)
				if err != nil || !bytes.Equal(got, value) {
					t.Fatalf("remote %d-byte round trip failed: %v", size, err)
				}
				t.Logf("%s STORE and FIND_VALUE round trip: %d raw bytes", kind, size)
			})
		}
	}
}

func TestLookupRejectsOversizedRemoteValue(t *testing.T) {
	value := sizedValue(MaxValueSize + 1)
	sum := sha256.Sum256(value)
	key := KademliaID(sum)
	remote := NewContact(testID("11"), "node-b")
	network := &callbackNode{}
	node := newTestNode("ff", "node-a", network)
	node.RoutingTable.AddContact(remote)
	network.send = func(_ string, data []byte) error {
		var request rpcMessage
		if err := json.Unmarshal(data, &request); err != nil {
			return err
		}
		node.handleFindValueReplyRPC(rpcMessage{Type: rpcFindValueReply, RequestID: request.RequestID, SenderID: remote.ID.String(), SenderAddress: remote.Address, Found: true, Value: value})
		return nil
	}
	if got, err := node.LookupData(key.String()); err == nil || len(got) != 0 {
		t.Fatalf("oversized remote value accepted: bytes=%d err=%v", len(got), err)
	}
}

func TestUnsupportedValuesAreNotSentOrServed(t *testing.T) {
	for _, size := range []int{0, MaxValueSize + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			value := sizedValue(size)
			sum := sha256.Sum256(value)
			key := KademliaID(sum)
			sends := 0
			var reply rpcMessage
			network := &callbackNode{send: func(_ string, data []byte) error {
				sends++
				return json.Unmarshal(data, &reply)
			}}
			node := newTestNode("ff", "node-a", network)
			remote := NewContact(testID("11"), "node-b")
			if err := node.sendStoreRPC(remote, &key, value); err == nil || sends != 0 {
				t.Fatalf("invalid outbound STORE: err=%v sends=%d", err, sends)
			}
			// Direct map writes bypass Store; they still must not expose unsupported values.
			node.DataStore = map[string][]byte{key.String(): value}
			if got, err := node.LookupData(key.String()); err == nil || len(got) != 0 {
				t.Fatalf("unsupported local value returned: bytes=%d err=%v", len(got), err)
			}
			node.handleFindValueRPC(rpcMessage{Type: rpcFindValue, RequestID: "invalid-local", SenderAddress: "node-b", Key: key.String()})
			if sends != 1 || reply.Type != rpcFindValueReply || reply.Found || len(reply.Value) != 0 {
				t.Fatalf("unsupported value served: sends=%d reply=%+v", sends, reply)
			}
		})
	}
}
