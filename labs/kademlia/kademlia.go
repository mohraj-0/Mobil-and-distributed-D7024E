package kademlia

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

const lookupContactCount = 10

const defaultAlpha = 3

const defaultRPCTimeout = 2 * time.Second

const (
	rpcFindNode      = "FIND_NODE"
	rpcFindNodeReply = "FIND_NODE_REPLY"
)

// Kademlia innehåller logiken för en Kademlia-nod.
type Kademlia struct {
	RoutingTable *RoutingTable
	Network      Node

	// Antal noder som frågas parallellt.
	// Default är 3.
	Alpha int

	// Timeout för RPC.
	RPCTimeout time.Duration

	// Sparar väntande FIND_NODE-svar.
	findNodeMu        sync.Mutex
	findNodeResponses map[string]chan []Contact

	// Används för fake lookup i tester.
	ContactLookup func(
		contact Contact,
		target *KademliaID,
		count int,
	) []Contact
}

// Kontakt som kan skickas i RPC-meddelanden.
type findNodeWireContact struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

// RPC-meddelande för FIND_NODE och FIND_NODE_REPLY.
type findNodeRPCMessage struct {
	Type          string                `json:"type"`
	RequestID     string                `json:"request_id"`
	SenderID      string                `json:"sender_id"`
	SenderAddress string                `json:"sender_address"`
	TargetID      string                `json:"target_id,omitempty"`
	Contacts      []findNodeWireContact `json:"contacts,omitempty"`
}

// Returnerar timeout för RPC.
func (kademlia *Kademlia) rpcTimeout() time.Duration {
	if kademlia.RPCTimeout <= 0 {
		return defaultRPCTimeout
	}

	return kademlia.RPCTimeout
}

// Skapar ett unikt ID för varje RPC-request.
func randomRequestID() (string, error) {
	bytes := make([]byte, 32)

	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(bytes), nil
}

// Skapar mapen som sparar väntande FIND_NODE-svar.
func (kademlia *Kademlia) ensureFindNodeResponses() {
	kademlia.findNodeMu.Lock()
	defer kademlia.findNodeMu.Unlock()

	if kademlia.findNodeResponses == nil {
		kademlia.findNodeResponses =
			make(map[string]chan []Contact)
	}
}

// Gör om Contact så att den kan skickas i RPC.
func contactsToWire(
	contacts []Contact,
) []findNodeWireContact {

	result := make(
		[]findNodeWireContact,
		0,
		len(contacts),
	)

	for _, contact := range contacts {
		if contact.ID == nil {
			continue
		}

		result = append(
			result,
			findNodeWireContact{
				ID:      contact.ID.String(),
				Address: contact.Address,
			},
		)
	}

	return result
}

// Gör om RPC-kontakter till vanliga Contact-objekt.
func wireToContacts(
	contacts []findNodeWireContact,
) []Contact {

	result := make(
		[]Contact,
		0,
		len(contacts),
	)

	for _, contact := range contacts {
		id := NewKademliaID(contact.ID)

		if id == nil {
			continue
		}

		result = append(
			result,
			NewContact(
				id,
				contact.Address,
			),
		)
	}

	return result
}

// Lyssnar efter inkommande RPC-meddelanden.
func (kademlia *Kademlia) ListenForRPC() {
	if kademlia.Network == nil {
		return
	}

	for {
		// Vänta på meddelande.
		message, err := kademlia.Network.Receive()
		if err != nil {
			return
		}

		// Hantera meddelandet i en goroutine.
		go kademlia.handleRPCMessage(message)
	}
}

// Hanterar inkommande RPC-meddelanden.
func (kademlia *Kademlia) handleRPCMessage(
	message Message,
) {
	var rpc findNodeRPCMessage

	// Gör om JSON-data till RPC.
	err := json.Unmarshal(
		message.Data,
		&rpc,
	)

	if err != nil {
		return
	}

	// Kontrollera vilken RPC-typ det är.
	switch rpc.Type {

	case rpcFindNode:
		kademlia.handleFindNodeRPC(rpc)

	case rpcFindNodeReply:
		kademlia.handleFindNodeReplyRPC(rpc)
	}
}

