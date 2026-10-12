package kademlia

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

const lookupContactCount = 10
const defaultAlpha = 3
const defaultK = 10
const defaultRPCTimeout = 2 * time.Second
const defaultRPCRetries = 3

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
	pointerMu        sync.Mutex
	pointerRPCMu     sync.Mutex
	pointerResponses map[string]chan rpcMessage
	RoutingTable     *RoutingTable
	Network          Node
	// Each receiver resolves trusted owner keys independently of the publisher.
	OwnershipVerifier OwnershipVerifier

	// Antal noder som frågas parallellt. Default är 3.
	Alpha int

	// Antal noder som data lagras på. Default är 10.
	K int

	// Antal försök för ett RPC innan vi ger upp.
	RPCRetries int

	// Timeout för RPC.
	RPCTimeout        time.Duration
	ReplicationPeriod time.Duration
	RefreshPeriod     time.Duration
	maintenanceMu     sync.Mutex
	maintenanceStop   chan struct{}
	maintenanceDone   chan struct{}
	evictionMu        sync.Mutex
	evictions         map[string]bool
	pingMu            sync.Mutex
	pingResponses     map[string]chan struct{}

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
	Status        string `json:"status,omitempty"`
	Error         string `json:"error,omitempty"`
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
	Source   Contact
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

func (kademlia *Kademlia) rpcRetries() int {
	if kademlia.RPCRetries <= 0 {
		return defaultRPCRetries
	}

	return kademlia.RPCRetries
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
	kademlia.RoutingTable.SetCapacity(kademlia.kValue())
	old, observed, full := kademlia.RoutingTable.Offer(contact)
	if !full || kademlia.Network == nil {
		return
	}
	kademlia.evictionMu.Lock()
	if kademlia.evictions == nil {
		kademlia.evictions = make(map[string]bool)
	}
	id := old.ID.String()
	if kademlia.evictions[id] {
		kademlia.evictionMu.Unlock()
		return
	}
	kademlia.evictions[id] = true
	kademlia.evictionMu.Unlock()
	go func() {
		defer func() { kademlia.evictionMu.Lock(); delete(kademlia.evictions, id); kademlia.evictionMu.Unlock() }()
		if _, err := kademlia.Ping(old); err != nil {
			kademlia.RoutingTable.ReplaceDead(old, contact, observed)
		}
	}()
}

