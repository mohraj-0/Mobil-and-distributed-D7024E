package registry

import "testing"

func TestRegistryPointerOwnershipUsesConfiguredDNS(t *testing.T) {
	r, _, sk := fixture(t)
	blob, err := r.StoreBlob([]byte("package"))
	if err != nil {
		t.Fatal(err)
	}
	record := signedRecord(t, sk, 1, blob, ZeroHash)
	hash, err := r.StoreVersionRecord(&record)
	if err != nil {
		t.Fatal(err)
	}
	p := LatestPointer{Tag: LatestPointerTag, Domain: record.Domain, Package: record.Package, Version: 1, VersionRecordHash: hash}
	if err := SignLatestPointer(&p, sk); err != nil {
		t.Fatal(err)
	}
	if err := r.VerifyOwnedLatestPointer(&p); err != nil {
		t.Fatal(err)
	}
	if err := r.VerifyOwnedLatestPointer(nil); err == nil {
		t.Fatal("accepted nil pointer")
	}
	bad := p
	bad.Version++
	if err := r.VerifyOwnedLatestPointer(&bad); err == nil {
		t.Fatal("accepted tampered pointer")
	}
	unknown := p
	unknown.Domain = "missing.com"
	if err := r.VerifyOwnedLatestPointer(&unknown); err == nil {
		t.Fatal("accepted unknown owner")
	}
	r.OwnershipVerifier = nil
	if err := r.VerifyOwnedLatestPointer(&p); err == nil {
		t.Fatal("accepted unconfigured ownership")
	}
}
