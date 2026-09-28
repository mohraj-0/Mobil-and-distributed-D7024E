package kademlia

import (
	"fmt"
	"testing"
)

// Testar att routing table kan lägga till kontakter
// och hitta de närmaste kontakterna med XOR-avstånd ( en Contact , information om en annan nod(varje contact representerar en nod))
func TestRoutingTable(t *testing.T) {
	t.Log("testing RoutingTable.AddContact and RoutingTable.FindClosestContacts")

	// Skapa en nod med ett  ID och adress
	me := NewContact(
		NewKademliaID(
			"FFFFFFFF00000000000000000000000000000000000000000000000000000000",
		),
		"localhost:8000",
	)

	// Skapa en routing table för noden
	rt := NewRoutingTable(me)

	// Kontakter som ska läggas till
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

	// Lägg till kontakterna(noder) i routing table
	for _, contact := range contactsToAdd {
		rt.AddContact(contact)
	}

	// ID som vi vill hitta närmaste kontakter till
	target := NewKademliaID(
		"2111111400000000000000000000000000000000000000000000000000000000",
	)

	// Hitta de närmaste kontakterna till target
	contacts := rt.FindClosestContacts(target, 20)

	if len(contacts) != len(contactsToAdd) {
		t.Fatalf(
			"FindClosestContacts returned %d contacts, want %d",
			len(contacts),
			len(contactsToAdd),
		)
	}

	// Kontrollera att den närmaste kontakten är den vi förväntar oss
	if !contacts[0].ID.Equals(target) {
		t.Fatalf(
			"closest contact = %s, want target %s",
			contacts[0].ID,
			target,
		)
	}

	// Kontrollera att kontakterna är sorterade efter XOR-avstånd till target
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

// Testar att FindClosestContacts bara returnerar
// det antal noder som vi ber om
func TestFindClosestContactsCountLimit(t *testing.T) {

	// Skapa en nod
	me := NewContact(
		NewKademliaID(
			"FFFFFFFF00000000000000000000000000000000000000000000000000000000",
		),
		"me",
	)

	// Skapa en routing table för noden
	rt := NewRoutingTable(me)

	// Lägg till tre kontakter (noder) i routing table
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

	// ID som vi söker närmaste noder till
	target := NewKademliaID(
		"0000000000000000000000000000000000000000000000000000000000000000",
	)

	// Hitta de två närmaste kontakterna(noder) till target
	contacts := rt.FindClosestContacts(target, 2)

	// Kontrollera att bara  två kontakter (noder) returneras
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

// Testar att routing table inte lägger till den egna noden
func TestRoutingTableDoesNotAddItself(t *testing.T) {
	meID := NewKademliaID(
		"FFFFFFFF00000000000000000000000000000000000000000000000000000000",
	)

	// Skapa den egna node
	me := NewContact(
		meID,
		"localhost:8000",
	)

	// Skapa en routing table
	rt := NewRoutingTable(me)

	//  Skapa en kontakt med samma ID som den egna noden
	self := NewContact(
		meID,
		"localhost:9000",
	)

	// Försök lägga till den egna noden
	rt.AddContact(self)

	// Sök efter noder nära det egna ID:t
	contacts := rt.FindClosestContacts(meID, 20)

	// Routing table ska inte innehålla den egna node
	if len(contacts) != 0 {
		t.Fatalf(
			"routing table contains local node, got %d contacts",
			len(contacts),
		)
	}
}

// Testar att rätt bucket väljs
func TestGetBucketIndex(t *testing.T) {
	meID := &KademliaID{}

	// Skapa en node
	me := NewContact(
		meID,
		"me",
	)

	// Skapa en routing table för noden
	rt := NewRoutingTable(me)

	// Första biten är annorlunda.
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

	// Ska hamna i sista bucketen
	if got := rt.getBucketIndex(lastBit); got != IDLength*8-1 {
		t.Fatalf(
			"bucket index = %d, want %d",
			got,
			IDLength*8-1,
		)
	}
}

// Testar att routing table kan hantera många noder

// den insert 10000 kontakter och kontrollerar att FindClosestContacts returnerar rätt antal kontakter
func TestRoutingTableHandlesManyContacts(t *testing.T) {
	const contactCount = 10000

	// Skapa en nod
	me := NewContact(
		NewRandomKademliaID(),
		"local-node",
	)

	// Skapa en routing table för noden
	rt := NewRoutingTable(me)

	// Lägg till 10 000 noder
	for i := 0; i < contactCount; i++ {
		id := NewRandomKademliaID()

		contact := NewContact(
			id,
			fmt.Sprintf("test-node-%d", i),
		)

		rt.AddContact(contact)
	}

	// Skapa ett slumpmässigt target-ID
	target := NewRandomKademliaID()

	// Hitta de närmaste noderna
	closest := rt.FindClosestContacts(
		target,
		bucketSize,
	)

	// Kontrollera att några noder hittades
	if len(closest) == 0 {
		t.Fatal("expected routing table to return contacts")
	}

	// Kontrollera att vi inte får fler än bucketSize
	if len(closest) > bucketSize {
		t.Fatalf(
			"returned %d contacts, expected at most %d",
			len(closest),
			bucketSize,
		)
	}

	// Visa resultat
	t.Logf(
		"routing table handled %d inserted contacts and returned %d closest contacts",
		contactCount,
		len(closest),
	)
}

// skapa 10 000 noder
// lägg dem i routing table
// sök efter närmaste noder
// kontrollera att resultatet ser rimligt ut
