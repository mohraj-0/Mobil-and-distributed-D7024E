# Immutable package registry

Part 2 code lives in `d7024e/registry`: signed record types, Ed25519
signing/verification, real and fake DNS ownership, publisher key helpers, and
immutable storage/history operations. Kademlia handles content-addressed bytes
and uses shared `metadata` types for authenticated mutable latest-pointer RPCs.

Create `Registry{Store: node, OwnershipVerifier: verifier}` where `node` is a
`*kademlia.Kademlia` and `verifier` is a `DNSOwnershipVerifier`. The existing
Part 2 helpers previously exported from `kademlia` now belong to `registry`;
use the same ownership verifier on `Registry` and `Kademlia`.

1. `StoreBlob(bytes)` stores binary package bytes and returns SHA-256 hex.
2. Build and sign a `VersionRecord` using `SignVersionRecord`. Use `ZeroHash`
   as the first record's predecessor. Later records reference the preceding
   complete signed record's content hash and have a strictly greater version.
3. `StoreVersionRecord(record)` authenticates the record and predecessor chain
   before storing the entire signed JSON record through the ordinary DHT path.
4. `TraverseHistory(headHash, domain, package)` returns newest-first entries only
   after verifying the complete ancestry. `ResolveVersionRecord` uses the same
   checks. `DownloadVersion` verifies history and downloads the referenced blob.

Every fetched object is independently checked against its requested SHA-256
hash. Records are verified using the DNS owner's key before their references
are followed. Missing objects, invalid signatures/hashes/references, links to
another namespace, cycles and non-increasing versions return errors with no
partial authenticated history. A visited set guards cycles; a cryptographically
valid content-addressed cycle would require breaking SHA-256 assumptions.

`MaxHistory` bounds record fetches per call; zero selects 1,024 records and a
negative limit is invalid. A chain beyond the configured bound is rejected,
including when adding a new record. Configure limits before concurrent use.
The cap bounds work, not wall-clock time: each DHT lookup retains its own RPC
timeout and retry policy.

Blobs and complete encoded records must each occupy 1–32,768 bytes. Oversized
values are rejected before storage. There is no chunking. Historical records
and blobs retain their content keys; adding a release never updates old keys.
Availability still depends on live replicas, as Part 1 storage is in memory.
Authenticating history does not fetch every historical blob; a download checks
the selected blob's availability and hash separately.

Mutable latest-pointer RPCs, atomic publication/fork prevention, catch-up and
read repair live in `kademlia/latest.go`. The single Part 1/Part 2 CLI lives in
`cmd/simshell`: `publish`, `install`, package history, DNS ownership, and structured
data-store inspection share the existing DHT commands and interactive dispatch.
See [CLI usage and demonstrations](../cmd/README.md).

Run from `labs`: `go test ./registry` and `go test -race ./...`.
