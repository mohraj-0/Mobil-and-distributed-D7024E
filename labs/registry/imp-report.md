# Registry implementation report

Append an entry here for each registry addition, including behavior, affected
files, validation, and remaining limitations.

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
path provides the history-validation foundation for #49 but does not claim
global consensus or resolution of owner equivocation across partitions. CLI
publication/install/display commands and the complete runnable demo remain
separate issues. Storage remains in memory and availability depends on live
replicas. DNS owner-key rotation can invalidate previously signed history.

### Follow-up requirement audit

All six requested behaviors are present. History validation is implemented in
`verifyLatest` and the serialized head checks in `acceptLatest`; this repository
has no separate existing history-policy interface to invoke. Added transport
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
and registry integration race tests pass. The integration merge is uncommitted.
