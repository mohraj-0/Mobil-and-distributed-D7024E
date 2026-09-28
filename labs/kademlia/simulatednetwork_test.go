package kademlia_test

import (
	"fmt"
	"testing"
	"time"

	"d7024e/kademlia"
)

// Testar att en nod inte kan skapas med tom adress
func TestSimulatedNetworkRejectsEmptyAddress(t *testing.T) {
	network := kademlia.NewSimulatedNetwork(
		0.0,
		0*time.Millisecond,
	)

	// Försök skapa en nod utan adress
	if _, err := network.NewNode(""); err == nil {
		t.Fatal("expected empty node address to return an error")
	}
}

// Testar att en nod inte kan skapas med en adress som redan används
// Testar att en simulerad nod bara accepterar rätt adress
func TestSimulatedNodeListen(t *testing.T) {
	network := kademlia.NewSimulatedNetwork(
		0.0,
		0*time.Millisecond,
	)

	// Skapa en nod med adress "node-A"
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

	// En annan adress ska ge error
	if err := node.Listen("node-B"); err == nil {
		t.Fatal("Listen with different address should return an error")
	}
}

// Testar att en stängd nod inte kan ta emot meddelanden
func TestSimulatedNodeReceiveAfterClose(t *testing.T) {
	network := kademlia.NewSimulatedNetwork(
		0.0,
		0*time.Millisecond,
	)

	// Skapa node-A
	node, err := network.NewNode("node-A")
	if err != nil {
		t.Fatalf("create node: %v", err)
	}

	// Stäng noden
	if err := node.Close(); err != nil {
		t.Fatalf("close node: %v", err)
	}

	// En stängd nod ska ge fel vid Receive
	if _, err := node.Receive(); err == nil {
		t.Fatal("Receive after Close should return an error")
	}
}

// Testar att en nod inte kan skicka till en tom adress
func TestSimulatedNodeEmptyDestination(t *testing.T) {
	network := kademlia.NewSimulatedNetwork(
		0.0,
		0*time.Millisecond,
	)

	// Skapa node-A
	node, err := network.NewNode("node-A")
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	defer node.Close()

	// Skicka till en tom adress ska ge fel
	if err := node.SendData("", []byte("hello")); err == nil {
		t.Fatal("SendData with empty destination should return an error")
	}
}

// Testar att nätverket väntar rätt tid innan ett meddelande skickas
// Testar att nätverket simulerar latens korrekt
func TestSimulatedNetworkLatency(t *testing.T) {
	delay := 20 * time.Millisecond

	// Skapa ett simulerat nätverk med latens
	// Skapa nätverk med 20 ms fördröjning
	network := kademlia.NewSimulatedNetwork(
		0.0,
		delay,
	)

	// skapa  noder A  i nätverket
	nodeA, err := network.NewNode("node-A")
	if err != nil {
		t.Fatalf("create node A: %v", err)
	}
	defer nodeA.Close()

	// skapa  noder B  i nätverket
	nodeB, err := network.NewNode("node-B")
	if err != nil {
		t.Fatalf("create node B: %v", err)
	}
	defer nodeB.Close()

	// Skicka ett meddelande från node-A till node-B och mät tiden det tar
	// Starta tidmätningen
	start := time.Now()

	// Skicka meddelandet
	if err := nodeA.SendData(
		"node-B",
		[]byte("hello"),
	); err != nil {
		t.Fatalf("send data: %v", err)
	}
	// Mät hur lång tid det tog
	elapsed := time.Since(start)

	// Kontrollera att fördröjningen är minst 20 ms
	if elapsed < delay {
		t.Fatalf(
			"SendData took %v, expected at least %v latency",
			elapsed,
			delay,
		)
	}

	// Ta emot meddelandet
	message, err := nodeB.Receive()
	if err != nil {
		t.Fatalf("receive: %v", err)
	}

	// Kontrollera att rätt meddelande kom fram
	if string(message.Data) != "hello" {
		t.Fatalf(
			"received %q, want %q",
			string(message.Data),
			"hello",
		)
	}
}

