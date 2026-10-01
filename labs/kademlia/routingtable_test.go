package kademlia

import (
	"fmt"
	"testing"
)

func TestRoutingTable(t *testing.T) {
	t.Log("testing RoutingTable.AddContact and RoutingTable.FindClosestContacts")

	me := NewContact(NewKademliaID("FFFFFFFF00000000000000000000000000000000000000000000000000000000"), "localhost:8000")
	rt := NewRoutingTable(me)
	t.Logf("local routing table owner: %s", me.String())

	contactsToAdd := []Contact{
		NewContact(NewKademliaID("FFFFFFFF00000000000000000000000000000000000000000000000000000000"), "localhost:8001"),
		NewContact(NewKademliaID("1111111100000000000000000000000000000000000000000000000000000000"), "localhost:8002"),
		NewContact(NewKademliaID("1111111200000000000000000000000000000000000000000000000000000000"), "localhost:8002"),
		NewContact(NewKademliaID("1111111300000000000000000000000000000000000000000000000000000000"), "localhost:8002"),
		NewContact(NewKademliaID("1111111400000000000000000000000000000000000000000000000000000000"), "localhost:8002"),
		NewContact(NewKademliaID("2111111400000000000000000000000000000000000000000000000000000000"), "localhost:8002"),
	}

	for _, contact := range contactsToAdd {
		rt.AddContact(contact)
		t.Logf("example added contact to routing table: %s", contact.String())
	}

	target := NewKademliaID("2111111400000000000000000000000000000000000000000000000000000000")
	contacts := rt.FindClosestContacts(target, 20)
	t.Logf("looking for contacts closest to target: %s", target)

	wantContacts := len(contactsToAdd) - 1 // the contact with our own ID is ignored
	if len(contacts) != wantContacts {
		t.Fatalf("FindClosestContacts returned %d contacts, want %d", len(contacts), wantContacts)
	}
	if !contacts[0].ID.Equals(target) {
		t.Fatalf("closest contact = %s, want target %s", contacts[0].ID, target)
	}

	for i := range contacts {
		distance := contacts[i].ID.CalcDistance(target)
		t.Logf("example closest result %d: %s distance=%s", i, contacts[i].String(), distance)
	}
}

func TestRoutingTreeSplitsBucketContainingLocalNode(t *testing.T) {
	me := NewContact(NewKademliaID("ff00000000000000000000000000000000000000000000000000000000000000"), "localhost:8000")
	rt := NewRoutingTableWithBranchBits(me, 2)
	firstBytes := []byte{0xc0, 0xd0, 0xe0, 0xf0}

	for i := 0; i < bucketSize+1; i++ {
		rt.AddContact(NewContact(testRoutingTreeID(firstBytes[i%len(firstBytes)], byte(i)), fmt.Sprintf("localhost:%d", 8100+i)))
	}

	if rt.BranchBits() != 2 {
		t.Fatalf("BranchBits = %d, want 2", rt.BranchBits())
	}
	if rt.root.isLeaf() {
		t.Fatal("root stayed as a leaf after the local bucket overflowed")
	}
	if len(rt.root.children) != 4 {
		t.Fatalf("root has %d children, want 4 for b=2", len(rt.root.children))
	}
	if rt.root.children[3].isLeaf() {
		t.Fatal("child containing the local node did not split after overflowing")
	}

	contacts := rt.FindClosestContacts(NewKademliaID("c000000000000000000000000000000000000000000000000000000000000001"), bucketSize+1)
	if len(contacts) != bucketSize+1 {
		t.Fatalf("routing tree kept %d contacts, want %d", len(contacts), bucketSize+1)
	}
}

func TestRoutingTreeDoesNotSplitBucketAwayFromLocalNode(t *testing.T) {
	me := NewContact(NewKademliaID("ff00000000000000000000000000000000000000000000000000000000000000"), "localhost:8000")
	rt := NewRoutingTableWithBranchBits(me, 2)

	for i := 0; i < bucketSize+1; i++ {
		rt.AddContact(NewContact(testRoutingTreeID(0x00, byte(i)), fmt.Sprintf("localhost:%d", 8200+i)))
	}

	if rt.root.isLeaf() {
		t.Fatal("root stayed as a leaf after the first overflow")
	}
	if !rt.root.children[0].isLeaf() {
		t.Fatal("bucket away from the local node split, but only the local subtree should split")
	}

	contacts := rt.FindClosestContacts(NewKademliaID("0000000000000000000000000000000000000000000000000000000000000000"), bucketSize+1)
	if len(contacts) != bucketSize {
		t.Fatalf("routing tree kept %d contacts, want capped bucket size %d", len(contacts), bucketSize)
	}
}

func testRoutingTreeID(firstByte byte, suffix byte) *KademliaID {
	id := &KademliaID{}
	id[0] = firstByte
	id[IDLength-1] = suffix
	return id
}
