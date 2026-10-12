# Issue #47: Immutable package blobs and version records

## Purpose and result

Issue #47 adds the immutable storage and authenticated history layer for Part 2. Package bytes and complete signed version records are stored through the existing Kademlia `Store` and `LookupData` methods. A caller can resolve a record by its hash, verify its complete predecessor history, and download the package associated with an older release.

The implementation also consolidates the existing Part 2 code into the `d7024e/registry` package. Kademlia remains responsible for the content-addressed DHT, while registry code interprets package metadata and domain ownership.

## Files and responsibilities

| File | Responsibility |
| --- | --- |
| [registry.go](registry.go) | Blob storage, signed-record storage, content verification, history traversal and historical downloads. |
| [records.go](records.go) | Existing version-record and latest-pointer structures, signing, signature verification and ownership verification helpers. |
| [dns.go](dns.go) | Existing real/fake DNS TXT resolution and domain-owner public-key verification. |
| [publisher_keys.go](publisher_keys.go) | Existing publisher key generation, encoding, loading and fake DNS configuration helpers. |
| [registry_test.go](registry_test.go) | New tests for issue #47. |
| [records_test.go](records_test.go), [dns_test.go](dns_test.go) | Existing signature and ownership tests, moved with their implementation. |
| [README.md](README.md) | Package usage, migration information, bounds and limitations. |

The moved cryptographic and DNS helpers were already implemented before this issue. The storage/history API and its tests are the new functionality. Ownership configuration now belongs to `Registry`, rather than `Kademlia`. Callers importing the moved Part 2 helpers must use `d7024e/registry`.

## Storage architecture

`Registry` holds three configuration fields:

- `Store`: a `ContentStore` implementation.
- `OwnershipVerifier`: the source of trusted domain-owner public keys.
- `MaxHistory`: the maximum number of records permitted in one history traversal.

The storage interface is deliberately small:

```go
type ContentStore interface {
    Store([]byte) (string, error)
    LookupData(string) ([]byte, error)
}
```

`*kademlia.Kademlia` implements this interface directly. Registry therefore reuses Part 1 replication, lookups, UDP RPCs and simulated networking without introducing a second transport. Tests can also supply an in-memory store to exercise malformed data independently of Kademlia's own validation.

## Package blobs

`StoreBlob` accepts arbitrary binary bytes, checks the size and passes a copy to the content store. It verifies that the returned key equals the SHA-256 hash of the bytes supplied. `LookupBlob` fetches by hash and independently checks the returned bytes against that hash.

Each blob retains its content key. Storing a different release produces a different key when its contents differ; storing identical bytes produces the same key. There is no registry operation that overwrites an older blob under its existing hash with different bytes.

## Complete signed version records

A `VersionRecord` contains the record tag, domain, package name, positive integer version, blob hash, predecessor record hash and signature. The predecessor of the first record is `ZeroHash`, a 64-character all-zero hexadecimal string. Later records refer to the hash of the complete signed predecessor record.

The existing signing helper serializes the unsigned fields in a fixed Go struct order, hashes that JSON with SHA-256 and signs the digest with Ed25519. Signatures and public keys use hexadecimal encoding. The complete record, including its signature, is then JSON-encoded and stored under the hash of those complete bytes.

`StoreVersionRecord` performs the following steps:

1. Reject an invalid history limit or nil record and take a value snapshot of the supplied record.
2. Encode the complete signed record and check its encoded size.
3. Resolve the domain owner's public key and verify the signature and required fields.
4. Validate the blob and predecessor hash formats. Hashes must be canonical lowercase SHA-256 hex; the zero hash is permitted only as the predecessor sentinel.
5. If a predecessor exists, authenticate its entire history in the same domain/package namespace.
6. Require the new version to be strictly greater than the predecessor version and ensure the new chain fits the configured limit.
7. Store the complete signed bytes through the existing content-addressed path.

DNS ownership uses `d7024e-ed25519=<hex-public-key>` TXT records. The resolver rejects missing, malformed or ambiguous ownership records. Trust comes from the configured ownership verifier, rather than a public key supplied with the version record.

## Resolving and traversing authenticated history

