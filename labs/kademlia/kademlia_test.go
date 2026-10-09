package kademlia

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type callbackNode struct {
	send           func(address string, data []byte) error
	receiveMessage Message
	receiveErr     error
	receivedOnce   bool
}

func testNode(
	idPrefix string,
	address string,
	network Node,
) *Kademlia {
	me := NewContact(
		testID(idPrefix),
		address,
	)

	return &Kademlia{
		RoutingTable: NewRoutingTable(me),
		Network:      network,
		Alpha:        3,
		K:            10,
		RPCTimeout:   50 * time.Millisecond,
	}
}

func (n *callbackNode) Listen(address string) error { return nil }
func (n *callbackNode) Close() error                { return nil }
func (n *callbackNode) Receive() (Message, error) {
	if !n.receivedOnce {
		n.receivedOnce = true
		if n.receiveErr == nil {
			return n.receiveMessage, nil
		}
	}
	if n.receiveErr != nil {
		return Message{}, n.receiveErr
	}
	return Message{}, errors.New("stop")
}
func (n *callbackNode) SendData(address string, data []byte) error {
	if n.send != nil {
		return n.send(address, data)
	}
	return nil
}

func testID(prefix string) *KademliaID {
	return NewKademliaID(prefix + "00000000000000000000000000000000000000000000000000000000000000")
}

func newTestNode(prefix, address string, network Node) *Kademlia {
	me := NewContact(testID(prefix), address)
	return &Kademlia{
		RoutingTable: NewRoutingTable(me),
		Network:      network,
		Alpha:        3,
		K:            10,
		RPCTimeout:   50 * time.Millisecond,
	}
}

func TestDefaultsAndRequestID(t *testing.T) {
	node := &Kademlia{}
	if node.alpha() != 3 {
		t.Fatalf("default alpha = %d, want 3", node.alpha())
	}
	if node.kValue() != 10 {
		t.Fatalf("default k = %d, want 10", node.kValue())
	}
	if node.rpcTimeout() != 2*time.Second {
		t.Fatalf("default timeout = %v, want 2s", node.rpcTimeout())
	}

	node.Alpha = 5
	node.K = 7
	node.RPCTimeout = 25 * time.Millisecond
	if node.alpha() != 5 || node.kValue() != 7 || node.rpcTimeout() != 25*time.Millisecond {
		t.Fatal("custom parameters were not returned")
	}

	first, err := randomRequestID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := randomRequestID()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 64 {
		t.Fatalf("request ID length = %d, want 64", len(first))
	}
	if first == second {
		t.Fatal("request IDs should be different")
	}
}

func TestEnsureMapsAndDataStore(t *testing.T) {
	node := &Kademlia{}
	node.ensureFindNodeResponses()
	node.ensureFindValueResponses()
	node.ensureStoreResponses()
	node.ensureDataStore()

	if node.findNodeResponses == nil || node.findValueResponses == nil || node.storeResponses == nil || node.DataStore == nil {
		t.Fatal("one or more maps were not initialized")
	}
}

func TestRoutingHelpersAndWireConversion(t *testing.T) {
	me := NewContact(testID("ff"), "node-me")
	node := &Kademlia{RoutingTable: NewRoutingTable(me)}
	contact := NewContact(testID("11"), "node-11")
	node.addContact(contact)

	closest := node.closestContacts(contact.ID, 1)
	if len(closest) != 1 || !closest[0].ID.Equals(contact.ID) {
		t.Fatal("closestContacts returned wrong result")
	}

	invalid := Contact{ID: nil, Address: "invalid"}
	wire := contactsToWire([]Contact{contact, invalid})
	if len(wire) != 1 {
		t.Fatalf("contactsToWire returned %d contacts, want 1", len(wire))
	}

	back := wireToContacts([]wireContact{
		wire[0],
		{ID: "invalid", Address: "bad"},
		{ID: contact.ID.String(), Address: ""},
	})
	if len(back) != 1 || !back[0].ID.Equals(contact.ID) {
		t.Fatal("wireToContacts returned wrong result")
	}

	if node.me().Address != "node-me" {
		t.Fatal("me() returned wrong contact")
	}
}

