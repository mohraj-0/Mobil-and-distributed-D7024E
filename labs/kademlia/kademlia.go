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
const defaultK = 10
const defaultRPCTimeout = 2 * time.Second
const defaultReplicationPeriod = 30 * time.Second

// MaxValueSize is the maximum raw value size supported by both UDP and simulated
// transports. JSON base64 encoding expands a 32 KiB value to about 44 KiB,
// leaving room for RPC metadata in a single UDP datagram.
const MaxValueSize = 32 * 1024

func validateValueSize(value []byte) error {
	if len(value) == 0 {
		return fmt.Errorf("data is empty")
	}
	if len(value) > MaxValueSize {
		return fmt.Errorf("value size %d exceeds maximum %d bytes", len(value), MaxValueSize)
	}
	return nil
}

// RPC-typer
const (
	rpcFindNode       = "FIND_NODE"
	rpcFindNodeReply  = "FIND_NODE_REPLY"
	rpcFindValue      = "FIND_VALUE"
	rpcFindValueReply = "FIND_VALUE_REPLY"
	rpcStore          = "STORE"
	rpcStoreReply     = "STORE_REPLY"
)

// Kademlia innehåller logiken för en lokal Kademlia-nod.
type Kademlia struct {
	RoutingTable *RoutingTable
	Network      Node

	// Antal noder som frågas parallellt. Default är 3.
	Alpha int

	// Antal noder som data lagras på. Default är 10.
	K int

	// Timeout för RPC.
	RPCTimeout time.Duration

	// Hur ofta lagrade värden ska replikeras.
	ReplicationPeriod time.Duration

	// Skyddar routing table när flera goroutines arbetar samtidigt.
	routingMu sync.Mutex

	// Väntande FIND_NODE-svar.
	findNodeMu        sync.Mutex
	findNodeResponses map[string]chan []Contact

	// Väntande FIND_VALUE-svar.
	findValueMu        sync.Mutex
	findValueResponses map[string]chan findValueResult

	// Väntande STORE-svar.
	storeMu        sync.Mutex
	storeResponses map[string]chan bool

	// Lokal key-value store.
	dataMu    sync.RWMutex
	DataStore map[string][]byte
}

// Kontakt som skickas i RPC-meddelanden.
type wireContact struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

// Gemensamt RPC-meddelande.
type rpcMessage struct {
	Type          string `json:"type"`
	RequestID     string `json:"request_id"`
	SenderID      string `json:"sender_id"`
	SenderAddress string `json:"sender_address"`

	TargetID string `json:"target_id,omitempty"`

	Key    string `json:"key,omitempty"`
	Value  []byte `json:"value,omitempty"`
	Found  bool   `json:"found,omitempty"`
	Stored bool   `json:"stored,omitempty"`

	Contacts []wireContact `json:"contacts,omitempty"`
}

// Resultat från FIND_VALUE.
type findValueResult struct {
	Found    bool
	Value    []byte
	Contacts []Contact
}

// Returnerar alpha-värdet. Default är 3.
func (kademlia *Kademlia) alpha() int {
	if kademlia.Alpha <= 0 {
		return defaultAlpha
	}
	return kademlia.Alpha
}

// Returnerar k-värdet. Default är 10.
func (kademlia *Kademlia) kValue() int {
	if kademlia.K <= 0 {
		return defaultK
	}
	return kademlia.K
}

// Returnerar timeout för RPC.
func (kademlia *Kademlia) rpcTimeout() time.Duration {
	if kademlia.RPCTimeout <= 0 {
		return defaultRPCTimeout
	}
	return kademlia.RPCTimeout
}

