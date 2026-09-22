package kademlia

import "testing"

func TestNewBucketIsEmpty(t *testing.T) {
	b := newBucket()

	if b == nil {
		t.Fatal("newBucket returned nil")
	}

	if b.Len() != 0 {
		t.Fatalf("new bucket length = %d, want 0", b.Len())
	}
}

func TestBucketAddContact(t *testing.T) {
	b := newBucket()

	contact := NewContact(
		NewKademliaID(
			"0100000000000000000000000000000000000000000000000000000000000000",
		),
		"node-1",
	)

	b.AddContact(contact)

	if b.Len() != 1 {
		t.Fatalf("bucket length = %d, want 1", b.Len())
	}
}

func TestBucketDoesNotDuplicateContact(t *testing.T) {
	b := newBucket()

	contact := NewContact(
		NewKademliaID(
			"0100000000000000000000000000000000000000000000000000000000000000",
		),
		"node-1",
	)

	b.AddContact(contact)
	b.AddContact(contact)

	if b.Len() != 1 {
		t.Fatalf(
			"bucket length after duplicate = %d, want 1",
			b.Len(),
		)
	}
}

func TestBucketMovesExistingContactToFront(t *testing.T) {
	b := newBucket()

	contact1 := NewContact(
		NewKademliaID(
			"0100000000000000000000000000000000000000000000000000000000000000",
		),
		"node-1",
	)

	contact2 := NewContact(
		NewKademliaID(
			"0200000000000000000000000000000000000000000000000000000000000000",
		),
		"node-2",
	)

	b.AddContact(contact1)
	b.AddContact(contact2)

	// contact2 ska ligga först eftersom den lades till senast.
	first := b.list.Front().Value.(Contact)

	if !first.ID.Equals(contact2.ID) {
		t.Fatal("latest contact is not at front")
	}

	// Lägg till contact1 igen.
	// Då ska den flyttas till fronten.
	b.AddContact(contact1)

	first = b.list.Front().Value.(Contact)

	if !first.ID.Equals(contact1.ID) {
		t.Fatal("existing contact was not moved to front")
	}
}

func TestBucketStopsAtBucketSize(t *testing.T) {
	b := newBucket()

	for i := 0; i < bucketSize+5; i++ {
		id := &KademliaID{}

		// Gör varje ID unikt.
		id[30] = byte(i >> 8)
		id[31] = byte(i)

		contact := NewContact(
			id,
			"node",
		)

		b.AddContact(contact)
	}

	if b.Len() != bucketSize {
		t.Fatalf(
			"bucket length = %d, want %d",
			b.Len(),
			bucketSize,
		)
	}
}

func TestBucketGetContactAndCalcDistance(t *testing.T) {
	b := newBucket()

	target := NewKademliaID(
		"0000000000000000000000000000000000000000000000000000000000000000",
	)

	contact1 := NewContact(
		NewKademliaID(
			"0100000000000000000000000000000000000000000000000000000000000000",
		),
		"node-1",
	)

	contact2 := NewContact(
		NewKademliaID(
			"0200000000000000000000000000000000000000000000000000000000000000",
		),
		"node-2",
	)

	b.AddContact(contact1)
	b.AddContact(contact2)

	contacts := b.GetContactAndCalcDistance(target)

	if len(contacts) != 2 {
		t.Fatalf(
			"got %d contacts, want 2",
			len(contacts),
		)
	}

	for _, contact := range contacts {
		if contact.distance == nil {
			t.Fatal("contact distance was not calculated")
		}
	}
}
