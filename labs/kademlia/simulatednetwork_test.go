package kademlia_test

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"d7024e/kademlia"
)

func TestSimulatedNetworkSendReceive(t *testing.T) {
	t.Log("testing that a simulated connection can send one message to a listening address")

	network := kademlia.NewSimulatedNetwork()
	senderAddr := kademlia.Address{IP: "127.0.0.1", Port: 8000}
	receiverAddr := kademlia.Address{IP: "127.0.0.1", Port: 8001}

	receiver, err := network.Listen(receiverAddr)
	if err != nil {
		t.Fatalf("listen receiver: %v", err)
	}
	defer receiver.Close()

	sender, err := network.Dial(receiverAddr)
	if err != nil {
		t.Fatalf("dial receiver: %v", err)
	}
	defer sender.Close()

	want := kademlia.Message{
		From:    senderAddr,
		To:      receiverAddr,
		Payload: []byte("ping"),
	}
	if err := sender.Send(want); err != nil {
		t.Fatalf("send message: %v", err)
	}
	t.Logf("example sent: from=%v to=%v payload=%q", want.From, want.To, string(want.Payload))

	got, err := receiver.Recv()
	if err != nil {
		t.Fatalf("recv message: %v", err)
	}

	if got.From != want.From || got.To != want.To || string(got.Payload) != string(want.Payload) {
		t.Fatalf("unexpected message: got %+v, want %+v", got, want)
	}
	t.Logf("example received: from=%v to=%v payload=%q", got.From, got.To, string(got.Payload))
}

func TestSimulatedNetworkErrors(t *testing.T) {
	t.Log("testing duplicate listens, dialing missing/closed addresses, and receiving after close")

	network := kademlia.NewSimulatedNetwork()
	addr := kademlia.Address{IP: "127.0.0.1", Port: 8100}

	listener, err := network.Listen(addr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	if _, err := network.Listen(addr); err == nil {
		t.Fatal("expected duplicate listen to fail")
	} else {
		t.Logf("example rejected duplicate listen on %v: %v", addr, err)
	}

	missingAddr := kademlia.Address{IP: "127.0.0.1", Port: 9999}
	if _, err := network.Dial(missingAddr); err == nil {
		t.Fatal("expected dialing a missing address to fail")
	} else {
		t.Logf("example rejected dial to missing address %v: %v", missingAddr, err)
	}

	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("second close should be idempotent: %v", err)
	}

	if _, err := network.Dial(addr); err == nil {
		t.Fatal("expected dialing a closed listener to fail")
	} else {
		t.Logf("example rejected dial to closed address %v: %v", addr, err)
	}
	if _, err := listener.Recv(); err == nil {
		t.Fatal("expected receiving on a closed listener to fail")
	} else {
		t.Logf("example rejected receive after close: %v", err)
	}
}

func TestSimulatedNetworkWorksFor1000Nodes(t *testing.T) {
	const nodeCount = 1000

	t.Logf("testing simulated network delivery and Kademlia lookup with %d nodes", nodeCount)

	network := kademlia.NewSimulatedNetwork()
	addresses := make([]kademlia.Address, nodeCount)
	listeners := make([]kademlia.Connection, nodeCount)
	contacts := make([]kademlia.Contact, nodeCount)
	listenersByAddress := make(map[kademlia.Address]kademlia.Connection, nodeCount)

	for i := 0; i < nodeCount; i++ {
		addresses[i] = kademlia.Address{IP: "10.0.0.1", Port: 10000 + i}
		contacts[i] = kademlia.NewContact(testKademliaID(i), fmt.Sprintf("%s:%d", addresses[i].IP, addresses[i].Port))

		listener, err := network.Listen(addresses[i])
		if err != nil {
			t.Fatalf("listen node %d: %v", i, err)
		}
		listeners[i] = listener
		listenersByAddress[addresses[i]] = listener
		defer listener.Close()
	}
	t.Logf("registered %d simulated listeners", nodeCount)

	me := kademlia.NewContact(&kademlia.KademliaID{}, "10.0.0.1:11000")
	node := &kademlia.Kademlia{RoutingTable: kademlia.NewRoutingTable(me)}
	for _, contact := range contacts {
		node.RoutingTable.AddContact(contact)
	}
	t.Logf("added %d contacts to the routing table", nodeCount)

	target := contacts[777]
	closest := node.LookupContact(&target)
	if len(closest) != 10 {
		t.Fatalf("lookup returned %d contacts, want 10", len(closest))
	}
	if !closest[0].ID.Equals(target.ID) {
		t.Fatalf("closest contact = %s, want target %s", closest[0].ID, target.ID)
	}
	t.Log("lookup returned 10 contacts and the target contact was closest")

	senderAddr := kademlia.Address{IP: "10.0.0.1", Port: 12000}
	var wg sync.WaitGroup
	errCh := make(chan error, len(closest))
	expectedPayloads := make(map[kademlia.Address]string, len(closest))

	for i, contact := range closest {
		to, err := addressFromContact(contact)
		if err != nil {
			t.Fatalf("lookup returned contact with invalid address %q: %v", contact.Address, err)
		}
		payload := []byte(fmt.Sprintf("message-%d", i))
		expectedPayloads[to] = string(payload)
		if i < 3 {
			t.Logf("example sent to closest contact %d: from=%v to=%v payload=%q", i, senderAddr, to, string(payload))
		}

		wg.Add(1)
		go func() {
			defer wg.Done()

			conn, err := network.Dial(to)
			if err != nil {
				errCh <- fmt.Errorf("dial %v: %w", to, err)
				return
			}
			defer conn.Close()

			if err := conn.Send(kademlia.Message{From: senderAddr, To: to, Payload: payload}); err != nil {
				errCh <- fmt.Errorf("send %v -> %v: %w", senderAddr, to, err)
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}

	receivedExamples := 0
	for addr, expectedPayload := range expectedPayloads {
		listener := listenersByAddress[addr]

		got, err := listener.Recv()
		if err != nil {
			t.Fatalf("recv node %v: %v", addr, err)
		}

		if got.From != senderAddr || got.To != addr || string(got.Payload) != expectedPayload {
			t.Fatalf("node %v got %+v, want from=%+v to=%+v payload=%q",
				addr, got, senderAddr, addr, expectedPayload)
		}
		if receivedExamples < 3 {
			t.Logf("example received by closest contact: from=%v to=%v payload=%q", got.From, got.To, string(got.Payload))
			receivedExamples++
		}
	}
	t.Log("verified every closest contact received its expected message")
}

func testKademliaID(index int) *kademlia.KademliaID {
	id := &kademlia.KademliaID{}

	bucketIndex := index % 100
	byteIndex := bucketIndex / 8
	bitIndex := uint(7 - bucketIndex%8)
	id[byteIndex] = 1 << bitIndex

	id[28] = byte(index >> 24)
	id[29] = byte(index >> 16)
	id[30] = byte(index >> 8)
	id[31] = byte(index)

	return id
}

func addressFromContact(contact kademlia.Contact) (kademlia.Address, error) {
	colonIndex := strings.LastIndex(contact.Address, ":")
	if colonIndex == -1 {
		return kademlia.Address{}, fmt.Errorf("missing port separator")
	}

	port, err := strconv.Atoi(contact.Address[colonIndex+1:])
	if err != nil {
		return kademlia.Address{}, err
	}

	return kademlia.Address{
		IP:   contact.Address[:colonIndex],
		Port: port,
	}, nil
}
