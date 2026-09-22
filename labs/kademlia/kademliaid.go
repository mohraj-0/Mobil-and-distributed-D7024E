package kademlia

import (
	"encoding/hex"
	"math/rand"
)

// IDLength är antal bytes i ett Kademlia-ID:
// 32 bytes = 256 bitar.
const IDLength = 32

// KademliaID är ett 256-bitars ID för både noder och data-keys.
type KademliaID [IDLength]byte

// NewKademliaID skapar ett ID från en hex-sträng.
// Strängen måste innehålla exakt 64 hex-tecken.
func NewKademliaID(data string) *KademliaID {
	decoded, err := hex.DecodeString(data)

	if err != nil || len(decoded) != IDLength {
		return nil
	}

	newKademliaID := KademliaID{}

	copy(
		newKademliaID[:],
		decoded,
	)

	return &newKademliaID
}

// NewRandomKademliaID skapar ett slumpmässigt Kademlia-ID.
func NewRandomKademliaID() *KademliaID {
	newKademliaID := KademliaID{}

	for i := 0; i < IDLength; i++ {
		newKademliaID[i] =
			uint8(rand.Intn(256))
	}

	return &newKademliaID
}

// Less jämför två ID:n byte för byte.
//
// Används bland annat för att sortera XOR-avstånd.
func (kademliaID KademliaID) Less(
	otherKademliaID *KademliaID,
) bool {

	if otherKademliaID == nil {
		return false
	}

	for i := 0; i < IDLength; i++ {

		if kademliaID[i] != otherKademliaID[i] {
			return kademliaID[i] <
				otherKademliaID[i]
		}
	}

	return false
}

// Equals kontrollerar om två Kademlia-ID:n är lika.
func (kademliaID KademliaID) Equals(
	otherKademliaID *KademliaID,
) bool {

	if otherKademliaID == nil {
		return false
	}

	for i := 0; i < IDLength; i++ {

		if kademliaID[i] !=
			otherKademliaID[i] {

			return false
		}
	}

	return true
}

// CalcDistance räknar XOR-avståndet mellan två ID:n.
//
// Ju mindre XOR-resultat,
// desto närmare ligger ID:na i Kademlia.
func (kademliaID KademliaID) CalcDistance(
	target *KademliaID,
) *KademliaID {

	if target == nil {
		return nil
	}

	result := KademliaID{}

	for i := 0; i < IDLength; i++ {
		result[i] =
			kademliaID[i] ^ target[i]
	}

	return &result
}

// String konverterar ID:t till en hex-sträng.
func (kademliaID *KademliaID) String() string {
	if kademliaID == nil {
		return ""
	}

	return hex.EncodeToString(
		kademliaID[:],
	)
}
