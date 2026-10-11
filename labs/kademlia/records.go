package kademlia

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const (
	// Taggarna gör att de två record-typerna tydligt skiljs åt.
	VersionRecordTag  = "version-record"
	LatestPointerTag  = "latest-pointer"
	SignatureEncoding = "hex"
	PublicKeyEncoding = "hex"
)

// VersionRecord beskriver en specifik version av ett paket.
type VersionRecord struct {
	Tag                   string `json:"tag"`
	Domain                string `json:"domain"`
	Package               string `json:"package"`
	Version               uint64 `json:"version"`
	BlobHash              string `json:"blob_hash"`
	PreviousVersionRecord string `json:"previous_version_record"`
	Signature             string `json:"sig"`
}

// LatestPointer pekar på den senaste versionen av ett paket.
type LatestPointer struct {
	Tag               string `json:"tag"`
	Domain            string `json:"domain"`
	Package           string `json:"package"`
	Version           uint64 `json:"version"`
	VersionRecordHash string `json:"version_record_hash"`
	Signature         string `json:"sig"`
}

// unsignedVersionRecord innehåller alla signerade fält
// för VersionRecord, men inte själva Signature.
// Samma data som VersionRecord, men utan signaturen.
// Den används när signaturen skapas.
type unsignedVersionRecord struct {
	Tag                   string `json:"tag"`
	Domain                string `json:"domain"`
	Package               string `json:"package"`
	Version               uint64 `json:"version"`
	BlobHash              string `json:"blob_hash"`
	PreviousVersionRecord string `json:"previous_version_record"`
}

// unsignedLatestPointer innehåller alla signerade fält
// för LatestPointer, men inte själva Signature.( Samma data som LatestPointer, men utan signaturen)
type unsignedLatestPointer struct {
	Tag               string `json:"tag"`
	Domain            string `json:"domain"`
	Package           string `json:"package"`
	Version           uint64 `json:"version"`
	VersionRecordHash string `json:"version_record_hash"`
}

// SignVersionRecord signerar en VersionRecord.
//
// Flöde:
// 1. SignVersionRecord skapar SHA-256 över recorden utan Signature.
// 2. Serialisera den deterministiskt med json.Marshal.
// 3. Beräkna SHA-256.
// 4. Signera digesten med Ed25519 private key.
// 5. Spara signaturen som hex-sträng.
func SignVersionRecord(
	record *VersionRecord,
	privateKey ed25519.PrivateKey,
) error {
	if record == nil {
		return fmt.Errorf("version record is nil")
	}

	if len(privateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("invalid Ed25519 private key")
	}

	unsigned := unsignedVersionRecord{
		Tag:                   record.Tag,
		Domain:                record.Domain,
		Package:               record.Package,
		Version:               record.Version,
		BlobHash:              record.BlobHash,
		PreviousVersionRecord: record.PreviousVersionRecord,
	}

	data, err := json.Marshal(unsigned)
	if err != nil {
		return fmt.Errorf("marshal unsigned version record: %w", err)
	}

	digest := sha256.Sum256(data)

	signature := ed25519.Sign(
		privateKey,
		digest[:],
	)

	record.Signature = hex.EncodeToString(signature)

	return nil
}

// SignLatestPointer signerar en LatestPointer.
//
// Flöde:
// 1. Skapa  SHA-256 över pointern utan Signature
// 2. Serialisera med json.Marshal.
// 3. Beräkna SHA-256.
// 4. Signera digesten med Ed25519 private key.
// 5. Spara signaturen som hex-sträng.
func SignLatestPointer(
	pointer *LatestPointer,
	privateKey ed25519.PrivateKey,
) error {
	if pointer == nil {
		return fmt.Errorf("latest pointer is nil")
	}

	if len(privateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("invalid Ed25519 private key")
	}

	unsigned := unsignedLatestPointer{
		Tag:               pointer.Tag,
		Domain:            pointer.Domain,
		Package:           pointer.Package,
		Version:           pointer.Version,
		VersionRecordHash: pointer.VersionRecordHash,
	}

	data, err := json.Marshal(unsigned)
	if err != nil {
		return fmt.Errorf("marshal unsigned latest pointer: %w", err)
	}

	digest := sha256.Sum256(data)

	signature := ed25519.Sign(
		privateKey,
		digest[:],
	)

	pointer.Signature = hex.EncodeToString(signature)

	return nil
}

// nu täcker täcker punkt 1:
//Sign the SHA-256 digest of each deterministic unsigned record with Ed25519, excluding only the signature field

