package registry

// Shared signed metadata is independent of registry storage and DHT transports.
import "d7024e/metadata"

type VersionRecord = metadata.VersionRecord
type LatestPointer = metadata.LatestPointer
type OwnershipVerifier = metadata.OwnershipVerifier
type TXTResolver = metadata.TXTResolver
type DNSOwnershipVerifier = metadata.DNSOwnershipVerifier
type FakeTXTResolver = metadata.FakeTXTResolver

const (
	VersionRecordTag   = metadata.VersionRecordTag
	LatestPointerTag   = metadata.LatestPointerTag
	SignatureEncoding  = metadata.SignatureEncoding
	PublicKeyEncoding  = metadata.PublicKeyEncoding
	ownershipTXTPrefix = "d7024e-ed25519="
)

var (
	SignVersionRecord          = metadata.SignVersionRecord
	SignLatestPointer          = metadata.SignLatestPointer
	VerifyVersionRecord        = metadata.VerifyVersionRecord
	VerifyLatestPointer        = metadata.VerifyLatestPointer
	VerifyLatestPointerRecord  = metadata.VerifyLatestPointerRecord
	EncodePublicKey            = metadata.EncodePublicKey
	DecodePublicKey            = metadata.DecodePublicKey
	ResolveDomainPublicKey     = metadata.ResolveDomainPublicKey
	VerifyVersionRecordWithDNS = metadata.VerifyVersionRecordWithDNS
	VerifyLatestPointerWithDNS = metadata.VerifyLatestPointerWithDNS
	NewFakeTXTResolver         = metadata.NewFakeTXTResolver
)