func TestLookupContactValidation(t *testing.T) {
	target := NewContact(testID("11"), "target")
	if contacts := (&Kademlia{}).LookupContact(&target); contacts != nil {
		t.Fatal("LookupContact without routing table should return nil")
	}

	me := NewContact(testID("ff"), "me")
	node := &Kademlia{RoutingTable: NewRoutingTable(me)}
	if contacts := node.LookupContact(nil); contacts != nil {
		t.Fatal("nil target should return nil")
	}
	if contacts := node.LookupContact(&Contact{}); contacts != nil {
		t.Fatal("nil target ID should return nil")
	}
}

func TestHandleFindNodeRPC(t *testing.T) {
	var sent rpcMessage
	network := &callbackNode{}
	node := newTestNode("ff", "node-a", network)
	known := NewContact(testID("22"), "node-known")
	node.RoutingTable.AddContact(known)

	network.send = func(address string, data []byte) error {
		if address != "node-b" {
			t.Fatalf("reply sent to %q, want node-b", address)
		}
		return json.Unmarshal(data, &sent)
	}

	node.handleFindNodeRPC(rpcMessage{
		Type: rpcFindNode, RequestID: "request-1",
		SenderID: testID("11").String(), SenderAddress: "node-b",
		TargetID: known.ID.String(),
	})

	if sent.Type != rpcFindNodeReply || sent.RequestID != "request-1" || len(sent.Contacts) == 0 {
		t.Fatal("invalid FIND_NODE reply")
	}
}

func TestHandleFindNodeReplyRPC(t *testing.T) {
	node := newTestNode("ff", "node-a", &callbackNode{})
	ch := make(chan []Contact, 1)
	node.findNodeResponses = map[string]chan []Contact{"request-1": ch}
	contact := NewContact(testID("22"), "node-c")

	node.handleFindNodeReplyRPC(rpcMessage{
		Type: rpcFindNodeReply, RequestID: "request-1",
		SenderID: testID("11").String(), SenderAddress: "node-b",
		Contacts: contactsToWire([]Contact{contact}),
	})

	select {
	case contacts := <-ch:
		if len(contacts) != 1 || !contacts[0].ID.Equals(contact.ID) {
			t.Fatal("wrong contacts")
		}
	default:
		t.Fatal("reply was not delivered")
	}

	node.handleFindNodeReplyRPC(rpcMessage{Type: rpcFindNodeReply, RequestID: "unknown"})
}

func TestSendFindNodeRPCSuccessAndTimeout(t *testing.T) {
	remote := NewContact(testID("11"), "node-b")
	target := testID("00")
	found := NewContact(testID("01"), "node-c")

	var node *Kademlia
	network := &callbackNode{}
	node = newTestNode("ff", "node-a", network)

	network.send = func(address string, data []byte) error {
		var request rpcMessage
		if err := json.Unmarshal(data, &request); err != nil {
			return err
		}
		node.handleFindNodeReplyRPC(rpcMessage{
			Type: rpcFindNodeReply, RequestID: request.RequestID,
			SenderID: remote.ID.String(), SenderAddress: remote.Address,
			Contacts: contactsToWire([]Contact{found}),
		})
		return nil
	}

	contacts, err := node.sendFindNodeRPC(remote, target)
	if err != nil {
		t.Fatalf("sendFindNodeRPC failed: %v", err)
	}
	if len(contacts) != 1 || !contacts[0].ID.Equals(found.ID) {
		t.Fatal("wrong FIND_NODE result")
	}

	network.send = func(address string, data []byte) error { return nil }
	node.RPCTimeout = time.Millisecond
	if _, err := node.sendFindNodeRPC(remote, target); err == nil {
		t.Fatal("expected timeout")
	}
}

