# Registry implementation report

Append an entry here for each registry addition, including behavior, affected
files, validation, and remaining limitations.

## Issue #50: stale-node catch-up and latest-pointer repair

### What changed and why

`kademlia/latest.go` now catches up the querying node after comparing all
reachable selected replicas, then sends the chosen pointer to replicas through
`UPDATE_LATEST`. Previously a lookup returned the newest compatible pointer
without updating the querying node or repairing stale replicas. A node that
missed publications could therefore remain stale even after a successful read.
The local accepted head now participates even when the node is outside the
closest replica set. This prevents a stale remote response from bypassing the
reader's own accepted history.

### How verification and repair work

The existing shared `metadata.TraverseHistory` fetches predecessor records all
the way to a valid zero-predecessor genesis. It verifies content hashes, owner
signatures, namespace, record references, and strictly increasing successor
versions, with cycle detection and a 1,024-record bound. This implementation
retains full genesis verification rather than stopping at a cached accepted
head. The pointer must authenticate and match the referenced record's version.
Missing records or failed verification cannot partially advance a head.

Lookup compares authenticated histories in either response order and chooses
the greatest version only when histories extend each other. Owner-signed forks
remain explicit conflicts; no repair runs when observed valid histories conflict.
The chosen history is committed through `commitLatest`, which re-reads the head
under the package lock. If a concurrent publication has overtaken the read,
lookup authenticates that committed head and returns it only if it extends the
selected result. A concurrent conflicting publication fails the read.

Repair uses the same authenticated RPC as publication, including receiver-side
record retrieval and final locked head checks. A delayed older repair is rejected
and cannot overwrite a newer state. Identical repair is idempotent. Repair is
best effort: a timeout, departed replica, or receiver rejection does not invalidate
an otherwise verified read. Catch-up retrieves records for verification without
caching them, preserving the existing immutable lookup behavior.

### Tests and validation

`kademlia/latest_catchup_test.go` adds disconnected/rejoining replicas missing
three publications and their records, both stale-first and newest-first reads,
repair of stale replicas, an unavailable intermediate with unchanged stored
state, the local-head constraint outside the selected replica set, and delayed
repair rejection. A barrier also holds repair authentication while a newer
publication commits, checking the final head re-check prevents rollback.
Another barrier delays a remote read while a newer compatible local publication
commits; lookup returns that newer head and repairs the remote to it.
Existing history tests cover invalid signatures, hashes, namespaces, ancestry,
version ordering, concurrent sibling updates, and partitioned owner equivocation.

Validation passed from `labs`: `GOMAXPROCS=2 go test -p 1 ./...` and focused
race checks with `go test -race -p 1 ./kademlia ./metadata ./registry -run
'TestLatest|TestHistory|TestRejectInvalidHistory|TestRegistryAcrossSimulatedDHT'
-count=1`. After adding the delayed-read barrier test, both its focused regular
run and `go test -race -p 1 ./kademlia -run TestLatest -count=1` passed.
Windows race checks used CGO with `C:\msys64\ucrt64\bin\gcc.exe`.
`gofmt` and `git diff --check` also passed. Initial test attempts encountered
Windows allocation/paging-file failures; subsequent retries completed normally.

### Remaining limitations

This remains per-node authenticated history validation rather than global
consensus. Unreachable forks cannot be detected, repair is triggered by reads,
heads remain in memory, and records must remain retrievable for full genesis
verification. Sequential replica requests and synchronous best-effort repair can
add RPC timeout latency to a lookup; no background repair queue is introduced.

## Issue #48: authenticated mutable latest-pointer RPCs

Registry storage and traversal live in `registry`. Signing, record types and DNS
helpers live in the dependency-free `metadata` package, with shared aliases in
`registry` and `kademlia` so receivers can authenticate pointers without creating
an import cycle. This report is the implementation log for the pointer RPC path.

### Additions

- `kademlia/latest.go`: `LatestKey`, `PublishLatest`, `LookupLatest`, receiver
  validation, and correlated update/read RPCs. The key is SHA-256 of
  `domain:package:latest`, independent of the signed JSON value.
- `kademlia/kademlia.go`: dispatches `UPDATE_LATEST`, `READ_LATEST`, and
  `LATEST_REPLY`; adds pending-response synchronization and status/error fields.
- `kademlia/maintenance.go`: replicates name-addressed latest pointers through
  authenticated publication; ordinary blobs and records retain content keys.