// Returnerar replication period.
// Om inget värde satts används default.
func (kademlia *Kademlia) replicationPeriod() time.Duration {
	if kademlia.ReplicationPeriod <= 0 {
		return defaultReplicationPeriod
	}

	return kademlia.ReplicationPeriod
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

func (kademlia *Kademlia) ensureFindNodeResponses() {
	kademlia.findNodeMu.Lock()
	defer kademlia.findNodeMu.Unlock()
	if kademlia.findNodeResponses == nil {
		kademlia.findNodeResponses = make(map[string]chan []Contact)
	}
}

func (kademlia *Kademlia) ensureFindValueResponses() {
	kademlia.findValueMu.Lock()
	defer kademlia.findValueMu.Unlock()
	if kademlia.findValueResponses == nil {
		kademlia.findValueResponses = make(map[string]chan findValueResult)
	}
}

func (kademlia *Kademlia) ensureStoreResponses() {
	kademlia.storeMu.Lock()
	defer kademlia.storeMu.Unlock()
	if kademlia.storeResponses == nil {
		kademlia.storeResponses = make(map[string]chan bool)
	}
}

func (kademlia *Kademlia) ensureDataStore() {
	kademlia.dataMu.Lock()
	defer kademlia.dataMu.Unlock()
	if kademlia.DataStore == nil {
		kademlia.DataStore = make(map[string][]byte)
	}
}

func (kademlia *Kademlia) me() Contact {
	kademlia.routingMu.Lock()
	defer kademlia.routingMu.Unlock()
	return kademlia.RoutingTable.me
}

func (kademlia *Kademlia) addContact(contact Contact) {
	if kademlia.RoutingTable == nil || contact.ID == nil {
		return
	}
	kademlia.routingMu.Lock()
	defer kademlia.routingMu.Unlock()
	kademlia.RoutingTable.AddContact(contact)
}

func (kademlia *Kademlia) closestContacts(target *KademliaID, count int) []Contact {
	if kademlia.RoutingTable == nil || target == nil || count <= 0 {
		return nil
	}
	kademlia.routingMu.Lock()
	defer kademlia.routingMu.Unlock()
	return kademlia.RoutingTable.FindClosestContacts(target, count)
}

func contactsToWire(contacts []Contact) []wireContact {
	result := make([]wireContact, 0, len(contacts))
	for _, contact := range contacts {
		if contact.ID == nil {
			continue
		}
		result = append(result, wireContact{
			ID:      contact.ID.String(),
			Address: contact.Address,
		})
	}
	return result
}

func wireToContacts(contacts []wireContact) []Contact {
	result := make([]Contact, 0, len(contacts))
	for _, contact := range contacts {
		id := NewKademliaID(contact.ID)
		if id == nil || contact.Address == "" {
			continue
		}
		result = append(result, NewContact(id, contact.Address))
	}
	return result
}

// Lyssnar efter inkommande RPC-meddelanden.
func (kademlia *Kademlia) ListenForRPC() {
	if kademlia.Network == nil {
		return
	}

	for {
		message, err := kademlia.Network.Receive()
		if err != nil {
			return
		}
		go kademlia.handleRPCMessage(message)
	}
}

// Hanterar alla RPC-typer.
func (kademlia *Kademlia) handleRPCMessage(message Message) {
	var rpc rpcMessage
	if err := json.Unmarshal(message.Data, &rpc); err != nil {
		return
	}

	switch rpc.Type {
	case rpcFindNode:
		kademlia.handleFindNodeRPC(rpc)
	case rpcFindNodeReply:
		kademlia.handleFindNodeReplyRPC(rpc)
	case rpcFindValue:
		kademlia.handleFindValueRPC(rpc)
	case rpcFindValueReply:
		kademlia.handleFindValueReplyRPC(rpc)
	case rpcStore:
		kademlia.handleStoreRPC(rpc)
	case rpcStoreReply:
		kademlia.handleStoreReplyRPC(rpc)
	}
}

func (kademlia *Kademlia) addRPCSender(rpc rpcMessage) {
	if rpc.SenderID == "" || rpc.SenderAddress == "" {
		return
	}
	id := NewKademliaID(rpc.SenderID)
	if id == nil {
		return
	}
	kademlia.addContact(NewContact(id, rpc.SenderAddress))
}

// Hanterar FIND_NODE.
func (kademlia *Kademlia) handleFindNodeRPC(rpc rpcMessage) {
	if kademlia.RoutingTable == nil || kademlia.Network == nil {
		return
	}

	targetID := NewKademliaID(rpc.TargetID)
	if targetID == nil || rpc.SenderAddress == "" {
		return
	}

	kademlia.addRPCSender(rpc)
	contacts := kademlia.closestContacts(targetID, kademlia.kValue())
	me := kademlia.me()
	if me.ID == nil {
		return
	}

	reply := rpcMessage{
		Type:          rpcFindNodeReply,
		RequestID:     rpc.RequestID,
		SenderID:      me.ID.String(),
		SenderAddress: me.Address,
		Contacts:      contactsToWire(contacts),
	}

	data, err := json.Marshal(reply)
	if err != nil {
		return
	}
	_ = kademlia.Network.SendData(rpc.SenderAddress, data)
}

// Hanterar FIND_NODE_REPLY.
func (kademlia *Kademlia) handleFindNodeReplyRPC(rpc rpcMessage) {
	kademlia.addRPCSender(rpc)
	contacts := wireToContacts(rpc.Contacts)

	for _, contact := range contacts {
		kademlia.addContact(contact)
	}

	kademlia.ensureFindNodeResponses()
	kademlia.findNodeMu.Lock()
	channel := kademlia.findNodeResponses[rpc.RequestID]
	kademlia.findNodeMu.Unlock()

	if channel == nil {
		return
	}

	select {
	case channel <- contacts:
	default:
	}
}

// Skickar FIND_NODE och väntar på svar.
func (kademlia *Kademlia) sendFindNodeRPC(contact Contact, target *KademliaID) ([]Contact, error) {
	if kademlia.Network == nil || kademlia.RoutingTable == nil {
		return nil, fmt.Errorf("network or routing table is not initialized")
	}
	if contact.ID == nil || contact.Address == "" || target == nil {
		return nil, fmt.Errorf("invalid FIND_NODE request")
	}

	requestID, err := randomRequestID()
	if err != nil {
		return nil, err
	}

	responseChannel := make(chan []Contact, 1)
	kademlia.ensureFindNodeResponses()
	kademlia.findNodeMu.Lock()
	kademlia.findNodeResponses[requestID] = responseChannel
	kademlia.findNodeMu.Unlock()

	defer func() {
		kademlia.findNodeMu.Lock()
		delete(kademlia.findNodeResponses, requestID)
		kademlia.findNodeMu.Unlock()
	}()

	me := kademlia.me()
	if me.ID == nil || me.Address == "" {
		return nil, fmt.Errorf("local contact is invalid")
	}

	request := rpcMessage{
		Type:          rpcFindNode,
		RequestID:     requestID,
		SenderID:      me.ID.String(),
		SenderAddress: me.Address,
		TargetID:      target.String(),
	}

	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	if err = kademlia.Network.SendData(contact.Address, data); err != nil {
		return nil, err
	}

	select {
	case contacts := <-responseChannel:
		return contacts, nil
	case <-time.After(kademlia.rpcTimeout()):
		return nil, fmt.Errorf("FIND_NODE timeout")
	}
}

// Hanterar FIND_VALUE.
func (kademlia *Kademlia) handleFindValueRPC(rpc rpcMessage) {
	if kademlia.RoutingTable == nil || kademlia.Network == nil {
		return
	}

	key := NewKademliaID(rpc.Key)
	if key == nil || rpc.SenderAddress == "" {
		return
	}

	kademlia.addRPCSender(rpc)
	kademlia.ensureDataStore()

	kademlia.dataMu.RLock()
	value, found := kademlia.DataStore[rpc.Key]
	found = found && validateValueSize(value) == nil
	if found {
		value = append([]byte(nil), value...)
	}
	kademlia.dataMu.RUnlock()

	me := kademlia.me()
	if me.ID == nil {
		return
	}

	reply := rpcMessage{
		Type:          rpcFindValueReply,
		RequestID:     rpc.RequestID,
		SenderID:      me.ID.String(),
		SenderAddress: me.Address,
		Key:           rpc.Key,
		Found:         found,
	}

	if found {
		reply.Value = value
	} else {
		contacts := kademlia.closestContacts(key, kademlia.kValue())
		reply.Contacts = contactsToWire(contacts)
	}

	data, err := json.Marshal(reply)
	if err != nil {
		return
	}
	_ = kademlia.Network.SendData(rpc.SenderAddress, data)
}

// Hanterar FIND_VALUE_REPLY.
func (kademlia *Kademlia) handleFindValueReplyRPC(rpc rpcMessage) {
	kademlia.addRPCSender(rpc)

	result := findValueResult{
		Found:    rpc.Found,
		Value:    append([]byte(nil), rpc.Value...),
		Contacts: wireToContacts(rpc.Contacts),
	}

	for _, contact := range result.Contacts {
		kademlia.addContact(contact)
	}

	kademlia.ensureFindValueResponses()
	kademlia.findValueMu.Lock()
	channel := kademlia.findValueResponses[rpc.RequestID]
	kademlia.findValueMu.Unlock()

	if channel == nil {
		return
	}

	select {
	case channel <- result:
	default:
	}
}

// Skickar FIND_VALUE och väntar på svar.
func (kademlia *Kademlia) sendFindValueRPC(contact Contact, key *KademliaID) (findValueResult, error) {
	var empty findValueResult

	if kademlia.Network == nil || kademlia.RoutingTable == nil {
		return empty, fmt.Errorf("network or routing table is not initialized")
	}
	if contact.ID == nil || contact.Address == "" || key == nil {
		return empty, fmt.Errorf("invalid FIND_VALUE request")
	}

	requestID, err := randomRequestID()
	if err != nil {
		return empty, err
	}

	responseChannel := make(chan findValueResult, 1)
	kademlia.ensureFindValueResponses()
	kademlia.findValueMu.Lock()
	kademlia.findValueResponses[requestID] = responseChannel
	kademlia.findValueMu.Unlock()

	defer func() {
		kademlia.findValueMu.Lock()
		delete(kademlia.findValueResponses, requestID)
		kademlia.findValueMu.Unlock()
	}()

	me := kademlia.me()
	if me.ID == nil || me.Address == "" {
		return empty, fmt.Errorf("local contact is invalid")
	}

	request := rpcMessage{
		Type:          rpcFindValue,
		RequestID:     requestID,
		SenderID:      me.ID.String(),
		SenderAddress: me.Address,
		Key:           key.String(),
	}

	data, err := json.Marshal(request)
	if err != nil {
		return empty, err
	}

	if err = kademlia.Network.SendData(contact.Address, data); err != nil {
		return empty, err
	}

	select {
	case result := <-responseChannel:
		return result, nil
	case <-time.After(kademlia.rpcTimeout()):
		return empty, fmt.Errorf("FIND_VALUE timeout")
	}
}

// Hanterar STORE och verifierar key == SHA-256(value).
func (kademlia *Kademlia) handleStoreRPC(rpc rpcMessage) {
	if kademlia.Network == nil || kademlia.RoutingTable == nil || rpc.SenderAddress == "" {
		return
	}

	kademlia.addRPCSender(rpc)

	hash := sha256.Sum256(rpc.Value)
	calculatedKey := KademliaID(hash)
	stored := validateValueSize(rpc.Value) == nil && calculatedKey.String() == rpc.Key

	if stored {
		kademlia.ensureDataStore()
		kademlia.dataMu.Lock()
		kademlia.DataStore[rpc.Key] = append([]byte(nil), rpc.Value...)
		kademlia.dataMu.Unlock()
	}

	me := kademlia.me()
	if me.ID == nil {
		return
	}

	reply := rpcMessage{
		Type:          rpcStoreReply,
		RequestID:     rpc.RequestID,
		SenderID:      me.ID.String(),
		SenderAddress: me.Address,
		Stored:        stored,
	}

	data, err := json.Marshal(reply)
	if err != nil {
		return
	}
	_ = kademlia.Network.SendData(rpc.SenderAddress, data)
}

// Hanterar STORE_REPLY.
func (kademlia *Kademlia) handleStoreReplyRPC(rpc rpcMessage) {
	kademlia.addRPCSender(rpc)
	kademlia.ensureStoreResponses()

	kademlia.storeMu.Lock()
	channel := kademlia.storeResponses[rpc.RequestID]
	kademlia.storeMu.Unlock()

	if channel == nil {
		return
	}

	select {
	case channel <- rpc.Stored:
	default:
	}
}

// Skickar STORE och väntar på svar.
func (kademlia *Kademlia) sendStoreRPC(contact Contact, key *KademliaID, value []byte) error {
	if err := validateValueSize(value); err != nil {
		return err
	}
	if kademlia.Network == nil || kademlia.RoutingTable == nil {
		return fmt.Errorf("network or routing table is not initialized")
	}
	if contact.ID == nil || contact.Address == "" || key == nil {
		return fmt.Errorf("invalid STORE request")
	}

	requestID, err := randomRequestID()
	if err != nil {
		return err
	}

	responseChannel := make(chan bool, 1)
	kademlia.ensureStoreResponses()
	kademlia.storeMu.Lock()
	kademlia.storeResponses[requestID] = responseChannel
	kademlia.storeMu.Unlock()

	defer func() {
		kademlia.storeMu.Lock()
		delete(kademlia.storeResponses, requestID)
		kademlia.storeMu.Unlock()
	}()

	me := kademlia.me()
	if me.ID == nil || me.Address == "" {
		return fmt.Errorf("local contact is invalid")
	}

	request := rpcMessage{
		Type:          rpcStore,
		RequestID:     requestID,
		SenderID:      me.ID.String(),
		SenderAddress: me.Address,
		Key:           key.String(),
		Value:         append([]byte(nil), value...),
	}

	data, err := json.Marshal(request)
	if err != nil {
		return err
	}

	if err = kademlia.Network.SendData(contact.Address, data); err != nil {
		return err
	}

	select {
	case stored := <-responseChannel:
		if !stored {
			return fmt.Errorf("STORE rejected")
		}
		return nil
	case <-time.After(kademlia.rpcTimeout()):
		return fmt.Errorf("STORE timeout")
	}
}

// Hittar noder nära ett target-ID.
func (kademlia *Kademlia) LookupContact(target *Contact) []Contact {
	if kademlia.RoutingTable == nil {
		fmt.Println("Routing table is not initialized")
		return nil
	}
	if target == nil || target.ID == nil {
		fmt.Println("Target is invalid")
		return nil
	}

	return kademlia.lookupContactParallel(target.ID, kademlia.kValue())
}

// Söker iterativt efter data med alpha parallella FIND_VALUE-requests.
func (kademlia *Kademlia) LookupData(hash string) ([]byte, error) {
	if kademlia.RoutingTable == nil {
		return nil, fmt.Errorf("routing table is not initialized")
	}
	if kademlia.Network == nil {
		return nil, fmt.Errorf("network is not initialized")
	}

	target := NewKademliaID(hash)
	if target == nil {
		return nil, fmt.Errorf("invalid hash")
	}

	kademlia.ensureDataStore()

	// Kontrollera först om datan finns lokalt.
	kademlia.dataMu.RLock()
	localValue, exists := kademlia.DataStore[hash]
	if exists {
		localValue = append([]byte(nil), localValue...)
	}
	kademlia.dataMu.RUnlock()

	if exists {
		sum := sha256.Sum256(localValue)
		calculatedKey := KademliaID(sum)
		if validateValueSize(localValue) == nil && calculatedKey.String() == hash {
			return localValue, nil
		}
	}

	candidates := kademlia.closestContacts(target, kademlia.kValue())
	for i := range candidates {
		candidates[i].CalcDistance(target)
	}
	sortContactsByDistance(candidates)

	seen := make(map[string]bool)
	queried := make(map[string]bool)

	for _, contact := range candidates {
		if contact.ID != nil {
			seen[contact.ID.String()] = true
		}
	}

	for {
		toQuery := make([]Contact, 0, kademlia.alpha())

		for _, contact := range candidates {
			if contact.ID == nil {
				continue
			}
			id := contact.ID.String()
			if queried[id] {
				continue
			}

			toQuery = append(toQuery, contact)
			if len(toQuery) == kademlia.alpha() {
				break
			}
		}

		if len(toQuery) == 0 {
			break
		}

		for _, contact := range toQuery {
			queried[contact.ID.String()] = true
		}

		type lookupResult struct {
			result findValueResult
			err    error
		}

		results := make(chan lookupResult, len(toQuery))

		// Strict parallelism.
		for _, contact := range toQuery {
			contact := contact
			go func() {
				result, err := kademlia.sendFindValueRPC(contact, target)
				results <- lookupResult{result: result, err: err}
			}()
		}

		// Vänta på alla probes i rundan.
		for i := 0; i < len(toQuery); i++ {
			response := <-results
			if response.err != nil {
				continue
			}

			if response.result.Found {
				if validateValueSize(response.result.Value) != nil {
					continue
				}
				sum := sha256.Sum256(response.result.Value)
				calculatedKey := KademliaID(sum)

				// Acceptera bara värdet om hash(value) == key.
				if calculatedKey.String() == target.String() {
					return append([]byte(nil), response.result.Value...), nil
				}
				fmt.Println("Corrupted value received: hash does not match key")
				continue
			}

			// Om värdet inte fanns, lägg till de närmare kontakter vi fick.
			for _, contact := range response.result.Contacts {
				if contact.ID == nil {
					continue
				}
				id := contact.ID.String()
				if seen[id] {
					continue
				}

				seen[id] = true
				contact.CalcDistance(target)
				candidates = append(candidates, contact)
			}
		}

		sortContactsByDistance(candidates)
		candidates = firstContacts(candidates, kademlia.kValue())
	}

	return nil, fmt.Errorf("value not found")
}

// ReplicateData replikerar alla lokalt lagrade värden
// till de k närmaste noderna igen.
func (kademlia *Kademlia) ReplicateData() {
	kademlia.ensureDataStore()

	// Kopiera värden först så att vi inte håller låset
	// medan nätverksanrop görs.
	kademlia.dataMu.RLock()

	values := make([][]byte, 0, len(kademlia.DataStore))

	for _, value := range kademlia.DataStore {
		values = append(
			values,
			append([]byte(nil), value...),
		)
	}

	kademlia.dataMu.RUnlock()

	// Store gör lookup och lagrar på de k närmaste noderna.
	for _, value := range values {
		_, err := kademlia.Store(value)

		if err != nil {
			fmt.Println(
				"Replication failed:",
				err,
			)
		}
	}
}

// StartReplication startar periodisk replikering.
// Stop-kanalen används för att avsluta bakgrundsarbetet.
func (kademlia *Kademlia) StartReplication(
	stop <-chan struct{},
) {
	ticker := time.NewTicker(
		kademlia.replicationPeriod(),
	)

	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			kademlia.ReplicateData()

		case <-stop:
			return
		}
	}
}