func TestFindValueRPCAndLookupData(t *testing.T) {
	value := []byte("hello-kademlia")
	sum := sha256.Sum256(value)
	key := KademliaID(sum)
	remote := NewContact(testID("11"), "node-b")

	var node *Kademlia
	network := &callbackNode{}
	node = newTestNode("ff", "node-a", network)
	node.RoutingTable.AddContact(remote)

	network.send = func(address string, data []byte) error {
		var request rpcMessage
		if err := json.Unmarshal(data, &request); err != nil {
			return err
		}
		switch request.Type {
		case rpcFindValue:
			node.handleFindValueReplyRPC(rpcMessage{
				Type: rpcFindValueReply, RequestID: request.RequestID,
				SenderID: remote.ID.String(), SenderAddress: remote.Address,
				Key: request.Key, Found: true, Value: value,
			})
		case rpcFindNode:
			node.handleFindNodeReplyRPC(rpcMessage{
				Type: rpcFindNodeReply, RequestID: request.RequestID,
				SenderID: remote.ID.String(), SenderAddress: remote.Address,
			})
		}
		return nil
	}

	got, err := node.LookupData(key.String())
	if err != nil {
		t.Fatalf("LookupData failed: %v", err)
	}
	if string(got) != string(value) {
		t.Fatal("LookupData returned wrong value")
	}

	node.DataStore = map[string][]byte{key.String(): append([]byte(nil), value...)}
	got, err = node.LookupData(key.String())
	if err != nil || string(got) != string(value) {
		t.Fatal("local LookupData failed")
	}
}

func TestHandleFindValueRPCFoundAndNotFound(t *testing.T) {
	value := []byte("stored-value")
	sum := sha256.Sum256(value)
	key := KademliaID(sum)
	var replies []rpcMessage

	network := &callbackNode{}
	node := newTestNode("ff", "node-a", network)
	node.DataStore = map[string][]byte{key.String(): value}
	network.send = func(address string, data []byte) error {
		var reply rpcMessage
		if err := json.Unmarshal(data, &reply); err != nil {
			return err
		}
		replies = append(replies, reply)
		return nil
	}

	req := rpcMessage{
		Type: rpcFindValue, RequestID: "fv-1",
		SenderID: testID("11").String(), SenderAddress: "node-b", Key: key.String(),
	}
	node.handleFindValueRPC(req)
	if len(replies) != 1 || !replies[0].Found || string(replies[0].Value) != string(value) {
		t.Fatal("existing value was not returned")
	}

	req.RequestID = "fv-2"
	req.Key = testID("33").String()
	node.handleFindValueRPC(req)
	if len(replies) != 2 || replies[1].Found {
		t.Fatal("missing value should return Found=false")
	}
}

func TestStoreLocalAndHandleStoreRPC(t *testing.T) {
	localNetwork := &callbackNode{}
	node := testNode("ff", "node-a", localNetwork)

	// Inga andra kontakter => Store lagrar lokalt.
	value := []byte("local-data")

	key, err := node.Store(value)
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	if string(node.DataStore[key]) != string(value) {
		t.Fatal("Store did not save the local value")
	}

	// Testa inkommande STORE med korrekt hash.
	remoteValue := []byte("remote-data")

	sum := sha256.Sum256(remoteValue)
	remoteKey := KademliaID(sum)

	var reply rpcMessage

	localNetwork.send = func(address string, data []byte) error {
		return json.Unmarshal(data, &reply)
	}

	node.handleStoreRPC(rpcMessage{
		Type:          rpcStore,
		RequestID:     "store-1",
		SenderID:      testID("11").String(),
		SenderAddress: "node-b",
		Key:           remoteKey.String(),
		Value:         remoteValue,
	})

	if !reply.Stored {
		t.Fatal("valid STORE should be accepted")
	}

	if string(node.DataStore[remoteKey.String()]) != string(remoteValue) {
		t.Fatal("valid STORE value was not saved")
	}

	// Nollställ svaret innan nästa test.
	reply = rpcMessage{}

	// Testa STORE med fel hash.
	node.handleStoreRPC(rpcMessage{
		Type:          rpcStore,
		RequestID:     "store-2",
		SenderID:      testID("11").String(),
		SenderAddress: "node-b",
		Key:           testID("44").String(),
		Value:         []byte("wrong-hash"),
	})

	if reply.Stored {
		t.Fatal("STORE with wrong hash should be rejected")
	}
}

