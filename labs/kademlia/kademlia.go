package kademlia

import "fmt"

const lookupContactCount = 10

// Kademlia contains the local routing table and lookup behavior for a node.
type Kademlia struct {
	RoutingTable *RoutingTable

	// ContactLookup asks another contact for contacts close to the target.
	// Tests can use this as a fake network while the real RPC layer is still simple.
	ContactLookup func(contact Contact, target *KademliaID, count int) []Contact
}

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

func (kademlia *Kademlia) lookupContactSequential(target *KademliaID, count int) []Contact {
	candidates := make([]Contact, 0, count)
	seen := make(map[string]bool)
	queried := make(map[string]bool)

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

	addContacts(kademlia.RoutingTable.FindClosestContacts(target, count))
	sortContactsByDistance(candidates)
	candidates = firstContacts(candidates, count)

	for {
		nextIndex := firstUnqueriedContact(candidates, queried)
		if nextIndex == -1 || kademlia.ContactLookup == nil {
			break
		}

		next := candidates[nextIndex]
		queried[next.ID.String()] = true

		seenBeforeQuery := len(seen)
		addContacts(kademlia.ContactLookup(next, target, count))
		sortContactsByDistance(candidates)
		candidates = firstContacts(candidates, count)

		if len(seen) == seenBeforeQuery && firstUnqueriedContact(candidates, queried) == -1 {
			break
		}
	}

	return candidates
}

func sortContactsByDistance(contacts []Contact) {
	candidates := ContactCandidates{contacts: contacts}
	candidates.Sort()
}

func firstContacts(contacts []Contact, count int) []Contact {
	if len(contacts) <= count {
		return contacts
	}
	return contacts[:count]
}

func firstUnqueriedContact(contacts []Contact, queried map[string]bool) int {
	for i, contact := range contacts {
		if contact.ID != nil && !queried[contact.ID.String()] {
			return i
		}
	}
	return -1
}

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

func (kademlia *Kademlia) LookupData(hash string) {
	if hash == "" {
		fmt.Println("Hash is empty")
		return
	}

	network := Network{}
	_ = network.SendFindDataMessage(hash)
	fmt.Println("Looking for data with hash:", hash)
}

func (kademlia *Kademlia) Store(data []byte) {
	if len(data) == 0 {
		fmt.Println("Data is empty")
		return
	}

	network := Network{}
	_ = network.SendStoreMessage(data)
	fmt.Println("Data sent for storage")
}
