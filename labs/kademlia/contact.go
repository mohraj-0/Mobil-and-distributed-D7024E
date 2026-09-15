package kademlia

import (
	"fmt"
	"sort"
)

// Contact beskriver en nod som vi känner till i nätverket.
// ID används för XOR-avstånd, Address används för nätverksmeddelanden,
// och distance sätts temporärt när vi sorterar mot ett visst target.
type Contact struct {
	ID       *KademliaID
	Address  string
	distance *KademliaID
}

// NewContact skapar en kontakt utan beräknat avstånd.
// Avståndet beror på vilket target vi söker, så det räknas ut senare.
func NewContact(id *KademliaID, address string) Contact {
	return Contact{id, address, nil}
}

// CalcDistance räknar ut XOR-avståndet från kontaktens ID till target.
// Resultatet sparas i contact.distance så sorteringen kan jämföra kontakter.
func (contact *Contact) CalcDistance(target *KademliaID) {
	contact.distance = contact.ID.CalcDistance(target)
}

// Less säger om den här kontakten ligger närmare target än otherContact.
// Den förutsätter att CalcDistance redan har körts för båda kontakterna.
func (contact *Contact) Less(otherContact *Contact) bool {
	return contact.distance.Less(otherContact.distance)
}

// String returnerar en kort textrepresentation för loggar och tester.
func (contact *Contact) String() string {
	return fmt.Sprintf(`contact("%s", "%s")`, contact.ID, contact.Address)
}

// ContactCandidates är en sorteringshjälp för lookup-resultat.
// Den implementerar sort.Interface genom Len, Swap och Less.
type ContactCandidates struct {
	contacts []Contact
}

// Append lägger till flera kontakter i kandidatlistan.
func (candidates *ContactCandidates) Append(contacts []Contact) {
	candidates.contacts = append(candidates.contacts, contacts...)
}

// GetContacts returnerar de första count kontakterna efter sortering.
func (candidates *ContactCandidates) GetContacts(count int) []Contact {
	if count > len(candidates.contacts) {
		count = len(candidates.contacts)
	}
	return candidates.contacts[:count]
}

// Sort sorterar kontakter så att närmaste kontakt ligger först.
func (candidates *ContactCandidates) Sort() {
	sort.Sort(candidates)
}

// Len returnerar antal kandidater och används av sort.Sort.
func (candidates *ContactCandidates) Len() int {
	return len(candidates.contacts)
}

// Swap byter plats på två kandidater och används av sort.Sort.
func (candidates *ContactCandidates) Swap(i, j int) {
	candidates.contacts[i], candidates.contacts[j] = candidates.contacts[j], candidates.contacts[i]
}

// Less jämför två kandidater baserat på deras beräknade XOR-avstånd.
func (candidates *ContactCandidates) Less(i, j int) bool {
	return candidates.contacts[i].Less(&candidates.contacts[j])
}
