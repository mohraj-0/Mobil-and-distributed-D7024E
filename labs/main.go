// Package main starts a small example of the Kademlia network.
package main

import (
	"d7024e/kademlia"
	"fmt"
	"time"
)

func main() {
	// Skapa två riktiga UDP-noder.
	nodeA := kademlia.NewUDPNode()
	nodeB := kademlia.NewUDPNode()

	// Starta node A på port 8000.
	err := nodeA.Listen("127.0.0.1:8000")
	if err != nil {
		fmt.Println("Could not start node A:", err)
		return
	}
	defer nodeA.Close()

	// Starta node B på port 8001.
	err = nodeB.Listen("127.0.0.1:8001")
	if err != nil {
		fmt.Println("Could not start node B:", err)
		return
	}
	defer nodeB.Close()

	// Starta mottagning på node A i en goroutine.
	go func() {
		message, err := nodeA.Receive()
		if err != nil {
			fmt.Println("Receive error:", err)
			return
		}

		fmt.Println("Node A received from:", message.From)
		fmt.Println("Message:", string(message.Data))
	}()

	// Vänta lite så mottagaren hinner starta.
	time.Sleep(200 * time.Millisecond)

	// Node B skickar ett PING till Node A.
	err = nodeB.SendData(
		"127.0.0.1:8000",
		[]byte("PING"),
	)

	if err != nil {
		fmt.Println("Send error:", err)
		return
	}

	fmt.Println("Node B sent PING to Node A")

	// Vänta så meddelandet hinner tas emot innan programmet avslutas.
	time.Sleep(500 * time.Millisecond)
}
