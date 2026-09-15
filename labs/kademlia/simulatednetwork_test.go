package kademlia

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestSimulatedNetworkSendReceive(t *testing.T) {
	network := NewSimulatedNetwork()
	senderAddr := Address{IP: "127.0.0.1", Port: 8000}
	receiverAddr := Address{IP: "127.0.0.1", Port: 8001}

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

	want := Message{
		From:    senderAddr,
		To:      receiverAddr,
		Payload: []byte("ping"),
	}
	if err := sender.Send(want); err != nil {
		t.Fatalf("send message: %v", err)
	}

	got, err := receiver.Recv()
	if err != nil {
		t.Fatalf("recv message: %v", err)
	}

	if got.From != want.From || got.To != want.To || string(got.Payload) != string(want.Payload) {
		t.Fatalf("unexpected message: got %+v, want %+v", got, want)
	}
}

func TestSimulatedNetworkErrors(t *testing.T) {
	network := NewSimulatedNetwork()
	addr := Address{IP: "127.0.0.1", Port: 8100}

	listener, err := network.Listen(addr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	if _, err := network.Listen(addr); err == nil {
		t.Fatal("expected duplicate listen to fail")
	}

	if _, err := network.Dial(Address{IP: "127.0.0.1", Port: 9999}); err == nil {
		t.Fatal("expected dialing a missing address to fail")
	}

	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("second close should be idempotent: %v", err)
	}

	if _, err := network.Dial(addr); err == nil {
		t.Fatal("expected dialing a closed listener to fail")
	}
	if _, err := listener.Recv(); err == nil {
		t.Fatal("expected receiving on a closed listener to fail")
	}
}

func TestSimulatedNetworkWorksFor1000Nodes(t *testing.T) {
	const nodeCount = 1000

	network := NewSimulatedNetwork()
	addresses := make([]Address, nodeCount)
	listeners := make([]Connection, nodeCount)
	contacts := make([]Contact, nodeCount)
	listenersByAddress := make(map[Address]Connection, nodeCount)

	for i := 0; i < nodeCount; i++ {
		addresses[i] = Address{IP: "10.0.0.1", Port: 10000 + i}
		contacts[i] = NewContact(testKademliaID(i), fmt.Sprintf("%s:%d", addresses[i].IP, addresses[i].Port))

		listener, err := network.Listen(addresses[i])
		if err != nil {
			t.Fatalf("listen node %d: %v", i, err)
		}
		listeners[i] = listener
		listenersByAddress[addresses[i]] = listener
		defer listener.Close()
	}

	me := NewContact(&KademliaID{}, "10.0.0.1:11000")
	kademlia := &Kademlia{RoutingTable: NewRoutingTable(me)}
	for _, contact := range contacts {
		kademlia.RoutingTable.AddContact(contact)
	}

	target := contacts[777]
	closest := kademlia.LookupContact(&target)
	if len(closest) != 10 {
		t.Fatalf("lookup returned %d contacts, want 10", len(closest))
	}
	if !closest[0].ID.Equals(target.ID) {
		t.Fatalf("closest contact = %s, want target %s", closest[0].ID, target.ID)
	}

	senderAddr := Address{IP: "10.0.0.1", Port: 12000}
	var wg sync.WaitGroup
	errCh := make(chan error, len(closest))
	expectedPayloads := make(map[Address]string, len(closest))

	for i, contact := range closest {
		to, err := addressFromContact(contact)
		if err != nil {
			t.Fatalf("lookup returned contact with invalid address %q: %v", contact.Address, err)
		}
		payload := []byte(fmt.Sprintf("message-%d", i))
		expectedPayloads[to] = string(payload)

		wg.Add(1)
		go func() {
			defer wg.Done()

			conn, err := network.Dial(to)
			if err != nil {
				errCh <- fmt.Errorf("dial %v: %w", to, err)
				return
			}
			defer conn.Close()

			if err := conn.Send(Message{From: senderAddr, To: to, Payload: payload}); err != nil {
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
	}
}

func testKademliaID(index int) *KademliaID {
	id := &KademliaID{}

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

func addressFromContact(contact Contact) (Address, error) {
	colonIndex := strings.LastIndex(contact.Address, ":")
	if colonIndex == -1 {
		return Address{}, fmt.Errorf("missing port separator")
	}

	port, err := strconv.Atoi(contact.Address[colonIndex+1:])
	if err != nil {
		return Address{}, err
	}

	return Address{
		IP:   contact.Address[:colonIndex],
		Port: port,
	}, nil
}
