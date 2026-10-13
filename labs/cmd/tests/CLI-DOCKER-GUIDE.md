# Part 1 and Part 2: interactive CLI with 50 Docker UDP servers

Use two PowerShell terminals in `labs`. Terminal 1 writes and publishes; terminal 2 retrieves and installs. Both enter `simnet>`, but these sessions use **real UDP with 50 Docker servers**. Type CLI commands directly, without `d`.

Only type CLI commands at `simnet>`. Docker commands, recipe helpers, and file checks belong in PowerShell. Type `exit` to return to PowerShell; the 50 servers continue running and keep the data. Do not redeploy the stack between writing and reading.

## 1. Prepare in terminal 1: PowerShell

Start Docker Desktop's Linux engine. Use a fresh package name instead of `demo` when repeating against an existing history. Starting the stack may restart servers and lose their in-memory values.

```powershell
New-Item -ItemType Directory -Force cli-check
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
go build -o cli-check/kadlab.exe ./cmd/simshell
.\docker-lab.ps1 -Action Prepare -PublisherKey cli-check/owner.key -OwnersFile cli-check/owners.json
if (-not (Test-Path cli-check/other.key)) { .\cli-check\kadlab.exe keygen cli-check/other.key }
. ./cmd/tests/recipe-demo.ps1
Update-Release 1
[IO.File]::WriteAllBytes("$PWD/cli-check/binary.bin", [byte[]](0,1,127,128,255))
[IO.File]::WriteAllBytes("$PWD/cli-check/limit.bin", [byte[]]::new(32768))
[IO.File]::WriteAllBytes("$PWD/cli-check/oversize.bin", [byte[]]::new(32769))
.\docker-lab.ps1 -Action Start -PublisherKey cli-check/owner.key -OwnersFile cli-check/owners.json
.\docker-lab.ps1 -Action Status
```

Wait for **1/1 bootstrap and 49/49 nodes**, then continue in terminal 1's PowerShell:

```powershell
$bootstrap = docker ps --filter 'label=com.docker.swarm.service.name=kadlab_kademlia-bootstrap' --format '{{.ID}}'
if (@($bootstrap).Count -ne 1) { throw 'Expected one running bootstrap container' }
docker exec $bootstrap mkdir -p /tmp/cli-check
foreach ($name in @('release.txt','binary.bin','limit.bin','oversize.bin','owner.key','other.key')) {
    docker cp "cli-check/$name" "${bootstrap}:/tmp/cli-check/$name"
    if ($LASTEXITCODE -ne 0) { throw "Could not copy $name" }
}
docker exec $bootstrap chmod 600 /tmp/cli-check/owner.key /tmp/cli-check/other.key

# Use only in PowerShell, between interactive writer sessions.
function Update-Release([int]$version) {
    Save-Recipe $version
    docker cp cli-check/release.txt "${bootstrap}:/tmp/cli-check/release.txt"
    if ($LASTEXITCODE -ne 0) { throw 'Could not copy the updated recipe' }
    docker exec $bootstrap cat /tmp/cli-check/release.txt
}

# Enter the interactive writer on UDP port 9000.
docker exec -it -e CLI_PORT=9000 $bootstrap sh -c 'exec /usr/local/bin/kadlab-shell --publisher-key /tmp/cli-check/owner.key 2>>/tmp/cli-check/writer.log'
```

Private keys are temporary copies for demonstration clients. Servers use the public ownership configuration; private keys are excluded from images and Swarm configs.

## 2. Enter terminal 2's reader: PowerShell

```powershell
$bootstrap = docker ps --filter 'label=com.docker.swarm.service.name=kadlab_kademlia-bootstrap' --format '{{.ID}}'
if (@($bootstrap).Count -ne 1) { throw 'Expected one running bootstrap container' }
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
. ./cmd/tests/recipe-demo.ps1

# Separate reader node on port 9001; no signing key needed.
docker exec -it -e CLI_PORT=9001 $bootstrap sh -c 'exec /usr/local/bin/kadlab-shell 2>>/tmp/cli-check/reader.log'
```

Both clients join the servers on port 8000. Different client ports prevent conflicts, even inside the same container. These are two additional nodes joining the 50-server network.

## 3. Part 1: put in terminal 1, get in terminal 2

