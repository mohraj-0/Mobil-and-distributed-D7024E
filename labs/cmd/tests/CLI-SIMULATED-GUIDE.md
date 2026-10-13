# Part 1 and Part 2: interactive CLI with 10 simulated nodes

Use **two PowerShell terminals in `labs`**. Terminal 1 runs the interactive `simnet>` CLI; terminal 2 updates recipes and verifies downloads. Type CLI commands directly, without `d`. No Docker engine or UDP sockets are needed.

All ten nodes share one in-process simulated network. Terminal 2 stays in PowerShell: starting another simulated CLI would create a separate network. All `put`, `get`, `publish`, `install`, and `show` commands run in terminal 1. Keep that session open through sections 3–5 and the local-store demonstration: `exit` destroys the simulated network and its data.

## 1. Prepare in terminal 1: PowerShell

```powershell
New-Item -ItemType Directory -Force cli-check
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
go build -o cli-check/kadlab.exe ./cmd/simshell

# Prepare creates/reuses ownership files; it does not start Docker.
.\docker-lab.ps1 -Action Prepare -PublisherKey cli-check/owner.key -OwnersFile cli-check/owners.json
if (-not (Test-Path cli-check/other.key)) { .\cli-check\kadlab.exe keygen cli-check/other.key }
. ./cmd/tests/recipe-demo.ps1
Update-Release 1
[IO.File]::WriteAllBytes("$PWD/cli-check/binary.bin", [byte[]](0,1,127,128,255))
[IO.File]::WriteAllBytes("$PWD/cli-check/limit.bin", [byte[]]::new(32768))
[IO.File]::WriteAllBytes("$PWD/cli-check/oversize.bin", [byte[]]::new(32769))

# Enter the CLI. k=10 includes this node among the ten storage replicas.
.\cli-check\kadlab.exe --transport simulated --nodes 10 --k 10 --owners-file cli-check/owners.json --publisher-key cli-check/owner.key 2>> cli-check/simulated.log
```

The source recipe must begin with `BLUEBERRY PIE`, `Version: 1`, and its initial change description. Probe logs are saved separately so CLI output remains readable. `show ds` will display local entries because the healthy ten-node network uses `k=10`.

## 2. Set up terminal 2: PowerShell

