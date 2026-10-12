package kademlia

import "fmt"

// VerifyOwnedVersionRecord verifierar en VersionRecord
// med den public key som nodens egen ownership-provider
// anger för recordens domän.
//
// Om provider saknas eller lookup misslyckas avvisas recorden.
// Noden accepterar aldrig en public key från själva uppdateringen.
func (r *Kademlia) VerifyOwnedVersionRecord(
	record *VersionRecord,
) error {
	if record == nil {
		return fmt.Errorf("version record is nil")
	}

	if r.OwnershipVerifier == nil {
		return fmt.Errorf("ownership verifier is not configured")
	}

	publicKey, err := r.OwnershipVerifier.ResolvePublicKey(
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
func (r *Kademlia) VerifyOwnedLatestPointer(
	pointer *LatestPointer,
) error {
	if pointer == nil {
		return fmt.Errorf("latest pointer is nil")
	}

	if r.OwnershipVerifier == nil {
		return fmt.Errorf("ownership verifier is not configured")
	}

	publicKey, err := r.OwnershipVerifier.ResolvePublicKey(
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