// Hanterar en FIND_NODE-request.
func (kademlia *Kademlia) handleFindNodeRPC(
	rpc findNodeRPCMessage,
) {
	if kademlia.RoutingTable == nil ||
		kademlia.Network == nil {
		return
	}

	senderID :=
		NewKademliaID(
			rpc.SenderID,
		)

	targetID :=
		NewKademliaID(
			rpc.TargetID,
		)

	if senderID == nil ||
		targetID == nil ||
		rpc.SenderAddress == "" {
		return
	}

	// Skapa kontakt för avsändaren.
	sender :=
		NewContact(
			senderID,
			rpc.SenderAddress,
		)

	// Lägg till avsändaren i routing table.
	kademlia.RoutingTable.AddContact(
		sender,
	)

	// Hitta närmaste kontakter till target.
	contacts :=
		kademlia.RoutingTable.FindClosestContacts(
			targetID,
			lookupContactCount,
		)

	me := kademlia.RoutingTable.me

	if me.ID == nil {
		return
	}

	// Skapa FIND_NODE_REPLY.
	reply := findNodeRPCMessage{
		Type:          rpcFindNodeReply,
		RequestID:     rpc.RequestID,
		SenderID:      me.ID.String(),
		SenderAddress: me.Address,
		Contacts:      contactsToWire(contacts),
	}

	// Gör om reply till JSON.
	data, err :=
		json.Marshal(reply)

	if err != nil {
		return
	}

	// Skicka svaret tillbaka.
	_ = kademlia.Network.SendData(
		rpc.SenderAddress,
		data,
	)
}

// Hanterar svaret från FIND_NODE.
func (kademlia *Kademlia) handleFindNodeReplyRPC(
	rpc findNodeRPCMessage,
) {
	// Gör om RPC-kontakter till Contact.
	contacts :=
		wireToContacts(
			rpc.Contacts,
		)

	// Lägg till kontakterna i routing table.
	if kademlia.RoutingTable != nil {
		for _, contact := range contacts {
			kademlia.RoutingTable.AddContact(
				contact,
			)
		}
	}

	kademlia.ensureFindNodeResponses()

	kademlia.findNodeMu.Lock()

	// Hitta rätt channel med RequestID.
	channel :=
		kademlia.findNodeResponses[rpc.RequestID]

	kademlia.findNodeMu.Unlock()

	if channel == nil {
		return
	}

	// Skicka svaret till rätt lookup.
	select {
	case channel <- contacts:
	default:
	}
}

// Skickar FIND_NODE och väntar på svar.
func (kademlia *Kademlia) sendFindNodeRPC(
	contact Contact,
	target *KademliaID,
) ([]Contact, error) {

	if kademlia.Network == nil {
		return nil,
			fmt.Errorf(
				"network is not initialized",
			)
	}

	if kademlia.RoutingTable == nil {
		return nil,
			fmt.Errorf(
				"routing table is not initialized",
			)
	}

	if contact.ID == nil ||
		contact.Address == "" ||
		target == nil {

		return nil,
			fmt.Errorf(
				"invalid FIND_NODE request",
			)
	}

	// Skapa unikt RequestID.
	requestID, err :=
		randomRequestID()

	if err != nil {
		return nil, err
	}

	// Channel där svaret ska komma.
	responseChannel :=
		make(chan []Contact, 1)

	kademlia.ensureFindNodeResponses()

	kademlia.findNodeMu.Lock()

	// Koppla RequestID till rätt channel.
	kademlia.findNodeResponses[requestID] =
		responseChannel

	kademlia.findNodeMu.Unlock()

	// Ta bort requesten när den är klar.
	defer func() {
		kademlia.findNodeMu.Lock()

		delete(
			kademlia.findNodeResponses,
			requestID,
		)

		kademlia.findNodeMu.Unlock()
	}()

	me := kademlia.RoutingTable.me

	if me.ID == nil {
		return nil,
			fmt.Errorf(
				"local node ID is missing",
			)
	}

	// Skapa FIND_NODE-request.
	request := findNodeRPCMessage{
		Type:          rpcFindNode,
		RequestID:     requestID,
		SenderID:      me.ID.String(),
		SenderAddress: me.Address,
		TargetID:      target.String(),
	}

	// Gör om request till JSON.
	data, err :=
		json.Marshal(
			request,
		)

	if err != nil {
		return nil, err
	}

	// Skicka FIND_NODE via nätverket.
	err =
		kademlia.Network.SendData(
			contact.Address,
			data,
		)

	if err != nil {
		return nil, err
	}

	// Vänta på svar eller timeout.
	select {

	case contacts :=
		<-responseChannel:

		return contacts, nil

	case <-time.After(
		kademlia.rpcTimeout(),
	):

		return nil,
			fmt.Errorf(
				"FIND_NODE timeout",
			)
	}
}

