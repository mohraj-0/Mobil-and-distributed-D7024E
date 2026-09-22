package kademlia

import (
	"crypto/sha256"
	"fmt"
)

const lookupContactCount = 10

// Kademlia representerar en lokal nods Kademlia-logik.
//
// RoutingTable innehåller de kontakter noden känner till.
//
// Network kan vara:
// - UDPNode för riktig nätverkskommunikation
// - SimulatedNode för tester och stora simulerade nätverk
//
// ContactLookup används i den förenklade lookup-algoritmen
// med fake routing tables och alpha = 1.
type Kademlia struct {
	RoutingTable *RoutingTable
	Network      Node

	ContactLookup func(
		contact Contact,
		target *KademliaID,
		count int,
	) []Contact
}

// LookupContact hittar kontakter vars node-ID ligger nära target.ID.
//
// Lookupen använder alpha = 1:
// endast en kontakt frågas åt gången.
func (kademlia *Kademlia) LookupContact(target *Contact) []Contact {
	if kademlia.RoutingTable == nil {
		fmt.Println("Routing table is not initialized")
		return nil
	}

	if target == nil || target.ID == nil {
		fmt.Println("Target is invalid")
		return nil
	}

	contacts := kademlia.lookupContactSequential(
		target.ID,
		lookupContactCount,
	)

	for _, contact := range contacts {
		fmt.Println("Found contact:", contact.String())
	}

	return contacts
}

// lookupContactSequential implementerar den förenklade
// Kademlia lookup-algoritmen.
//
// Den börjar med kontakter från den lokala routingtabellen.
// Därefter frågar den en kontakt i taget efter närmare noder.
func (kademlia *Kademlia) lookupContactSequential(
	target *KademliaID,
	count int,
) []Contact {

	if count <= 0 {
		return nil
	}

	candidates := make([]Contact, 0, count)

	// seen används för att undvika dubbletter.
	seen := make(map[string]bool)

	// queried håller reda på vilka noder som redan har frågats.
	queried := make(map[string]bool)

	// addContacts lägger till nya kontakter.
	addContacts := func(contacts []Contact) {
		for _, contact := range contacts {

			if contact.ID == nil {
				continue
			}

			key := contact.ID.String()

			if seen[key] {
				continue
			}

			// Räkna ut XOR-avståndet till target.
			contact.CalcDistance(target)

			seen[key] = true

			candidates = append(
				candidates,
				contact,
			)
		}
	}

	// Börja med vår egen routing table.
	initialContacts :=
		kademlia.RoutingTable.FindClosestContacts(
			target,
			count,
		)

	addContacts(initialContacts)

	sortContactsByDistance(candidates)

	candidates =
		firstContacts(
			candidates,
			count,
		)

	// Alpha = 1.
	//
	// Vi frågar endast en kontakt i taget.
	for {

		nextIndex :=
			firstUnqueriedContact(
				candidates,
				queried,
			)

		if nextIndex == -1 {
			break
		}

		if kademlia.ContactLookup == nil {
			break
		}

		next := candidates[nextIndex]

		queried[next.ID.String()] = true

		seenBeforeQuery := len(seen)

		// Fråga den valda noden efter kontakter
		// som ligger närmare target.
		newContacts :=
			kademlia.ContactLookup(
				next,
				target,
				count,
			)

		addContacts(newContacts)

		sortContactsByDistance(candidates)

		candidates =
			firstContacts(
				candidates,
				count,
			)

		// Om lookupen inte hittade några nya kontakter
		// och inga o-frågade kandidater finns kvar är vi färdiga.
		if len(seen) == seenBeforeQuery &&
			firstUnqueriedContact(
				candidates,
				queried,
			) == -1 {

			break
		}
	}

	return candidates
}

// sortContactsByDistance sorterar kontakterna
// efter XOR-avståndet till target.
//
// CalcDistance måste redan ha körts.
func sortContactsByDistance(contacts []Contact) {
	candidates := ContactCandidates{
		contacts: contacts,
	}

	candidates.Sort()
}

