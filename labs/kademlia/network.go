package kademlia

import (
	"errors"
	"fmt"
	"net"
	"sync"
)

const defaultNetworkAddress = "127.0.0.1:8000"

// Network skickar Kademlia-kontrollmeddelanden över UDP.
// Address används av FIND_DATA och STORE som mottagaradress; om den är tom
// används defaultNetworkAddress.
type Network struct {
	Address string
}

// Listen startar en UDP-lyssnare för en nod.
// Funktionen blockerar i en loop och skriver ut varje mottaget UDP-meddelande.
func Listen(ip string, port int) {
	address := fmt.Sprintf("%s:%d", ip, port)

	// ResolveUDPAddr gör textadressen, t.ex. "127.0.0.1:8000",
	// till en UDP-adress som net-paketet kan binda till.
	udpAddr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		fmt.Println("Error resolving UDP address:", err)
		return
	}

	// ListenUDP öppnar porten så att andra noder kan skicka UDP-paket hit.
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		fmt.Println("Error listening on UDP:", err)
		return
	}
	defer conn.Close()

	fmt.Println("Listening on", address)

	buffer := make([]byte, 1024)
	for {
		// ReadFromUDP väntar tills ett UDP-paket kommer in.
		// remoteAddr är avsändarens adress och buffer[:n] är själva meddelandet.
		n, remoteAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Println("Error reading from UDP:", err)
			continue
		}

		fmt.Println("Message from:", remoteAddr)
		fmt.Println("Message:", string(buffer[:n]))
	}
}

// SendPingMessage skickar ett PING till en kontakt.
// PING används för att kontrollera att en annan nod går att nå.
func (network *Network) SendPingMessage(contact *Contact) error {
	if contact == nil {
		return errors.New("contact is nil")
	}

	// Kontaktens Address är mottagaren, t.ex. "127.0.0.1:8001".
	if err := network.sendUDPMessage(contact.Address, []byte("PING")); err != nil {
		fmt.Println("Error sending PING:", err)
		return err
	}

	fmt.Println("PING sent to", contact.Address)
	return nil
}

// SendFindContactMessage skickar FIND_CONTACT till en kontakt.
// I en full Kademlia-implementation skulle mottagaren svara med noder som
// ligger nära ett target-ID. Här skickas bara kontrollmeddelandet.
func (network *Network) SendFindContactMessage(contact *Contact) error {
	if contact == nil {
		return errors.New("contact is nil")
	}

	if err := network.sendUDPMessage(contact.Address, []byte("FIND_CONTACT")); err != nil {
		fmt.Println("Error sending FIND_CONTACT:", err)
		return err
	}

	fmt.Println("FIND_CONTACT sent to", contact.Address)
	return nil
}

// SendFindDataMessage skickar FIND_DATA följt av en hash/key.
// Hashen fungerar som target-ID när man letar efter data i Kademlia-ID-rymden.
func (network *Network) SendFindDataMessage(hash string) error {
	message := []byte("FIND_DATA " + hash)
	if err := network.sendUDPMessage(network.destinationAddress(), message); err != nil {
		fmt.Println("Error sending FIND_DATA:", err)
		return err
	}

	fmt.Println("FIND_DATA sent for hash:", hash)
	return nil
}

// SendStoreMessage skickar STORE följt av bytes som ska lagras.
// Den här funktionen skickar bara meddelandet; den implementerar inte en lokal datastore.
func (network *Network) SendStoreMessage(data []byte) error {
	message := append([]byte("STORE "), data...)
	if err := network.sendUDPMessage(network.destinationAddress(), message); err != nil {
		fmt.Println("Error sending STORE:", err)
		return err
	}

	fmt.Println("STORE message sent")
	return nil
}

// destinationAddress väljer mottagare för meddelanden som inte har en Contact.
// Detta gör tester enklare eftersom testet kan sätta Network.Address till en
// tillfällig UDP-port.
func (network *Network) destinationAddress() string {
	if network.Address != "" {
		return network.Address
	}
	return defaultNetworkAddress
}

// sendUDPMessage är den gemensamma lågnivåfunktionen för alla UDP-sändningar.
// De publika Send...-funktionerna bygger först rätt payload och skickar sedan hit.
func (network *Network) sendUDPMessage(address string, message []byte) error {
	udpAddr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return fmt.Errorf("resolve UDP address %q: %w", address, err)
	}

	// DialUDP skapar en UDP-anslutning till mottagaren. UDP är connectionless,
	// men Go använder conn-objektet för Write-anropet.
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return fmt.Errorf("connect to UDP address %q: %w", address, err)
	}
	defer conn.Close()

	if _, err := conn.Write(message); err != nil {
		return fmt.Errorf("write UDP message to %q: %w", address, err)
	}

	return nil
}

// Address identifies a node in the in-memory network used by tests and the CLI.
// It mirrors a UDP address without opening a socket.
type Address struct {
	IP   string
	Port int
}

// Message is the common message envelope for the in-memory network.
type Message struct {
	From    Address
	To      Address
	Payload []byte
}

// SimulatedNetworkAPI describes the operations a node needs from a network:
// listening on an address and dialing another address.
type SimulatedNetworkAPI interface {
	Listen(addr Address) (Connection, error)
	Dial(addr Address) (Connection, error)
}

// Connection is an in-memory link. Listener connections can receive messages;
// dialed connections are used for sending.
type Connection interface {
	Send(msg Message) error
	Recv() (Message, error)
	Close() error
}

// SimulatedNetwork routes messages between addresses with Go channels.
type SimulatedNetwork struct {
	mu        sync.RWMutex
	listeners map[Address]chan Message
}

// SimulatedConnection is a connection against SimulatedNetwork.
type SimulatedConnection struct {
	addr    Address
	network *SimulatedNetwork
	recvCh  chan Message
	closed  bool
	mu      sync.RWMutex
}

const simulatedNetworkBufferSize = 1024

// NewSimulatedNetwork creates an empty in-memory network.
func NewSimulatedNetwork() *SimulatedNetwork {
	return &SimulatedNetwork{
		listeners: make(map[Address]chan Message),
	}
}

// Listen registers an address so it can receive in-memory messages.
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

// Dial creates a sending connection to a registered in-memory address.
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

// Send places a message in the destination listener's channel.
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

// Recv waits for the next message on a listener connection.
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

// Close closes the connection. Listener connections are removed from the network.
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