Open a second terminal in `labs`. Keep it in PowerShell throughout the demonstration:

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
. ./cmd/tests/recipe-demo.ps1
Get-Content cli-check/release.txt
```

Both terminals share the local files. Terminal 2 changes the recipe and reads files downloaded by terminal 1; it is not an independent network client. For two independent CLI clients communicating between processes, use the [local UDP guide](CLI-UDP-GUIDE.md) or [Docker UDP guide](CLI-DOCKER-GUIDE.md).

## 3. Part 1: put and get in terminal 1, verify in terminal 2

Terminal 1, at `simnet>`, one command per line:

```text
ping 127.0.0.1:8009
show rt
show nodes
put cli-check/release.txt
show ds
```

Expect pong and round-trip time, routing buckets with shortened peer IDs, all ten simulated nodes, and the recipe's full SHA-256 key. `show ds` should show its shortened key and byte count.

Copy the hash printed by `put` into the following commands in terminal 1, replacing `RECIPE_KEY` with the actual hash:

```text
get RECIPE_KEY
get RECIPE_KEY cli-check/sim-download.txt
show ds
put cli-check/binary.bin
put cli-check/limit.bin
put cli-check/oversize.bin
```

`get` prints the recipe and source node, or saves the requested output file. Binary input and 32,768 bytes must succeed; 32,769 bytes must report a size error. Copy the binary upload's hash into terminal 1's next command:

```text
get BINARY_KEY cli-check/sim-binary.bin
```

Terminal 2, PowerShell, after both downloads succeed:

```powershell
Get-Content cli-check/sim-download.txt
Get-FileHash cli-check/release.txt -Algorithm SHA256
(Get-FileHash cli-check/release.txt).Hash -eq (Get-FileHash cli-check/sim-download.txt).Hash
(Get-FileHash cli-check/binary.bin).Hash -eq (Get-FileHash cli-check/sim-binary.bin).Hash
```

Both comparisons must print `True` before updating the recipe. Missing-file errors mean verification could not complete. Do not exit terminal 1 to perform these checks.

Terminal 1, at `simnet>`, run individually:

```text
ping bad-address
ping 127.0.0.1:7999
put missing-file.txt
get bad-key
get ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff
```

Expect malformed-address, unreachable-peer, missing-file, invalid-key, and not-found errors respectively. PowerShell variables, comments, and loops do not belong at `simnet>`.

## 4. Part 2: publish ten versions and install through the simulation

Terminal 1, at `simnet>`:

```text
show dns example.com
show dns missing.example
publish example.com:demo:1 cli-check/release.txt
show example.com:demo
install example.com:demo:1 --output cli-check/sim-1.txt
install example.com:demo:latest --output cli-check/sim-latest.txt
```

Expect the owner's public key, an unknown-owner error, replica acceptance plus `published`, and verified version 1. Use a fresh package name instead of `demo` if this network already contains that history.

For **each version 2 through 10**, leave terminal 1 open:

1. Terminal 2, PowerShell: run `Update-Release N`, replacing `N` with the next version number. It saves and prints the actual recipe and its hash in the same source file.
2. Terminal 1, `simnet>`: run that version's publication from the table below.
3. Wait for publication to finish, then run the per-version install commands below in terminal 1.
4. Terminal 2 verifies that download before updating the recipe again.

Unlike the Docker demonstration, do not exit or restart the simulated CLI between versions: its process owns the entire network.

| N | Recipe change | Terminal 1 command at `simnet>` |
|---|---|---|
| 2 | Reduce sugar to 120 g. | `publish example.com:demo:2 cli-check/release.txt` |
| 3 | Add 1 tablespoon lemon juice. | `publish --prev=2 example.com:demo:3 cli-check/release.txt` |
| 4 | Add 1 teaspoon lemon zest. | `publish example.com:demo:4 cli-check/release.txt` |
| 5 | Increase cornstarch to 3 tablespoons. | `publish example.com:demo:5 cli-check/release.txt` |
| 6 | Chill dough for 30 minutes. | `publish example.com:demo:6 cli-check/release.txt` |
| 7 | Increase oven temperature to 190 C. | `publish example.com:demo:7 cli-check/release.txt` |
| 8 | Increase baking time to 45 minutes. | `publish example.com:demo:8 cli-check/release.txt` |
| 9 | Add an egg glaze. | `publish example.com:demo:9 cli-check/release.txt` |
| 10 | Cool for 2 hours. | `publish --prev=9 example.com:demo:10 cli-check/release.txt` |

After each publication, terminal 1 runs these at `simnet>`. Replace `N` in the version and filename with the actual number:

```text
show example.com:demo
install example.com:demo:N --output cli-check/sim-N.txt
install example.com:demo:latest --output cli-check/sim-latest.txt
```

Terminal 2, PowerShell, replaces the number in `$version` with the version just published:

```powershell
$version = 2
Get-Content "cli-check/sim-$version.txt"
[IO.File]::ReadAllText("$PWD/cli-check/sim-$version.txt") -ceq (Get-RecipeText $version)
[IO.File]::ReadAllText("$PWD/cli-check/sim-latest.txt") -ceq (Get-RecipeText $version)
```

Expect `True` for both comparisons. `Get-RecipeText N` generates the expected text without modifying files. Each version retains earlier recipe improvements.

After version 10, expect history `10 -> 9 -> ... -> 1 -> genesis`. Retrieve every historical version in terminal 1, at `simnet>`:

```text
install example.com:demo:1 --output cli-check/sim-1.txt
install example.com:demo:2 --output cli-check/sim-2.txt
install example.com:demo:3 --output cli-check/sim-3.txt
install example.com:demo:4 --output cli-check/sim-4.txt
install example.com:demo:5 --output cli-check/sim-5.txt
install example.com:demo:6 --output cli-check/sim-6.txt
install example.com:demo:7 --output cli-check/sim-7.txt
install example.com:demo:8 --output cli-check/sim-8.txt
install example.com:demo:9 --output cli-check/sim-9.txt
install example.com:demo:10 --output cli-check/sim-10.txt
install example.com:demo:1
show ds
```

Terminal 2, PowerShell:

```powershell
foreach ($version in 1..10) {
    $actual = [IO.File]::ReadAllText("$PWD/cli-check/sim-$version.txt")
    $matches = $actual -ceq (Get-RecipeText $version)
    Write-Output $actual
    "version=$version matches=$matches"
    if (-not $matches) { throw "Incorrect recipe for version $version" }
}
[IO.File]::ReadAllText("$PWD/demo-1.pkg") -ceq (Get-RecipeText 1)
```

Expect `matches=True` for every version and `True` for the default version 1 install. Old versions retain their original bytes after the source file becomes version 10.

## 5. Invalid updates: CLI attempts, PowerShell checks

Terminal 1, at `simnet>`, run each separately:

| Command | Expected rejection |
|---|---|
| `publish example.com:demo:5 cli-check/release.txt` | Rollback at sender. |
| `publish example.com:demo:10 cli-check/release.txt` | Duplicate version. |
| `publish --prev=3 example.com:demo:11 cli-check/release.txt` | Fork at sender. |
| `publish --force example.com:demo:5 cli-check/release.txt` | Forced rollback at receivers. |
| `publish --force --prev=3 example.com:demo:11 cli-check/release.txt` | Forced fork at receivers. |
| `publish --force --prev=0 example.com:demo:11 cli-check/release.txt` | Competing genesis at receivers. |
| `publish --force --prev=ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff example.com:demo:11 cli-check/release.txt` | Missing ancestry at receivers. |
| `publish --prev=3 example.com:demo:11 cli-check/release.txt` | Sender rejects again; force does not persist. |
| `publish example.com:demo:0 cli-check/release.txt` | Invalid version. |
| `publish --prev=99 example.com:demo:11 cli-check/release.txt` | Unknown predecessor. |

Force skips sender preflight; receivers still validate. Failed attempts may leave immutable blobs/records but must not change accepted history. After **each attempt**, terminal 1 runs:

```text
show example.com:demo
install example.com:demo:latest --output cli-check/sim-latest.txt
```

History must still contain ten versions, with latest 10. Terminal 2 verifies the downloaded bytes:

```powershell
Get-Content cli-check/sim-latest.txt
[IO.File]::ReadAllText("$PWD/cli-check/sim-latest.txt") -ceq (Get-RecipeText 10)
```

Expect `True`. Terminal 1 also tests missing entries:

```text
install example.com:demo:99
install example.com:missing:latest
```

Expect not found for both. Keep terminal 1 open for the local-store demonstration below.

## 6. Decoded local store and exit

In the original owner session, terminal 1, at `simnet>`:

```text
put cli-check/release.txt
show ds
publish example.com:visible:1 cli-check/release.txt
show ds
show rt
show example.com:visible
install example.com:visible:latest --output cli-check/sim-visible.txt
install example.com:demo:latest --output cli-check/sim-latest.txt
```

Use a fresh name instead of `visible` on repeat runs. With all ten nodes healthy and `k=10`, expect the blob and decoded `version-record` and `latest-pointer` entries for visible version 1. `show ds` and `show rt` inspect the current node only; `get HASH` displays the actual blob contents.

Terminal 2, PowerShell, verifies both downloads:

```powershell
[IO.File]::ReadAllText("$PWD/cli-check/sim-visible.txt") -ceq (Get-RecipeText 10)
[IO.File]::ReadAllText("$PWD/cli-check/sim-latest.txt") -ceq (Get-RecipeText 10)
```

Expect `True`. Only now type `exit` in terminal 1 and run section 7's wrong-owner session. Simulation exit destroys all nodes and their in-memory values; downloaded files remain available to terminal 2.

## 7. Wrong-owner demonstration

The signer is configured at startup. After completing section 6 and exiting its owner session, run this in terminal 1's PowerShell to create a **fresh** simulated network with the non-owner signer:

```powershell
.\cli-check\kadlab.exe --transport simulated --nodes 10 --k 10 --owners-file cli-check/owners.json --publisher-key cli-check/other.key 2>> cli-check/simulated.log
```

At terminal 1's `simnet>`:

```text
publish example.com:intruder:1 cli-check/release.txt
publish --force example.com:intruder:1 cli-check/release.txt
show example.com:intruder
exit
```

Expect sender rejection without force, receiver signature/ownership rejection with force, and no accepted intruder package. Receivers still trust the original owner's public key. The previous demo history is gone after owner-session exit, so this check runs in a new network rather than the Docker guide's persistent server network.

## 8. Status, logs, and targeted checks: PowerShell only

Terminal 2 can inspect logs while the CLI runs:

```powershell
Get-Content cli-check/simulated.log -Tail 20
```

Use terminal 1's CLI `show nodes`, `show rt`, and `show ds` to inspect simulation state. Run targeted checks in either PowerShell terminal:

```powershell
go test ./kademlia -run 'TestStoreLocalAndHandleStoreRPC|TestLookupDataRejectsCorruptedRemoteValue' -count=1 -v
go test ./cmd/simshell -run '^TestCLIInstallCatchesUpSeveralMissedPublications$' -count=1 -v
go test ./metadata ./registry -run 'TestHistoryCycle' -count=1 -v
```

These tests cover wrong-hash stores, corrupted values, missed-update catch-up, and cyclic history that normal CLI commands cannot directly create. Require PASS; this demonstration does not replace the full engineering suite or report.

## 9. Finish: terminal 1

If any simulated CLI remains open, type `exit` at `simnet>`. Both terminals then return to PowerShell. No Docker stack needs stopping. Host keys, ownership configuration, recipes, logs, and downloaded files remain available.
