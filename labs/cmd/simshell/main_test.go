package main

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"d7024e/kademlia"
)

func TestNodeIDUsesHashOfIPPipePort(t *testing.T) {
	address := "127.0.0.1:8000"
	sum := sha256.Sum256([]byte("127.0.0.1|8000"))
	want := hex.EncodeToString(sum[:])

	if got := hashID(address).String(); got != want {
		t.Fatalf("hashID(%q) = %s, want hash(IP|port) %s", address, got, want)
	}
}

func TestNewNodeUsesHashOfIPPipePortAsNodeID(t *testing.T) {
	node, contact, transport, err := newNode("127.0.0.1:9000", "127.0.0.1:9000", kademlia.NewSimulatedNodeWithNetwork(kademlia.NewSimulatedNetwork()))
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()

	sum := sha256.Sum256([]byte("127.0.0.1|9000"))
	want := hex.EncodeToString(sum[:])
	if contact.ID.String() != want {
		t.Fatalf("contact ID = %s, want %s", contact.ID.String(), want)
	}
	if node.RoutingTable.Me().ID.String() != want {
		t.Fatalf("routing table local ID = %s, want %s", node.RoutingTable.Me().ID.String(), want)
	}
}

func TestNewNodeCanBindDifferentAddressThanItAdvertises(t *testing.T) {
	node, contact, transport, err := newNode("127.0.0.1:9001", "bootstrap:9001", kademlia.NewSimulatedNodeWithNetwork(kademlia.NewSimulatedNetwork()))
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()

	sum := sha256.Sum256([]byte("bootstrap|9001"))
	want := hex.EncodeToString(sum[:])
	if contact.Address != "bootstrap:9001" {
		t.Fatalf("contact address = %s, want bootstrap:9001", contact.Address)
	}
	if contact.ID.String() != want {
		t.Fatalf("contact ID = %s, want %s", contact.ID.String(), want)
	}
	if node.RoutingTable.Me().Address != "bootstrap:9001" {
		t.Fatalf("routing table address = %s, want bootstrap:9001", node.RoutingTable.Me().Address)
	}
}
