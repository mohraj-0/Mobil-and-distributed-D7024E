package kademlia

import (
	"testing"
	"time"
)

// Testar att Listen kan starta på en UDP-port.
func TestListen(t *testing.T) {
	go Listen("127.0.0.1", 9000)

	// Ger listenern lite tid att starta.
	time.Sleep(100 * time.Millisecond)
}

// Testar att SendPingMessage kan skicka ett PING.
func TestSendPingMessage(t *testing.T) {
	go Listen("127.0.0.1", 9001)

	time.Sleep(100 * time.Millisecond)

	id := NewKademliaID(
		"FFFFFFFF00000000000000000000000000000000000000000000000000000000",
	)

	contact := NewContact(id, "127.0.0.1:9001")

	network := Network{}

	network.SendPingMessage(&contact)
}

// Testar att SendFindContactMessage kan skicka ett FIND_CONTACT.
func TestSendFindContactMessage(t *testing.T) {
	go Listen("127.0.0.1", 9002)

	time.Sleep(100 * time.Millisecond)

	id := NewKademliaID(
		"1111111100000000000000000000000000000000000000000000000000000000",
	)

	contact := NewContact(id, "127.0.0.1:9002")

	network := Network{}

	network.SendFindContactMessage(&contact)
}

// Testar att SendFindDataMessage går att köra.
func TestSendFindDataMessage(t *testing.T) {
	network := Network{}

	network.SendFindDataMessage("testhash")
}

// Testar att SendStoreMessage går att köra.
func TestSendStoreMessage(t *testing.T) {
	network := Network{}

	data := []byte("Hello Kademlia")

	network.SendStoreMessage(data)
}