// Returnerar alpha-värdet.
// Default är 3.
func (kademlia *Kademlia) alpha() int {
	if kademlia.Alpha <= 0 {
		return defaultAlpha
	}

	return kademlia.Alpha
}

// Hittar noder nära ett target-ID.
func (kademlia *Kademlia) LookupContact(
	target *Contact,
) []Contact {

	if kademlia.RoutingTable == nil {
		fmt.Println(
			"Routing table is not initialized",
		)
		return nil
	}

	if target == nil ||
		target.ID == nil {

		fmt.Println(
			"Target is invalid",
		)
		return nil
	}

	// Kör parallell lookup.
	contacts :=
		kademlia.lookupContactParallel(
			target.ID,
			lookupContactCount,
		)

	for _, contact := range contacts {
		fmt.Println(
			"Found contact:",
			contact.String(),
		)
	}

	return contacts
}

// Sorterar kontakter efter XOR-avstånd.
func sortContactsByDistance(
	contacts []Contact,
) {
	candidates := ContactCandidates{
		contacts: contacts,
	}

	candidates.Sort()
}

// Returnerar högst count kontakter.
func firstContacts(
	contacts []Contact,
	count int,
) []Contact {

	if count <= 0 {
		return nil
	}

	if len(contacts) <= count {
		return contacts
	}

	return contacts[:count]
}

// Fake lookup för tester utan riktig UDP.
func NewFakeContactLookup(
	tables map[string]*RoutingTable,
) func(
	Contact,
	*KademliaID,
	int,
) []Contact {

	return func(
		contact Contact,
		target *KademliaID,
		count int,
	) []Contact {

		if contact.ID == nil {
			return nil
		}

		table :=
			tables[
				contact.ID.String()
			]

		if table == nil {
			return nil
		}

		return table.FindClosestContacts(
			target,
			count,
		)
	}
}

// Söker efter data med en hash/key.
func (kademlia *Kademlia) LookupData(
	hash string,
) {
	if hash == "" {
		fmt.Println(
			"Hash is empty",
		)
		return
	}

	if kademlia.Network == nil {
		fmt.Println(
			"Network is not initialized",
		)
		return
	}

	if kademlia.RoutingTable == nil {
		fmt.Println(
			"Routing table is not initialized",
		)
		return
	}

	target :=
		NewKademliaID(hash)

	if target == nil {
		fmt.Println(
			"Invalid hash",
		)
		return
	}

	// Enkel FIND_DATA-version.
	contacts :=
		kademlia.RoutingTable.FindClosestContacts(
			target,
			1,
		)

	if len(contacts) == 0 {
		fmt.Println(
			"No contact found",
		)
		return
	}

	contact := contacts[0]

	message :=
		[]byte(
			"FIND_DATA " +
				hash,
		)

	err :=
		kademlia.Network.SendData(
			contact.Address,
			message,
		)

	if err != nil {
		fmt.Println(
			"Could not send FIND_DATA:",
			err,
		)
		return
	}

	fmt.Println(
		"Looking for data with hash:",
		hash,
	)
}

// Lagrar data i Kademlia.
func (kademlia *Kademlia) Store(
	data []byte,
) {
	if len(data) == 0 {
		fmt.Println(
			"Data is empty",
		)
		return
	}

	if kademlia.Network == nil {
		fmt.Println(
			"Network is not initialized",
		)
		return
	}

	if kademlia.RoutingTable == nil {
		fmt.Println(
			"Routing table is not initialized",
		)
		return
	}

	// Skapa SHA-256 key.
	hash :=
		sha256.Sum256(data)

	key :=
		KademliaID(hash)

	// Hitta närmaste nod.
	contacts :=
		kademlia.RoutingTable.FindClosestContacts(
			&key,
			1,
		)

	if len(contacts) == 0 {
		fmt.Println(
			"No contact found for storage",
		)
		return
	}

	contact := contacts[0]

	// STORE <key> <data>
	message :=
		append(
			[]byte(
				"STORE "+
					key.String()+
					" ",
			),
			data...,
		)

	err :=
		kademlia.Network.SendData(
			contact.Address,
			message,
		)

	if err != nil {
		fmt.Println(
			"Could not send STORE:",
			err,
		)
		return
	}

	fmt.Println(
		"Data sent for storage with key:",
		key.String(),
	)
}

