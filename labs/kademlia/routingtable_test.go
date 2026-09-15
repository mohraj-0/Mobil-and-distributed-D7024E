package kademlia

import (
	"testing"
)

func TestRoutingTable(t *testing.T) {
	t.Log("testing RoutingTable.AddContact and RoutingTable.FindClosestContacts")

	me := NewContact(NewKademliaID("FFFFFFFF00000000000000000000000000000000000000000000000000000000"), "localhost:8000")
	rt := NewRoutingTable(me)
	t.Logf("local routing table owner: %s", me.String())

	contactsToAdd := []Contact{
		NewContact(NewKademliaID("FFFFFFFF00000000000000000000000000000000000000000000000000000000"), "localhost:8001"),
		NewContact(NewKademliaID("1111111100000000000000000000000000000000000000000000000000000000"), "localhost:8002"),
		NewContact(NewKademliaID("1111111200000000000000000000000000000000000000000000000000000000"), "localhost:8002"),
		NewContact(NewKademliaID("1111111300000000000000000000000000000000000000000000000000000000"), "localhost:8002"),
		NewContact(NewKademliaID("1111111400000000000000000000000000000000000000000000000000000000"), "localhost:8002"),
		NewContact(NewKademliaID("2111111400000000000000000000000000000000000000000000000000000000"), "localhost:8002"),
	}

	for _, contact := range contactsToAdd {
		rt.AddContact(contact)
		t.Logf("example added contact to routing table: %s", contact.String())
	}

	target := NewKademliaID("2111111400000000000000000000000000000000000000000000000000000000")
	contacts := rt.FindClosestContacts(target, 20)
	t.Logf("looking for contacts closest to target: %s", target)

	if len(contacts) != len(contactsToAdd) {
		t.Fatalf("FindClosestContacts returned %d contacts, want %d", len(contacts), len(contactsToAdd))
	}
	if !contacts[0].ID.Equals(target) {
		t.Fatalf("closest contact = %s, want target %s", contacts[0].ID, target)
	}

	for i := range contacts {
		distance := contacts[i].ID.CalcDistance(target)
		t.Logf("example closest result %d: %s distance=%s", i, contacts[i].String(), distance)
	}
}