// Lagrar data på de k närmaste noderna.
func (kademlia *Kademlia) Store(data []byte) (string, error) {
	if err := validateValueSize(data); err != nil {
		return "", err
	}
	if kademlia.RoutingTable == nil {
		return "", fmt.Errorf("routing table is not initialized")
	}
	if kademlia.Network == nil {
		return "", fmt.Errorf("network is not initialized")
	}

	hash := sha256.Sum256(data)
	key := KademliaID(hash)

	contacts := kademlia.lookupContactParallel(&key, kademlia.kValue())
	me := kademlia.me()

	all := append([]Contact{}, contacts...)
	if me.ID != nil {
		all = append(all, me)
	}

	unique := make([]Contact, 0, len(all))
	seen := make(map[string]bool)

	for _, contact := range all {
		if contact.ID == nil {
			continue
		}
		id := contact.ID.String()
		if seen[id] {
			continue
		}

		seen[id] = true
		contact.CalcDistance(&key)
		unique = append(unique, contact)
	}

	sortContactsByDistance(unique)
	unique = firstContacts(unique, kademlia.kValue())
	kademlia.ensureDataStore()

	var wg sync.WaitGroup
	var resultMu sync.Mutex
	successful := 0

	for _, contact := range unique {
		contact := contact

		// Om vi själva är bland de k närmaste lagrar vi lokalt.
		if me.ID != nil && contact.ID.Equals(me.ID) {
			kademlia.dataMu.Lock()
			kademlia.DataStore[key.String()] = append([]byte(nil), data...)
			kademlia.dataMu.Unlock()
			resultMu.Lock()
			successful++
			resultMu.Unlock()
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := kademlia.sendStoreRPC(contact, &key, data); err == nil {
				resultMu.Lock()
				successful++
				resultMu.Unlock()
			}
		}()
	}

	wg.Wait()

	if successful == 0 {
		return key.String(), fmt.Errorf("could not store value on any node")
	}

	return key.String(), nil
}

