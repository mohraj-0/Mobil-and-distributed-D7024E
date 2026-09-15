package kademlia

import (
	"errors"
	"fmt"
	"sync"
)

// Address identifierar en nod i det simulerade nätverket.
// Den ersätter en riktig UDP-adress när tester kör helt i minnet.
type Address struct {
	IP   string
	Port int
}

// Message är ett meddelande mellan två simulerade noder.
// From och To beskriver rutten, Payload är själva datan.
type Message struct {
	From    Address
	To      Address
	Payload []byte
}

// SimulatedNetworkAPI beskriver de nätverksoperationer som en nod behöver:
// lyssna på en adress och skapa en anslutning till en annan adress.
type SimulatedNetworkAPI interface {
	Listen(addr Address) (Connection, error)
	Dial(addr Address) (Connection, error)
}

// Connection beskriver en simulerad länk.
// Samma interface används både för mottagare och sändare i testerna.
type Connection interface {
	Send(msg Message) error
	Recv() (Message, error)
	Close() error
}

// SimulatedNetwork routar meddelanden mellan adresser med Go-kanaler.
// listeners mappar varje Address till nodens mottagarkanal.
type SimulatedNetwork struct {
	mu        sync.RWMutex
	listeners map[Address]chan Message
}

// SimulatedConnection är en anslutning mot SimulatedNetwork.
// Om recvCh är satt kan anslutningen ta emot meddelanden; annars används den
// bara för att skicka.
type SimulatedConnection struct {
	addr    Address
	network *SimulatedNetwork
	recvCh  chan Message
	closed  bool
	mu      sync.RWMutex
}

const simulatedNetworkBufferSize = 1024

// NewSimulatedNetwork skapar ett tomt simulerat nätverk.
func NewSimulatedNetwork() *SimulatedNetwork {
	return &SimulatedNetwork{
		listeners: make(map[Address]chan Message),
	}
}

// Listen registrerar en adress så att noden kan ta emot meddelanden.
// Den returnerade Connection har en recvCh och fungerar som nodens inbox.
func (n *SimulatedNetwork) Listen(addr Address) (Connection, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if _, exists := n.listeners[addr]; exists {
		return nil, fmt.Errorf("address already in use: %+v", addr)
	}

	recvCh := make(chan Message, simulatedNetworkBufferSize)

	// Adressen blir söknyckeln som Send använder för att hitta rätt inbox.
	n.listeners[addr] = recvCh

	return &SimulatedConnection{
		addr:    addr,
		network: n,
		recvCh:  recvCh,
	}, nil
}

// Dial skapar en sändaranslutning till en registrerad adress.
// Den öppnar ingen riktig socket; den kontrollerar bara att mottagaren finns.
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

// Send lägger meddelandet i mottagarens kanal.
// Det simulerar nätverksleverans utan UDP, vilket gör lookup-tester snabbare.
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

	// Icke-blockerande send: om inboxen är full returnerar vi fel istället för
	// att testet hänger.
	select {
	case recvCh <- msg:
		return nil
	default:
		return fmt.Errorf("message queue full for destination: %+v", msg.To)
	}
}

// Recv väntar på nästa meddelande i anslutningens inbox.
// Bara connections skapade via Listen har en recvCh.
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

// Close stänger anslutningen.
// För lyssnare tas adressen bort från nätverket och inbox-kanalen stängs.
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
