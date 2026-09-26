package kademlia_test

import (
	"fmt"
	"testing"
	"time"

	"d7024e/kademlia"
)

func TestSimulatedNetworkRejectsEmptyAddress(t *testing.T) {
	network := kademlia.NewSimulatedNetwork(
		0.0,
		0*time.Millisecond,
	)

	if _, err := network.NewNode(""); err == nil {
		t.Fatal("expected empty node address to return an error")
	}
}

func TestSimulatedNodeListen(t *testing.T) {
	network := kademlia.NewSimulatedNetwork(
		0.0,
		0*time.Millisecond,
	)

	node, err := network.NewNode("node-A")
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	defer node.Close()

	// Samma adress ska fungera.
	if err := node.Listen("node-A"); err != nil {
		t.Fatalf("Listen with same address returned error: %v", err)
	}

	// Tom adress ska ge error.
	if err := node.Listen(""); err == nil {
		t.Fatal("Listen with empty address should return an error")
	}

	// En annan adress ska ge error.
	if err := node.Listen("node-B"); err == nil {
		t.Fatal("Listen with different address should return an error")
	}
}

func TestSimulatedNodeReceiveAfterClose(t *testing.T) {
	network := kademlia.NewSimulatedNetwork(
		0.0,
		0*time.Millisecond,
	)

	node, err := network.NewNode("node-A")
	if err != nil {
		t.Fatalf("create node: %v", err)
	}

	if err := node.Close(); err != nil {
		t.Fatalf("close node: %v", err)
	}

	if _, err := node.Receive(); err == nil {
		t.Fatal("Receive after Close should return an error")
	}
}

func TestSimulatedNodeEmptyDestination(t *testing.T) {
	network := kademlia.NewSimulatedNetwork(
		0.0,
		0*time.Millisecond,
	)

	node, err := network.NewNode("node-A")
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	defer node.Close()

	if err := node.SendData("", []byte("hello")); err == nil {
		t.Fatal("SendData with empty destination should return an error")
	}
}

func TestSimulatedNetworkLatency(t *testing.T) {
	delay := 20 * time.Millisecond

	network := kademlia.NewSimulatedNetwork(
		0.0,
		delay,
	)

	nodeA, err := network.NewNode("node-A")
	if err != nil {
		t.Fatalf("create node A: %v", err)
	}
	defer nodeA.Close()

	nodeB, err := network.NewNode("node-B")
	if err != nil {
		t.Fatalf("create node B: %v", err)
	}
	defer nodeB.Close()

	start := time.Now()

	if err := nodeA.SendData(
		"node-B",
		[]byte("hello"),
	); err != nil {
		t.Fatalf("send data: %v", err)
	}

	elapsed := time.Since(start)

	if elapsed < delay {
		t.Fatalf(
			"SendData took %v, expected at least %v latency",
			elapsed,
			delay,
		)
	}

	message, err := nodeB.Receive()
	if err != nil {
		t.Fatalf("receive: %v", err)
	}

	if string(message.Data) != "hello" {
		t.Fatalf(
			"received %q, want %q",
			string(message.Data),
			"hello",
		)
	}
}

func TestSimulatedNetworkPacketLossBelowZero(t *testing.T) {
	// Negativ packet loss ska ändras till 0.
	network := kademlia.NewSimulatedNetwork(
		-1.0,
		0*time.Millisecond,
	)

	nodeA, err := network.NewNode("node-A")
	if err != nil {
		t.Fatalf("create node A: %v", err)
	}
	defer nodeA.Close()

	nodeB, err := network.NewNode("node-B")
	if err != nil {
		t.Fatalf("create node B: %v", err)
	}
	defer nodeB.Close()

	if err := nodeA.SendData(
		"node-B",
		[]byte("hello"),
	); err != nil {
		t.Fatalf("send data: %v", err)
	}

	message, err := nodeB.Receive()
	if err != nil {
		t.Fatalf("receive data: %v", err)
	}

	if string(message.Data) != "hello" {
		t.Fatalf(
			"received %q, want hello",
			string(message.Data),
		)
	}
}

func TestSimulatedNetworkPacketLossAboveOne(t *testing.T) {
	// Packet loss över 1 ska ändras till 1.
	network := kademlia.NewSimulatedNetwork(
		2.0,
		0*time.Millisecond,
	)

	nodeA, err := network.NewNode("node-A")
	if err != nil {
		t.Fatalf("create node A: %v", err)
	}
	defer nodeA.Close()

	nodeB, err := network.NewNode("node-B")
	if err != nil {
		t.Fatalf("create node B: %v", err)
	}
	defer nodeB.Close()

	if err := nodeA.SendData(
		"node-B",
		[]byte("hello"),
	); err != nil {
		t.Fatalf("send data: %v", err)
	}

	sent, received, dropped := network.Stats()

	if sent != 1 {
		t.Fatalf("sent = %d, want 1", sent)
	}

	if received != 0 {
		t.Fatalf("received = %d, want 0", received)
	}

	if dropped != 1 {
		t.Fatalf("dropped = %d, want 1", dropped)
	}
}

func TestSimulatedNetworkWorksFor1000Nodes(t *testing.T) {
	const nodeCount = 1000

	network := kademlia.NewSimulatedNetwork(
		0.0,
		0*time.Millisecond,
	)

	nodes := make([]*kademlia.SimulatedNode, nodeCount)

	// Skapa 1000 noder.
	for i := 0; i < nodeCount; i++ {
		address := fmt.Sprintf("node-%d", i)

		node, err := network.NewNode(address)
		if err != nil {
			t.Fatalf("create node %d: %v", i, err)
		}

		nodes[i] = node
	}

	defer func() {
		for _, node := range nodes {
			_ = node.Close()
		}
	}()

	// Varje nod skickar ett meddelande till nästa nod.
	//
	// node-0 -> node-1
	// node-1 -> node-2
	// ...
	// node-999 -> node-0
	for i := 0; i < nodeCount; i++ {
		next := (i + 1) % nodeCount

		destination := fmt.Sprintf("node-%d", next)
		message := fmt.Sprintf("hello-from-node-%d", i)

		err := nodes[i].SendData(
			destination,
			[]byte(message),
		)

		if err != nil {
			t.Fatalf(
				"node %d could not send to node %d: %v",
				i,
				next,
				err,
			)
		}
	}

	// Kontrollera att alla 1000 noder faktiskt fick sitt meddelande.
	for i := 0; i < nodeCount; i++ {
		previous := (i - 1 + nodeCount) % nodeCount

		got, err := nodes[i].Receive()
		if err != nil {
			t.Fatalf(
				"node %d could not receive: %v",
				i,
				err,
			)
		}

		wantSender := fmt.Sprintf("node-%d", previous)
		wantData := fmt.Sprintf("hello-from-node-%d", previous)

		if got.From != wantSender {
			t.Fatalf(
				"node %d received from %q, want %q",
				i,
				got.From,
				wantSender,
			)
		}

		if string(got.Data) != wantData {
			t.Fatalf(
				"node %d received %q, want %q",
				i,
				string(got.Data),
				wantData,
			)
		}
	}

	sent, received, dropped := network.Stats()

	if sent != nodeCount {
		t.Fatalf("sent=%d, want %d", sent, nodeCount)
	}

	if received != nodeCount {
		t.Fatalf("received=%d, want %d", received, nodeCount)
	}

	if dropped != 0 {
		t.Fatalf("dropped=%d, want 0", dropped)
	}

	t.Logf(
		"1000-node network works: sent=%d received=%d dropped=%d",
		sent,
		received,
		dropped,
	)
}