- `kademlia/latest_test.go`: tests first publication and updates across two
  replicas with simulated and UDP transports, authenticated reads, idempotent
  replay, rollback rejection, unknown tags, malformed JSON, namespace mismatch,
  invalid signatures, missing records, immutable STORE protection, replication,
  and RPC timeout classification.

### Validation and API behavior

Each receiver uses its configured DNS ownership verifier. Pointer signatures
and every referenced version-record signature and content hash are checked.
The head record must agree with pointer namespace/version metadata. History
must stay in the same namespace, decrease strictly when walking predecessors,
and terminate at the 64-zero genesis predecessor. A visited set and 1,024-record
limit bound traversal. Blob references must be valid SHA-256 keys; historical
blob availability is not required to authenticate history.

History fetches occur before the commit lock. Final head comparison and
replacement are serialized and occur under the data-store lock. Updates must
extend the locally accepted chain; identical replay succeeds, while forks,
rollbacks, and same-version replacements fail without changing the head.

`PublishLatest(pointer)` returns a map from replica address to error plus an
overall error if no replica accepts. Partial acceptance is visible in the map.
Both publication and lookup select the configured K closest discovered peers,
including the local node when appropriate. `LookupLatest(domain, package)`
compares verified compatible histories and reports conflicting branches.

`LatestError.Status` distinguishes `malformed`, `authentication`, `conflict`,
`missing_data`, `timeout`, `transport`, and aggregate `rejected` results. RPCs
use existing Node transports, random request IDs, timeout and retry settings;
pending channels are removed when calls finish. Ordinary blob lookup preserves
typed timeout and corrupt-response authentication errors, and ancestry validation
propagates these instead of treating every failed fetch as missing data.

Namespaces must already be canonical: lowercase domain without a trailing
dot, trimmed package name, and no whitespace or colon separators. Package
case is preserved. Signed identifiers are rejected rather than rewritten.
Pointer values retain the existing 32 KiB limit.

### Verification

Run from `labs` in PowerShell:

```powershell
$env:GOMAXPROCS='2'
go test -p 1 ./...
go test -race -p 1 ./kademlia -run TestLatestTransportAndValidation -count=1
```

On Windows, race tests require CGO and a C compiler. The initial unrestricted
build exhausted host memory; the regular suite passed with reduced parallelism.
The full race suite encountered a ThreadSanitizer host allocation failure
(Windows error 1455), rather than a reported application data race.
The focused `TestLatestTransportAndValidation` race run passed for both
transports after the final changes; the full regular suite also passed.

### Remaining work

Read repair and stale-node integration scenarios remain issue #50. This RPC
path implements the #49 history-validation policy but does not claim
global consensus or resolution of owner equivocation across partitions. CLI
publication/install/display commands and the complete runnable demo remain
separate issues. Storage remains in memory and availability depends on live
replicas. DNS owner-key rotation can invalidate previously signed history.

### Follow-up requirement audit

All six requested behaviors are present. The initial history validation was
implemented in `verifyLatest` and serialized head checks in `acceptLatest`;
issue #49 now shares the ancestry policy with registry traversal. Added transport
coverage for fork rejection, missing pointer reads, immutable lookup protection,
and timeouts during referenced-record retrieval. Updated the corrupt immutable
response regression to assert the explicit authentication result. The full
regular suite passes with `GOMAXPROCS=2` and `go test -p 1 ./...`.
The focused race run for `TestLatestTransportAndValidation` and
`TestLookupDataRejectsCorruptedRemoteValue` also passes. `git diff --check`
reports no whitespace errors.

### CI package migration fix

Integrated main's issue #47 registry migration. Its removal of the record types
and owner-verification methods from `kademlia` caused the undefined-symbol CI
failure. Shared `metadata` definitions and receiver-side ownership methods now
connect the pointer transport to the migrated layout. The registry integration
test signs a registry pointer, publishes it to two DHT replicas and reads it from
the other replica. The full regular suite passes. The full race suite encounters
Windows ThreadSanitizer allocation error 1455; focused pointer, corrupted-response
and registry integration race tests pass. The integration was initially left
uncommitted and has since been merged into main.

## Issue #49: atomic rejection of unauthorized or conflicting publications

### Implementation and why

