package metadata

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// GeneratePublisherKeyPair skapar ett nytt Ed25519-nyckelpar.
//
// Private key används lokalt av paketägaren för signering.
// Public key konfigureras i ownership-providern, till exempel fake DNS.
//
// Private key får aldrig skickas i RPC eller lagras i ett record.
func GeneratePublisherKeyPair() (
	ed25519.PublicKey,
	ed25519.PrivateKey,
	error,
) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf(
			"generate Ed25519 key pair: %w",
			err,
		)
	}

	return publicKey, privateKey, nil
}

// EncodePublisherPrivateKey kodar private key som hex.
//
// Detta format kan användas för att spara nyckeln lokalt.
// Private key ska inte skickas över nätverket.
func EncodePublisherPrivateKey(
	privateKey ed25519.PrivateKey,
) (string, error) {

	if len(privateKey) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("invalid Ed25519 private key")
	}

	return hex.EncodeToString(privateKey), nil
}

// LoadPublisherPrivateKey läser en hex-kodad Ed25519 private key.
//
// Funktionen används när publishern vill ladda sin tidigare
// skapade privata nyckel från lokal konfiguration.
func LoadPublisherPrivateKey(
	encoded string,
) (ed25519.PrivateKey, error) {

	data, err := hex.DecodeString(
		strings.TrimSpace(encoded),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid publisher private key encoding",
		)
	}

	if len(data) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf(
			"invalid Ed25519 private key size",
		)
	}

	return ed25519.PrivateKey(data), nil
}

// ConfigureTestDomainOwner kopplar en testdomän till publisherns
// public key i fake DNS.
//
// Ownership kommer alltså från DNS-providern och inte från
// någon public key som skickas tillsammans med en update.
func ConfigureTestDomainOwner(
	fakeDNS *FakeTXTResolver,
	domain string,
	publicKey ed25519.PublicKey,
) error {

	if fakeDNS == nil {
		return fmt.Errorf("fake DNS resolver is nil")
	}

	domain = strings.TrimSpace(domain)
	if domain == "" {
		return fmt.Errorf("domain is empty")
	}

	encodedKey, err := EncodePublicKey(publicKey)
	if err != nil {
		return err
	}

	txtRecord := ownershipTXTPrefix + encodedKey

	fakeDNS.records[domain] = []string{
		txtRecord,
	}

	return nil
}
