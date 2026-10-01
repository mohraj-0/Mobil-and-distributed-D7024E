package kademlia

import "fmt"

// bestämmer vad noder ska göra (när de tar emot meddelanden)

type Kademlia struct {
	// spara nodens routing table
	RoutingTable *RoutingTable
}

// LookupContact letar efter noder som ligger nära target nodens ID
func (kademlia *Kademlia) LookupContact(target *Contact) {
	// Kontrollerar att routing table finns
	if kademlia.RoutingTable == nil {
		fmt.Println("Routing table is not initialized")
		return
	}

	// Kontrollerar att target finns
	if target == nil || target.ID == nil {
		fmt.Println("Target is invalid")
		return
	}

	// Letar efter de 10 närmaste noderna till target ID
	contacts := kademlia.RoutingTable.FindClosestContacts(target.ID, 10)

	// Skriver ut de noder som hittades
	for _, contact := range contacts {
		fmt.Println("Found contact:", contact.String())
	}
}

// LookupData letar efter data som hör till en viss hash
func (kademlia *Kademlia) LookupData(hash string) {
	// Kontrollerar att hash inte är tom
	if hash == "" {
		fmt.Println("Hash is empty")
		return
	}

	// Skickar en fråga till nätverket efter data med den här hashen
	network := Network{}
	network.SendFindDataMessage(hash)

	// Visar vilken hash vi söker efter
	fmt.Println("Looking for data with hash:", hash)

}

// skickar data till nätverket för att lagras
func (kademlia *Kademlia) Store(data []byte) {
	// Kontrollerar att data inte är tom
	if len(data) == 0 {
		fmt.Println("Data is empty")
		return
	}

	// Skapar ett Network objekt
	network := Network{}

	// Skickar datan för lagring
	network.SendStoreMessage(data)

	// Visar att datan skickades
	fmt.Println("Data sent for storage")
}