func (kademlia *Kademlia) closestContacts(target *KademliaID, count int) []Contact {
	if kademlia.RoutingTable == nil || target == nil || count <= 0 {
		return nil
	}
	kademlia.RoutingTable.SetCapacity(kademlia.kValue())
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
	case "UPDATE_LATEST", "READ_LATEST":
		kademlia.handleLatestRPC(rpc)
	case "LATEST_REPLY":
		kademlia.handleLatestReply(rpc)
	case "PING":
		kademlia.handlePing(rpc)
	case "PING_REPLY":
		kademlia.handlePingReply(rpc)
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
	kademlia.findNodeMu.Lock()
	pending := kademlia.findNodeResponses[rpc.RequestID] != nil
	kademlia.findNodeMu.Unlock()
	if !pending {
		return
	}
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

	// Skickar RPC-förfrågan och försöker igen vid fel eller timeout.
	// Samma RequestID används för alla försök så att svaret kan matchas rätt.
	// Efter maximalt antal försök returneras det senaste felet.
	var lastErr error

	for attempt := 0; attempt < kademlia.rpcRetries(); attempt++ {
		log.Printf("event=probe rpc=%s target=%s attempt=%d request_id=%s", request.Type, contact.Address, attempt+1, requestID)
		err = kademlia.Network.SendData(contact.Address, data)

		if err != nil {
			lastErr = err
			continue
		}

		select {
		case contacts := <-responseChannel:
			return contacts, nil

		case <-time.After(kademlia.rpcTimeout()):
			lastErr = fmt.Errorf("FIND_NODE timeout")
		}
	}

	return nil, fmt.Errorf(
		"FIND_NODE failed after %d attempts: %v",
		kademlia.rpcRetries(),
		lastErr,
	)
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
	kademlia.findValueMu.Lock()
	pending := kademlia.findValueResponses[rpc.RequestID] != nil
	kademlia.findValueMu.Unlock()
	if !pending {
		return
	}
	kademlia.addRPCSender(rpc)

	result := findValueResult{
		Found:    rpc.Found,
		Value:    append([]byte(nil), rpc.Value...),
		Contacts: wireToContacts(rpc.Contacts),
		Source:   NewContact(NewKademliaID(rpc.SenderID), rpc.SenderAddress),
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

	// Skickar RPC-förfrågan och försöker igen vid fel eller timeout.
	// Samma RequestID används för alla försök så att svaret kan matchas rätt.
	// Efter maximalt antal försök returneras det senaste felet.
	var lastErr error

	for attempt := 0; attempt < kademlia.rpcRetries(); attempt++ {
		log.Printf("event=probe rpc=%s target=%s attempt=%d request_id=%s", request.Type, contact.Address, attempt+1, requestID)
		err = kademlia.Network.SendData(contact.Address, data)

		if err != nil {
			lastErr = err
			continue
		}

		select {
		case result := <-responseChannel:
			return result, nil

		case <-time.After(kademlia.rpcTimeout()):
			lastErr = latestError("timeout", "FIND_VALUE timeout")
		}
	}

	return empty, fmt.Errorf(
		"FIND_VALUE failed after %d attempts: %w",
		kademlia.rpcRetries(),
		lastErr,
	)
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
	kademlia.storeMu.Lock()
	pending := kademlia.storeResponses[rpc.RequestID] != nil
	kademlia.storeMu.Unlock()
	if !pending {
		return
	}
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

	// Skickar RPC-förfrågan och försöker igen vid fel eller timeout.
	// Samma RequestID används för alla försök så att svaret kan matchas rätt.
	// Efter maximalt antal försök returneras det senaste felet.
	var lastErr error

	for attempt := 0; attempt < kademlia.rpcRetries(); attempt++ {
		err = kademlia.Network.SendData(contact.Address, data)

		if err != nil {
			lastErr = err
			continue
		}

		select {
		case stored := <-responseChannel:
			if !stored {
				return fmt.Errorf("STORE rejected")
			}

			return nil

		case <-time.After(kademlia.rpcTimeout()):
			lastErr = fmt.Errorf("STORE timeout")
		}
	}

	return fmt.Errorf(
		"STORE failed after %d attempts: %v",
		kademlia.rpcRetries(),
		lastErr,
	)
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
	value, _, err := kademlia.LookupDataFrom(hash)
	return value, err
}

func (kademlia *Kademlia) LookupDataFrom(hash string) (value []byte, source Contact, err error) {
	defer func() {
		log.Printf("event=lookup_result type=value key=%s success=%t error=%q", hash, err == nil, errText(err))
	}()

	if kademlia.RoutingTable == nil {
		return nil, Contact{}, fmt.Errorf("routing table is not initialized")
	}
	if kademlia.Network == nil {
		return nil, Contact{}, fmt.Errorf("network is not initialized")
	}

	target := NewKademliaID(hash)
	if target == nil {
		return nil, Contact{}, fmt.Errorf("invalid hash")
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
			return localValue, kademlia.me(), nil
		}
		log.Printf("event=hash_error key=%s source=local", hash)
	}

	candidates := kademlia.closestContacts(target, kademlia.kValue())
	for i := range candidates {
		candidates[i].CalcDistance(target)
	}
	sortContactsByDistance(candidates)

	seen := make(map[string]bool)
	queried := make(map[string]bool)
	failed := make(map[string]bool)
	var lookupErr error

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
			if queried[id] || failed[id] {
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
			result  findValueResult
			err     error
			contact Contact
		}

		results := make(chan lookupResult, len(toQuery))

		// Strict parallelism.
		for _, contact := range toQuery {
			contact := contact
			go func() {
				result, err := kademlia.sendFindValueRPC(contact, target)
				results <- lookupResult{result: result, err: err, contact: contact}
			}()
		}

		// Vänta på alla probes i rundan.
		for i := 0; i < len(toQuery); i++ {
			response := <-results
			if response.err != nil {
				lookupErr = response.err
				kademlia.RoutingTable.Remove(response.contact)
				failed[response.contact.ID.String()] = true
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
					return append([]byte(nil), response.result.Value...), response.result.Source, nil
				}
				log.Printf("event=hash_error key=%s source=%s", hash, response.contact.Address)
				lookupErr = latestError("authentication", "record hash mismatch")
				continue
			}

			// Om värdet inte fanns, lägg till de närmare kontakter vi fick.
			for _, contact := range response.result.Contacts {
				if contact.ID == nil {
					continue
				}
				id := contact.ID.String()
				if seen[id] || failed[id] {
					continue
				}

				seen[id] = true
				contact.CalcDistance(target)
				candidates = append(candidates, contact)
			}
		}

		sortContactsByDistance(candidates)
		candidates = liveCandidates(candidates, failed)
	}

	if lookupErr != nil {
		return nil, Contact{}, lookupErr
	}
	return nil, Contact{}, latestError("missing_data", "value not found")
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

	if _, err := kademlia.Ping(bootstrap); err != nil {
		return fmt.Errorf("bootstrap unreachable: %w", err)
	}
	kademlia.addContact(bootstrap)
	me := kademlia.me()
	if me.ID == nil {
		return fmt.Errorf("local node ID is missing")
	}

	// Först hittar noden sina grannar.
	kademlia.LookupContact(&me)

	// Refresh each actual leaf range, including empty buckets. The dynamic tree
	// need not contain 256 leaves; b controls how eligible buckets split.
	for _, id := range kademlia.RoutingTable.RefreshTargets(0) {
		kademlia.LookupContact(&Contact{ID: id})
	}
	return nil
}

