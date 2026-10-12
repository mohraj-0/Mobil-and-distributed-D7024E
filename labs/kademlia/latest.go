package kademlia

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// LatestError preserves machine-readable rejection reasons across transports.
type LatestError struct {
	Status string
	Detail string
}

func (e *LatestError) Error() string          { return e.Status + ": " + e.Detail }
func latestError(status, detail string) error { return &LatestError{status, detail} }

// LatestKey is independent of the signed value. Identifiers must be canonical.
func LatestKey(domain, pkg string) string { return hashValue([]byte(domain + ":" + pkg + ":latest")) }
func canonicalNamespace(domain, pkg string) bool {
	return domain != "" && pkg != "" && domain == strings.ToLower(strings.TrimSpace(domain)) && !strings.HasSuffix(domain, ".") && pkg == strings.TrimSpace(pkg) && !strings.ContainsAny(domain+pkg, ":\r\n\t ")
}
func validHash(s string) bool {
	return len(s) == 64 && NewKademliaID(s) != nil && s == strings.ToLower(s)
}

// verifyLatest fetches and authenticates complete ancestry without mutating heads.
func (n *Kademlia) verifyLatest(key string, value []byte) (LatestPointer, []string, error) {
	var p LatestPointer
	if validateValueSize(value) != nil || json.Unmarshal(value, &p) != nil || !canonicalNamespace(p.Domain, p.Package) || key != LatestKey(p.Domain, p.Package) || !validHash(p.VersionRecordHash) || p.Tag != LatestPointerTag {
		return p, nil, latestError("malformed", "invalid pointer or name-derived key")
	}
	if err := n.VerifyOwnedLatestPointer(&p); err != nil {
		return p, nil, latestError("authentication", err.Error())
	}
	hashes := []string{}
	seen := map[string]bool{}
	hash := p.VersionRecordHash
	version := p.Version
	for hash != strings.Repeat("0", 64) {
		if !validHash(hash) || seen[hash] || len(hashes) >= 1024 {
			return p, nil, latestError("conflict", "invalid or cyclic ancestry")
		}
		seen[hash] = true
		value, err := n.LookupData(hash)
		if err != nil {
			var failure *LatestError
			if errors.As(err, &failure) {
				return p, nil, failure
			}
			return p, nil, latestError("missing_data", err.Error())
		}
		if hashValue(value) != hash {
			return p, nil, latestError("authentication", "record hash mismatch")
		}
		var r VersionRecord
		if json.Unmarshal(value, &r) != nil {
			return p, nil, latestError("malformed", "invalid version record")
		}
		if err := n.VerifyOwnedVersionRecord(&r); err != nil {
			return p, nil, latestError("authentication", err.Error())
		}
		if r.Domain != p.Domain || r.Package != p.Package || !validHash(r.BlobHash) || (len(hashes) == 0 && r.Version != p.Version) || (len(hashes) > 0 && r.Version >= version) {
			return p, nil, latestError("conflict", "record metadata or version ordering mismatch")
		}
		hashes = append(hashes, hash)
		version = r.Version
		hash = r.PreviousVersionRecord
	}
	if len(hashes) == 0 {
		return p, nil, latestError("conflict", "pointer references genesis sentinel")
	}
	return p, hashes, nil
}

// acceptLatest serializes final head checks after potentially slow record fetches.
func (n *Kademlia) acceptLatest(key string, value []byte) error {
	p, history, err := n.verifyLatest(key, value)
	if err != nil {
		return err
	}
	n.pointerMu.Lock()
	defer n.pointerMu.Unlock()
	n.ensureDataStore()
	n.dataMu.Lock()
	defer n.dataMu.Unlock()
	if old, ok := n.DataStore[key]; ok {
		var head LatestPointer
		if json.Unmarshal(old, &head) != nil {
			return latestError("conflict", "existing key is not a pointer")
		}
		if p == head {
			return nil
		}
		if p.Version <= head.Version {
			return latestError("conflict", "rollback or same-version replacement")
		}
		extends := false
		for _, h := range history {
			if h == head.VersionRecordHash {
				extends = true
				break
			}
		}
		if !extends {
			return latestError("conflict", "update does not extend accepted history")
		}
	}
	n.DataStore[key] = append([]byte(nil), value...)
	return nil
}

