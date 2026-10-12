$ErrorActionPreference = 'Stop'
Push-Location (Join-Path $PSScriptRoot '..')
try {
    New-Item -ItemType Directory -Force experiments/scalability-1000 | Out-Null
    go run ./experiments -node-counts 10,20,50,100,200,500,1000 -alphas 3 -k-values 20 -seeds 1,2,3 -lookups 30 -raw-out experiments/scalability-1000/lookup_raw.csv -summary-out experiments/scalability-1000/lookup_summary.csv -report-out experiments/scalability-1000/report.md 2> experiments/scalability-1000/runtime.log
    if ($LASTEXITCODE -ne 0) { throw 'Lookup experiment failed; inspect runtime.log' }
    python experiments/plot_scalability.py --input experiments/scalability-1000
    if ($LASTEXITCODE -ne 0) { throw 'Plot generation failed' }
} finally {
    Pop-Location
}