func TestStoreAcceptsValuesLargerThan255Bytes(t *testing.T) {
	node := testNode("ff", "node-a", &callbackNode{})
	value := bytes.Repeat([]byte("x"), 1024)
	sum := sha256.Sum256(value)
	wantID := KademliaID(sum)
	wantKey := wantID.String()

	key, err := node.Store(value)
	if err != nil {
		t.Fatalf("Store failed for 1024-byte value: %v", err)
	}
	if key != wantKey {
		t.Fatalf("Store key = %s, want %s", key, wantKey)
	}
	if got := node.DataStore[key]; !bytes.Equal(got, value) {
		t.Fatalf("stored value length = %d, want %d", len(got), len(value))
	}
}

func TestSendStoreRPCSuccessRejectedAndTimeout(t *testing.T) {
	value := []byte("store-value")
	sum := sha256.Sum256(value)
	key := KademliaID(sum)
	remote := NewContact(testID("11"), "node-b")

	var node *Kademlia
	network := &callbackNode{}
	node = newTestNode("ff", "node-a", network)

	network.send = func(address string, data []byte) error {
		var request rpcMessage
		if err := json.Unmarshal(data, &request); err != nil {
			return err
		}
		node.handleStoreReplyRPC(rpcMessage{
			Type: rpcStoreReply, RequestID: request.RequestID,
			SenderID: remote.ID.String(), SenderAddress: remote.Address, Stored: true,
		})
		return nil
	}
	if err := node.sendStoreRPC(remote, &key, value); err != nil {
		t.Fatalf("sendStoreRPC failed: %v", err)
	}

	network.send = func(address string, data []byte) error {
		var request rpcMessage
		if err := json.Unmarshal(data, &request); err != nil {
			return err
		}
		node.handleStoreReplyRPC(rpcMessage{
			Type: rpcStoreReply, RequestID: request.RequestID,
			SenderID: remote.ID.String(), SenderAddress: remote.Address, Stored: false,
		})
		return nil
	}
	if err := node.sendStoreRPC(remote, &key, value); err == nil {
		t.Fatal("rejected STORE should fail")
	}

	network.send = func(address string, data []byte) error { return nil }
	node.RPCTimeout = time.Millisecond
	if err := node.sendStoreRPC(remote, &key, value); err == nil {
		t.Fatal("STORE should time out")
	}
}

func TestLookupDataValidationAndNotFound(t *testing.T) {
	if _, err := (&Kademlia{}).LookupData(""); err == nil {
		t.Fatal("missing routing table should fail")
	}

	me := NewContact(testID("ff"), "node-a")
	node := &Kademlia{RoutingTable: NewRoutingTable(me)}
	if _, err := node.LookupData(testID("11").String()); err == nil {
		t.Fatal("missing network should fail")
	}

	node.Network = &callbackNode{}
	if _, err := node.LookupData("invalid"); err == nil {
		t.Fatal("invalid hash should fail")
	}
	if _, err := node.LookupData(testID("22").String()); err == nil {
		t.Fatal("missing value should return error")
	}
}

