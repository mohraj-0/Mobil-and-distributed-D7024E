package kademlia_test

import (
	"testing"

	"d7024e/kademlia"
)

func TestNewKademliaIDValid(t *testing.T) {
	input := "ffffffff00000000000000000000000000000000000000000000000000000000"

	id := kademlia.NewKademliaID(input)

	if id == nil {
		t.Fatal("NewKademliaID returned nil for valid input")
	}

	if id.String() != input {
		t.Fatalf("got %s, want %s", id.String(), input)
	}
}

func TestNewKademliaIDInvalid(t *testing.T) {
	id := kademlia.NewKademliaID("invalid")

	if id != nil {
		t.Fatal("NewKademliaID should return nil for invalid input")
	}
}

func TestNewKademliaIDWrongLength(t *testing.T) {
	id := kademlia.NewKademliaID("ff")

	if id != nil {
		t.Fatal("NewKademliaID should return nil for wrong length")
	}
}

func TestKademliaIDEquals(t *testing.T) {
	id1 := kademlia.NewKademliaID(
		"0100000000000000000000000000000000000000000000000000000000000000",
	)

	id2 := kademlia.NewKademliaID(
		"0100000000000000000000000000000000000000000000000000000000000000",
	)

	id3 := kademlia.NewKademliaID(
		"0200000000000000000000000000000000000000000000000000000000000000",
	)

	if !id1.Equals(id2) {
		t.Fatal("equal IDs were reported as different")
	}

	if id1.Equals(id3) {
		t.Fatal("different IDs were reported as equal")
	}

	if id1.Equals(nil) {
		t.Fatal("Equals(nil) should return false")
	}
}

func TestKademliaIDLess(t *testing.T) {
	small := kademlia.NewKademliaID(
		"0100000000000000000000000000000000000000000000000000000000000000",
	)

	large := kademlia.NewKademliaID(
		"0200000000000000000000000000000000000000000000000000000000000000",
	)

	if !small.Less(large) {
		t.Fatal("expected small ID to be less than large ID")
	}

	if large.Less(small) {
		t.Fatal("expected large ID not to be less than small ID")
	}

	if small.Less(nil) {
		t.Fatal("Less(nil) should return false")
	}
}

func TestKademliaIDCalcDistance(t *testing.T) {
	id1 := kademlia.NewKademliaID(
		"0100000000000000000000000000000000000000000000000000000000000000",
	)

	id2 := kademlia.NewKademliaID(
		"0300000000000000000000000000000000000000000000000000000000000000",
	)

	distance := id1.CalcDistance(id2)

	want := kademlia.NewKademliaID(
		"0200000000000000000000000000000000000000000000000000000000000000",
	)

	if distance == nil {
		t.Fatal("CalcDistance returned nil")
	}

	if !distance.Equals(want) {
		t.Fatalf(
			"distance = %s, want %s",
			distance.String(),
			want.String(),
		)
	}

	if id1.CalcDistance(nil) != nil {
		t.Fatal("CalcDistance(nil) should return nil")
	}
}

func TestNewRandomKademliaID(t *testing.T) {
	id := kademlia.NewRandomKademliaID()

	if id == nil {
		t.Fatal("NewRandomKademliaID returned nil")
	}

	if len(id.String()) != kademlia.IDLength*2 {
		t.Fatalf(
			"random ID string length = %d, want %d",
			len(id.String()),
			kademlia.IDLength*2,
		)
	}
}

func TestNilKademliaIDString(t *testing.T) {
	var id *kademlia.KademliaID

	if id.String() != "" {
		t.Fatal("nil KademliaID String() should return empty string")
	}
}
