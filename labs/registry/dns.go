package registry

import (
	"crypto/ed25519"
	"fmt"
	"net"
	"strings"
)

// TXT-format för domänägarens Ed25519 public key.
// Exempel:
// d7024e-ed25519=0123456789abcdef...
const ownershipTXTPrefix = "d7024e-ed25519="

// OwnershipVerifier anger hur en nod hämtar den public key
// som den litar på för en viss domän.
//
// Olika implementationer kan använda till exempel:
// - fake DNS i simuleringar
// - riktig DNS TXT i ett verkligt nätverk
type OwnershipVerifier interface {
	ResolvePublicKey(domain string) (ed25519.PublicKey, error)
}

// TXTResolver gör DNS-uppslagning testbar.
// I riktiga programmet används systemets DNS-resolver.
type TXTResolver interface {
	LookupTXT(name string) ([]string, error)
}

// systemTXTResolver använder riktig DNS.
type systemTXTResolver struct{}

// DNSOwnershipVerifier använder DNS TXT-records
// för att hitta domänägarens Ed25519 public key.
type DNSOwnershipVerifier struct {
	Resolver TXTResolver
}

// FakeTXTResolver är en enkel, deterministisk DNS-provider
// som används i simuleringar och tester.
//
// Alla simulerade noder kan dela samma instans,
// så de ser samma DNS TXT-records.
type FakeTXTResolver struct {
	records map[string][]string
}

// ResolvePublicKey hämtar den public key som DNS anger
// som ägare för domänen.
func (v DNSOwnershipVerifier) ResolvePublicKey(
	domain string,
) (ed25519.PublicKey, error) {

	return ResolveDomainPublicKey(
		domain,
		v.Resolver,
	)
}

func (systemTXTResolver) LookupTXT(name string) ([]string, error) {
	return net.LookupTXT(name)
}

// ResolveDomainPublicKey hämtar exakt en Ed25519 public key
// från domänens DNS TXT-record.
//
// Format:
// d7024e-ed25519=<hex encoded public key>
//
// Funktionen avvisar domänen om ownership-record saknas,
// är felaktig eller om flera ownership-records finns.
func ResolveDomainPublicKey(
	domain string,
	resolver TXTResolver,
) (ed25519.PublicKey, error) {

	domain = strings.TrimSpace(domain)
	if domain == "" {
		return nil, fmt.Errorf("domain is empty")
	}

	if resolver == nil {
		resolver = systemTXTResolver{}
	}

	records, err := resolver.LookupTXT(domain)
	if err != nil {
		return nil, fmt.Errorf(
			"DNS TXT lookup failed for %s: %w",
			domain,
			err,
		)
	}

	var ownershipRecords []string

	for _, txt := range records {
		txt = strings.TrimSpace(txt)

		if strings.HasPrefix(txt, ownershipTXTPrefix) {
			ownershipRecords = append(
				ownershipRecords,
				txt,
			)
		}
	}

	// Ingen ownership-record -> avvisa.
	if len(ownershipRecords) == 0 {
		return nil, fmt.Errorf(
			"no ownership TXT record found for %s",
			domain,
		)
	}

	// Flera ownership-records -> tvetydigt -> avvisa.
	if len(ownershipRecords) != 1 {
		return nil, fmt.Errorf(
			"ambiguous ownership TXT records for %s",
			domain,
		)
	}

	encodedKey := strings.TrimPrefix(
		ownershipRecords[0],
		ownershipTXTPrefix,
	)

	if encodedKey == "" {
		return nil, fmt.Errorf(
			"ownership TXT record contains no public key",
		)
	}

	publicKey, err := DecodePublicKey(encodedKey)
	if err != nil {
		return nil, fmt.Errorf(
			"malformed ownership public key for %s: %w",
			domain,
			err,
		)
	}

	return publicKey, nil
}

// VerifyVersionRecordWithDNS hämtar public key från DNS
// och verifierar sedan VersionRecord innan dess metadata betros.
func VerifyVersionRecordWithDNS(
	record *VersionRecord,
	resolver TXTResolver,
) error {

	if record == nil {
		return fmt.Errorf("version record is nil")
	}

	publicKey, err := ResolveDomainPublicKey(
		record.Domain,
		resolver,
	)
	if err != nil {
		return err
	}

	if err := VerifyVersionRecord(record, publicKey); err != nil {
		return fmt.Errorf("version record verification failed: %w", err)
	}

	return nil
}

// VerifyLatestPointerWithDNS hämtar public key från DNS
// och verifierar sedan LatestPointer innan dess metadata betros.
func VerifyLatestPointerWithDNS(
	pointer *LatestPointer,
	resolver TXTResolver,
) error {

	if pointer == nil {
		return fmt.Errorf("latest pointer is nil")
	}

	publicKey, err := ResolveDomainPublicKey(
		pointer.Domain,
		resolver,
	)
	if err != nil {
		return err
	}

	if err := VerifyLatestPointer(pointer, publicKey); err != nil {
		return fmt.Errorf("latest pointer verification failed: %w", err)
	}

	return nil
}

// nu är klart DNS-delen av punkt 2

// NewFakeTXTResolver skapar en fake DNS-provider
// med en bestämd uppsättning TXT-records.
func NewFakeTXTResolver(records map[string][]string) *FakeTXTResolver {
	copyRecords := make(map[string][]string)

	for domain, txtRecords := range records {
		copyRecords[domain] = append([]string(nil), txtRecords...)
	}

	return &FakeTXTResolver{
		records: copyRecords,
	}
}

// LookupTXT returnerar de TXT-records som är konfigurerade
// för domänen. Ingen riktig DNS används.
func (f *FakeTXTResolver) LookupTXT(
	name string,
) ([]string, error) {

	if f == nil {
		return nil, fmt.Errorf("fake DNS resolver is nil")
	}

	records, exists := f.records[name]
	if !exists {
		return nil, fmt.Errorf(
			"no TXT records configured for domain %s",
			name,
		)
	}

	return append([]string(nil), records...), nil
}
