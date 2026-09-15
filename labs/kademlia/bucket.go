package kademlia

import (
	"container/list"
)

// bucket är en k-bucket i routingtabellen.
// Den håller en ordnad lista av kontakter där fronten är senast sedd kontakt.
type bucket struct {
	list *list.List
}

// newBucket skapar en tom bucket med en dubbellänkad lista.
func newBucket() *bucket {
	bucket := &bucket{}
	bucket.list = list.New()
	return bucket
}

// AddContact lägger till en kontakt eller flyttar en redan känd kontakt längst fram.
// Om bucketen är full och kontakten är ny droppas kontakten i denna förenklade
// implementation, alltså ingen ping/eviction av äldsta noden görs.
func (bucket *bucket) AddContact(contact Contact) {
	var element *list.Element
	for e := bucket.list.Front(); e != nil; e = e.Next() {
		nodeID := e.Value.(Contact).ID

		if (contact).ID.Equals(nodeID) {
			element = e
		}
	}

	if element == nil {
		if bucket.list.Len() < bucketSize {
			bucket.list.PushFront(contact)
		}
	} else {
		bucket.list.MoveToFront(element)
	}
}

// GetContactAndCalcDistance returnerar bucketens kontakter med avståndet till
// target redan uträknat. Det behövs innan ContactCandidates kan sortera dem.
func (bucket *bucket) GetContactAndCalcDistance(target *KademliaID) []Contact {
	var contacts []Contact

	for elt := bucket.list.Front(); elt != nil; elt = elt.Next() {
		contact := elt.Value.(Contact)
		contact.CalcDistance(target)
		contacts = append(contacts, contact)
	}

	return contacts
}

// Len returnerar hur många kontakter som ligger i bucketen.
func (bucket *bucket) Len() int {
	return bucket.list.Len()
}
