// TODO: Add package documentation for `main`, like this:
// Package main something something...
package main

import (
	"d7024e/kademlia"
	"time"
)

//	func main() {
//		fmt.Println("Pretending to run the kademlia app...")
//		// Using stuff from the kademlia package here. Something like...
//		id := kademlia.NewKademliaID("FFFFFFFF0000000000000000000000000000000000000000000000000000００００")
//		contact := kademlia.NewContact(id, "localhost:8０００")
//		fmt.Println(contact.String())
//		fmt.Printf("%v\n", contact)
//	}

func main() {

	// Startar en nod som lyssnar på UDP-port 8000
	go kademlia.Listen("127.0.0.1", 8000)

	// Nod B lyssnar på port 8001
	go kademlia.Listen("127.0.0.1", 8001)

	// Väntar lite så att listenern hinner starta
	time.Sleep(1 * time.Second)

	// Skapar ett ID för noden
	id := kademlia.NewKademliaID(
		"FFFFFFFF00000000000000000000000000000000000000000000000000000000",
	)

	// Skapar en kontakt som pekar på noden som lyssnar
	contact := kademlia.NewContact(id, "127.0.0.1:8000")

	// Skapar ett Network-objekt
	network := kademlia.Network{}

	// Skickar PING till noden
	network.SendPingMessage(&contact)

	// Väntar lite så att meddelandet hinner tas emot
	time.Sleep(1 * time.Second)

}
