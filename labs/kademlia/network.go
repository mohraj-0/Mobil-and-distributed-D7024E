package kademlia

import (
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"
)

// Message representerar ett meddelande mellan två noder.
type Message struct {
	From string
	Data []byte
}

// Node beskriver vad Kademlia behöver från nätverket.
//
// Kademlia-logiken behöver inte veta om kommunikationen sker
// via riktig UDP eller via ett simulerat nätverk.
type Node interface {
	Listen(address string) error
	Close() error
	Receive() (Message, error)
	SendData(address string, data []byte) error
}

//
// ============================================================
// UDP NODE
// ============================================================
//

// UDPNode används när noder kommunicerar över riktigt UDP-nätverk.
type UDPNode struct {
	conn *net.UDPConn
}

// NewUDPNode skapar en ny UDP-node.
func NewUDPNode() *UDPNode {
	return &UDPNode{}
}

// Listen öppnar UDP-porten för noden.
func (node *UDPNode) Listen(address string) error {
	if address == "" {
		return errors.New("address cannot be empty")
	}

	udpAddress, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return fmt.Errorf("invalid UDP address %q: %w", address, err)
	}

	conn, err := net.ListenUDP("udp", udpAddress)
	if err != nil {
		return fmt.Errorf("could not listen on %q: %w", address, err)
	}

	node.conn = conn

	return nil
}

// Close stänger UDP-anslutningen.
func (node *UDPNode) Close() error {
	if node.conn == nil {
		return nil
	}

	return node.conn.Close()
}

// Receive väntar på ett UDP-meddelande.
func (node *UDPNode) Receive() (Message, error) {
	if node.conn == nil {
		return Message{}, errors.New("UDP node is not listening")
	}

	buffer := make([]byte, 4096)

	n, remoteAddress, err := node.conn.ReadFromUDP(buffer)
	if err != nil {
		return Message{}, fmt.Errorf("could not receive UDP packet: %w", err)
	}

	// Kopiera bara de bytes som faktiskt togs emot.
	data := make([]byte, n)
	copy(data, buffer[:n])

	return Message{
		From: remoteAddress.String(),
		Data: data,
	}, nil
}

// SendData skickar ett UDP-meddelande till en annan nod.
func (node *UDPNode) SendData(address string, data []byte) error {
	if node.conn == nil {
		return errors.New("UDP node is not listening")
	}

	if address == "" {
		return errors.New("destination address cannot be empty")
	}

	remoteAddress, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return fmt.Errorf("invalid destination address %q: %w", address, err)
	}

	_, err = node.conn.WriteToUDP(data, remoteAddress)
	if err != nil {
		return fmt.Errorf("could not send UDP packet: %w", err)
	}

	return nil
}

//
// ============================================================
// SIMULATED NETWORK
// ============================================================
//

// SimulatedNetwork används för tester med många noder.
//
// Alla noder körs i samma program och använder Go-channels
// istället för riktiga UDP-paket.
type SimulatedNetwork struct {
	mu sync.RWMutex

	nodes map[string]*SimulatedNode

	packetLoss float64
	latency    time.Duration

	sentPackets     int
	receivedPackets int
	droppedPackets  int
}

// NewSimulatedNetwork skapar ett nytt simulerat nätverk.
//
// packetLoss:
// 0.0 = inga paket tappas
// 0.1 = ungefär 10 % tappas
// 1.0 = alla paket tappas
//
// latency:
// exempel: 10 * time.Millisecond
func NewSimulatedNetwork(
	packetLoss float64,
	latency time.Duration,
) *SimulatedNetwork {

	if packetLoss < 0 {
		packetLoss = 0
	}

	if packetLoss > 1 {
		packetLoss = 1
	}

	return &SimulatedNetwork{
		nodes:      make(map[string]*SimulatedNode),
		packetLoss: packetLoss,
		latency:    latency,
	}
}

