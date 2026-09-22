package kademlia_test

import (
	"net"
	"testing"
	"time"

	"d7024e/kademlia"
)

// Testar att ett UDPNode kan skicka ett PING-meddelande.
func TestSendPingMessage(t *testing.T) {
	address, received, closeReceiver := startUDPReceiver(t)
	defer closeReceiver()

	node := kademlia.NewUDPNode()

	if err := node.Listen("127.0.0.1:0"); err != nil {
		t.Fatalf("start UDP node: %v", err)
	}
	defer node.Close()

	if err := node.SendData(address, []byte("PING")); err != nil {
		t.Fatalf("send PING: %v", err)
	}

	got := waitForUDPMessage(t, received)

	if got != "PING" {
		t.Fatalf("received %q, want %q", got, "PING")
	}
}

// Testar FIND_CONTACT.
func TestSendFindContactMessage(t *testing.T) {
	address, received, closeReceiver := startUDPReceiver(t)
	defer closeReceiver()

	node := kademlia.NewUDPNode()

	if err := node.Listen("127.0.0.1:0"); err != nil {
		t.Fatalf("start UDP node: %v", err)
	}
	defer node.Close()

	want := "FIND_CONTACT"

	if err := node.SendData(address, []byte(want)); err != nil {
		t.Fatalf("send FIND_CONTACT: %v", err)
	}

	got := waitForUDPMessage(t, received)

	if got != want {
		t.Fatalf("received %q, want %q", got, want)
	}
}

// Testar FIND_DATA.
func TestSendFindDataMessage(t *testing.T) {
	address, received, closeReceiver := startUDPReceiver(t)
	defer closeReceiver()

	node := kademlia.NewUDPNode()

	if err := node.Listen("127.0.0.1:0"); err != nil {
		t.Fatalf("start UDP node: %v", err)
	}
	defer node.Close()

	hash := "testhash"
	want := "FIND_DATA " + hash

	if err := node.SendData(address, []byte(want)); err != nil {
		t.Fatalf("send FIND_DATA: %v", err)
	}

	got := waitForUDPMessage(t, received)

	if got != want {
		t.Fatalf("received %q, want %q", got, want)
	}
}

// Testar STORE.
func TestSendStoreMessage(t *testing.T) {
	address, received, closeReceiver := startUDPReceiver(t)
	defer closeReceiver()

	node := kademlia.NewUDPNode()

	if err := node.Listen("127.0.0.1:0"); err != nil {
		t.Fatalf("start UDP node: %v", err)
	}
	defer node.Close()

	want := "STORE Hello Kademlia"

	if err := node.SendData(address, []byte(want)); err != nil {
		t.Fatalf("send STORE: %v", err)
	}

	got := waitForUDPMessage(t, received)

	if got != want {
		t.Fatalf("received %q, want %q", got, want)
	}
}

// Testar att UDPNode själv kan ta emot ett riktigt UDP-paket.
func TestUDPNodeReceive(t *testing.T) {
	address := freeUDPAddress(t)

	node := kademlia.NewUDPNode()

	if err := node.Listen(address); err != nil {
		t.Fatalf("start UDP node: %v", err)
	}
	defer node.Close()

	sender, err := net.Dial("udp", address)
	if err != nil {
		t.Fatalf("dial UDP node: %v", err)
	}
	defer sender.Close()

	want := "hello-node"

	if _, err := sender.Write([]byte(want)); err != nil {
		t.Fatalf("write UDP message: %v", err)
	}

	message, err := node.Receive()
	if err != nil {
		t.Fatalf("receive UDP message: %v", err)
	}

	if string(message.Data) != want {
		t.Fatalf(
			"received data %q, want %q",
			string(message.Data),
			want,
		)
	}

	if message.From == "" {
		t.Fatal("received message has empty sender address")
	}
}

// Receive innan Listen ska ge fel.
func TestUDPNodeReceiveBeforeListen(t *testing.T) {
	node := kademlia.NewUDPNode()

	if _, err := node.Receive(); err == nil {
		t.Fatal("Receive before Listen returned nil error")
	}
}

// SendData innan Listen ska ge fel.
func TestUDPNodeSendBeforeListen(t *testing.T) {
	node := kademlia.NewUDPNode()

	err := node.SendData(
		"127.0.0.1:8000",
		[]byte("PING"),
	)

	if err == nil {
		t.Fatal("SendData before Listen returned nil error")
	}
}

// Testar felaktiga destinationer.
func TestUDPNodeSendErrors(t *testing.T) {
	node := kademlia.NewUDPNode()

	if err := node.Listen("127.0.0.1:0"); err != nil {
		t.Fatalf("start UDP node: %v", err)
	}
	defer node.Close()

	if err := node.SendData("", []byte("PING")); err == nil {
		t.Fatal("empty destination returned nil error")
	}

	if err := node.SendData("bad address", []byte("PING")); err == nil {
		t.Fatal("invalid destination returned nil error")
	}
}

// Listen med tom adress ska ge fel.
func TestUDPNodeListenEmptyAddress(t *testing.T) {
	node := kademlia.NewUDPNode()

	if err := node.Listen(""); err == nil {
		t.Fatal("Listen with empty address returned nil error")
	}
}

// Listen med felaktig adress ska ge fel.
func TestUDPNodeListenInvalidAddress(t *testing.T) {
	node := kademlia.NewUDPNode()

	if err := node.Listen("bad address"); err == nil {
		t.Fatal("Listen with invalid address returned nil error")
	}
}

// Close innan Listen ska fungera.
func TestUDPNodeCloseBeforeListen(t *testing.T) {
	node := kademlia.NewUDPNode()

	if err := node.Close(); err != nil {
		t.Fatalf("Close before Listen returned error: %v", err)
	}
}

// Close efter Listen ska fungera.
func TestUDPNodeClose(t *testing.T) {
	node := kademlia.NewUDPNode()

	if err := node.Listen("127.0.0.1:0"); err != nil {
		t.Fatalf("start UDP node: %v", err)
	}

	if err := node.Close(); err != nil {
		t.Fatalf("close UDP node: %v", err)
	}
}

// Skapar en vanlig UDP-mottagare för SendData-tester.
func startUDPReceiver(
	t *testing.T,
) (
	string,
	<-chan string,
	func(),
) {
	t.Helper()

	conn, err := net.ListenPacket(
		"udp",
		"127.0.0.1:0",
	)

	if err != nil {
		t.Fatalf("start UDP receiver: %v", err)
	}

	received := make(chan string, 1)

	go func() {
		buffer := make([]byte, 4096)

		n, _, err := conn.ReadFrom(buffer)
		if err != nil {
			return
		}

		received <- string(buffer[:n])
	}()

	return conn.LocalAddr().String(),
		received,
		func() {
			_ = conn.Close()
		}
}

// Hittar en ledig lokal UDP-adress.
func freeUDPAddress(t *testing.T) string {
	t.Helper()

	conn, err := net.ListenPacket(
		"udp",
		"127.0.0.1:0",
	)

	if err != nil {
		t.Fatalf("find free UDP address: %v", err)
	}

	address := conn.LocalAddr().String()

	_ = conn.Close()

	return address
}

// Väntar högst en sekund på UDP-meddelande.
func waitForUDPMessage(
	t *testing.T,
	received <-chan string,
) string {
	t.Helper()

	select {
	case message := <-received:
		return message

	case <-time.After(time.Second):
		t.Fatal("timed out waiting for UDP message")
		return ""
	}
}