// VerifyVersionRecord verifierar signaturen för en VersionRecord
// med domänägarens Ed25519 public key.
func VerifyVersionRecord(
	record *VersionRecord,
	publicKey ed25519.PublicKey,
) error {
	if err := validateVersionRecord(record); err != nil {
		return err

	}

	if len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid Ed25519 public key")
	}

	if record.Tag != VersionRecordTag {
		return fmt.Errorf("invalid version record tag")
	}

	// Gör samma unsigned record som användes vid signeringen.
	unsigned := unsignedVersionRecord{
		Tag:                   record.Tag,
		Domain:                record.Domain,
		Package:               record.Package,
		Version:               record.Version,
		BlobHash:              record.BlobHash,
		PreviousVersionRecord: record.PreviousVersionRecord,
	}

	data, err := json.Marshal(unsigned)
	if err != nil {
		return fmt.Errorf("marshal unsigned version record: %w", err)
	}

	// Beräkna samma SHA-256 digest som vid signeringen.
	digest := sha256.Sum256(data)

	// Signaturen lagras som hex, därför måste den avkodas.
	signature, err := hex.DecodeString(record.Signature)
	if err != nil {
		return fmt.Errorf("invalid version record signature encoding")
	}

	if len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("invalid version record signature size")
	}

	// Kontrollera signaturen med public key.
	if !ed25519.Verify(publicKey, digest[:], signature) {
		return fmt.Errorf("invalid version record signature")
	}

	return nil
}

// VerifyLatestPointer verifierar signaturen för en LatestPointer
// med domänägarens Ed25519 public key.
func VerifyLatestPointer(
	pointer *LatestPointer,
	publicKey ed25519.PublicKey,
) error {
	if err := validateLatestPointer(pointer); err != nil {
		return err
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid Ed25519 public key")
	}

	if pointer.Tag != LatestPointerTag {
		return fmt.Errorf("invalid latest pointer tag")
	}

	// Gör samma unsigned pointer som användes vid signeringen.
	unsigned := unsignedLatestPointer{
		Tag:               pointer.Tag,
		Domain:            pointer.Domain,
		Package:           pointer.Package,
		Version:           pointer.Version,
		VersionRecordHash: pointer.VersionRecordHash,
	}

	data, err := json.Marshal(unsigned)
	if err != nil {
		return fmt.Errorf("marshal unsigned latest pointer: %w", err)
	}

	// Beräkna SHA-256 digest.
	digest := sha256.Sum256(data)

	// Avkoda signaturen från hex.
	signature, err := hex.DecodeString(pointer.Signature)
	if err != nil {
		return fmt.Errorf("invalid latest pointer signature encoding")
	}

	if len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("invalid latest pointer signature size")
	}

	// Kontrollera signaturen med public key.
	if !ed25519.Verify(publicKey, digest[:], signature) {
		return fmt.Errorf("invalid latest pointer signature")
	}

	return nil
}

// Vi har gjort verifieringsfunktionerna i records.go,
// men punkt 2 säger att verifieringen ska ske med domänägarens public key som hämtas via DNS.
// Så det som återstår för punkt 2 är dns.go,
// där vi hämtar public key från DNS TXT-record och sedan använder den i VerifyVersionRecord() och VerifyLatestPointer()

// validateVersionRecord kontrollerar att VersionRecord
// har rätt tag och nödvändiga fält.
func validateVersionRecord(record *VersionRecord) error {
	if record == nil {
		return fmt.Errorf("version record is nil")
	}

	if record.Tag != VersionRecordTag {
		return fmt.Errorf("invalid version record tag")
	}

	if record.Domain == "" {
		return fmt.Errorf("domain is empty")
	}

	if record.Package == "" {
		return fmt.Errorf("package is empty")
	}

	if record.Version == 0 {
		return fmt.Errorf("invalid version")
	}

	if record.BlobHash == "" {
		return fmt.Errorf("blob hash is empty")
	}

	if record.PreviousVersionRecord == "" {
		return fmt.Errorf("previous version record is empty")
	}

	if record.Signature == "" {
		return fmt.Errorf("signature is empty")
	}

	return nil
}

// validateLatestPointer kontrollerar att LatestPointer
// har rätt tag och nödvändiga fält.
// funktionen kontrollerar att recorden har rätt Tag och att viktiga delar som Domain, Package, Version, BlobHash osv. inte är tomma eller felaktiga
func validateLatestPointer(pointer *LatestPointer) error {
	if pointer == nil {
		return fmt.Errorf("latest pointer is nil")
	}

	if pointer.Tag != LatestPointerTag {
		return fmt.Errorf("invalid latest pointer tag")
	}

	if pointer.Domain == "" {
		return fmt.Errorf("domain is empty")
	}

	if pointer.Package == "" {
		return fmt.Errorf("package is empty")
	}

	if pointer.Version == 0 {
		return fmt.Errorf("invalid version")
	}

	if pointer.VersionRecordHash == "" {
		return fmt.Errorf("version record hash is empty")
	}

	if pointer.Signature == "" {
		return fmt.Errorf("signature is empty")
	}

	return nil
}