// NewNode skapar en ny nod i det simulerade nätverket.
func (network *SimulatedNetwork) NewNode(address string) (*SimulatedNode, error) {
	if address == "" {
		return nil, errors.New("address cannot be empty")
	}

	network.mu.Lock()
	defer network.mu.Unlock()

	if _, exists := network.nodes[address]; exists {
		return nil, fmt.Errorf("node %q already exists", address)
	}

	node := &SimulatedNode{
		address: address,
		network: network,
		inbox:   make(chan Message, 100),
	}

	network.nodes[address] = node

	return node, nil
}

// removeNode tar bort en nod från nätverket.
func (network *SimulatedNetwork) removeNode(address string) {
	network.mu.Lock()
	defer network.mu.Unlock()

	delete(network.nodes, address)
}

// Stats returnerar enkel statistik för experiment.
//
// Detta kan senare användas i rapporten.
func (network *SimulatedNetwork) Stats() (
	sent int,
	received int,
	dropped int,
) {
	network.mu.RLock()
	defer network.mu.RUnlock()

	return network.sentPackets,
		network.receivedPackets,
		network.droppedPackets
}

//
// ============================================================
// SIMULATED NODE
// ============================================================
//

// SimulatedNode fungerar som en vanlig Node men använder channels
// istället för UDP.
type SimulatedNode struct {
	address string
	network *SimulatedNetwork
	inbox   chan Message

	closed bool
	mu     sync.RWMutex
}

// Listen behövs för att SimulatedNode ska implementera Node.
//
// Noden skapas redan med en adress via network.NewNode(),
// därför kontrollerar vi bara att adressen stämmer.
func (node *SimulatedNode) Listen(address string) error {
	if address == "" {
		return errors.New("address cannot be empty")
	}

	node.mu.Lock()
	defer node.mu.Unlock()

	if node.address == "" {
		node.address = address
	}

	if node.address != address {
		return fmt.Errorf(
			"node already has address %q",
			node.address,
		)
	}

	node.closed = false

	return nil
}

// Close markerar noden som stängd och tar bort den från nätverket.
func (node *SimulatedNode) Close() error {
	node.mu.Lock()

	if node.closed {
		node.mu.Unlock()
		return nil
	}

	node.closed = true
	node.mu.Unlock()

	node.network.removeNode(node.address)

	return nil
}

// Receive väntar tills ett meddelande kommer till noden.
func (node *SimulatedNode) Receive() (Message, error) {
	node.mu.RLock()
	closed := node.closed
	node.mu.RUnlock()

	if closed {
		return Message{}, errors.New("node is closed")
	}

	message, ok := <-node.inbox
	if !ok {
		return Message{}, errors.New("node inbox is closed")
	}

	node.network.mu.Lock()
	node.network.receivedPackets++
	node.network.mu.Unlock()

	return message, nil
}

// SendData skickar ett meddelande genom det simulerade nätverket.
func (node *SimulatedNode) SendData(
	address string,
	data []byte,
) error {

	if address == "" {
		return errors.New("destination address cannot be empty")
	}

	node.mu.RLock()
	closed := node.closed
	node.mu.RUnlock()

	if closed {
		return errors.New("node is closed")
	}

	node.network.mu.Lock()

	node.network.sentPackets++

	target, exists := node.network.nodes[address]

	// Simulera packet loss.
	if rand.Float64() < node.network.packetLoss {
		node.network.droppedPackets++
		node.network.mu.Unlock()
		return nil
	}

	latency := node.network.latency

	node.network.mu.Unlock()

	if !exists {
		return fmt.Errorf(
			"destination node %q does not exist",
			address,
		)
	}

	// Kopiera datan så att avsändaren inte kan ändra innehållet
	// efter att meddelandet skickats.
	messageData := make([]byte, len(data))
	copy(messageData, data)

	message := Message{
		From: node.address,
		Data: messageData,
	}

	// Om latency är satt simulerar vi nätverksfördröjning.
	if latency > 0 {
		time.Sleep(latency)
	}

	select {
	case target.inbox <- message:
		return nil

	case <-time.After(2 * time.Second):
		return fmt.Errorf(
			"timed out sending message to %q",
			address,
		)
	}
}
