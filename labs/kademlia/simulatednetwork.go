package kademlia

import (
	"errors"
	"fmt"
	"sync"
)

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
type SimulatedNetworkAPI interface {
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

const simulatedNetworkBufferSize = 1024

// Creates an empty simulated network.
func NewSimulatedNetwork() *SimulatedNetwork {
	return &SimulatedNetwork{
		listeners: make(map[Address]chan Message),
	}
}

// Registers a node so it can receive messages.
func (n *SimulatedNetwork) Listen(addr Address) (Connection, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if _, exists := n.listeners[addr]; exists {
		return nil, fmt.Errorf("address already in use: %+v", addr)
	}

	recvCh := make(chan Message, simulatedNetworkBufferSize)
	n.listeners[addr] = recvCh

	return &SimulatedConnection{
		addr:    addr,
		network: n,
		recvCh:  recvCh,
	}, nil
}

// Creates a connection to another registered node.
func (n *SimulatedNetwork) Dial(addr Address) (Connection, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if _, exists := n.listeners[addr]; !exists {
		return nil, fmt.Errorf("address not found: %+v", addr)
	}

	return &SimulatedConnection{
		addr:    addr,
		network: n,
	}, nil
}

// Sends a message to another node.
func (c *SimulatedConnection) Send(msg Message) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return errors.New("connection closed")
	}
	c.mu.RUnlock()

	c.network.mu.RLock()
	defer c.network.mu.RUnlock()

	recvCh, exists := c.network.listeners[msg.To]
	if !exists {
		return fmt.Errorf("destination address not found: %+v", msg.To)
	}

	select {
	case recvCh <- msg:
		return nil
	default:
		return fmt.Errorf("message queue full for destination: %+v", msg.To)
	}
}

// Waits for and receives a message.
func (c *SimulatedConnection) Recv() (Message, error) {
	c.mu.RLock()
	if c.closed || c.recvCh == nil {
		c.mu.RUnlock()
		return Message{}, errors.New("connection is not listening")
	}
	recvCh := c.recvCh
	c.mu.RUnlock()

	msg, ok := <-recvCh
	if !ok {
		return Message{}, errors.New("connection closed")
	}

	return msg, nil
}

// Closes this simulated connection.
func (c *SimulatedConnection) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}

	c.closed = true
	recvCh := c.recvCh
	c.recvCh = nil
	c.mu.Unlock()

	if recvCh == nil {
		return nil
	}

	c.network.mu.Lock()
	defer c.network.mu.Unlock()

	if currentCh, exists := c.network.listeners[c.addr]; exists && currentCh == recvCh {
		delete(c.network.listeners, c.addr)
		close(recvCh)
	}

	return nil
}