// Join ansluter noden till ett befintligt Kademlia-nätverk.
func (kademlia *Kademlia) Join(bootstrap Contact) error {
	if kademlia.RoutingTable == nil {
		return fmt.Errorf("routing table is not initialized")
	}
	if kademlia.Network == nil {
		return fmt.Errorf("network is not initialized")
	}
	if bootstrap.ID == nil || bootstrap.Address == "" {
		return fmt.Errorf("invalid bootstrap contact")
	}

	kademlia.addContact(bootstrap)
	me := kademlia.me()
	if me.ID == nil {
		return fmt.Errorf("local node ID is missing")
	}

	// Först hittar noden sina grannar.
	kademlia.LookupContact(&me)

	// Sedan refreshas alla 256 buckets.
	for bucketIndex := 0; bucketIndex < IDLength*8; bucketIndex++ {
		kademlia.Refresh(bucketIndex)
	}

	return nil
}

// Refresh gör en lookup efter ett slumpmässigt ID i vald bucket.
func (kademlia *Kademlia) Refresh(bucketIndex int) {
	if kademlia.RoutingTable == nil {
		return
	}
	if bucketIndex < 0 || bucketIndex >= IDLength*8 {
		return
	}

	me := kademlia.me()
	if me.ID == nil {
		return
	}

	target := *me.ID
	byteIndex := bucketIndex / 8
	bitIndex := bucketIndex % 8

	// Flippa den första biten som skiljer target från vårt eget ID.
	mask := byte(1 << (7 - bitIndex))
	target[byteIndex] ^= mask

	// Randomisera alla mindre signifikanta bitar.
	randomBytes := make([]byte, IDLength)
	if _, err := rand.Read(randomBytes); err != nil {
		return
	}

	for bit := bucketIndex + 1; bit < IDLength*8; bit++ {
		b := bit / 8
		position := bit % 8
		bitMask := byte(1 << (7 - position))

		if randomBytes[b]&bitMask != 0 {
			target[b] |= bitMask
		} else {
			target[b] &^= bitMask
		}
	}

	targetContact := NewContact(&target, "")
	kademlia.LookupContact(&targetContact)
}