// firstContacts returnerar högst count kontakter.
func firstContacts(
	contacts []Contact,
	count int,
) []Contact {

	if count <= 0 {
		return nil
	}

	if len(contacts) <= count {
		return contacts
	}

	return contacts[:count]
}

// firstUnqueriedContact hittar den första kontakten
// som ännu inte har blivit frågad.
func firstUnqueriedContact(
	contacts []Contact,
	queried map[string]bool,
) int {

	for i, contact := range contacts {

		if contact.ID == nil {
			continue
		}

		if !queried[contact.ID.String()] {
			return i
		}
	}

	return -1
}

// NewFakeContactLookup skapar ett förenklat nätverk
// baserat på flera routing tables.
//
// Detta används för att testa lookup-algoritmen
// utan riktig UDP.
//
// tables:
// node-ID -> nodens routing table
func NewFakeContactLookup(
	tables map[string]*RoutingTable,
) func(
	Contact,
	*KademliaID,
	int,
) []Contact {

	return func(
		contact Contact,
		target *KademliaID,
		count int,
	) []Contact {

		if contact.ID == nil {
			return nil
		}

		table :=
			tables[contact.ID.String()]

		if table == nil {
			return nil
		}

		return table.FindClosestContacts(
			target,
			count,
		)
	}
}

// LookupData söker efter data med hjälp av en hash/key.
//
// Först hittar vi den kontakt som ligger närmast key:n.
// Därefter skickar vi FIND_DATA via Network.
func (kademlia *Kademlia) LookupData(hash string) {
	if hash == "" {
		fmt.Println("Hash is empty")
		return
	}

	if kademlia.Network == nil {
		fmt.Println("Network is not initialized")
		return
	}

	if kademlia.RoutingTable == nil {
		fmt.Println("Routing table is not initialized")
		return
	}

	target := NewKademliaID(hash)

	if target == nil {
		fmt.Println("Invalid hash")
		return
	}

	// Första versionen använder den närmaste
	// kontakten från routing table.
	//
	// Senare kan detta kopplas till full LookupContact.
	contacts :=
		kademlia.RoutingTable.FindClosestContacts(
			target,
			1,
		)

	if len(contacts) == 0 {
		fmt.Println("No contact found")
		return
	}

	contact := contacts[0]

	message :=
		[]byte(
			"FIND_DATA " +
				hash,
		)

	err :=
		kademlia.Network.SendData(
			contact.Address,
			message,
		)

	if err != nil {
		fmt.Println(
			"Could not send FIND_DATA:",
			err,
		)
		return
	}

	fmt.Println(
		"Looking for data with hash:",
		hash,
	)
}

// Store lagrar data i Kademlia.
//
// En SHA-256 hash skapas från datan.
// Hashen används som Kademlia-key.
//
// Därefter hittar vi noden som ligger närmast key:n
// och skickar ett STORE-meddelande dit.
func (kademlia *Kademlia) Store(data []byte) {
	if len(data) == 0 {
		fmt.Println("Data is empty")
		return
	}

	if kademlia.Network == nil {
		fmt.Println("Network is not initialized")
		return
	}

	if kademlia.RoutingTable == nil {
		fmt.Println("Routing table is not initialized")
		return
	}

	// Skapa 256-bitars key från datan.
	hash := sha256.Sum256(data)

	key := KademliaID(hash)

	// Hitta kontakten som ligger närmast key:n.
	contacts :=
		kademlia.RoutingTable.FindClosestContacts(
			&key,
			1,
		)

	if len(contacts) == 0 {
		fmt.Println(
			"No contact found for storage",
		)
		return
	}

	contact := contacts[0]

	// STORE <key> <data>
	message :=
		append(
			[]byte(
				"STORE "+
					key.String()+
					" ",
			),
			data...,
		)

	err :=
		kademlia.Network.SendData(
			contact.Address,
			message,
		)

	if err != nil {
		fmt.Println(
			"Could not send STORE:",
			err,
		)
		return
	}

	fmt.Println(
		"Data sent for storage with key:",
		key.String(),
	)
}