func TestLookupDataRejectsCorruptedRemoteValue(t *testing.T) {
	goodValue := []byte("expected-value")
	badValue := []byte("corrupted-value")
	sum := sha256.Sum256(goodValue)
	key := KademliaID(sum)
	remote := NewContact(testID("11"), "node-b")

	var node *Kademlia
	network := &callbackNode{}
	node = newTestNode("ff", "node-a", network)
	node.RoutingTable.AddContact(remote)
	var replies atomic.Int32

	network.send = func(address string, data []byte) error {
		var request rpcMessage
		if err := json.Unmarshal(data, &request); err != nil {
			return err
		}
		switch request.Type {
		case rpcFindValue:
			replies.Add(1)
			node.handleFindValueReplyRPC(rpcMessage{
				Type: rpcFindValueReply, RequestID: request.RequestID,
				SenderID: remote.ID.String(), SenderAddress: remote.Address,
				Key: request.Key, Found: true, Value: badValue,
			})
		case rpcFindNode:
			node.handleFindNodeReplyRPC(rpcMessage{
				Type: rpcFindNodeReply, RequestID: request.RequestID,
				SenderID: remote.ID.String(), SenderAddress: remote.Address,
			})
		}
		return nil
	}

	got, err := node.LookupData(key.String())
	if err == nil || len(got) != 0 {
		t.Fatalf("LookupData = %q, %v; want no value and an error", got, err)
	}
	if replies.Load() != 1 {
		t.Fatalf("corrupted replies sent = %d, want 1; lookup error: %v", replies.Load(), err)
	}
	if err.Error() != "value not found" {
		t.Fatalf("LookupData error = %v, want value not found after rejecting corruption", err)
	}
	if _, cached := node.DataStore[key.String()]; cached {
		t.Fatal("LookupData cached the corrupted value")
	}
}

func TestRPCRepliesMustMatchPendingRequestID(t *testing.T) {
	node := newTestNode("ff", "node-a", &callbackNode{})

	findNodeCh := make(chan []Contact, 1)
	node.findNodeResponses = map[string]chan []Contact{"find-node-ok": findNodeCh}
	node.handleFindNodeReplyRPC(rpcMessage{Type: rpcFindNodeReply, RequestID: "find-node-wrong"})
	assertNoReceive(t, findNodeCh, "FIND_NODE reply with wrong request ID")
	node.handleFindNodeReplyRPC(rpcMessage{Type: rpcFindNodeReply, RequestID: "find-node-ok"})
	assertReceive(t, findNodeCh, "FIND_NODE reply with matching request ID")

	findValueCh := make(chan findValueResult, 1)
	node.findValueResponses = map[string]chan findValueResult{"find-value-ok": findValueCh}
	node.handleFindValueReplyRPC(rpcMessage{Type: rpcFindValueReply, RequestID: "find-value-wrong", Found: true})
	assertNoReceive(t, findValueCh, "FIND_VALUE reply with wrong request ID")
	node.handleFindValueReplyRPC(rpcMessage{Type: rpcFindValueReply, RequestID: "find-value-ok", Found: true})
	assertReceive(t, findValueCh, "FIND_VALUE reply with matching request ID")

	storeCh := make(chan bool, 1)
	node.storeResponses = map[string]chan bool{"store-ok": storeCh}
	node.handleStoreReplyRPC(rpcMessage{Type: rpcStoreReply, RequestID: "store-wrong", Stored: true})
	assertNoReceive(t, storeCh, "STORE reply with wrong request ID")
	node.handleStoreReplyRPC(rpcMessage{Type: rpcStoreReply, RequestID: "store-ok", Stored: true})
	assertReceive(t, storeCh, "STORE reply with matching request ID")
}

func TestStoreValidation(t *testing.T) {
	node := &Kademlia{}
	if _, err := node.Store([]byte{}); err == nil {
		t.Fatal("empty Store should fail")
	}
	if _, err := node.Store([]byte("x")); err == nil {
		t.Fatal("missing routing table should fail")
	}

	me := NewContact(testID("ff"), "node-a")
	node.RoutingTable = NewRoutingTable(me)
	if _, err := node.Store([]byte("x")); err == nil {
		t.Fatal("missing network should fail")
	}
}

