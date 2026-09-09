package kademlia

import (
	"fmt"
	"net"
)

type Network struct {
}

// Listen gör att noden börja lyssna på UDP medelande
func Listen(ip string, port int) {
	// sätter ihop ip och port till en address
	address := fmt.Sprintf("%s:%d", ip, port)

	// gör om address till en UDP address som go kan använda
	udpAddr, err := net.ResolveUDPAddr("udp", address)
	// om addressen inte kan skapas, skriv ut fel
	if err != nil {
		fmt.Println("Error resolving UDP address:", err)
		return
	}
	// Öppnar UDP-porten och börjar lyssna på adressen
	conn, err := net.ListenUDP("udp", udpAddr)
	// Om UDP-porten inte kan öppnas, skriv ut fel
	if err != nil {
		fmt.Println("Error listening on UDP:", err)
		return
	}
	// Stänger UDP-anslutningen när funktionen avslutas
	defer conn.Close()

	// Visar att noden har börjat lyssna
	fmt.Println("Listening on", address)

	// (ta emot ett UDP-meddelande)

	// Skapar plats för inkommande data
	buffer := make([]byte, 1024)

	// Väntar på ett UDP-meddelande
	// Loop hela tiden så att noden kan ta emot flera meddelanden
	for {
		n, remoteAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Println("Error reading from UDP:", err)
			continue
		}

		// Gör om mottagen data från bytes till text
		message := string(buffer[:n])

		// Skriver ut avsändaren
		fmt.Println("Message from:", remoteAddr)

		// Skriver ut meddelandet
		fmt.Println("Message:", message)
	}
}

// skicka ett UDP-meddelande, från en node till annan nod
func (network *Network) SendPingMessage(contact *Contact) {

	// Gör om  adress till en UDP-adress

	udpAddr, err := net.ResolveUDPAddr("udp", contact.Address)

	// Om adressen inte går att använda
	if err != nil {
		fmt.Println("Error resolving contact address:", err)
		return
	}

	// Skapar en UDP-anslutning till den andra noden
	conn, err := net.DialUDP("udp", nil, udpAddr)

	// Om anslutningen inte kan skapas, skriv ut fel
	if err != nil {
		fmt.Println("Error connecting to UDP node:", err)
		return
	}

	// Stänger UDP-anslutningen när funktionen är klar
	defer conn.Close()

	// Själva meddelandet som vi vill skicka
	message := []byte("PING")

	// Skickar PING-meddelandet till den andra noden
	_, err = conn.Write(message)

	// Om meddelandet inte kunde skickas, error
	if err != nil {
		fmt.Println("Error sending PING:", err)
		return
	}

	// Visar att PING skickades
	fmt.Println("PING sent to", contact.Address)

}

// Skickar en fråga till en annan nod för att hitta nära noder(med target ID)
func (network *Network) SendFindContactMessage(contact *Contact) {

	// Gör om  adress till en UDP-adress
	udpAddr, err := net.ResolveUDPAddr("udp", contact.Address)
	if err != nil {
		fmt.Println("Error resolving contact address:", err)
		return
	}

	// Skapar en UDP-anslutning till den andra noden
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		fmt.Println("Error connecting to UDP node:", err)
		return
	}

	// Stänger anslutningen när funktionen är klar
	defer conn.Close()

	// Skapar ett enkelt meddelande
	message := []byte("FIND_CONTACT")

	// Skickar meddelandet till den andra noden
	_, err = conn.Write(message)

	// Om något går fel
	if err != nil {
		fmt.Println("Error sending FIND_CONTACT:", err)
		return
	}
	// Visar att meddelandet skickades
	fmt.Println("FIND_CONTACT sent to", contact.Address)
}

// Skickar en fråga till en annan nod för att hitta data med en viss hash
func (network *Network) SendFindDataMessage(hash string) {

	// Här använder vi en exempeladress tills vi senare kopplar funktionen
	// till en riktig Contact eller routing table.
	udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:8000")
	if err != nil {
		fmt.Println("Error resolving UDP address:", err)
		return
	}

	// Skapar en UDP-anslutning
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		fmt.Println("Error connecting to UDP node:", err)
		return
	}

	// Stänger anslutningen när funktionen är klar
	defer conn.Close()

	// Skapar meddelandet och lägger med hashen
	message := []byte("FIND_DATA " + hash)

	// Skickar meddelandet
	_, err = conn.Write(message)
	if err != nil {
		fmt.Println("Error sending FIND_DATA:", err)
		return
	}

	// Visar att meddelandet skickades
	fmt.Println("FIND_DATA sent for hash:", hash)

}

// Skickar data till en annan nod för lagring
func (network *Network) SendStoreMessage(data []byte) {

	// Tillfällig adress tills vi senare kopplar funktionen
	// till en riktig Contact eller routing table.
	udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:8000")
	if err != nil {
		fmt.Println("Error resolving UDP address:", err)
		return
	}

	// Skapar en UDP-anslutning
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		fmt.Println("Error connecting to UDP node:", err)
		return
	}

	// Stänger anslutningen när funktionen är klar
	defer conn.Close()

	// Lägger till "STORE " före själva datan
	message := append([]byte("STORE "), data...)

	// Skickar meddelandet
	_, err = conn.Write(message)
	if err != nil {
		fmt.Println("Error sending STORE:", err)
		return
	}

	// Visar att datan skickades
	fmt.Println("STORE message sent")
}