`TraverseHistory(headHash, domain, package)` walks from the supplied head to the first record and returns `HistoryEntry` values in newest-first order. Each entry contains the content hash and decoded record.

Before following each predecessor, traversal:

- Checks a visited set and the record-count limit.
- Fetches the complete record and verifies its size and SHA-256 content hash.
- Decodes the JSON and requires the expected domain and package.
- Verifies the record signature through the configured domain ownership provider.
- Checks reference formats and rejects a predecessor already in the visited set.
- Requires predecessor versions to decrease strictly while walking backward.

Missing records, hash mismatches, invalid signatures, malformed references, cross-domain/package links, cycles and invalid version ordering produce errors. A failed traversal returns no partial authenticated history. A record is returned through `ResolveVersionRecord` only after its complete ancestry has passed these checks.

The visited-set guard is defensive: constructing a genuine cycle of SHA-256-addressed records would require breaking the cryptographic assumptions. The cycle guard is tested directly rather than claiming that the tests construct such a valid cryptographic cycle.

## Historical downloads and immutability

`DownloadVersion(recordHash, domain, package)` authenticates the requested record and its ancestry, then fetches its blob through `LookupBlob`. This allows callers to download historical releases by their immutable record hashes.

Adding a new record does not update any previous record or blob. History authentication verifies metadata; it does not download every blob mentioned in the chain. The selected blob's availability and integrity are checked when downloading that version.

This issue does not enforce a single globally accepted history head. Two individually valid signed branches can still exist as immutable objects. Atomic latest-pointer updates and fork prevention belong to subsequent issues.

## Bounds, errors and concurrency

Blobs and complete encoded records must each contain **1 through 32,768 bytes**. The upper bound is reused from `kademlia.MaxValueSize`. Errors identify the object type, actual size and supported range. Oversized objects are rejected before invoking storage. No chunking has been added.

`MaxHistory = 0` selects `DefaultMaxHistory`, currently **1,024 records**. Positive values configure the cap; negative values are rejected. Traversal is iterative and performs at most that many record fetches before rejecting a longer chain. This bounds work per call, but not total elapsed time: each DHT lookup still uses its existing retry and timeout policy.

Traversal uses per-call slices and maps. Blob operations copy byte slices, and record storage takes a value snapshot. Shared DHT state remains protected by Kademlia's existing synchronization. Registry configuration and fake DNS configuration should be established before concurrent operations and kept stable while those operations run.

Stored data remains in memory and depends on surviving replicas. Content addressing preserves identity and integrity, but does not guarantee availability after all replicas are lost.

## Tests and validation

| Test | What it verifies |
| --- | --- |
| `TestHistoryAndHistoricalBinaryAvailability` | Binary data containing zero and non-ASCII bytes, a complete two-version history, old/new downloads and unchanged older record bytes. |
| `TestRejectInvalidHistory` | Tampered bytes, tampered signatures, missing predecessors, cross-package/domain links, invalid ordering, malformed references and traversal limits. |
| `TestStoreLimitsAndPredecessorValidation` | Blob size boundaries, oversized encoded records, missing predecessors and unauthorized records. |
| `TestHistoryCycleGuard` | Repeated hashes are rejected as cycles and visited-set growth is bounded. |
| `TestBlobTamperingAndInvalidConfiguration` | Blob hash mismatches, negative history limits and missing ownership/storage configuration. |
| `TestRegistryAcrossSimulatedDHT` | Blob and record storage followed by a download using another Kademlia instance over the simulated network. |

The existing signature-tampering and DNS ownership tests were preserved in the new package. After implementation, the full suite and race detector passed for `cmd/simshell`, `experiments`, `kademlia` and `registry`. `git diff --check` also passed.

Reproduce the checks from `labs`:

```sh
go test ./registry
go test ./...
go test -race ./...
```

The race detector requires CGO and a supported C compiler. These results validate the implemented immutable layer; they do not establish completion of the later mutable-pointer or CLI workflows.

## Remaining Part 2 work

Authenticated mutable latest-pointer RPCs, atomic rejection of conflicting publications and rollbacks, stale-node catch-up, mutable-pointer replication and complete CLI integration remain separate issues. Existing latest-pointer signing and verification helpers are available in `registry`, but issue #47 does not connect them to a publication workflow.