Terminal 1, at `simnet>`, one command per line:

```text
ping kademlia-bootstrap:8000
show rt
show nodes
put /tmp/cli-check/release.txt
show ds
```

Copy the full SHA-256 key printed by `put`. Terminal 2, at `simnet>`, replaces `RECIPE_KEY` below with that actual hash:

```text
get RECIPE_KEY
get RECIPE_KEY /tmp/cli-check/reader-download.txt
show rt
show ds
```

Expect the full version 1 blueberry pie recipe and its source node. `show rt` lists this client's known peers. In UDP mode, `show nodes` currently prints only the local client. `show ds` shows local storage, which may be empty: default `k=10` stores on the nodes closest to the key. The reader retrieves through UDP despite the clients sharing a container filesystem.

Terminal 1, at `simnet>`:

```text
put /tmp/cli-check/binary.bin
put /tmp/cli-check/limit.bin
put /tmp/cli-check/oversize.bin
```

Binary input and the 32,768-byte file must succeed; 32,769 bytes must report a size error. Copy the binary upload's hash into terminal 2:

```text
get BINARY_KEY /tmp/cli-check/reader-binary-copy.bin
exit
```

Terminal 2, back in PowerShell, verifies the bytes and re-enters the reader:

```powershell
docker cp "${bootstrap}:/tmp/cli-check/reader-download.txt" cli-check/docker-download.txt
docker cp "${bootstrap}:/tmp/cli-check/reader-binary-copy.bin" cli-check/docker-binary-copy.bin
Get-Content cli-check/docker-download.txt
(Get-FileHash cli-check/release.txt).Hash -eq (Get-FileHash cli-check/docker-download.txt).Hash
(Get-FileHash cli-check/binary.bin).Hash -eq (Get-FileHash cli-check/docker-binary-copy.bin).Hash
docker exec -it -e CLI_PORT=9001 $bootstrap sh -c 'exec /usr/local/bin/kadlab-shell 2>>/tmp/cli-check/reader.log'
```

Both comparisons must print `True` before updating the recipe. Missing-file errors mean verification could not complete.

Either terminal, at `simnet>`, run individually:

```text
ping bad-address
ping 127.0.0.1:7999
put missing-file.txt
get bad-key
get ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff
```

Expect malformed-address, unreachable-peer, missing-file, invalid-key, and not-found errors respectively. Do not type PowerShell variables, comments, or loops at the interactive prompt.

## 4. Part 2: publish ten versions and install from terminal 2

Terminal 1, at `simnet>`:

```text
show dns example.com
show dns missing.example
publish example.com:demo:1 /tmp/cli-check/release.txt
```

Expect the owner's public key, an unknown-owner error, and replica acceptance plus `published` for version 1. Terminal 2, at `simnet>`:

```text
show example.com:demo
install example.com:demo:1 --output /tmp/cli-check/reader-v1.txt
install example.com:demo:latest --output /tmp/cli-check/reader-latest.txt
```

For **each version 2 through 10**, keep terminal 2 open and follow these steps:

1. Terminal 1: type `exit` to return to PowerShell.
2. Run `Update-Release N`, replacing `N` with the next version number. This updates the same recipe file, prints its contents/hash, and copies it into Docker.
3. Re-enter terminal 1's writer using the PowerShell command below.
4. Run the appropriate publication from the table at `simnet>`.
5. Terminal 2: install that version and latest using the reader commands below.

```powershell
docker exec -it -e CLI_PORT=9000 $bootstrap sh -c 'exec /usr/local/bin/kadlab-shell --publisher-key /tmp/cli-check/owner.key 2>>/tmp/cli-check/writer.log'
```

Restarting the writer client preserves history on the running servers. `Update-Release` belongs in PowerShell, never at `simnet>`.

