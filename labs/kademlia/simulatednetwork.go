package kademlia

import "sync"

// Identifies a node in the simulated network.
type Address struct {
	IP   string
	Port int
}

// A message sent between simulated nodes.
type Message struct {
	From    Address
	To      Address
	Payload []byte
}

// General network contract.
type Network interface {
	Listen(addr Address) (Connection, error)
	Dial(addr Address) (Connection, error)
}

// General connection contract.
type Connection interface {
	Send(msg Message) error
	Recv() (Message, error)
	Close() error
}

// Simulated network.
// Think: address -> receive channel
type SimulatedNetwork struct {
	mu        sync.RWMutex
	listeners map[Address]chan Message
}

// Simulated connection.
type SimulatedConnection struct {
	addr    Address
	network *SimulatedNetwork
	recvCh  chan Message
	closed  bool
	mu      sync.RWMutex
}

// Creates an empty simulated network.
func NewSimulatedNetwork() *SimulatedNetwork {
	// TODO
	return nil
}

// Registers a node so it can receive messages.
func (n *SimulatedNetwork) Listen(addr Address) (Connection, error) {
	// TODO
	return nil, nil
}

// Creates a connection to another registered node.
func (n *SimulatedNetwork) Dial(addr Address) (Connection, error) {
	// TODO
	return nil, nil
}

// Sends a message to another node.
func (c *SimulatedConnection) Send(msg Message) error {
	// TODO
	return nil
}

// Waits for and receives a message.
func (c *SimulatedConnection) Recv() (Message, error) {
	// TODO
	return Message{}, nil
}

// Closes this simulated connection.
func (c *SimulatedConnection) Close() error {
	// TODO
	return nil
}
