package kademlia_test

import (
	"testing"

	"d7024e/kademlia"
)

func TestLookupContactWithoutRoutingTable(t *testing.T) {
	t.Log("testing that LookupContact handles a missing routing table without crashing")

	node := &kademlia.Kademlia{}
	target := kademlia.NewContact(kademliaTestID("ff"), "127.0.0.1:8000")

	if contacts := node.LookupContact(&target); contacts != nil {
		t.Fatalf("LookupContact without routing table returned %d contacts, want nil", len(contacts))
	}
}

func TestLookupContactUsesLocalRoutingTable(t *testing.T) {
	t.Log("testing that LookupContact returns closest contacts from the local routing table")

	me := kademlia.NewContact(kademliaTestID("ff"), "127.0.0.1:8000")
	target := kademlia.NewContact(kademliaTestID("11"), "127.0.0.1:8001")
	node := &kademlia.Kademlia{RoutingTable: kademlia.NewRoutingTable(me)}

	node.RoutingTable.AddContact(target)

	contacts := node.LookupContact(&target)
	if len(contacts) != 1 {
		t.Fatalf("LookupContact returned %d contacts, want 1", len(contacts))
	}
	if !contacts[0].ID.Equals(target.ID) {
		t.Fatalf("closest contact = %s, want target %s", contacts[0].ID, target.ID)
	}

	t.Logf("example lookup result: target=%s closest=%s", target.ID, contacts[0].ID)
}

func TestLookupContactSequentialAlphaOneFindsCloserContacts(t *testing.T) {
	t.Log("testing iterative lookup with alpha=1 over fake routing tables")

	me := kademlia.NewContact(kademliaTestID("ff"), "127.0.0.1:8000")
	firstHop := kademlia.NewContact(kademliaTestID("80"), "127.0.0.1:8001")
	secondHop := kademlia.NewContact(kademliaTestID("40"), "127.0.0.1:8002")
	target := kademlia.NewContact(kademliaTestID("00"), "127.0.0.1:8003")

	node := &kademlia.Kademlia{RoutingTable: kademlia.NewRoutingTable(me)}
	node.RoutingTable.AddContact(firstHop)
	t.Logf("starting table only knows first hop: %s", firstHop.ID)

	firstHopTable := kademlia.NewRoutingTable(firstHop)
	firstHopTable.AddContact(secondHop)
	t.Logf("first queried node returns a closer second hop: %s", secondHop.ID)

	secondHopTable := kademlia.NewRoutingTable(secondHop)
	secondHopTable.AddContact(target)
	t.Logf("second queried node returns the target contact: %s", target.ID)

	queried := make([]string, 0, 2)
	fakeLookup := kademlia.NewFakeContactLookup(map[string]*kademlia.RoutingTable{
		firstHop.ID.String():  firstHopTable,
		secondHop.ID.String(): secondHopTable,
	})
	node.ContactLookup = func(contact kademlia.Contact, targetID *kademlia.KademliaID, count int) []kademlia.Contact {
		queried = append(queried, contact.ID.String())
		t.Logf("alpha=1 query: asking %s for contacts close to %s", contact.ID, targetID)
		return fakeLookup(contact, targetID, count)
	}

	contacts := node.LookupContact(&target)
	if len(contacts) == 0 {
		t.Fatal("LookupContact returned no contacts")
	}
	if !contacts[0].ID.Equals(target.ID) {
		t.Fatalf("closest contact = %s, want target %s", contacts[0].ID, target.ID)
	}
	if len(queried) != 3 {
		t.Fatalf("queried %d contacts, want 3 sequential alpha=1 queries", len(queried))
	}
	if queried[0] != firstHop.ID.String() || queried[1] != secondHop.ID.String() || queried[2] != target.ID.String() {
		t.Fatalf("query order = %v, want first hop, second hop, target", queried)
	}

	t.Logf("example final closest contact: target=%s closest=%s", target.ID, contacts[0].ID)
}

func TestLookupDataEmptyHash(t *testing.T) {
	t.Log("testing that LookupData handles an empty hash without sending a message")

	node := &kademlia.Kademlia{}
	node.LookupData("")
}

func TestStoreEmptyData(t *testing.T) {
	t.Log("testing that Store handles empty data without sending a message")

	node := &kademlia.Kademlia{}
	node.Store([]byte{})
}

func kademliaTestID(prefix string) *kademlia.KademliaID {
	return kademlia.NewKademliaID(prefix + "00000000000000000000000000000000000000000000000000000000000000")
}
