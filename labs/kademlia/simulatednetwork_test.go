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

	nodes := make(
		map[string]*kademlia.SimulatedNode,
		nodeCount,
	)

	for i := 0; i < nodeCount; i++ {
		address := fmt.Sprintf("node-%d", i)

		node, err := network.NewNode(address)
		if err != nil {
			t.Fatalf(
				"create node %d: %v",
				i,
				err,
			)
		}

		nodes[address] = node
	}

	if len(nodes) != nodeCount {
		t.Fatalf(
			"created %d nodes, want %d",
			len(nodes),
			nodeCount,
		)
	}

	t.Logf(
		"successfully created %d simulated nodes",
		len(nodes),
	)

	for _, node := range nodes {
		_ = node.Close()
	}
}