// Söker efter noder med alpha parallella probes.
func (kademlia *Kademlia) lookupContactParallel(target *KademliaID, count int) []Contact {
	if count <= 0 || target == nil || kademlia.RoutingTable == nil || kademlia.Network == nil {
		return nil
	}

	candidates := make([]Contact, 0, count)
	seen := make(map[string]bool)
	queried := make(map[string]bool)
	me := kademlia.me()

	addContacts := func(contacts []Contact) {
		for _, contact := range contacts {
			if contact.ID == nil {
				continue
			}

			// Lägg inte till oss själva i shortlist.
			if me.ID != nil && contact.ID.Equals(me.ID) {
				continue
			}

			key := contact.ID.String()
			if seen[key] {
				continue
			}

			contact.CalcDistance(target)
			seen[key] = true
			candidates = append(candidates, contact)
		}
	}

	initialContacts := kademlia.closestContacts(target, count)
	addContacts(initialContacts)
	sortContactsByDistance(candidates)
	candidates = firstContacts(candidates, count)

	for {
		toQuery := make([]Contact, 0, kademlia.alpha())

		for _, contact := range candidates {
			if contact.ID == nil {
				continue
			}
			id := contact.ID.String()
			if queried[id] {
				continue
			}

			toQuery = append(toQuery, contact)
			if len(toQuery) == kademlia.alpha() {
				break
			}
		}

		if len(toQuery) == 0 {
			break
		}

		for _, contact := range toQuery {
			queried[contact.ID.String()] = true
		}

		responses := kademlia.queryContactsParallel(toQuery, target)

		for _, response := range responses {
			addContacts(response)
		}

		sortContactsByDistance(candidates)
		candidates = firstContacts(candidates, count)
	}

	return candidates
}

// Frågar flera noder samtidigt med strict parallelism.
func (kademlia *Kademlia) queryContactsParallel(contacts []Contact, target *KademliaID) [][]Contact {
	results := make(chan []Contact, len(contacts))

	for _, contact := range contacts {
		contact := contact
		go func() {
			found, err := kademlia.sendFindNodeRPC(contact, target)
			if err != nil {
				results <- nil
				return
			}
			results <- found
		}()
	}

	responses := make([][]Contact, 0, len(contacts))

	// Strict parallelism: vänta på hela rundan.
	for i := 0; i < len(contacts); i++ {
		response := <-results
		if response != nil {
			responses = append(responses, response)
		}
	}

	return responses
}

// Sorterar kontakter efter XOR-avstånd.
func sortContactsByDistance(contacts []Contact) {
	candidates := ContactCandidates{contacts: contacts}
	candidates.Sort()
}

// Returnerar högst count kontakter.
func firstContacts(contacts []Contact, count int) []Contact {
	if count <= 0 {
		return nil
	}
	if len(contacts) <= count {
		return contacts
	}
	return contacts[:count]
}
