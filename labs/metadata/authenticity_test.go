package metadata

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func signedMetadata(t *testing.T) (VersionRecord, LatestPointer, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pk, sk, err := GeneratePublisherKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	blob := sha256.Sum256([]byte("package"))
	r := VersionRecord{Tag: VersionRecordTag, Domain: "example.com", Package: "demo", Version: 1, BlobHash: hex.EncodeToString(blob[:]), PreviousVersionRecord: ZeroHash}
	if err := SignVersionRecord(&r, sk); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	p := LatestPointer{Tag: LatestPointerTag, Domain: r.Domain, Package: r.Package, Version: r.Version, VersionRecordHash: hex.EncodeToString(hash[:])}
	if err := SignLatestPointer(&p, sk); err != nil {
		t.Fatal(err)
	}
	return r, p, pk, sk
}

func TestPointerRecordAuthenticationAndBinding(t *testing.T) {
	r, p, pk, sk := signedMetadata(t)
	if err := VerifyLatestPointerRecord(&p, &r, pk); err != nil {
		t.Fatal(err)
	}
	if err := VerifyLatestPointerRecord(nil, &r, pk); err == nil {
		t.Fatal("accepted nil pointer")
	}
	if err := VerifyLatestPointerRecord(&p, nil, pk); err == nil {
		t.Fatal("accepted nil record")
	}
	for _, change := range []func(*LatestPointer){
		func(p *LatestPointer) { p.Domain = "other.com" },
		func(p *LatestPointer) { p.Package = "other" },
		func(p *LatestPointer) { p.Version++ },
		func(p *LatestPointer) { p.VersionRecordHash = ZeroHash },
	} {
		candidate := p
		change(&candidate)
		if err := SignLatestPointer(&candidate, sk); err != nil {
			t.Fatal(err)
		}
		if err := VerifyLatestPointerRecord(&candidate, &r, pk); err == nil {
			t.Fatal("accepted correctly signed pointer bound to wrong record")
		}
	}
}

func TestMalformedSignedMetadataIsRejected(t *testing.T) {
	r, p, pk, _ := signedMetadata(t)
	if err := VerifyVersionRecord(nil, pk); err == nil {
		t.Fatal("accepted nil record")
	}
	if err := VerifyLatestPointer(nil, pk); err == nil {
		t.Fatal("accepted nil pointer")
	}
	for _, change := range []func(*VersionRecord){
		func(r *VersionRecord) { r.Tag = "blob" }, func(r *VersionRecord) { r.Domain = "" },
		func(r *VersionRecord) { r.Package = "" }, func(r *VersionRecord) { r.Version = 0 },
		func(r *VersionRecord) { r.BlobHash = "" }, func(r *VersionRecord) { r.PreviousVersionRecord = "" },
		func(r *VersionRecord) { r.Signature = "" }, func(r *VersionRecord) { r.Signature = "not hex" },
		func(r *VersionRecord) { r.Signature = "00" }, func(r *VersionRecord) { r.Version++ },
	} {
		candidate := r
		change(&candidate)
		if err := VerifyVersionRecord(&candidate, pk); err == nil {
			t.Fatal("accepted malformed/tampered version record")
		}
	}
	for _, change := range []func(*LatestPointer){
		func(p *LatestPointer) { p.Tag = "blob" }, func(p *LatestPointer) { p.Domain = "" },
		func(p *LatestPointer) { p.Package = "" }, func(p *LatestPointer) { p.Version = 0 },
		func(p *LatestPointer) { p.VersionRecordHash = "" }, func(p *LatestPointer) { p.Signature = "" },
		func(p *LatestPointer) { p.Signature = "not hex" }, func(p *LatestPointer) { p.Signature = "00" },
		func(p *LatestPointer) { p.Version++ },
	} {
		candidate := p
		change(&candidate)
		if err := VerifyLatestPointer(&candidate, pk); err == nil {
			t.Fatal("accepted malformed/tampered latest pointer")
		}
	}
	if err := VerifyVersionRecord(&r, nil); err == nil {
		t.Fatal("accepted invalid verification key")
	}
	if err := VerifyLatestPointer(&p, nil); err == nil {
		t.Fatal("accepted invalid verification key")
	}
}

func TestPublisherKeyLoadingFailures(t *testing.T) {
	_, _, pk, sk := signedMetadata(t)
	encoded, err := EncodePublisherPrivateKey(sk)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPublisherPrivateKey(" \n" + encoded + "\n")
	if err != nil || !loaded.Equal(sk) {
		t.Fatalf("key round trip: %v", err)
	}
	for _, encoded := range []string{"not hex", "00"} {
		if _, err := LoadPublisherPrivateKey(encoded); err == nil {
			t.Fatal("accepted bad signing key")
		}
	}
	if _, err := EncodePublisherPrivateKey(nil); err == nil {
		t.Fatal("encoded invalid signing key")
	}
	if _, err := EncodePublicKey(nil); err == nil {
		t.Fatal("encoded invalid public key")
	}
	if err := ConfigureTestDomainOwner(nil, "example.com", pk); err == nil {
		t.Fatal("configured nil DNS")
	}
	dns := NewFakeTXTResolver(nil)
	if err := ConfigureTestDomainOwner(dns, "", pk); err == nil {
		t.Fatal("configured empty domain")
	}
	if err := ConfigureTestDomainOwner(dns, "example.com", nil); err == nil {
		t.Fatal("configured bad public key")
	}
}