| N | Recipe change | Terminal 1 command at `simnet>` |
|---|---|---|
| 2 | Reduce sugar to 120 g. | `publish example.com:demo:2 /tmp/cli-check/release.txt` |
| 3 | Add 1 tablespoon lemon juice. | `publish --prev=2 example.com:demo:3 /tmp/cli-check/release.txt` |
| 4 | Add 1 teaspoon lemon zest. | `publish example.com:demo:4 /tmp/cli-check/release.txt` |
| 5 | Increase cornstarch to 3 tablespoons. | `publish example.com:demo:5 /tmp/cli-check/release.txt` |
| 6 | Chill dough for 30 minutes. | `publish example.com:demo:6 /tmp/cli-check/release.txt` |
| 7 | Increase oven temperature to 190 C. | `publish example.com:demo:7 /tmp/cli-check/release.txt` |
| 8 | Increase baking time to 45 minutes. | `publish example.com:demo:8 /tmp/cli-check/release.txt` |
| 9 | Add an egg glaze. | `publish example.com:demo:9 /tmp/cli-check/release.txt` |
| 10 | Cool for 2 hours. | `publish --prev=9 example.com:demo:10 /tmp/cli-check/release.txt` |

After each publication, terminal 2 runs these at `simnet>`. Replace `N` in the version and filename with the actual number:

```text
show example.com:demo
install example.com:demo:N --output /tmp/cli-check/reader-vN.txt
install example.com:demo:latest --output /tmp/cli-check/reader-latest.txt
```

After version 10, expect history `10 -> 9 -> ... -> 1 -> genesis`. Retrieve every historical version in terminal 2, at `simnet>`:

```text
install example.com:demo:1 --output /tmp/cli-check/reader-v1.txt
install example.com:demo:2 --output /tmp/cli-check/reader-v2.txt
install example.com:demo:3 --output /tmp/cli-check/reader-v3.txt
install example.com:demo:4 --output /tmp/cli-check/reader-v4.txt
install example.com:demo:5 --output /tmp/cli-check/reader-v5.txt
install example.com:demo:6 --output /tmp/cli-check/reader-v6.txt
install example.com:demo:7 --output /tmp/cli-check/reader-v7.txt
install example.com:demo:8 --output /tmp/cli-check/reader-v8.txt
install example.com:demo:9 --output /tmp/cli-check/reader-v9.txt
install example.com:demo:10 --output /tmp/cli-check/reader-v10.txt
install example.com:demo:1
exit
```

Terminal 2, back in PowerShell:

```powershell
foreach ($version in 1..10) {
    docker cp "${bootstrap}:/tmp/cli-check/reader-v$version.txt" "cli-check/docker-v$version.txt"
    if ($LASTEXITCODE -ne 0) { throw "Missing download for version $version" }
    $actual = [IO.File]::ReadAllText("$PWD/cli-check/docker-v$version.txt")
    $matches = $actual -ceq (Get-RecipeText $version)
    Write-Output $actual
    "version=$version matches=$matches"
    if (-not $matches) { throw "Incorrect recipe for version $version" }
}
docker exec $bootstrap cat /app/demo-1.pkg
docker exec -it -e CLI_PORT=9001 $bootstrap sh -c 'exec /usr/local/bin/kadlab-shell 2>>/tmp/cli-check/reader.log'
```

Expect `matches=True` for all ten versions and the original version 1 recipe in `demo-1.pkg`.

## 5. Invalid updates: writer attempts, reader checks

Terminal 1, at `simnet>`, run each separately:

| Command | Expected rejection |
|---|---|
| `publish example.com:demo:5 /tmp/cli-check/release.txt` | Rollback at sender. |
| `publish example.com:demo:10 /tmp/cli-check/release.txt` | Duplicate version. |
| `publish --prev=3 example.com:demo:11 /tmp/cli-check/release.txt` | Fork at sender. |
| `publish --force example.com:demo:5 /tmp/cli-check/release.txt` | Forced rollback at receivers. |
| `publish --force --prev=3 example.com:demo:11 /tmp/cli-check/release.txt` | Forced fork at receivers. |
| `publish --force --prev=0 example.com:demo:11 /tmp/cli-check/release.txt` | Competing genesis at receivers. |
| `publish --force --prev=ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff example.com:demo:11 /tmp/cli-check/release.txt` | Missing ancestry at receivers. |
| `publish --prev=3 example.com:demo:11 /tmp/cli-check/release.txt` | Sender rejects again; force does not persist. |
| `publish example.com:demo:0 /tmp/cli-check/release.txt` | Invalid version. |
| `publish --prev=99 example.com:demo:11 /tmp/cli-check/release.txt` | Unknown predecessor. |

