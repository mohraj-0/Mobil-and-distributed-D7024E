package kademlia

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

// Testar att rätt domänägare kan verifieras,
// men att en annan ägares nyckel inte accepteras.
func TestOwnershipVerifierOwnerAndNonOwnerKeys(t *testing.T) {
	ownerPublicKey, ownerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate owner key pair: %v", err)
	}

	otherPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate other key pair: %v", err)
	}

	fakeDNS := NewFakeTXTResolver(map[string][]string{
		"example.com": {
			ownershipTXTPrefix + mustEncodePublicKey(t, ownerPublicKey),
		},
	})

	verifier := DNSOwnershipVerifier{
		Resolver: fakeDNS,
	}

	record := VersionRecord{
		Tag:                   VersionRecordTag,
		Domain:                "example.com",
		Package:               "mypackage",
		Version:               1,
		BlobHash:              "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PreviousVersionRecord: "0000000000000000000000000000000000000000000000000000000000000000",
	}

	if err := SignVersionRecord(&record, ownerPrivateKey); err != nil {
		t.Fatalf("sign version record: %v", err)
	}

	// Rätt owner key ska fungera.
	publicKey, err := verifier.ResolvePublicKey("example.com")
	if err != nil {
		t.Fatalf("resolve owner key: %v", err)
	}

	if err := VerifyVersionRecord(&record, publicKey); err != nil {
		t.Fatalf("valid owner key rejected: %v", err)
	}

	// Fel owner key ska inte kunna verifiera samma record.
	if err := VerifyVersionRecord(&record, otherPublicKey); err == nil {
		t.Fatal("expected non-owner key to be rejected")
	}
}

// Testar att en okänd domän avvisas.
func TestOwnershipVerifierRejectsUnknownDomain(t *testing.T) {
	fakeDNS := NewFakeTXTResolver(map[string][]string{})

	verifier := DNSOwnershipVerifier{
		Resolver: fakeDNS,
	}

	if _, err := verifier.ResolvePublicKey("unknown.example"); err == nil {
		t.Fatal("expected unknown domain to be rejected")
	}
}

// Testar att trasig TXT-data avvisas.
func TestOwnershipVerifierRejectsMalformedTXT(t *testing.T) {
	fakeDNS := NewFakeTXTResolver(map[string][]string{
		"example.com": {
			ownershipTXTPrefix + "not-a-valid-ed25519-key",
		},
	})

	verifier := DNSOwnershipVerifier{
		Resolver: fakeDNS,
	}

	if _, err := verifier.ResolvePublicKey("example.com"); err == nil {
		t.Fatal("expected malformed TXT data to be rejected")
	}
}

// Testar att två separata fake DNS-konfigurationer
// kan ha olika ägare för samma domän utan att påverka varandra.
func TestIndependentSimulatedDomainConfigurations(t *testing.T) {
	publicKeyA, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key A: %v", err)
	}

	publicKeyB, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key B: %v", err)
	}

	fakeDNSA := NewFakeTXTResolver(map[string][]string{
		"example.com": {
			ownershipTXTPrefix + mustEncodePublicKey(t, publicKeyA),
		},
	})

	fakeDNSB := NewFakeTXTResolver(map[string][]string{
		"example.com": {
			ownershipTXTPrefix + mustEncodePublicKey(t, publicKeyB),
		},
	})

	verifierA := DNSOwnershipVerifier{
		Resolver: fakeDNSA,
	}

	verifierB := DNSOwnershipVerifier{
		Resolver: fakeDNSB,
	}

	resolvedA, err := verifierA.ResolvePublicKey("example.com")
	if err != nil {
		t.Fatalf("resolve key A: %v", err)
	}

	resolvedB, err := verifierB.ResolvePublicKey("example.com")
	if err != nil {
		t.Fatalf("resolve key B: %v", err)
	}

	if string(resolvedA) == string(resolvedB) {
		t.Fatal("expected independent DNS configurations to use different keys")
	}
}

// Hjälpfunktion för tester.
// Gör om Ed25519 public key till samma hex-format
// som används i DNS TXT-records.
func mustEncodePublicKey(
	t *testing.T,
	publicKey ed25519.PublicKey,
) string {
	t.Helper()

	encoded, err := EncodePublicKey(publicKey)
	if err != nil {
		t.Fatalf("encode public key: %v", err)
	}

	return encoded
}
