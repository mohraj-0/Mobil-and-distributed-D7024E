package kademlia

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLatestTransportAndValidation(t *testing.T) {
	for _, kind := range []string{"simulated", "udp"} {
		t.Run(kind, func(t *testing.T) {
			pub, priv, _ := ed25519.GenerateKey(rand.Reader)
			verifier := DNSOwnershipVerifier{Resolver: NewFakeTXTResolver(map[string][]string{"example.com": {ownershipTXTPrefix + hex.EncodeToString(pub)}})}
			network := NewSimulatedNetwork()
			nodes := make([]*Kademlia, 2)
			for i := range nodes {
				var transport Node = NewSimulatedNodeWithNetwork(network)
				address := []string{"127.0.0.1:18500", "127.0.0.1:18501"}[i]
				if kind == "udp" {
					transport = NewUDPNode()
					address = "127.0.0.1:0"
				}
				if err := transport.Listen(address); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { transport.Close() })
				if udp, ok := transport.(*UDPNode); ok {
					address = udp.address
				}
				nodes[i] = &Kademlia{RoutingTable: NewRoutingTable(NewContact(testID([]string{"11", "ff"}[i]), address)), Network: transport, OwnershipVerifier: verifier, K: 2, RPCRetries: 1, RPCTimeout: 100 * time.Millisecond}
				go nodes[i].ListenForRPC()
			}
			a, b := nodes[0], nodes[1]
			a.addContact(b.me())
			b.addContact(a.me())
			blob, err := a.Store([]byte("package"))
			if err != nil {
				t.Fatal(err)
			}
			previous := strings.Repeat("0", 64)
			var first, latest LatestPointer
			for version := uint64(1); version <= 2; version++ {
				r := VersionRecord{Tag: VersionRecordTag, Domain: "example.com", Package: "demo", Version: version, BlobHash: blob, PreviousVersionRecord: previous}
				SignVersionRecord(&r, priv)
				data, _ := json.Marshal(r)
				hash, err := a.Store(data)
				if err != nil {
					t.Fatal(err)
				}
				latest = LatestPointer{Tag: LatestPointerTag, Domain: r.Domain, Package: r.Package, Version: version, VersionRecordHash: hash}
				SignLatestPointer(&latest, priv)
				results, err := a.PublishLatest(latest)
				if err != nil || len(results) != 2 {
					t.Fatalf("publish: %v %v", results, err)
				}
				for address, err := range results {
					if err != nil {
						t.Fatalf("%s: %v", address, err)
					}
				}
				got, err := b.LookupLatest(r.Domain, r.Package)
				if err != nil || *got != latest {
					t.Fatalf("lookup: %v %v", got, err)
				}
				if version == 1 {
					first = latest
				}
				previous = hash
			}
			key := LatestKey(latest.Domain, latest.Package)
			before := string(b.DataSnapshot()[key])
			dataReplay, _ := json.Marshal(latest)
			if _, err := a.latestRPC(b.me(), "UPDATE_LATEST", key, dataReplay); err != nil {
				t.Fatalf("identical replay: %v", err)
			}
			check := func(p LatestPointer, status string) {
				t.Helper()
				data, _ := json.Marshal(p)
				_, err := a.latestRPC(b.me(), "UPDATE_LATEST", key, data)
				var e *LatestError
				if !errors.As(err, &e) || e.Status != status {
					t.Fatalf("want %s got %v", status, err)
				}
				if string(b.DataSnapshot()[key]) != before {
					t.Fatal("rejection changed head")
				}
			}
			check(first, "conflict")
			fork := VersionRecord{Tag: VersionRecordTag, Domain: latest.Domain, Package: latest.Package, Version: 3, BlobHash: blob, PreviousVersionRecord: strings.Repeat("0", 64)}
			if err := SignVersionRecord(&fork, priv); err != nil {
				t.Fatal(err)
			}
			forkData, _ := json.Marshal(fork)
			forkHash, err := a.Store(forkData)
			if err != nil {
				t.Fatal(err)
			}
			forkPointer := latest
			forkPointer.Version, forkPointer.VersionRecordHash = 3, forkHash
			if err := SignLatestPointer(&forkPointer, priv); err != nil {
				t.Fatal(err)
			}
			check(forkPointer, "conflict")
			wrong := VersionRecord{Tag: VersionRecordTag, Domain: latest.Domain, Package: "other", Version: 3, BlobHash: blob, PreviousVersionRecord: previous}
			SignVersionRecord(&wrong, priv)
			encoded, _ := json.Marshal(wrong)
			wrongHash, err := a.Store(encoded)
			if err != nil {
				t.Fatal(err)
			}
			mismatch := latest
			mismatch.Version = 3
			mismatch.VersionRecordHash = wrongHash
			SignLatestPointer(&mismatch, priv)
			check(mismatch, "conflict")
			bad := latest
			bad.Tag = "unknown"
			check(bad, "malformed")
			bad = latest
			bad.Version = 3
			check(bad, "authentication")
			bad = latest
			bad.Version = 3
			bad.VersionRecordHash = strings.Repeat("a", 64)
			SignLatestPointer(&bad, priv)
			check(bad, "missing_data")
			_, err = a.latestRPC(b.me(), "UPDATE_LATEST", key, []byte("{"))
			var e *LatestError
			if !errors.As(err, &e) || e.Status != "malformed" {
				t.Fatal(err)
			}
			data, _ := json.Marshal(latest)
			if err = a.sendStoreRPC(b.me(), NewKademliaID(key), data); err == nil {
				t.Fatal("immutable STORE accepted named pointer")
			}
			if _, err := b.LookupData(key); err == nil {
				t.Fatal("immutable lookup accepted a mutable pointer")
			}
			_, err = a.latestRPC(b.me(), "READ_LATEST", LatestKey("example.com", "absent"), nil)
			if !errors.As(err, &e) || e.Status != "missing_data" {
				t.Fatalf("missing pointer: %v", err)
			}
			a.Replicate()
			if _, ok := b.DataSnapshot()[hashValue(data)]; ok {
				t.Fatal("replication stored pointer under content hash")
			}
			if string(b.DataSnapshot()[key]) != before {
				t.Fatal("replication changed pointer")
			}
			// A live UDP endpoint that never serves RPCs must produce timeout, not missing_data.
			var silent Node = NewSimulatedNodeWithNetwork(network)
			address := "127.0.0.1:18502"
			if kind == "udp" {
				silent = NewUDPNode()
				address = "127.0.0.1:0"
			}
			if err := silent.Listen(address); err != nil {
				t.Fatal(err)
			}
			defer silent.Close()
			if udp, ok := silent.(*UDPNode); ok {
				address = udp.address
			}
			_, err = a.latestRPC(NewContact(testID("77"), address), "READ_LATEST", key, nil)
			if !errors.As(err, &e) || e.Status != "timeout" {
				t.Fatalf("timeout: %v", err)
			}
			a.addContact(NewContact(testID("77"), address))
			missingRecord := latest
			missingRecord.VersionRecordHash = strings.Repeat("b", 64)
			if err := SignLatestPointer(&missingRecord, priv); err != nil {
				t.Fatal(err)
			}
			missingValue, _ := json.Marshal(missingRecord)
			_, _, err = a.verifyLatest(key, missingValue)
			if !errors.As(err, &e) || e.Status != "timeout" {
				t.Fatalf("ancestry timeout: %v", err)
			}
		})
	}
}
