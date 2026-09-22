package kademlia_test

import (
	"testing"

	"d7024e/kademlia"
)

func TestNewContact(t *testing.T) {
	id := kademlia.NewKademliaID(
		"0100000000000000000000000000000000000000000000000000000000000000",
	)

	contact := kademlia.NewContact(
		id,
		"127.0.0.1:8000",
	)

	if contact.ID == nil {
		t.Fatal("contact ID is nil")
	}

	if !contact.ID.Equals(id) {
		t.Fatal("contact ID does not match")
	}

	if contact.Address != "127.0.0.1:8000" {
		t.Fatalf(
			"address = %q, want %q",
			contact.Address,
			"127.0.0.1:8000",
		)
	}
}

func TestContactCalcDistanceAndLess(t *testing.T) {
	target := kademlia.NewKademliaID(
		"0000000000000000000000000000000000000000000000000000000000000000",
	)

	near := kademlia.NewContact(
		kademlia.NewKademliaID(
			"0100000000000000000000000000000000000000000000000000000000000000",
		),
		"node-near",
	)

	far := kademlia.NewContact(
		kademlia.NewKademliaID(
			"8000000000000000000000000000000000000000000000000000000000000000",
		),
		"node-far",
	)

	near.CalcDistance(target)
	far.CalcDistance(target)

	if !near.Less(&far) {
		t.Fatal("near contact should be closer than far contact")
	}

	if far.Less(&near) {
		t.Fatal("far contact should not be closer than near contact")
	}
}

func TestContactString(t *testing.T) {
	id := kademlia.NewKademliaID(
		"0100000000000000000000000000000000000000000000000000000000000000",
	)

	contact := kademlia.NewContact(
		id,
		"127.0.0.1:8000",
	)

	got := contact.String()

	want := `contact("0100000000000000000000000000000000000000000000000000000000000000", "127.0.0.1:8000")`

	if got != want {
		t.Fatalf(
			"String() = %q, want %q",
			got,
			want,
		)
	}
}

func TestContactCandidatesSort(t *testing.T) {
	target := kademlia.NewKademliaID(
		"0000000000000000000000000000000000000000000000000000000000000000",
	)

	contact1 := kademlia.NewContact(
		kademlia.NewKademliaID(
			"8000000000000000000000000000000000000000000000000000000000000000",
		),
		"node-1",
	)

	contact2 := kademlia.NewContact(
		kademlia.NewKademliaID(
			"0100000000000000000000000000000000000000000000000000000000000000",
		),
		"node-2",
	)

	contact3 := kademlia.NewContact(
		kademlia.NewKademliaID(
			"4000000000000000000000000000000000000000000000000000000000000000",
		),
		"node-3",
	)

	contact1.CalcDistance(target)
	contact2.CalcDistance(target)
	contact3.CalcDistance(target)

	candidates := []kademlia.Contact{
		contact1,
		contact2,
		contact3,
	}

	// Vi använder routing table indirekt för att verifiera sorteringsordningen.
	me := kademlia.NewContact(
		kademlia.NewKademliaID(
			"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		),
		"me",
	)

	rt := kademlia.NewRoutingTable(me)

	for _, contact := range candidates {
		rt.AddContact(contact)
	}

	got := rt.FindClosestContacts(target, 3)

	if len(got) != 3 {
		t.Fatalf(
			"got %d contacts, want 3",
			len(got),
		)
	}

	if got[0].Address != "node-2" {
		t.Fatalf(
			"closest contact = %q, want node-2",
			got[0].Address,
		)
	}

	if got[1].Address != "node-3" {
		t.Fatalf(
			"second contact = %q, want node-3",
			got[1].Address,
		)
	}

	if got[2].Address != "node-1" {
		t.Fatalf(
			"third contact = %q, want node-1",
			got[2].Address,
		)
	}
}

func TestContactCalcDistanceNilTarget(t *testing.T) {
	contact := kademlia.NewContact(
		kademlia.NewKademliaID(
			"0100000000000000000000000000000000000000000000000000000000000000",
		),
		"node",
	)

	// Ska inte krascha.
	contact.CalcDistance(nil)
}

func TestContactLessWithNil(t *testing.T) {
	contact := kademlia.NewContact(
		kademlia.NewKademliaID(
			"0100000000000000000000000000000000000000000000000000000000000000",
		),
		"node",
	)

	if contact.Less(nil) {
		t.Fatal("Less(nil) should return false")
	}
}

func TestNilContactString(t *testing.T) {
	var contact *kademlia.Contact

	got := contact.String()

	if got != `contact("<nil>", "")` {
		t.Fatalf(
			"nil contact String() = %q",
			got,
		)
	}
}
