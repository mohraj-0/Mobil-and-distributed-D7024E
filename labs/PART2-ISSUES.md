# Part 2 GitHub issue drafts

Repository: https://github.com/mohraj-0/Mobil-and-distributed-D7024E

Status: **drafts only**. GitHub issue creation returned HTTP 403 (`Resource not accessible by integration`); no issues were created. These 13 drafts cover the Part 2 scope in `PART2-DESIGN.md` and `LAB-SPEC.md`. The current code contains Part 1 storage/RPCs and CLI but no registry records, publication ownership, or registry commands.

## Issue index

1. [Part 2: Deliver the authenticated decentralized package registry](#tracker)
2. [Part 2: Define registry records, package identifiers, and version ordering](#schema)
3. [Part 2: Implement domain ownership verification and publisher key configuration](#ownership)
4. [Part 2: Sign and verify version records and latest pointers](#signatures)
5. [Part 2: Store and traverse immutable package blobs and version records](#immutable)
6. [Part 2: Add DHT RPC support for authenticated mutable latest pointers](#mutable)
7. [Part 2: Reject unauthorized publications, forks, cycles, and rollbacks atomically](#history)
8. [Part 2: Catch up stale nodes by verifying missing history and repairing latest pointers](#catchup)
9. [Part 2: Add publish command with --force and --prev](#publish)
10. [Part 2: Add install for latest and specific package versions](#install)
11. [Part 2: Add package history, DNS ownership, and structured data-store display](#show)
12. [Part 2: Add end-to-end authenticity, history, catch-up, and CLI regression tests](#tests)
13. [Part 2: Document cryptographic choices, registry behavior, and a reproducible demonstration](#docs)

<a id="tracker"></a>

## Part 2: Deliver the authenticated decentralized package registry

Implement the package registry described in `labs/PART2-DESIGN.md` and `labs/LAB-SPEC.md` using the existing Kademlia DHT and `network.go` transports.

### Dependencies

- [Part 2: Define registry records, package identifiers, and version ordering](#schema)
- [Part 2: Implement domain ownership verification and publisher key configuration](#ownership)
- [Part 2: Sign and verify version records and latest pointers](#signatures)
- [Part 2: Store and traverse immutable package blobs and version records](#immutable)
- [Part 2: Add DHT RPC support for authenticated mutable latest pointers](#mutable)
- [Part 2: Reject unauthorized publications, forks, cycles, and rollbacks atomically](#history)
- [Part 2: Catch up stale nodes by verifying missing history and repairing latest pointers](#catchup)
- [Part 2: Add publish command with --force and --prev](#publish)
- [Part 2: Add install for latest and specific package versions](#install)
- [Part 2: Add package history, DNS ownership, and structured data-store display](#show)
- [Part 2: Add end-to-end authenticity, history, catch-up, and CLI regression tests](#tests)
- [Part 2: Document cryptographic choices, registry behavior, and a reproducible demonstration](#docs)

### Acceptance criteria

- [ ] Complete the linked implementation issues with their acceptance tests.
- [ ] Demonstrate publish, install, package history, DNS ownership, and interpreted data-store output in addition to Part 1 commands.
- [ ] Show receiving nodes rejecting unauthorized, forked, and rollback updates, including forced client attempts.
- [ ] Demonstrate a stale node catching up after missing multiple versions.
- [ ] Document cryptographic choices, key configuration, version ordering, size limits, and consistency limitations; meet inherited CI, race, 1,000-node, and coverage requirements.

<a id="schema"></a>

## Part 2: Define registry records, package identifiers, and version ordering

Implement the package registry described in `labs/PART2-DESIGN.md` and `labs/LAB-SPEC.md` using the existing Kademlia DHT and `network.go` transports.

### Dependencies

None; this establishes the shared data model.

### Acceptance criteria

- [ ] Add typed version-record and latest-pointer schemas matching labs/PART2-DESIGN.md, including domain, package, version, referenced hashes, and signature.
- [ ] Define domain/package normalization and unambiguous parsing of DOMAIN:PACKAGE[:VERSION]; reserve latest for lookup and reject malformed identifiers.
- [ ] Choose non-negative integer versions or documented semantic versions with a total order; compare numerically rather than lexicographically.
- [ ] Define deterministic unsigned record serialization and SHA-256 digests for signing; hash the complete signed version record for its immutable key.
- [ ] Use the all-zero 256-bit hash for the first record's predecessor and SHA-256 of normalized domain:package:latest for the pointer key.
- [ ] Test deterministic encoding, namespace isolation, version ordering, malformed fields, hash lengths, and genesis handling.

<a id="ownership"></a>

## Part 2: Implement domain ownership verification and publisher key configuration

Implement the package registry described in `labs/PART2-DESIGN.md` and `labs/LAB-SPEC.md` using the existing Kademlia DHT and `network.go` transports.

### Dependencies

- [Part 2: Define registry records, package identifiers, and version ordering](#schema)

### Acceptance criteria

- [ ] Introduce an ownership-verifier interface that resolves the public key a node trusts for a domain.
- [ ] Implement a deterministic configurable fake DNS/TXT provider shared by simulated nodes; actual DNS can be an optional provider, not a prerequisite.
- [ ] Use Ed25519 keys with a documented TXT format such as d7024e-ed25519=<base64-public-key>; fail closed on missing, malformed, or ambiguous ownership records.
- [ ] Provide a documented way to generate/load the publisher private key and configure the matching public key for a test domain; do not infer ownership from a key supplied in an update.
- [ ] Ensure every receiving node verifies against its ownership provider; define lookup failures and key-change behavior without silently trusting arbitrary keys.
- [ ] Test owner/non-owner keys, unknown domains, malformed TXT data, and independent simulated-domain configurations.

<a id="signatures"></a>

## Part 2: Sign and verify version records and latest pointers

Implement the package registry described in `labs/PART2-DESIGN.md` and `labs/LAB-SPEC.md` using the existing Kademlia DHT and `network.go` transports.

### Dependencies

- [Part 2: Define registry records, package identifiers, and version ordering](#schema)
- [Part 2: Implement domain ownership verification and publisher key configuration](#ownership)

### Acceptance criteria

- [ ] Sign the SHA-256 digest of each deterministic unsigned record with Ed25519, excluding only the signature field.
- [ ] Verify both record types with the domain owner's resolved public key before trusting their metadata.
- [ ] Bind the signature to tag, domain, package, version, blob hash/predecessor or version-record hash; reject wrong tags, altered fields, and malformed signatures.
- [ ] Cross-check latest-pointer domain/package/version against the referenced authenticated version record.
- [ ] Add negative tests for tampering with every signed field, a different domain owner's key, and reusing a signature on the other record type.
- [ ] Keep private keys out of RPCs, logs, and show output; document the signature/public-key encodings.

<a id="immutable"></a>

## Part 2: Store and traverse immutable package blobs and version records

Implement the package registry described in `labs/PART2-DESIGN.md` and `labs/LAB-SPEC.md` using the existing Kademlia DHT and `network.go` transports.

### Dependencies

- [Part 2: Define registry records, package identifiers, and version ordering](#schema)
- [Part 2: Sign and verify version records and latest pointers](#signatures)

### Acceptance criteria

- [ ] Store package bytes and complete signed version records through the existing content-addressed Store/LookupData path.
- [ ] Resolve a version record by hash; verify its hash, signature, namespace, and predecessor before treating it as authenticated history.
- [ ] Traverse predecessor hashes with a visited set and a bounded traversal policy; report missing records, cycles, and cross-package/domain links.
- [ ] Keep older blobs and records immutable and accessible; publishing a new version must not overwrite historical entries.
- [ ] Respect the existing 1–32,768-byte value limit and provide clear errors for oversized blobs or encoded records; optional chunking requires a separately documented design.
- [ ] Test binary round trips, complete history traversal, tampered bytes, missing predecessors, and historical version availability.

<a id="mutable"></a>

## Part 2: Add DHT RPC support for authenticated mutable latest pointers

Implement the package registry described in `labs/PART2-DESIGN.md` and `labs/LAB-SPEC.md` using the existing Kademlia DHT and `network.go` transports.

### Dependencies

- [Part 2: Define registry records, package identifiers, and version ordering](#schema)
- [Part 2: Sign and verify version records and latest pointers](#signatures)
- [Part 2: Store and traverse immutable package blobs and version records](#immutable)

### Acceptance criteria

- [ ] Add a distinct mutable-pointer update/read path at SHA-256(domain:package:latest); do not weaken hash(value)==key checks for ordinary blobs and version records.
- [ ] Use the existing network.go Node transports and request-ID/timeout handling for both simulated and UDP RPCs.
- [ ] Make receiving nodes verify pointer signatures and referenced records and invoke the history-validation policy before accepting an update.
- [ ] Return explicit acceptance/rejection results to publishers and distinguish timeout, missing data, authentication failure, and conflicting history.
- [ ] Replicate and retrieve pointers across the configured K peers; dispatch periodic replication by value type so pointers are not accidentally stored under hash(pointer).
- [ ] Test first publication, pointer updates, unknown tags, malformed requests, protected immutable storage, and transport round trips.

<a id="history"></a>

## Part 2: Reject unauthorized publications, forks, cycles, and rollbacks atomically

Implement the package registry described in `labs/PART2-DESIGN.md` and `labs/LAB-SPEC.md` using the existing Kademlia DHT and `network.go` transports.

### Dependencies

- [Part 2: Sign and verify version records and latest pointers](#signatures)
- [Part 2: Store and traverse immutable package blobs and version records](#immutable)
- [Part 2: Add DHT RPC support for authenticated mutable latest pointers](#mutable)

### Acceptance criteria

- [ ] Implement per-package validation: genesis has a zero predecessor; successors have strictly greater versions and extend the accepted chain.
- [ ] Reject non-owner signatures, competing branches, older versions, same-version replacements, invalid namespace links, and cycles without changing the current head.
- [ ] Treat replay of an identical accepted pointer as idempotent; reject different records at the same version.
- [ ] Require incoming pointer metadata to agree with the authenticated referenced record and verify ancestry before allowing skipped versions.
- [ ] Serialize final validation and head replacement per package. If records are fetched outside the lock, re-check the head before committing; concurrent siblings must not both be accepted by one node.
- [ ] Explain limitations under partitioned replicas and owner equivocation; do not claim global consensus or silently choose one conflicting branch.
- [ ] Test state remains unchanged on every rejection and test concurrent sibling updates under go test -race.

<a id="catchup"></a>

## Part 2: Catch up stale nodes by verifying missing history and repairing latest pointers

Implement the package registry described in `labs/PART2-DESIGN.md` and `labs/LAB-SPEC.md` using the existing Kademlia DHT and `network.go` transports.

### Dependencies

- [Part 2: Store and traverse immutable package blobs and version records](#immutable)
- [Part 2: Add DHT RPC support for authenticated mutable latest pointers](#mutable)
- [Part 2: Reject unauthorized publications, forks, cycles, and rollbacks atomically](#history)

### Acceptance criteria

- [ ] When a node learns of a newer pointer, fetch predecessor records until reaching its accepted head or valid genesis; verify every hash, signature, namespace, and strictly increasing version.
- [ ] Advance only when the newer chain extends locally accepted history; reject conflicting branches, missing records, and failed verification.
- [ ] Latest lookup must compare responses from replicas and choose the newest valid compatible history rather than returning the first stale response.
- [ ] Repair stale replicas through the same authenticated update validation, and preserve newer state if a delayed response or repair arrives.
- [ ] Do not partially advance the head when catch-up fails; handle concurrent updates by re-checking the committed head.
- [ ] Test a node missing multiple publications, rejoining after departure, stale response ordering, unavailable intermediate records, invalid ancestry, and no rollback during repair.

<a id="publish"></a>

## Part 2: Add publish command with --force and --prev

Implement the package registry described in `labs/PART2-DESIGN.md` and `labs/LAB-SPEC.md` using the existing Kademlia DHT and `network.go` transports.

### Dependencies

- [Part 2: Implement domain ownership verification and publisher key configuration](#ownership)
- [Part 2: Store and traverse immutable package blobs and version records](#immutable)
- [Part 2: Add DHT RPC support for authenticated mutable latest pointers](#mutable)
- [Part 2: Reject unauthorized publications, forks, cycles, and rollbacks atomically](#history)
- [Part 2: Catch up stale nodes by verifying missing history and repairing latest pointers](#catchup)

### Acceptance criteria

- [ ] Support publish [--force] [--prev=PREVIOUS-VERSION] DOMAIN:PACKAGE:VERSION FILENAME in both Cobra commands and the interactive shell.
- [ ] Default the predecessor to the accepted latest record, or the all-zero hash for first publication; resolve explicit previous versions to record hashes.
- [ ] Without --force, validate version order and predecessor compatibility before attempting uploads; reject invalid input locally with useful errors.
- [ ] With --force, bypass client history preflight and actually attempt invalid updates, including stale predecessors and rollback; never bypass receiver authentication/history checks.
- [ ] Support --force with --prev together; report receiver rejection and do not print publication success when the pointer was rejected.
- [ ] Read configured signing keys, sign/upload immutable entries, and then publish the pointer; expose partial publication failures clearly.
- [ ] Test first/next publication, explicit predecessor, client preflight, forced fork/rollback attempts, missing files, size limits, and missing signing keys.

<a id="install"></a>

## Part 2: Add install for latest and specific package versions

Implement the package registry described in `labs/PART2-DESIGN.md` and `labs/LAB-SPEC.md` using the existing Kademlia DHT and `network.go` transports.

### Dependencies

- [Part 2: Store and traverse immutable package blobs and version records](#immutable)
- [Part 2: Add DHT RPC support for authenticated mutable latest pointers](#mutable)
- [Part 2: Catch up stale nodes by verifying missing history and repairing latest pointers](#catchup)

### Acceptance criteria

- [ ] Support install DOMAIN:PACKAGE:VERSION, including literal latest, in Cobra and the interactive shell.
- [ ] Resolve the latest pointer by the name-derived key, validate/catch up its history, and traverse to the requested historical version.
- [ ] Download the referenced blob and verify its content hash and authenticated record before creating the output file.
- [ ] Document output filename/location behavior; avoid unsafe paths from package identifiers and do not overwrite existing files silently.
- [ ] Return useful errors for unknown packages/versions, unavailable records/blobs, invalid signatures, conflicting histories, and oversized data.
- [ ] Test latest and historical binary downloads from a node with no local copy, stale replicas, tampered records/blobs, and output-file failure behavior.

<a id="show"></a>

## Part 2: Add package history, DNS ownership, and structured data-store display

Implement the package registry described in `labs/PART2-DESIGN.md` and `labs/LAB-SPEC.md` using the existing Kademlia DHT and `network.go` transports.

### Dependencies

- [Part 2: Implement domain ownership verification and publisher key configuration](#ownership)
- [Part 2: Store and traverse immutable package blobs and version records](#immutable)
- [Part 2: Add DHT RPC support for authenticated mutable latest pointers](#mutable)
- [Part 2: Catch up stale nodes by verifying missing history and repairing latest pointers](#catchup)

### Acceptance criteria

- [ ] Support show DOMAIN:PACKAGE and display the verified version chain concisely, including versions, record hashes, blob references, and predecessors.
- [ ] Support show dns DOMAIN and display the public key the node's verifier believes owns that domain, or a clear no-owner/lookup-error result.
- [ ] Extend show ds to distinguish blobs, version-records, and latest-pointers and show concise structured metadata instead of only hashes.
- [ ] Preserve show rt, show nodes, and other Part 1 commands; wire new forms through both Cobra and interactive command dispatch.
- [ ] Use a synchronized data-store snapshot for display so concurrent RPC updates do not race with map iteration.
- [ ] Test output for first/multiple versions, unknown domain/package, malformed records, and Part 1 command compatibility.

<a id="tests"></a>

## Part 2: Add end-to-end authenticity, history, catch-up, and CLI regression tests

Implement the package registry described in `labs/PART2-DESIGN.md` and `labs/LAB-SPEC.md` using the existing Kademlia DHT and `network.go` transports.

### Dependencies

- [Part 2: Add publish command with --force and --prev](#publish)
- [Part 2: Add install for latest and specific package versions](#install)
- [Part 2: Add package history, DNS ownership, and structured data-store display](#show)
- [Part 2: Reject unauthorized publications, forks, cycles, and rollbacks atomically](#history)
- [Part 2: Catch up stale nodes by verifying missing history and repairing latest pointers](#catchup)

### Acceptance criteria

- [ ] Build multi-node tests using network.go's simulated network, with independent node heads and controllable missed updates.
- [ ] Cover authorized publication, non-owner rejection, immutable history, forced forks, cycles/malformed ancestry, rollback, duplicate replay, and same-version conflicts.
- [ ] Verify stale replicas recover across several missed versions and installs retrieve authenticated latest and historical blobs.
- [ ] Exercise the required CLI command syntax and show output end to end, including --force/--prev combinations.
- [ ] Add representative UDP integration coverage and retain the existing 1,000-node simulated-network test in the normal suite.
- [ ] Run go test ./... and go test -race ./... in CI; assess coverage against the inherited 80% target and add meaningful missing tests.
- [ ] Ensure tests assert receiver state and actual remote communication, not just printed output or client-side rejection.

<a id="docs"></a>

## Part 2: Document cryptographic choices, registry behavior, and a reproducible demonstration

Implement the package registry described in `labs/PART2-DESIGN.md` and `labs/LAB-SPEC.md` using the existing Kademlia DHT and `network.go` transports.

### Dependencies

- [Part 2: Define registry records, package identifiers, and version ordering](#schema)
- [Part 2: Implement domain ownership verification and publisher key configuration](#ownership)
- [Part 2: Sign and verify version records and latest pointers](#signatures)
- [Part 2: Add publish command with --force and --prev](#publish)
- [Part 2: Add install for latest and specific package versions](#install)
- [Part 2: Add package history, DNS ownership, and structured data-store display](#show)
- [Part 2: Add end-to-end authenticity, history, catch-up, and CLI regression tests](#tests)

### Acceptance criteria

- [ ] Document the implemented Ed25519 signature scheme, canonical serialization, SHA-256 signing/hash rules, and DNS TXT/public-key encoding.
- [ ] Document version ordering, identifier normalization, genesis predecessor, immutable records, and the name-derived mutable pointer key.
- [ ] Explain receiver validation, no-fork/no-rollback rules, catch-up/read repair, and limitations around stale replicas, partitions, ownership-key changes, and equivocation.
- [ ] Provide exact key-generation/configuration and fake-DNS or real-DNS setup steps without requiring private keys in source control.
- [ ] Include a runnable demo: publish two versions, install latest/older, show chain/DNS/data store, force invalid updates, and demonstrate stale-node catch-up.
- [ ] Document 32 KiB per-value limits, output-file behavior, remaining omissions, test/coverage/race evidence, and the relationship to PART2-DESIGN.
- [ ] Preserve Part 1 usage documentation and add the registry design/testing choices to the assignment report.

