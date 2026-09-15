package kademlia_test

import (
	"net"
	"testing"
	"time"

	"d7024e/kademlia"
)

func TestSendPingMessage(t *testing.T) {
	t.Log("testing that SendPingMessage sends a UDP PING message to a contact address")

	address, received, closeReceiver := startUDPReceiver(t)
	defer closeReceiver()

	contact := kademlia.NewContact(testNetworkKademliaID("ff"), address)
	network := kademlia.Network{}

	if err := network.SendPingMessage(&contact); err != nil {
		t.Fatalf("send PING: %v", err)
	}
	t.Logf("example sent: to=%s payload=%q", address, "PING")

	got := waitForUDPMessage(t, received)
	if got != "PING" {
		t.Fatalf("received payload %q, want %q", got, "PING")
	}
	t.Logf("example received: from UDP receiver payload=%q", got)
}

func TestSendFindContactMessage(t *testing.T) {
	t.Log("testing that SendFindContactMessage sends a UDP FIND_CONTACT message")

	address, received, closeReceiver := startUDPReceiver(t)
	defer closeReceiver()

	contact := kademlia.NewContact(testNetworkKademliaID("11"), address)
	network := kademlia.Network{}

	if err := network.SendFindContactMessage(&contact); err != nil {
		t.Fatalf("send FIND_CONTACT: %v", err)
	}
	t.Logf("example sent: to=%s payload=%q", address, "FIND_CONTACT")

	got := waitForUDPMessage(t, received)
	if got != "FIND_CONTACT" {
		t.Fatalf("received payload %q, want %q", got, "FIND_CONTACT")
	}
	t.Logf("example received: from UDP receiver payload=%q", got)
}

func TestSendFindDataMessage(t *testing.T) {
	t.Log("testing that SendFindDataMessage sends FIND_DATA followed by the requested hash")

	address, received, closeReceiver := startUDPReceiver(t)
	defer closeReceiver()

	network := kademlia.Network{Address: address}
	hash := "testhash"
	want := "FIND_DATA " + hash

	if err := network.SendFindDataMessage(hash); err != nil {
		t.Fatalf("send FIND_DATA: %v", err)
	}
	t.Logf("example sent: to=%s payload=%q", address, want)

	got := waitForUDPMessage(t, received)
	if got != want {
		t.Fatalf("received payload %q, want %q", got, want)
	}
	t.Logf("example received: from UDP receiver payload=%q", got)
}

func TestSendStoreMessage(t *testing.T) {
	t.Log("testing that SendStoreMessage sends STORE followed by the bytes to store")

	address, received, closeReceiver := startUDPReceiver(t)
	defer closeReceiver()

	network := kademlia.Network{Address: address}
	data := []byte("Hello Kademlia")
	want := "STORE " + string(data)

	if err := network.SendStoreMessage(data); err != nil {
		t.Fatalf("send STORE: %v", err)
	}
	t.Logf("example sent: to=%s payload=%q", address, want)

	got := waitForUDPMessage(t, received)
	if got != want {
		t.Fatalf("received payload %q, want %q", got, want)
	}
	t.Logf("example received: from UDP receiver payload=%q", got)
}

func TestNetworkSendErrors(t *testing.T) {
	t.Log("testing that network send methods return useful errors for invalid inputs")

	network := kademlia.Network{}

	if err := network.SendPingMessage(nil); err == nil {
		t.Fatal("SendPingMessage(nil) returned nil error, want an error")
	} else {
		t.Logf("example rejected nil contact for PING: %v", err)
	}

	if err := network.SendFindContactMessage(nil); err == nil {
		t.Fatal("SendFindContactMessage(nil) returned nil error, want an error")
	} else {
		t.Logf("example rejected nil contact for FIND_CONTACT: %v", err)
	}

	badContact := kademlia.NewContact(testNetworkKademliaID("22"), "bad address")
	if err := network.SendPingMessage(&badContact); err == nil {
		t.Fatal("SendPingMessage with bad address returned nil error, want an error")
	} else {
		t.Logf("example rejected invalid address %q: %v", badContact.Address, err)
	}
}

func startUDPReceiver(t *testing.T) (string, <-chan string, func()) {
	t.Helper()

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("start UDP receiver: %v", err)
	}

	received := make(chan string, 1)
	go func() {
		buffer := make([]byte, 1024)
		n, _, err := conn.ReadFrom(buffer)
		if err != nil {
			return
		}
		received <- string(buffer[:n])
	}()

	return conn.LocalAddr().String(), received, func() {
		_ = conn.Close()
	}
}

func waitForUDPMessage(t *testing.T, received <-chan string) string {
	t.Helper()

	select {
	case message := <-received:
		return message
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for UDP message")
		return ""
	}
}

func testNetworkKademliaID(prefix string) *kademlia.KademliaID {
	return kademlia.NewKademliaID(prefix + "00000000000000000000000000000000000000000000000000000000000000")
}
