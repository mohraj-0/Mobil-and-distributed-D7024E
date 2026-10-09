# Lookup experiments

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
all of this has to be verified.