func TestRefreshAndJoinValidation(t *testing.T) {
	node := &Kademlia{}
	node.Refresh(0)

	bootstrap := NewContact(testID("11"), "node-b")
	if err := node.Join(bootstrap); err == nil {
		t.Fatal("Join without routing table should fail")
	}

	me := NewContact(testID("ff"), "node-a")
	node.RoutingTable = NewRoutingTable(me)
	if err := node.Join(bootstrap); err == nil {
		t.Fatal("Join without network should fail")
	}

	node.Network = &callbackNode{}
	if err := node.Join(Contact{}); err == nil {
		t.Fatal("invalid bootstrap should fail")
	}

	node.Refresh(-1)
	node.Refresh(IDLength * 8)
	node.Refresh(0)
}

func TestJoinSuccess(t *testing.T) {
	bootstrap := NewContact(testID("11"), "node-b")
	var node *Kademlia
	network := &callbackNode{}
	node = newTestNode("ff", "node-a", network)

	network.send = func(address string, data []byte) error {
		var request rpcMessage
		if err := json.Unmarshal(data, &request); err != nil {
			return err
		}
		if request.Type == rpcFindNode {
			node.handleFindNodeReplyRPC(rpcMessage{
				Type: rpcFindNodeReply, RequestID: request.RequestID,
				SenderID: bootstrap.ID.String(), SenderAddress: bootstrap.Address,
			})
		}
		return nil
	}

	if err := node.Join(bootstrap); err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	closest := node.RoutingTable.FindClosestContacts(bootstrap.ID, 1)
	if len(closest) == 0 {
		t.Fatal("bootstrap was not added")
	}
}

func TestJoinPerformsSelfLookupAndAllBucketRefreshes(t *testing.T) {
	bootstrap := NewContact(testID("11"), "node-b")
	var node *Kademlia
	network := &callbackNode{}
	node = newTestNode("ff", "node-a", network)

	findNodeRequests := 0
	network.send = func(address string, data []byte) error {
		var request rpcMessage
		if err := json.Unmarshal(data, &request); err != nil {
			return err
		}
		if request.Type == rpcFindNode {
			findNodeRequests++
			node.handleFindNodeReplyRPC(rpcMessage{
				Type: rpcFindNodeReply, RequestID: request.RequestID,
				SenderID: bootstrap.ID.String(), SenderAddress: bootstrap.Address,
			})
		}
		return nil
	}

	if err := node.Join(bootstrap); err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	want := IDLength*8 + 1
	if findNodeRequests != want {
		t.Fatalf("Join sent %d FIND_NODE requests, want self lookup plus 256 bucket refreshes = %d", findNodeRequests, want)
	}
}

func TestHandleRPCMessageAndListenForRPC(t *testing.T) {
	me := NewContact(testID("ff"), "node-a")
	req := rpcMessage{
		Type: rpcFindNode, RequestID: "listen-1",
		SenderID: testID("11").String(), SenderAddress: "node-b", TargetID: me.ID.String(),
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	network := &callbackNode{receiveMessage: Message{From: "node-b", Data: data}}
	node := &Kademlia{RoutingTable: NewRoutingTable(me), Network: network, RPCTimeout: 10 * time.Millisecond}

	done := make(chan struct{})
	go func() { node.ListenForRPC(); close(done) }()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("ListenForRPC did not stop")
	}

	node.handleRPCMessage(Message{Data: []byte("{")})
	unknown, _ := json.Marshal(rpcMessage{Type: "UNKNOWN"})
	node.handleRPCMessage(Message{Data: unknown})
	(&Kademlia{}).ListenForRPC()
}