func (n *Kademlia) handleLatestRPC(r rpcMessage) {
	if n.Network == nil || r.SenderAddress == "" || r.RequestID == "" {
		return
	}
	reply := rpcMessage{Type: "LATEST_REPLY", RequestID: r.RequestID, Key: r.Key, Status: "accepted"}
	var err error
	if !validHash(r.Key) {
		err = latestError("malformed", "invalid key")
	} else if r.Type == "UPDATE_LATEST" {
		err = n.acceptLatest(r.Key, r.Value)
	} else {
		value, ok := n.DataSnapshot()[r.Key]
		if !ok {
			err = latestError("missing_data", "pointer not found")
		} else {
			_, _, err = n.verifyLatest(r.Key, value)
			if err == nil {
				reply.Value = value
				reply.Found = true
			}
		}
	}
	if err != nil {
		reply.Status = "malformed"
		var e *LatestError
		if errors.As(err, &e) {
			reply.Status = e.Status
		}
		reply.Error = err.Error()
	}
	data, _ := json.Marshal(reply)
	_ = n.Network.SendData(r.SenderAddress, data)
}
func (n *Kademlia) handleLatestReply(r rpcMessage) {
	n.pointerRPCMu.Lock()
	ch := n.pointerResponses[r.RequestID]
	n.pointerRPCMu.Unlock()
	if ch != nil {
		select {
		case ch <- r:
		default:
		}
	}
}
func (n *Kademlia) latestRPC(c Contact, kind, key string, value []byte) (rpcMessage, error) {
	if n.Network == nil || n.RoutingTable == nil {
		return rpcMessage{}, latestError("transport", "node is not initialized")
	}
	id, err := randomRequestID()
	if err != nil {
		return rpcMessage{}, err
	}
	ch := make(chan rpcMessage, 1)
	n.pointerRPCMu.Lock()
	if n.pointerResponses == nil {
		n.pointerResponses = map[string]chan rpcMessage{}
	}
	n.pointerResponses[id] = ch
	n.pointerRPCMu.Unlock()
	defer func() { n.pointerRPCMu.Lock(); delete(n.pointerResponses, id); n.pointerRPCMu.Unlock() }()
	me := n.me()
	request := rpcMessage{Type: kind, RequestID: id, SenderAddress: me.Address, Key: key, Value: value}
	if me.ID != nil {
		request.SenderID = me.ID.String()
	}
	data, err := json.Marshal(request)
	if err != nil {
		return rpcMessage{}, err
	}
	for attempt := 0; attempt < n.rpcRetries(); attempt++ {
		if err = n.Network.SendData(c.Address, data); err != nil {
			continue
		}
		timer := time.NewTimer(n.rpcTimeout())
		select {
		case r := <-ch:
			timer.Stop()
			if r.Key != key {
				return r, latestError("malformed", "reply key mismatch")
			}
			if r.Status != "accepted" {
				return r, latestError(r.Status, r.Error)
			}
			return r, nil
		case <-timer.C:
			err = latestError("timeout", "latest RPC timed out")
		}
	}
	var e *LatestError
	if errors.As(err, &e) {
		return rpcMessage{}, err
	}
	return rpcMessage{}, latestError("transport", fmt.Sprint(err))
}

func (n *Kademlia) latestPeers(key string) []Contact {
	id := NewKademliaID(key)
	peers := n.lookupContactParallel(id, n.kValue())
	peers = append(peers, n.me())
	unique := []Contact{}
	seen := map[string]bool{}
	for _, c := range peers {
		if c.ID != nil && !seen[c.ID.String()] {
			seen[c.ID.String()] = true
			c.CalcDistance(id)
			unique = append(unique, c)
		}
	}
	sortContactsByDistance(unique)
	return firstContacts(unique, n.kValue())
}

// PublishLatest returns a result for every selected replica, including rejections.
func (n *Kademlia) PublishLatest(p LatestPointer) (map[string]error, error) {
	value, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	key := LatestKey(p.Domain, p.Package)
	if _, _, err = n.verifyLatest(key, value); err != nil {
		return nil, err
	}
	if n.RoutingTable == nil || n.Network == nil {
		return nil, latestError("transport", "node is not initialized")
	}
	results := map[string]error{}
	accepted := 0
	me := n.me()
	for _, c := range n.latestPeers(key) {
		if c.ID.Equals(me.ID) {
			err = n.acceptLatest(key, value)
		} else {
			_, err = n.latestRPC(c, "UPDATE_LATEST", key, value)
		}
		results[c.Address] = err
		if err == nil {
			accepted++
		}
	}
	if accepted == 0 {
		return results, latestError("rejected", "no replica accepted pointer")
	}
	return results, nil
}

// LookupLatest compares authenticated replica responses. Conflicting branches
// are reported rather than silently resolved. Read repair is a separate policy.
func (n *Kademlia) LookupLatest(domain, pkg string) (*LatestPointer, error) {
	if !canonicalNamespace(domain, pkg) {
		return nil, latestError("malformed", "invalid namespace")
	}
	if n.RoutingTable == nil || n.Network == nil {
		return nil, latestError("transport", "node is not initialized")
	}
	key := LatestKey(domain, pkg)
	me := n.me()
	var best *LatestPointer
	var ancestry []string
	last := latestError("missing_data", "pointer not found")
	for _, c := range n.latestPeers(key) {
		var value []byte
		if c.ID.Equals(me.ID) {
			value = n.DataSnapshot()[key]
			if len(value) == 0 {
				continue
			}
		} else {
			r, err := n.latestRPC(c, "READ_LATEST", key, nil)
			if err != nil {
				last = err
				continue
			}
			value = r.Value
		}
		p, h, err := n.verifyLatest(key, value)
		if err != nil {
			last = err
			continue
		}
		if best != nil {
			newer, older, chain := p, *best, h
			if p.Version < best.Version {
				newer, older, chain = *best, p, ancestry
			}
			compatible := newer == older
			for _, hash := range chain {
				if hash == older.VersionRecordHash && newer.Version > older.Version {
					compatible = true
				}
			}
			if !compatible {
				return nil, latestError("conflict", "replicas report conflicting history")
			}
		}
		if best == nil || p.Version > best.Version {
			copy := p
			best = &copy
			ancestry = h
		}
	}
	if best == nil {
		return nil, last
	}
	return best, nil
}
