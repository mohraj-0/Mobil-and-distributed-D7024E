package kademlia

import "fmt"

const lookupContactCount = 10

// Kademlia representerar en lokal nods Kademlia-logik.
// RoutingTable innehåller de kontakter noden redan känner till.
// ContactLookup är kopplingen till "nätverket": i tester kan den vara en fake
// routingtabell, och senare kan den bytas mot riktiga RPC-anrop.
type Kademlia struct {
	RoutingTable *RoutingTable

	// ContactLookup frågar en annan kontakt efter noder nära target-ID:t.
	ContactLookup func(contact Contact, target *KademliaID, count int) []Contact
}

// LookupContact hittar kontakter vars node-ID ligger nära target.ID.
// Den använder en iterativ lookup med alpha = 1: en kontakt frågas åt gången.
func (kademlia *Kademlia) LookupContact(target *Contact) []Contact {
	if kademlia.RoutingTable == nil {
		fmt.Println("Routing table is not initialized")
		return nil
	}

	if target == nil || target.ID == nil {
		fmt.Println("Target is invalid")
		return nil
	}

	contacts := kademlia.lookupContactSequential(target.ID, lookupContactCount)
	for _, contact := range contacts {
		fmt.Println("Found contact:", contact.String())
	}

	return contacts
}

// lookupContactSequential är själva lookup-algoritmen.
// Den börjar med de bästa kandidaterna från den lokala routingtabellen och
// frågar sedan den närmaste o-frågade kandidaten efter ännu närmare kontakter.
func (kademlia *Kademlia) lookupContactSequential(target *KademliaID, count int) []Contact {
	candidates := make([]Contact, 0, count)

	// seen hindrar samma kontakt från att läggas till flera gånger.
	// queried håller reda på vilka kandidater som redan har frågats.
	seen := make(map[string]bool)
	queried := make(map[string]bool)

	// addContacts normaliserar nya kontakter: den ignorerar nil-ID:n,
	// tar bort dubbletter och räknar ut XOR-avståndet till target.
	addContacts := func(contacts []Contact) {
		for _, contact := range contacts {
			if contact.ID == nil {
				continue
			}

			key := contact.ID.String()
			if seen[key] {
				continue
			}

			contact.CalcDistance(target)
			seen[key] = true
			candidates = append(candidates, contact)
		}
	}

	// Första kandidatlistan kommer från vår egen routingtabell.
	addContacts(kademlia.RoutingTable.FindClosestContacts(target, count))
	sortContactsByDistance(candidates)
	candidates = firstContacts(candidates, count)

	for {
		// Alpha = 1 betyder att vi bara frågar en kontakt per iteration:
		// den närmaste kontakten som inte redan har blivit frågad.
		nextIndex := firstUnqueriedContact(candidates, queried)
		if nextIndex == -1 || kademlia.ContactLookup == nil {
			break
		}

		next := candidates[nextIndex]
		queried[next.ID.String()] = true

		seenBeforeQuery := len(seen)

		// ContactLookup motsvarar FIND_NODE/FIND_CONTACT i nätverket.
		// Den frågade noden returnerar kontakter som den känner till nära target.
		addContacts(kademlia.ContactLookup(next, target, count))
		sortContactsByDistance(candidates)
		candidates = firstContacts(candidates, count)

		// Om frågan inte gav några nya kontakter och inget o-frågat finns kvar
		// är lookupen färdig.
		if len(seen) == seenBeforeQuery && firstUnqueriedContact(candidates, queried) == -1 {
			break
		}
	}

	return candidates
}

// sortContactsByDistance sorterar kandidater efter deras redan uträknade XOR-avstånd.
func sortContactsByDistance(contacts []Contact) {
	candidates := ContactCandidates{contacts: contacts}
	candidates.Sort()
}

// firstContacts kapar kandidatlistan till högst count kontakter.
func firstContacts(contacts []Contact, count int) []Contact {
	if len(contacts) <= count {
		return contacts
	}
	return contacts[:count]
}

// firstUnqueriedContact hittar nästa kandidat som lookupen ännu inte har frågat.
func firstUnqueriedContact(contacts []Contact, queried map[string]bool) int {
	for i, contact := range contacts {
		if contact.ID != nil && !queried[contact.ID.String()] {
			return i
		}
	}
	return -1
}

// NewFakeContactLookup skapar en testvariant av nätverkslookupen.
// Map:en säger vilken routingtabell varje kontakt "äger", så lookupen kan
// simulera att vi frågar andra noder utan riktiga UDP/RPC-meddelanden.
func NewFakeContactLookup(tables map[string]*RoutingTable) func(Contact, *KademliaID, int) []Contact {
	return func(contact Contact, target *KademliaID, count int) []Contact {
		if contact.ID == nil {
			return nil
		}

		table := tables[contact.ID.String()]
		if table == nil {
			return nil
		}

		return table.FindClosestContacts(target, count)
	}
}

// LookupData skickar en FIND_DATA-fråga för en hash.
// Den nuvarande koden har ingen riktig datastore, så funktionen visar bara
// hur nätverksmeddelandet skulle skickas.
func (kademlia *Kademlia) LookupData(hash string) {
	if hash == "" {
		fmt.Println("Hash is empty")
		return
	}

	network := Network{}
	_ = network.SendFindDataMessage(hash)
	fmt.Println("Looking for data with hash:", hash)
}

// Store skickar ett STORE-meddelande med data.
// Själva lagringen hos mottagande noder är inte implementerad här ännu.
func (kademlia *Kademlia) Store(data []byte) {
	if len(data) == 0 {
		fmt.Println("Data is empty")
		return
	}

	network := Network{}
	_ = network.SendStoreMessage(data)
	fmt.Println("Data sent for storage")
}
