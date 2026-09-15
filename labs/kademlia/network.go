package kademlia

import (
	"errors"
	"fmt"
	"net"
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
