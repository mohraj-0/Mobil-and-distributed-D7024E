package kademlia

import (
	"encoding/hex"
	"math/rand"
)

// IDLength är antal bytes i ett Kademlia-ID: 32 bytes = 256 bitar.
const IDLength = 32 // 256 bit / 8 bits/byte = 32 bytes

// KademliaID är ett 256-bitars ID för både noder och data-keys.
// Samma typ används för node lookup och data lookup eftersom Kademlia placerar
// allt i samma ID-rymd.
type KademliaID [IDLength]byte

// NewKademliaID skapar ett ID från en hex-sträng.
// Strängen måste representera 32 bytes, alltså 64 hex-tecken.
func NewKademliaID(data string) *KademliaID {
	decoded, _ := hex.DecodeString(data)

	newKademliaID := KademliaID{}
	for i := 0; i < IDLength; i++ {
		newKademliaID[i] = decoded[i]
	}

	return &newKademliaID
}

// NewRandomKademliaID skapar ett slumpmässigt node-ID.
// Den här enkla varianten använder math/rand och är främst för labb/test.
func NewRandomKademliaID() *KademliaID {
	newKademliaID := KademliaID{}
	for i := 0; i < IDLength; i++ {
		newKademliaID[i] = uint8(rand.Intn(256))
	}
	return &newKademliaID
}

// Less jämför två ID:n byte för byte som stora heltal.
// Det används när XOR-avstånd ska sorteras från närmast till längst bort.
func (kademliaID KademliaID) Less(otherKademliaID *KademliaID) bool {
	for i := 0; i < IDLength; i++ {
		if kademliaID[i] != otherKademliaID[i] {
			return kademliaID[i] < otherKademliaID[i]
		}
	}
	return false
}

// Equals kontrollerar om två ID:n är exakt lika.
func (kademliaID KademliaID) Equals(otherKademliaID *KademliaID) bool {
	for i := 0; i < IDLength; i++ {
		if kademliaID[i] != otherKademliaID[i] {
			return false
		}
	}
	return true
}

// CalcDistance räknar ut Kademlia-avståndet mellan två ID:n med XOR.
// Ju mindre XOR-resultat, desto närmare ligger noderna/keys i ID-rymden.
func (kademliaID KademliaID) CalcDistance(target *KademliaID) *KademliaID {
	result := KademliaID{}
	for i := 0; i < IDLength; i++ {
		result[i] = kademliaID[i] ^ target[i]
	}
	return &result
}

// String gör ID:t till hex så att det kan loggas och jämföras som text.
func (kademliaID *KademliaID) String() string {
	return hex.EncodeToString(kademliaID[0:IDLength])
}