// punkt 3 är i praktiken klar nu

// VerifyLatestPointerRecord kontrollerar att en LatestPointer
// verkligen pekar på rätt autentiserad VersionRecord.
func VerifyLatestPointerRecord(
	pointer *LatestPointer,
	record *VersionRecord,
	publicKey ed25519.PublicKey,
) error {

	// Verifiera först latest-pointer.
	if err := VerifyLatestPointer(pointer, publicKey); err != nil {
		return fmt.Errorf("invalid latest pointer: %w", err)
	}

	// Verifiera sedan version-record.
	if err := VerifyVersionRecord(record, publicKey); err != nil {
		return fmt.Errorf("invalid version record: %w", err)
	}

	// Domain måste vara samma.
	if pointer.Domain != record.Domain {
		return fmt.Errorf("domain does not match version record")
	}

	// Package måste vara samma.
	if pointer.Package != record.Package {
		return fmt.Errorf("package does not match version record")
	}

	// Version måste vara samma.
	if pointer.Version != record.Version {
		return fmt.Errorf("version does not match version record")
	}

	// Beräkna hash av hela VersionRecord.
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal version record: %w", err)
	}

	hash := sha256.Sum256(data)
	recordHash := hex.EncodeToString(hash[:])

	// LatestPointer måste verkligen peka på denna VersionRecord.
	if pointer.VersionRecordHash != recordHash {
		return fmt.Errorf("version record hash does not match latest pointer")
	}

	return nil
}

// punkt 4 gjord. DNS-public key kommer senare från dns.go

//börja med punkt 6

// EncodePublicKey converts an Ed25519 public key to hexadecimal.
// This format can later be used in the DNS TXT record.
func EncodePublicKey(publicKey ed25519.PublicKey) (string, error) {
	if len(publicKey) != ed25519.PublicKeySize {
		return "", fmt.Errorf("invalid Ed25519 public key")
	}

	return hex.EncodeToString(publicKey), nil
}

// DecodePublicKey converts a hexadecimal public key back
// to an Ed25519 public key.
func DecodePublicKey(encoded string) (ed25519.PublicKey, error) {
	data, err := hex.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid public key encoding")
	}

	if len(data) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid Ed25519 public key size")
	}

	return ed25519.PublicKey(data), nil
}

// VerifyOwnedVersionRecord verifierar en VersionRecord
// med den public key som nodens egen ownership-provider
// anger för recordens domän.
//
// Om provider saknas eller lookup misslyckas avvisas recorden.
// Noden accepterar aldrig en public key från själva uppdateringen.
func (kademlia *Kademlia) VerifyOwnedVersionRecord(
	record *VersionRecord,
) error {
	if record == nil {
		return fmt.Errorf("version record is nil")
	}

	if kademlia.OwnershipVerifier == nil {
		return fmt.Errorf("ownership verifier is not configured")
	}

	publicKey, err := kademlia.OwnershipVerifier.ResolvePublicKey(
		record.Domain,
	)
	if err != nil {
		return fmt.Errorf(
			"owner key lookup failed for %s: %w",
			record.Domain,
			err,
		)
	}

	if err := VerifyVersionRecord(record, publicKey); err != nil {
		return fmt.Errorf(
			"version record ownership verification failed: %w",
			err,
		)
	}

	return nil
}

// VerifyOwnedLatestPointer verifierar en LatestPointer
// med den public key som nodens egen ownership-provider
// anger för pointerns domän.
//
// Public key hämtas alltid från ownership-providern,
// aldrig från själva uppdateringen.
func (kademlia *Kademlia) VerifyOwnedLatestPointer(
	pointer *LatestPointer,
) error {
	if pointer == nil {
		return fmt.Errorf("latest pointer is nil")
	}

	if kademlia.OwnershipVerifier == nil {
		return fmt.Errorf("ownership verifier is not configured")
	}

	publicKey, err := kademlia.OwnershipVerifier.ResolvePublicKey(
		pointer.Domain,
	)
	if err != nil {
		return fmt.Errorf(
			"owner key lookup failed for %s: %w",
			pointer.Domain,
			err,
		)
	}

	if err := VerifyLatestPointer(pointer, publicKey); err != nil {
		return fmt.Errorf(
			"latest pointer ownership verification failed: %w",
			err,
		)
	}

	return nil
}
