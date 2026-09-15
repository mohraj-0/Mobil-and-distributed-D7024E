package kademlia

import "testing"

// Testar att LookupContact klarar av en tom routing table.
func TestLookupContactWithoutRoutingTable(t *testing.T) {
	k := &Kademlia{}

	id := NewKademliaID(
		"FFFFFFFF00000000000000000000000000000000000000000000000000000000",
	)

	contact := NewContact(id, "127.0.0.1:8000")

	// Ska inte krascha även om RoutingTable är nil
	k.LookupContact(&contact)
}

// Testar LookupContact med en riktig routing table.
func TestLookupContact(t *testing.T) {
	myID := NewKademliaID(
		"FFFFFFFF00000000000000000000000000000000000000000000000000000000",
	)

	me := NewContact(myID, "127.0.0.1:8000")

	k := &Kademlia{
		RoutingTable: NewRoutingTable(me),
	}

	targetID := NewKademliaID(
		"1111111100000000000000000000000000000000000000000000000000000000",
	)

	target := NewContact(targetID, "127.0.0.1:8001")

	k.RoutingTable.AddContact(target)

	// Ska kunna köra utan att krascha
	k.LookupContact(&target)
}

// Testar att LookupData klarar en tom hash.
func TestLookupDataEmptyHash(t *testing.T) {
	k := &Kademlia{}

	// Ska inte krascha
	k.LookupData("")
}

// Testar att Store klarar tom data.
func TestStoreEmptyData(t *testing.T) {
	k := &Kademlia{}

	// Ska inte krascha
	k.Store([]byte{})
}