// Testar att negativ packet loss ändras till 0
func TestSimulatedNetworkPacketLossBelowZero(t *testing.T) {
	// Negativ packet loss ska ändras till 0.
	network := kademlia.NewSimulatedNetwork(
		-1.0,
		0*time.Millisecond,
	)

	// Skapa node-A
	nodeA, err := network.NewNode("node-A")
	if err != nil {
		t.Fatalf("create node A: %v", err)
	}
	defer nodeA.Close()

	// Skapa node-B
	nodeB, err := network.NewNode("node-B")
	if err != nil {
		t.Fatalf("create node B: %v", err)
	}
	defer nodeB.Close()

	// Skicka ett meddelande från node-A till node-B
	if err := nodeA.SendData(
		"node-B",
		[]byte("hello"),
	); err != nil {
		t.Fatalf("send data: %v", err)
	}

	// Ta emot meddelandet på node-B
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

// Testar att packet loss över 1 ändras till 1
func TestSimulatedNetworkPacketLossAboveOne(t *testing.T) {
	// Packet loss över 1 ska ändras till 1.
	network := kademlia.NewSimulatedNetwork(
		2.0,
		0*time.Millisecond,
	)

	// Skapa node-A
	nodeA, err := network.NewNode("node-A")
	if err != nil {
		t.Fatalf("create node A: %v", err)
	}
	defer nodeA.Close()

	// Skapa node-B
	nodeB, err := network.NewNode("node-B")
	if err != nil {
		t.Fatalf("create node B: %v", err)
	}
	defer nodeB.Close()

	// Skicka ett meddelande från node-A till node-B
	if err := nodeA.SendData(
		"node-B",
		[]byte("hello"),
	); err != nil {
		t.Fatalf("send data: %v", err)
	}

	// Hämta antal skickade, mottagna och tappade meddelanden
	sent, received, dropped := network.Stats()

	// Kontrollera att meddelandet skickades
	if sent != 1 {
		t.Fatalf("sent = %d, want 1", sent)
	}

	// Kontrollera att meddelandet inte mottogs
	if received != 0 {
		t.Fatalf("received = %d, want 0", received)
	}

	// Kontrollera att meddelandet tappades
	if dropped != 1 {
		t.Fatalf("dropped = %d, want 1", dropped)
	}
}

// 1000 noder skapas
//  varje nod skickar till nästa
//  alla meddelanden tas emot
//  avsändare och data kontrolleras

// Testar att 1000 simulerade noder kan kommunicera med varandra
func TestSimulatedNetworkWorksFor1000Nodes(t *testing.T) {
	const nodeCount = 1000

	// Skapa ett simulerat nätverk utan packet loss och utan latens
	network := kademlia.NewSimulatedNetwork(
		0.0,
		0*time.Millisecond,
	)

	//  Lista för alla noder
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

	// Stäng alla noder när testet är klart
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

	// Kontrollera att alla  noder  fick rätt meddelande
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

		// Kontrollera att meddelandet kom från rätt nod
		wantSender := fmt.Sprintf("node-%d", previous)

		// Kontrollera att meddelandet är rätt
		wantData := fmt.Sprintf("hello-from-node-%d", previous)

		// rätt avsändare
		if got.From != wantSender {
			t.Fatalf(
				"node %d received from %q, want %q",
				i,
				got.From,
				wantSender,
			)
		}

		// Kontrollera meddelandet
		if string(got.Data) != wantData {
			t.Fatalf(
				"node %d received %q, want %q",
				i,
				string(got.Data),
				wantData,
			)
		}
	}

	// Hämta antal skickade, mottagna och tappade meddelanden
	sent, received, dropped := network.Stats()

	// Kontrollera antal skickade.
	if sent != nodeCount {
		t.Fatalf("sent=%d, want %d", sent, nodeCount)
	}

	// Kontrollera antal mottagna
	if received != nodeCount {
		t.Fatalf("received=%d, want %d", received, nodeCount)
	}

	// Kontrollera  meddelanden tappades
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

// Testar att det simulation nätverket klarar många noder
// och att meddelanden kan skickas och tas emot  mellan dem
func TestLargeSimulatedNetworkTraffic(t *testing.T) {
	const totalNodes = 10000

	network := kademlia.NewSimulatedNetwork(
		0.0,
		0*time.Millisecond,
	)

	nodes := make([]*kademlia.SimulatedNode, totalNodes)

	// Create a large simulated network.
	for i := 0; i < totalNodes; i++ {
		address := fmt.Sprintf("sim-node-%d", i)

		node, err := network.NewNode(address)
		if err != nil {
			t.Fatalf("could not create node %d: %v", i, err)
		}

		nodes[i] = node
	}

	defer func() {
		for _, node := range nodes {
			_ = node.Close()
		}
	}()

	// Different nodes communicate across the large network.
	type transmission struct {
		sender   int
		receiver int
		payload  string
	}
	// Väljer några noder på olika platser i det simulerade nätverket
	// och anger vilka meddelanden de ska skicka till varandra
	transmissions := []transmission{
		{sender: 0, receiver: 9999, payload: "first-message"},
		{sender: 125, receiver: 5000, payload: "second-message"},
		{sender: 3000, receiver: 42, payload: "third-message"},
		{sender: 7500, receiver: 2500, payload: "fourth-message"},
		{sender: 9999, receiver: 1, payload: "last-message"},
	}

	// Skicka alla meddelanden i listan
	for _, tx := range transmissions {
		// Skapa adressen till noden som ska ta emot meddelandet
		destination := fmt.Sprintf("sim-node-%d", tx.receiver)

		// Skicka meddelandet från sender-noden till receiver-noden
		err := nodes[tx.sender].SendData(
			destination,
			[]byte(tx.payload),
		)

		if err != nil {
			t.Fatalf(
				"node %d could not send to node %d: %v",
				tx.sender,
				tx.receiver,
				err,
			)
		}
	}

	// Kontrollera att rätt noder fick rätt meddelanden
	for _, tx := range transmissions {

		// Ta emot meddelandet på receiver-noden
		message, err := nodes[tx.receiver].Receive()
		if err != nil {
			t.Fatalf(
				"node %d could not receive message: %v",
				tx.receiver,
				err,
			)
		}

		// Skapa namnet på den nod som vi förväntar oss skickade meddelandet
		expectedSender := fmt.Sprintf("sim-node-%d", tx.sender)

		// Kontrollera att meddelandet kom från rätt nod
		if message.From != expectedSender {
			t.Errorf(
				"receiver %d got sender %q, expected %q",
				tx.receiver,
				message.From,
				expectedSender,
			)
		}

		// Kontrollera att själva meddelandet är rätt
		if string(message.Data) != tx.payload {
			t.Errorf(
				"receiver %d got data %q, expected %q",
				tx.receiver,
				string(message.Data),
				tx.payload,
			)
		}
	}

	// Kontrollera hur många meddelanden som skickades, togs emot och tappades
	sent, received, dropped := network.Stats()

	// Kontrollera att rätt antal meddelanden skickades
	if sent != len(transmissions) {
		t.Errorf(
			"sent=%d, expected %d",
			sent,
			len(transmissions),
		)
	}

	// Kontrollera att rätt antal meddelanden togs emot
	if received != len(transmissions) {
		t.Errorf(
			"received=%d, expected %d",
			received,
			len(transmissions),
		)
	}

	// Kontrollera att inga meddelanden tappades
	if dropped != 0 {
		t.Errorf("dropped=%d, expected 0", dropped)
	}

	// Skriv ut resultatet i Git Bash när testet körs
	t.Logf(
		"large simulation successful: nodes=%d sent=%d received=%d dropped=%d",
		totalNodes,
		sent,
		received,
		dropped,
	)
}