func TestLookupContactParallelFindsCloserContact(t *testing.T) {
	firstHop := NewContact(testID("80"), "node-b")
	closer := NewContact(testID("10"), "node-c")
	target := NewContact(testID("00"), "target")

	var node *Kademlia
	network := &callbackNode{}
	node = newTestNode("ff", "node-a", network)
	node.RoutingTable.AddContact(firstHop)

	network.send = func(address string, data []byte) error {
		var request rpcMessage
		if err := json.Unmarshal(data, &request); err != nil {
			return err
		}

		var contacts []Contact
		sender := firstHop
		switch address {
		case firstHop.Address:
			contacts = []Contact{closer}
		case closer.Address:
			contacts = []Contact{target}
			sender = closer
		case target.Address:
			sender = target
		}

		node.handleFindNodeReplyRPC(rpcMessage{
			Type: rpcFindNodeReply, RequestID: request.RequestID,
			SenderID: sender.ID.String(), SenderAddress: sender.Address,
			Contacts: contactsToWire(contacts),
		})
		return nil
	}

	contacts := node.LookupContact(&target)
	if len(contacts) == 0 {
		t.Fatal("LookupContact returned no contacts")
	}
	if !contacts[0].ID.Equals(target.ID) {
		t.Fatalf("closest = %s, want %s", contacts[0].ID, target.ID)
	}
}

func TestLookupContactUsesAlphaParallelProbes(t *testing.T) {
	contacts := []Contact{
		NewContact(testID("10"), "node-1"),
		NewContact(testID("20"), "node-2"),
		NewContact(testID("30"), "node-3"),
	}
	target := testID("00")

	var node *Kademlia
	network := &callbackNode{}
	node = newTestNode("ff", "node-a", network)
	node.Alpha = len(contacts)

	var mu sync.Mutex
	started := 0
	allStarted := make(chan struct{})
	network.send = func(address string, data []byte) error {
		var request rpcMessage
		if err := json.Unmarshal(data, &request); err != nil {
			return err
		}

		mu.Lock()
		started++
		if started == len(contacts) {
			close(allStarted)
		}
		mu.Unlock()

		select {
		case <-allStarted:
		case <-time.After(150 * time.Millisecond):
			return errors.New("probe did not start in parallel with the rest of the alpha batch")
		}

		sender := contacts[0]
		for _, contact := range contacts {
			if contact.Address == address {
				sender = contact
				break
			}
		}
		node.handleFindNodeReplyRPC(rpcMessage{
			Type: rpcFindNodeReply, RequestID: request.RequestID,
			SenderID: sender.ID.String(), SenderAddress: sender.Address,
			Contacts: contactsToWire([]Contact{sender}),
		})
		return nil
	}

	responses := node.queryContactsParallel(contacts, target)
	if len(responses) != len(contacts) {
		t.Fatalf("queryContactsParallel returned %d responses, want %d", len(responses), len(contacts))
	}
}

func assertNoReceive[T any](t *testing.T, ch <-chan T, name string) {
	t.Helper()

	select {
	case <-ch:
		t.Fatalf("%s was delivered", name)
	default:
	}
}

func assertReceive[T any](t *testing.T, ch <-chan T, name string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("%s was not delivered", name)
	}
}