Force skips sender preflight; receivers still validate. Failed attempts may leave immutable blobs/records but must not change accepted history. After **each attempt**, terminal 2, at `simnet>`:

```text
show example.com:demo
install example.com:demo:latest --output /tmp/cli-check/reader-latest.txt
```

History must still contain ten versions, with latest 10. Also test these in terminal 2:

```text
install example.com:demo:99
install example.com:missing:latest
exit
```

Expect not found for both missing entries. Terminal 2's PowerShell checks latest and re-enters:

```powershell
docker cp "${bootstrap}:/tmp/cli-check/reader-latest.txt" cli-check/docker-latest.txt
[IO.File]::ReadAllText("$PWD/cli-check/docker-latest.txt") -ceq (Get-RecipeText 10)
docker exec -it -e CLI_PORT=9001 $bootstrap sh -c 'exec /usr/local/bin/kadlab-shell 2>>/tmp/cli-check/reader.log'
```

Expect `True`.

## 6. Wrong owner on the same network

Terminal 1 types `exit`, then uses the other signer in PowerShell:

```powershell
docker exec -it -e CLI_PORT=9000 $bootstrap sh -c 'exec /usr/local/bin/kadlab-shell --publisher-key /tmp/cli-check/other.key 2>>/tmp/cli-check/writer.log'
```

Terminal 1, at `simnet>`:

```text
publish example.com:intruder:1 /tmp/cli-check/release.txt
publish --force example.com:intruder:1 /tmp/cli-check/release.txt
exit
```

Expect sender rejection without force and receiver signature/ownership rejection with force. Terminal 2, at `simnet>`:

```text
show example.com:intruder
show example.com:demo
install example.com:demo:latest --output /tmp/cli-check/reader-latest.txt
```

No accepted intruder package should exist; demo latest remains 10, even after writer exit. Keep the reader open.

## 7. Decoded local store and exit

Terminal 1's PowerShell starts the owner client with `k=52` to allow storage across 50 servers and both clients. This setting is for demonstrating local storage; earlier tests use default `k=10`. Keep the reader running and all servers healthy.

```powershell
docker exec -it -e CLI_PORT=9000 $bootstrap sh -c 'exec /usr/local/bin/kadlab-shell --k 52 --publisher-key /tmp/cli-check/owner.key 2>>/tmp/cli-check/writer.log'
```

Terminal 1, at `simnet>`:

```text
put /tmp/cli-check/release.txt
show ds
publish example.com:visible:1 /tmp/cli-check/release.txt
show ds
show rt
exit
```

Use a fresh name instead of `visible` on repeat runs. Expect the blob and decoded `version-record` and `latest-pointer` entries for visible version 1. `show ds` and `show rt` inspect this client only.

Terminal 2, still at `simnet>`, retrieves after writer exit:

```text
show example.com:visible
install example.com:visible:latest --output /tmp/cli-check/reader-visible.txt
install example.com:demo:latest --output /tmp/cli-check/reader-latest.txt
exit
```

## 8. Status, logs, and targeted checks: PowerShell only

```powershell
docker stack services kadlab
docker ps --filter 'label=com.docker.stack.namespace=kadlab' --format 'table {{.Names}}\t{{.Status}}'
docker service logs --tail 50 kadlab_kademlia-nodes
docker exec $bootstrap tail -n 20 /tmp/cli-check/writer.log
docker exec $bootstrap tail -n 20 /tmp/cli-check/reader.log

go test ./kademlia -run 'TestStoreLocalAndHandleStoreRPC|TestLookupDataRejectsCorruptedRemoteValue' -count=1 -v
go test ./cmd/simshell -run '^TestCLIInstallCatchesUpSeveralMissedPublications$' -count=1 -v
go test ./metadata ./registry -run 'TestHistoryCycle' -count=1 -v
```

These tests cover wrong-hash stores, corrupted values, missed-update catch-up, and cyclic history that normal CLI commands cannot directly create. Require PASS; this guide does not replace the full engineering suite.

## 9. Finish after both clients exit: terminal 1, PowerShell

```powershell
docker exec $bootstrap rm -f /tmp/cli-check/owner.key /tmp/cli-check/other.key
.\docker-lab.ps1 -Action Stop
```

Stop removes the stack and loses its in-memory values. Host keys and public ownership configuration remain available.
