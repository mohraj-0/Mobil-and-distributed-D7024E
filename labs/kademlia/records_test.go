package kademlia

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

// Skapar ett Ed25519-nyckelpar för tester.
func testKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate Ed25519 key: %v", err)
	}

	return publicKey, privateKey
}

// Skapar en giltig VersionRecord för tester.
func testVersionRecord() VersionRecord {
	return VersionRecord{
		Tag:                   VersionRecordTag,
		Domain:                "example.com",
		Package:               "mypackage",
		Version:               1,
		BlobHash:              "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PreviousVersionRecord: "0000000000000000000000000000000000000000000000000000000000000000",
	}
}

// Skapar en giltig LatestPointer för tester.
func testLatestPointer() LatestPointer {
	return LatestPointer{
		Tag:               LatestPointerTag,
		Domain:            "example.com",
		Package:           "mypackage",
		Version:           1,
		VersionRecordHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
}

// Testar att alla signerade fält i VersionRecord är bundna
// till signaturen. Om något ändras ska verifieringen misslyckas.
func TestVerifyVersionRecordRejectsTamperedFields(t *testing.T) {
	publicKey, privateKey := testKeyPair(t)

	tests := []struct {
		name   string
		change func(*VersionRecord)
	}{
		{
			name: "tag",
			change: func(record *VersionRecord) {
				record.Tag = "wrong-tag"
			},
		},
		{
			name: "domain",
			change: func(record *VersionRecord) {
				record.Domain = "attacker.com"
			},
		},
		{
			name: "package",
			change: func(record *VersionRecord) {
				record.Package = "otherpackage"
			},
		},
		{
			name: "version",
			change: func(record *VersionRecord) {
				record.Version = 2
			},
		},
		{
			name: "blob hash",
			change: func(record *VersionRecord) {
				record.BlobHash =
					"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
			},
		},
		{
			name: "previous version record",
			change: func(record *VersionRecord) {
				record.PreviousVersionRecord =
					"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := testVersionRecord()

			// Signera först en korrekt VersionRecord.
			if err := SignVersionRecord(&record, privateKey); err != nil {
				t.Fatalf("sign version record: %v", err)
			}

			// Kontrollera först att originalet är giltigt.
			if err := VerifyVersionRecord(&record, publicKey); err != nil {
				t.Fatalf("valid version record rejected: %v", err)
			}

			// Ändra ett signerat fält.
			test.change(&record)

			// Verifieringen måste misslyckas.
			if err := VerifyVersionRecord(&record, publicKey); err == nil {
				t.Fatalf(
					"expected verification to fail after tampering with %s",
					test.name,
				)
			}
		})
	}
}

// Testar att alla signerade fält i LatestPointer är bundna
// till signaturen.
func TestVerifyLatestPointerRejectsTamperedFields(t *testing.T) {
	publicKey, privateKey := testKeyPair(t)

	tests := []struct {
		name   string
		change func(*LatestPointer)
	}{
		{
			name: "tag",
			change: func(pointer *LatestPointer) {
				pointer.Tag = "wrong-tag"
			},
		},
		{
			name: "domain",
			change: func(pointer *LatestPointer) {
				pointer.Domain = "attacker.com"
			},
		},
		{
			name: "package",
			change: func(pointer *LatestPointer) {
				pointer.Package = "otherpackage"
			},
		},
		{
			name: "version",
			change: func(pointer *LatestPointer) {
				pointer.Version = 2
			},
		},
		{
			name: "version record hash",
			change: func(pointer *LatestPointer) {
				pointer.VersionRecordHash =
					"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pointer := testLatestPointer()

			// Signera först en korrekt LatestPointer.
			if err := SignLatestPointer(&pointer, privateKey); err != nil {
				t.Fatalf("sign latest pointer: %v", err)
			}

			// Kontrollera att originalet är giltigt.
			if err := VerifyLatestPointer(&pointer, publicKey); err != nil {
				t.Fatalf("valid latest pointer rejected: %v", err)
			}

			// Ändra ett signerat fält.
			test.change(&pointer)

			// Verifieringen måste misslyckas.
			if err := VerifyLatestPointer(&pointer, publicKey); err == nil {
				t.Fatalf(
					"expected verification to fail after tampering with %s",
					test.name,
				)
			}
		})
	}
}

// Testar att en record inte kan verifieras med
// en annan domänägares public key.
func TestRejectsDifferentDomainOwnerKey(t *testing.T) {
	ownerPublicKey, ownerPrivateKey := testKeyPair(t)
	attackerPublicKey, _ := testKeyPair(t)

	record := testVersionRecord()

	if err := SignVersionRecord(&record, ownerPrivateKey); err != nil {
		t.Fatalf("sign version record: %v", err)
	}

	// Kontrollera att rätt ägares nyckel fungerar.
	if err := VerifyVersionRecord(&record, ownerPublicKey); err != nil {
		t.Fatalf("valid owner key rejected: %v", err)
	}

	// En annan domänägares public key måste avvisas.
	if err := VerifyVersionRecord(&record, attackerPublicKey); err == nil {
		t.Fatal("expected verification to fail with different domain owner's key")
	}
}

// Testar att en VersionRecord-signatur inte kan
// återanvändas på en LatestPointer.
func TestRejectsVersionRecordSignatureOnLatestPointer(t *testing.T) {
	publicKey, privateKey := testKeyPair(t)

	record := testVersionRecord()

	if err := SignVersionRecord(&record, privateKey); err != nil {
		t.Fatalf("sign version record: %v", err)
	}

	pointer := testLatestPointer()

	// Försök återanvända VersionRecord-signaturen.
	pointer.Signature = record.Signature

	if err := VerifyLatestPointer(&pointer, publicKey); err == nil {
		t.Fatal(
			"expected latest pointer verification to fail with version record signature",
		)
	}
}

// Testar att en LatestPointer-signatur inte kan
// återanvändas på en VersionRecord.
func TestRejectsLatestPointerSignatureOnVersionRecord(t *testing.T) {
	publicKey, privateKey := testKeyPair(t)

	pointer := testLatestPointer()

	if err := SignLatestPointer(&pointer, privateKey); err != nil {
		t.Fatalf("sign latest pointer: %v", err)
	}

	record := testVersionRecord()

	// Försök återanvända LatestPointer-signaturen.
	record.Signature = pointer.Signature

	if err := VerifyVersionRecord(&record, publicKey); err == nil {
		t.Fatal(
			"expected version record verification to fail with latest pointer signature",
		)
	}
}

// punkt 5 som klar