`metadata/history.go` now provides the shared ancestry-validation policy used by
both `registry.Registry.TraverseHistory` and receiving DHT nodes. Sharing it
prevents immutable registry reads and mutable pointer updates from applying
different history rules. Traversal authenticates every record's content hash
and owner signature, requires matching domain/package links, and checks that
successor versions strictly increase. Genesis is the oldest record whose
predecessor is the 64-zero sentinel; its version may be any positive value.
The sentinel cannot itself be used as a pointer head. Blob hashes must be
nonzero SHA-256 keys. A visited set rejects cycles, and a 1,024-record default
limit bounds traversal work. Registry callers retain their configurable limit.
Errors preserve authentication, malformed-data, missing-data and conflict
categories, including wrapped transport timeouts from record retrieval.

`kademlia/latest.go` authenticates the pointer and complete ancestry before
committing, and compares pointer version/domain/package with the referenced
record. Skipping published versions is allowed only when the complete chain
is present, authenticated and extends the current head; a larger version
number alone never authorizes replacement.

Final validation and replacement use a lock for the name-derived package key.
History and DNS lookups happen outside this lock so slow network operations
cannot hold up commits. `commitLatest` re-reads the current head under the
package lock and data-store lock, checks for identical replay, rejects versions
at or below the current version, and requires the accepted record hash in the
incoming ancestry. Only then does it replace the pointer. Therefore two valid
siblings cannot both commit: whichever commits second observes the first as
the new head and fails either the version or ancestry check. Independent
packages have separate commit locks. Lock references include queued waiters
and entries are removed when their last caller finishes, avoiding a permanent
lock entry for every package encountered. Incoming bytes are copied before
validation so the committed bytes are the authenticated bytes.

No rejected update writes the head. Record fetches do not cache data, and
immutable record storage keeps its hash(value)==key checks. Identical accepted
pointers succeed without rewriting stored bytes; different records at the
same version are conflicts even when both carry valid owner signatures.

### Tests

`kademlia/latest_history_test.go` checks whole-store snapshots before and after
rejections for non-owner pointer and record signatures, rollback, same-version
replacement, competing genesis, immediate and ancestral namespace mismatches,
pointer/version disagreement, equal or decreasing chain versions, unavailable
skipped ancestry, invalid predecessor/genesis, zero blob/head references, and
untrusted cyclic storage. Successful tests cover genesis, skipped versions and
idempotent replay. An explicit stale-validation test changes the head between
authentication and commit and confirms that the final check rejects a sibling.

Concurrent sibling tests run 20 equal-version and 20 different-version races.
A barrier puts both publications in authentication at the same time; exactly
one may succeed, its bytes must be the stored head, and replay of the loser must
leave the entire store unchanged. A separate test holds one package lock and
confirms that a different package can commit. These tests run under `go test
-race` along with the simulated/UDP pointer transport tests.

`metadata/history_test.go` exercises self-cycle and multi-record-cycle visited
guards directly, plus the traversal bound. A truly hash-consistent cyclic
record fixture would require SHA-256 fixed points; the receiver test instead
models malicious cyclic storage and confirms it is rejected by immutable hash
verification before affecting the head. No hash checks are bypassed for tests.

### Partition and owner-equivocation limitations

This is atomic validation on each node, not global consensus. Partitioned
replicas can each accept a different owner-signed branch when neither has
observed the other's head. Signatures prove ownership, not that the owner has
published only one history. Likewise a fresh node can accept an authenticated
branch without knowing that another replica has already accepted a competing
one. Versions do not resolve those forks.

`LookupLatest` compares authenticated replica histories and returns an explicit
conflict when branches do not extend each other, including a higher-version
competing branch. It does not silently choose a winner or mutate either head.
The partition/equivocation test creates two accepted owner-signed branches,
connects the replicas and checks conflict responses and unchanged state on
both nodes. An unreachable branch cannot be detected by a read of reachable
replicas alone. Resolving equivocation, agreement across partitions and durable
head persistence require additional policy beyond this implementation; heads
remain in memory and DNS key rotation may invalidate old signatures.

### Validation

From `labs`, the full regular suite passes with `GOMAXPROCS=2` and
`go test -p 1 ./...`. Focused history, pointer and registry race tests pass with
`go test -race -p 1 ./kademlia ./metadata ./registry -run
'TestLatest|TestHistory|TestRejectInvalidHistory|TestRegistryAcrossSimulatedDHT'
-count=1`. This includes all 40 sibling races and the independent-package lock
test. On Windows the race run uses CGO and the configured GCC compiler.