// TestThousandNodesCommunicateOverSimulatedNetwork verifies the requirement that
// at least 1,000 Kademlia instances communicate over network.go's simulated network.
// It also verifies storage and retrieval from a remote replica and runs in normal CI.
// Every instance sends and answers a real RPC over network.go's shared simulation.
func TestThousandNodesCommunicateOverSimulatedNetwork(t *testing.T) {
	const nodeCount = 1000
	network := NewSimulatedNetwork()
	nodes := make([]*Kademlia, nodeCount)
	contacts := make([]Contact, nodeCount)
	transports := make([]*SimulatedNode, 0, nodeCount)
	var listeners sync.WaitGroup
	t.Cleanup(func() {
		for _, transport := range transports {
			if err := transport.Close(); err != nil {
				t.Error(err)
			}
		}
		listeners.Wait()
		network.mu.RLock()
		remaining := len(network.listeners)
		network.mu.RUnlock()
		if remaining != 0 {
			t.Errorf("cleanup left %d simulated listeners", remaining)
		}
	})

	value := []byte("value stored and retrieved across a 1000-node simulated network")
	for i := range nodes {
		address := fmt.Sprintf("127.0.0.1:%d", 10000+i)
		transport := NewSimulatedNodeWithNetwork(network)
		if err := transport.Listen(address); err != nil {
			t.Fatalf("listen node %d: %v", i, err)
		}
		transports = append(transports, transport)
		id := KademliaID(sha256.Sum256([]byte(address)))
		if i == 0 {
			// Make node 0 the closest replica for this value, ensuring it is stored.
			id = KademliaID(sha256.Sum256(value))
		}
		contacts[i] = NewContact(&id, address)
		nodes[i] = &Kademlia{RoutingTable: NewRoutingTable(contacts[i]), Network: transport, Alpha: 3, K: 3, RPCTimeout: 5 * time.Second}
	}
	// Give each node a small initial set of peers, avoiding a million-contact setup.
	for i, node := range nodes {
		for bit := 0; bit < 10; bit++ {
			peer := i ^ (1 << bit)
			if peer < nodeCount {
				node.RoutingTable.AddContact(contacts[peer])
			}
		}
		node.RoutingTable.AddContact(contacts[(i+1)%nodeCount])
	}
	for _, node := range nodes {
		listeners.Add(1)
		go func(node *Kademlia) {
			defer listeners.Done()
			node.ListenForRPC()
		}(node)
	}

	// A ring ensures all 1000 nodes both send a request and answer another node.
	// Bound concurrent requests to keep this test practical under the race detector.
	jobs := make(chan int, nodeCount)
	results := make(chan error, nodeCount)
	for i := range nodes {
		jobs <- i
	}
	close(jobs)
	for worker := 0; worker < 32; worker++ {
		go func() {
			for i := range jobs {
				peer := (i + 1) % nodeCount
				found, err := nodes[i].sendFindNodeRPC(contacts[peer], contacts[i].ID)
				if err != nil {
					results <- fmt.Errorf("node %d -> node %d: %w", i, peer, err)
					continue
				}
				matched := false
				for _, contact := range found {
					if contact.ID != nil && contact.ID.Equals(contacts[i].ID) && contact.Address == contacts[i].Address {
						matched = true
					}
				}
				if !matched {
					results <- fmt.Errorf("node %d received a reply without the requested contact", i)
				} else {
					results <- nil
				}
			}
		}()
	}
	completed := 0
	for range nodes {
		if err := <-results; err != nil {
			t.Error(err)
		} else {
			completed++
		}
	}
	if completed != nodeCount {
		t.Fatalf("completed %d/%d request/response exchanges", completed, nodeCount)
	}
	t.Logf("%d live nodes completed %d FIND_NODE request/response exchanges over network.go's simulated network", nodeCount, completed)

	key, err := nodes[0].Store(value)
	if err != nil {
		t.Fatalf("store on 1000-node network: %v", err)
	}
	// Choose a non-replica so FIND_VALUE must cross the simulated network.
	reader := -1
	for i := 1; i < nodeCount; i++ {
		nodes[i].dataMu.RLock()
		_, cached := nodes[i].DataStore[key]
		nodes[i].dataMu.RUnlock()
		if !cached {
			reader = i
			break
		}
	}
	if reader < 0 {
		t.Fatal("no non-replica available for remote lookup")
	}
	nodes[reader].addContact(contacts[0])
	got, err := nodes[reader].LookupData(key)
	if err != nil || !bytes.Equal(got, value) {
		t.Fatalf("remote lookup from node %d: value=%q error=%v", reader, got, err)
	}
	t.Logf("node 0 stored %d bytes; non-replica node %d retrieved identical bytes", len(value), reader)
}
