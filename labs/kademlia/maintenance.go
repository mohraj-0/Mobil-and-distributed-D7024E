package kademlia

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func hashValue(value []byte) string { hash := sha256.Sum256(value); return hex.EncodeToString(hash[:]) }

// Ping measures a direct correlated UDP request/response exchange. An unknown
// destination can be pinged without first knowing or trusting its node ID.
func (n *Kademlia) Ping(contact Contact) (time.Duration, error) {
	if n.Network == nil || n.RoutingTable == nil || contact.Address == "" {
		return 0, fmt.Errorf("invalid ping destination")
	}
	id, err := randomRequestID()
	if err != nil {
		return 0, err
	}
	ch := make(chan struct{}, 1)
	n.pingMu.Lock()
	if n.pingResponses == nil {
		n.pingResponses = make(map[string]chan struct{})
	}
	n.pingResponses[id] = ch
	n.pingMu.Unlock()
	defer func() { n.pingMu.Lock(); delete(n.pingResponses, id); n.pingMu.Unlock() }()
	me := n.me()
	if me.ID == nil {
		return 0, fmt.Errorf("local ID missing")
	}
	data, err := json.Marshal(rpcMessage{Type: "PING", RequestID: id, SenderID: me.ID.String(), SenderAddress: me.Address})
	if err != nil {
		return 0, err
	}
	for attempt := 0; attempt < n.rpcRetries(); attempt++ {
		start := time.Now()
		if err = n.Network.SendData(contact.Address, data); err != nil {
			continue
		}
		timer := time.NewTimer(n.rpcTimeout())
		select {
		case <-ch:
			timer.Stop()
			return time.Since(start), nil
		case <-timer.C:
			err = fmt.Errorf("PING timeout")
		}
	}
	return 0, fmt.Errorf("PING failed: %w", err)
}

func (n *Kademlia) handlePing(r rpcMessage) {
	if n.Network == nil || n.RoutingTable == nil || r.SenderAddress == "" {
		return
	}
	n.addRPCSender(r)
	me := n.me()
	if me.ID == nil {
		return
	}
	data, err := json.Marshal(rpcMessage{Type: "PING_REPLY", RequestID: r.RequestID, SenderID: me.ID.String(), SenderAddress: me.Address})
	if err == nil {
		_ = n.Network.SendData(r.SenderAddress, data)
	}
}

func (n *Kademlia) handlePingReply(r rpcMessage) {
	n.pingMu.Lock()
	ch := n.pingResponses[r.RequestID]
	n.pingMu.Unlock()
	if ch == nil {
		return
	}
	// Signal first so a routing eviction probe cannot block response delivery.
	select {
	case ch <- struct{}{}:
	default:
	}
	n.addRPCSender(r)
}

// DataSnapshot owns copies of the map and values; callers may inspect it while
// RPC handlers store data concurrently.
func (n *Kademlia) DataSnapshot() map[string][]byte {
	n.dataMu.RLock()
	defer n.dataMu.RUnlock()
	result := make(map[string][]byte, len(n.DataStore))
	for key, value := range n.DataStore {
		result[key] = append([]byte(nil), value...)
	}
	return result
}

// Replicate restores k copies of each locally held content-addressed value.
// Copies are never expired, including copies outside the current closest k.
func (n *Kademlia) Replicate() {
	for key, value := range n.DataSnapshot() {
		// Part 2 mutable pointers are not content-addressed blobs.
		if hashValue(value) != key {
			continue
		}
		_, _ = n.Store(value)
	}
}

func (n *Kademlia) StartMaintenance() {
	n.maintenanceMu.Lock()
	defer n.maintenanceMu.Unlock()
	if n.maintenanceStop != nil {
		return
	}
	replication, refresh := n.ReplicationPeriod, n.RefreshPeriod
	if replication <= 0 {
		replication = time.Hour
	}
	if refresh <= 0 {
		refresh = time.Hour
	}
	stop, done := make(chan struct{}), make(chan struct{})
	n.maintenanceStop, n.maintenanceDone = stop, done
	go func() {
		defer close(done)
		replicaTicker, refreshTicker := time.NewTicker(replication), time.NewTicker(refresh)
		defer replicaTicker.Stop()
		defer refreshTicker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-replicaTicker.C:
				n.Replicate()
			case <-refreshTicker.C:
				if n.RoutingTable != nil {
					for _, target := range n.RoutingTable.RefreshTargets(refresh) {
						select {
						case <-stop:
							return
						default:
						}
						n.LookupContact(&Contact{ID: target})
					}
				}
			}
		}
	}()
}

func (n *Kademlia) StopMaintenance() {
	n.maintenanceMu.Lock()
	stop, done := n.maintenanceStop, n.maintenanceDone
	if stop != nil {
		close(stop)
		n.maintenanceStop = nil
	}
	n.maintenanceMu.Unlock()
	if done != nil {
		<-done
	}
}
