# Lookup experiments

`-value-bytes` supports **1–32,768 raw bytes (32 KiB)** per value. See [Kademlia value sizes](../kademlia/README.md) for transport limits and boundary tests.

Run it from `labs`:

```powershell
go run .\experiments `
  -node-counts 20,50 `
  -alphas 1,3,5 `
  -k-values 10 `
  -seeds 1,2,3,4,5 `
  -lookups 20
```

Outputs:

- `experiments/lookup_raw.csv`: one row per observed probe plus one final result row per lookup
- `experiments/lookup_summary.csv`: average and variance by configuration and lookup type
- `experiments/report.md`: generated report text explaining setup, measurement, expectations, and observed results

Raw CSV columns include:

- `lookup_type`: `node` or `value`
- `hop`: estimated lookup round, grouped by `alpha` probes because the lookup sends strict parallel batches
- `event`: `probe` or `result`
- `rpc`: `FIND_NODE` or `FIND_VALUE`
- `success`: final lookup success/failure on `result` rows
- `metric_count`: probe number on `probe` rows, total probes on `result` rows

The topology and key-value pairs are generated with `math/rand` from the given
seed list, so rerunning with the same flags recreates the same experiment.

## Loss, latency, alpha, churn, and replication experiments

From `labs`, run:

```powershell
go run ./experiments -resilience -node-counts 8 -alphas 1,3,5 -k-values 1,3,5 -seeds 1,2,3 -lookups 10 -loss-rates 0,0.1,0.3 -latencies 0ms,5ms,20ms -churn-rates 0,0.1,0.5 -churn-window 1s -timeout 200ms
```

Outputs go to `experiments/resilience` (change with `-resilience-out`):

- `requests.csv`: individual lookup request/response durations, with an explicit response-received flag. Missing responses have blank durations; do not interpret them as zero milliseconds.
- `trials.csv`: lookup durations, probes, estimated hops, success, storage errors, and actual join/departure counts, alongside each configuration's parameters.
- `probes.csv`: per-probe and final lookup events.
- `report.md`: aggregated success rates, mean probes, and mean lookup durations for each configuration.

The loss/latency sweep varies all combinations of loss and delay at the first alpha and last k. The alpha sweep varies alpha at that k and the largest configured latency, with no loss or churn. Put the desired baseline k last in `-k-values`. The churn/replication sweep varies all combinations of churn and k with no packet loss or delay. Every trial creates a fresh network and stores its value under healthy conditions before introducing failures. This isolates lookup behavior from failed initial replication. Value success requires that the returned bytes match the stored bytes. Node success requires both returning the target and receiving a response from it: this implementation can return a known contact even if its probe timed out.

### What to expect

- **Request/response time versus latency and loss:** successful RPCs should take roughly twice the one-way delay plus scheduling and processing time. Loss is applied independently to requests and responses; delivery of both has probability `(1-loss)^2`. Lost requests or replies cause timeout waits. The timing of successful responses alone can conceal those failures, so compare response-received fractions and total lookup durations too. Delays approaching half the RPC timeout can cause healthy responses to arrive too late.
- **Alpha versus probes and lookup time:** larger alpha sends more probes concurrently, reducing the number of sequential rounds when probe counts stay similar. Node lookup time should roughly follow rounds times RTT; timeout rounds instead approach the configured timeout. Alpha need not reduce probe counts. Value lookups can stop early, while wider batches may already have sent extra probes. Fully populated routing tables make node probe counts largely depend on k, limiting conclusions about sparse deployments. Logged hops are `ceil(probes/alpha)`, an estimate rather than a measured path length.
- **Churn versus reliability:** churn rate is departures per node per simulated second. Over the configured exposure window, each original node independently leaves with probability `1-exp(-rate*window)`. Each departure creates a replacement with a fresh identity and empty store, preserving network size. Replacement routing tables are seeded from current membership; surviving nodes retain stale contacts. Higher churn loses replicas and adds dead contacts, so lookup success should decline and timeout cost can increase. The exposure is simulated without sleeping; this is a controlled join/leave phase between storage and lookup, not continuous churn during in-flight RPCs. Background refresh and repair are excluded.
- **K versus reliability:** k controls both the replica count (capped by network size) and routing shortlist size in this implementation. More replicas should increase the chance a value survives churn, but can increase lookup traffic. If departure probability is p and there are k independent replicas, the probability at least one survives is `1-p^k`; that is an availability reference, not a guarantee that routing finds it. At zero churn, healthy lossless trials should succeed for all tested k.

Use multiple seeds and enough trials to compare success rates. Timing and concurrent packet-loss assignment vary with scheduling, even with seeded topology and random choices; the results are not byte-for-byte reproducible. Request timings include late responses received before the lookup finishes. Outstanding responses after completion are logged as missing. Results include local value hits with zero probes; account for these when interpreting value timings.

Run the tests with `go test ./experiments -v`. They verify rate validation, churn exposure, complete packet loss, minimum round-trip delay, lossless value lookup, replacement of all nodes, and the actual replica count for different k values.
