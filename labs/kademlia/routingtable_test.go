package kademlia

import "testing"

func TestRoutingTable(t *testing.T) {
	t.Log("testing RoutingTable.AddContact and RoutingTable.FindClosestContacts")

	me := NewContact(
		NewKademliaID(
			"FFFFFFFF00000000000000000000000000000000000000000000000000000000",
		),
		"localhost:8000",
	)

	rt := NewRoutingTable(me)

	contactsToAdd := []Contact{
		NewContact(
			NewKademliaID(
				"1111111100000000000000000000000000000000000000000000000000000000",
			),
			"localhost:8002",
		),

		NewContact(
			NewKademliaID(
				"1111111200000000000000000000000000000000000000000000000000000000",
			),
			"localhost:8003",
		),

		NewContact(
			NewKademliaID(
				"1111111300000000000000000000000000000000000000000000000000000000",
			),
			"localhost:8004",
		),

		NewContact(
			NewKademliaID(
				"1111111400000000000000000000000000000000000000000000000000000000",
			),
			"localhost:8005",
		),

		NewContact(
			NewKademliaID(
				"2111111400000000000000000000000000000000000000000000000000000000",
			),
			"localhost:8006",
		),
	}

	for _, contact := range contactsToAdd {
		rt.AddContact(contact)
	}

	target := NewKademliaID(
		"2111111400000000000000000000000000000000000000000000000000000000",
	)

	contacts := rt.FindClosestContacts(target, 20)

	if len(contacts) != len(contactsToAdd) {
		t.Fatalf(
			"FindClosestContacts returned %d contacts, want %d",
			len(contacts),
			len(contactsToAdd),
		)
	}

	// Exakt target ska ligga först eftersom XOR-avståndet är 0.
	if !contacts[0].ID.Equals(target) {
		t.Fatalf(
			"closest contact = %s, want target %s",
			contacts[0].ID,
			target,
		)
	}

	// Kontrollera sorteringen efter XOR-avstånd.
	for i := 0; i < len(contacts)-1; i++ {
		currentDistance := contacts[i].ID.CalcDistance(target)
		nextDistance := contacts[i+1].ID.CalcDistance(target)

		if nextDistance.Less(currentDistance) {
			t.Fatalf(
				"contacts are not sorted by XOR distance at index %d",
				i,
			)
		}
	}
}

// Testar att count faktiskt begränsar antalet resultat.
func TestFindClosestContactsCountLimit(t *testing.T) {
	me := NewContact(
		NewKademliaID(
			"FFFFFFFF00000000000000000000000000000000000000000000000000000000",
		),
		"me",
	)

	rt := NewRoutingTable(me)

	rt.AddContact(NewContact(
		NewKademliaID(
			"1000000000000000000000000000000000000000000000000000000000000000",
		),
		"node-1",
	))

	rt.AddContact(NewContact(
		NewKademliaID(
			"2000000000000000000000000000000000000000000000000000000000000000",
		),
		"node-2",
	))

	rt.AddContact(NewContact(
		NewKademliaID(
			"3000000000000000000000000000000000000000000000000000000000000000",
		),
		"node-3",
	))

	target := NewKademliaID(
		"0000000000000000000000000000000000000000000000000000000000000000",
	)

	contacts := rt.FindClosestContacts(target, 2)

	if len(contacts) != 2 {
		t.Fatalf(
			"got %d contacts, want 2",
			len(contacts),
		)
	}
}

// Testar en tom routing table.
func TestFindClosestContactsEmptyTable(t *testing.T) {
	me := NewContact(
		NewKademliaID(
			"FFFFFFFF00000000000000000000000000000000000000000000000000000000",
		),
		"me",
	)

	rt := NewRoutingTable(me)

	target := NewKademliaID(
		"0000000000000000000000000000000000000000000000000000000000000000",
	)

	contacts := rt.FindClosestContacts(target, 20)

	if len(contacts) != 0 {
		t.Fatalf(
			"empty routing table returned %d contacts, want 0",
			len(contacts),
		)
	}
}

// Testar att den lokala noden inte läggs till som kontakt.
func TestRoutingTableDoesNotAddItself(t *testing.T) {
	meID := NewKademliaID(
		"FFFFFFFF00000000000000000000000000000000000000000000000000000000",
	)

	me := NewContact(
		meID,
		"localhost:8000",
	)

	rt := NewRoutingTable(me)

	// Samma ID som den lokala noden.
	self := NewContact(
		meID,
		"localhost:9000",
	)

	rt.AddContact(self)

	contacts := rt.FindClosestContacts(meID, 20)

	if len(contacts) != 0 {
		t.Fatalf(
			"routing table contains local node, got %d contacts",
			len(contacts),
		)
	}
}

// Testar bucket-index för olika XOR-avstånd.
func TestGetBucketIndex(t *testing.T) {
	meID := &KademliaID{}

	me := NewContact(
		meID,
		"me",
	)

	rt := NewRoutingTable(me)

	// Första biten skiljer sig.
	firstBit := &KademliaID{}
	firstBit[0] = 0x80

	if got := rt.getBucketIndex(firstBit); got != 0 {
		t.Fatalf(
			"bucket index = %d, want 0",
			got,
		)
	}

	// Sista biten skiljer sig.
	lastBit := &KademliaID{}
	lastBit[IDLength-1] = 0x01

	if got := rt.getBucketIndex(lastBit); got != IDLength*8-1 {
		t.Fatalf(
			"bucket index = %d, want %d",
			got,
			IDLength*8-1,
		)
	}
}