// Refresh gör en lookup efter ett slumpmässigt ID i vald bucket.
func (kademlia *Kademlia) Refresh(bucketIndex int) {
	if kademlia.RoutingTable == nil {
		return
	}
	if bucketIndex < 0 {
		return
	}

	target := kademlia.RoutingTable.RefreshIDForBucket(bucketIndex)
	if target == nil {
		return
	}
	targetContact := NewContact(target, "")
	kademlia.LookupContact(&targetContact)
}

// Söker efter noder med alpha parallella probes.
func (kademlia *Kademlia) lookupContactParallel(target *KademliaID, count int) (answer []Contact) {
	if count <= 0 || target == nil || kademlia.RoutingTable == nil || kademlia.Network == nil {
		return nil
	}
	defer func() { log.Printf("event=lookup_result type=node target=%s success=%t", target, len(answer) > 0) }()
	kademlia.RoutingTable.SetCapacity(kademlia.kValue())
	candidates := kademlia.closestContacts(target, count*kademlia.alpha())
	seen, queried, failed := map[string]bool{}, map[string]bool{}, map[string]bool{}
	me := kademlia.me()
	for _, c := range candidates {
		seen[c.ID.String()] = true
		c.CalcDistance(target)
	}
	for {
		candidates = liveCandidates(candidates, failed)
		sortContactsByDistance(candidates)
		shortlist := firstContacts(candidates, count)
		batch := []Contact{}
		for _, c := range shortlist {
			if !queried[c.ID.String()] {
				batch = append(batch, c)
				if len(batch) == kademlia.alpha() {
					break
				}
			}
		}
		if len(batch) == 0 {
			return shortlist
		}
		type result struct {
			contact  Contact
			contacts []Contact
			err      error
		}
		results := make(chan result, len(batch))
		for _, c := range batch {
			queried[c.ID.String()] = true
			go func(c Contact) {
				contacts, err := kademlia.sendFindNodeRPC(c, target)
				results <- result{c, contacts, err}
			}(c)
		}
		for range batch {
			r := <-results
			if r.err != nil {
				failed[r.contact.ID.String()] = true
				kademlia.RoutingTable.Remove(r.contact)
				continue
			}
			for _, c := range r.contacts {
				if c.ID == nil || c.ID.Equals(me.ID) || seen[c.ID.String()] {
					continue
				}
				seen[c.ID.String()] = true
				c.CalcDistance(target)
				candidates = append(candidates, c)
			}
		}
	}
}

func liveCandidates(contacts []Contact, failed map[string]bool) []Contact {
	result := make([]Contact, 0, len(contacts))
	for _, c := range contacts {
		if c.ID != nil && !failed[c.ID.String()] {
			result = append(result, c)
		}
	}
	return result
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
