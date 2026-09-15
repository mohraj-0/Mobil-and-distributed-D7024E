package kademlia

import (
	"errors"
	"fmt"
	"net"
)

const defaultNetworkAddress = "127.0.0.1:8000"

// Network sends Kademlia UDP control messages to other nodes.
type Network struct {
	Address string
}

// Listen starts a UDP listener and prints every received message.
func Listen(ip string, port int) {
	address := fmt.Sprintf("%s:%d", ip, port)

	udpAddr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		fmt.Println("Error resolving UDP address:", err)
		return
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		fmt.Println("Error listening on UDP:", err)
		return
	}
	defer conn.Close()

	fmt.Println("Listening on", address)

	buffer := make([]byte, 1024)
	for {
		n, remoteAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Println("Error reading from UDP:", err)
			continue
		}

		fmt.Println("Message from:", remoteAddr)
		fmt.Println("Message:", string(buffer[:n]))
	}
}

func (network *Network) SendPingMessage(contact *Contact) error {
	if contact == nil {
		return errors.New("contact is nil")
	}

	if err := network.sendUDPMessage(contact.Address, []byte("PING")); err != nil {
		fmt.Println("Error sending PING:", err)
		return err
	}

	fmt.Println("PING sent to", contact.Address)
	return nil
}

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

func (network *Network) SendFindDataMessage(hash string) error {
	message := []byte("FIND_DATA " + hash)
	if err := network.sendUDPMessage(network.destinationAddress(), message); err != nil {
		fmt.Println("Error sending FIND_DATA:", err)
		return err
	}

	fmt.Println("FIND_DATA sent for hash:", hash)
	return nil
}

func (network *Network) SendStoreMessage(data []byte) error {
	message := append([]byte("STORE "), data...)
	if err := network.sendUDPMessage(network.destinationAddress(), message); err != nil {
		fmt.Println("Error sending STORE:", err)
		return err
	}

	fmt.Println("STORE message sent")
	return nil
}

func (network *Network) destinationAddress() string {
	if network.Address != "" {
		return network.Address
	}
	return defaultNetworkAddress
}

func (network *Network) sendUDPMessage(address string, message []byte) error {
	udpAddr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return fmt.Errorf("resolve UDP address %q: %w", address, err)
	}

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
