package main

import (
	"fmt"
	"time"

	"d7024e/kademlia"
)

func main() {
	const receiverAddress = "127.0.0.1:8000"

	go kademlia.Listen("127.0.0.1", 8000)
	time.Sleep(250 * time.Millisecond)

	id := kademlia.NewKademliaID("FFFFFFFF00000000000000000000000000000000000000000000000000000000")
	contact := kademlia.NewContact(id, receiverAddress)
	network := kademlia.Network{}

	if err := network.SendPingMessage(&contact); err != nil {
		fmt.Println("send ping:", err)
		return
	}

	time.Sleep(250 * time.Millisecond)
}