// Söker efter noder med alpha parallella probes.
func (kademlia *Kademlia) lookupContactParallel(
	target *KademliaID,
	count int,
) []Contact {

	if count <= 0 ||
		kademlia.RoutingTable == nil {
		return nil
	}

	candidates :=
		make(
			[]Contact,
			0,
			count,
		)

	// Noder vi redan känner till.
	seen :=
		make(map[string]bool)

	// Noder som redan har frågats.
	queried :=
		make(map[string]bool)

	// Lägg till nya kontakter utan dubbletter.
	addContacts :=
		func(
			contacts []Contact,
		) {

			for _, contact :=
				range contacts {

				if contact.ID == nil {
					continue
				}

				key :=
					contact.ID.String()

				if seen[key] {
					continue
				}

				// Räkna XOR-avstånd.
				contact.CalcDistance(
					target,
				)

				seen[key] = true

				candidates =
					append(
						candidates,
						contact,
					)
			}
		}

	// Börja med närmaste noder från routing table.
	initialContacts :=
		kademlia.RoutingTable.FindClosestContacts(
			target,
			count,
		)

	addContacts(
		initialContacts,
	)

	sortContactsByDistance(
		candidates,
	)

	candidates =
		firstContacts(
			candidates,
			count,
		)

	alpha :=
		kademlia.alpha()

	for {
		// Välj upp till alpha o-frågade noder.
		toQuery :=
			make(
				[]Contact,
				0,
				alpha,
			)

		for _, contact :=
			range candidates {

			if contact.ID == nil {
				continue
			}

			id :=
				contact.ID.String()

			if queried[id] {
				continue
			}

			toQuery =
				append(
					toQuery,
					contact,
				)

			// Max alpha noder per runda.
			if len(toQuery) == alpha {
				break
			}
		}

		// Inga fler noder att fråga.
		if len(toQuery) == 0 {
			break
		}

		// Avsluta bara om både fake lookup
		// och riktigt network saknas.
		if kademlia.ContactLookup == nil &&
			kademlia.Network == nil {

			break
		}

		// Markera noderna som frågade.
		for _, contact :=
			range toQuery {

			queried[
				contact.ID.String()
			] = true
		}

		// Fråga alla valda noder samtidigt.
		responses :=
			kademlia.queryContactsParallel(
				toQuery,
				target,
				count,
			)

		// Lägg till nya kontakter.
		for _, response :=
			range responses {

			addContacts(
				response,
			)
		}

		// Sortera efter XOR-avstånd.
		sortContactsByDistance(
			candidates,
		)

		candidates =
			firstContacts(
				candidates,
				count,
			)
	}

	return candidates
}

// Frågar flera noder samtidigt med strict parallelism.
func (kademlia *Kademlia) queryContactsParallel(
	contacts []Contact,
	target *KademliaID,
	count int,
) [][]Contact {

	results :=
		make(
			chan []Contact,
			len(contacts),
		)

	// Starta en goroutine per probe.
	for _, contact :=
		range contacts {

		contact := contact

		go func() {

			// Fake lookup används endast i tester.
			if kademlia.ContactLookup != nil {

				found :=
					kademlia.ContactLookup(
						contact,
						target,
						count,
					)

				results <- found
				return
			}

			// Riktig FIND_NODE RPC.
			found, err :=
				kademlia.sendFindNodeRPC(
					contact,
					target,
				)

			if err != nil {
				results <- nil
				return
			}

			results <- found
		}()
	}

	responses :=
		make(
			[][]Contact,
			0,
			len(contacts),
		)

	// Strict parallelism:
	// vänta på alla svar eller timeout.
	for i := 0;
		i < len(contacts);
		i++ {

		response :=
			<-results

		if response != nil {
			responses =
				append(
					responses,
					response,
				)
		}
	}

	return responses
}