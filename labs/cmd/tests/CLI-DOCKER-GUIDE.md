# Part 1 and Part 2: UDP with 50 Docker servers

Run PowerShell in `labs`. Uses fake DNS ownership. Probe logs are saved separately; replication experiments are omitted.

`d` runs a fresh real UDP client for each command; the independent servers keep the data. Use two PowerShell terminals: terminal 1 writes and publishes; terminal 2 retrieves and verifies through the same 50-server network.

## What changes in the recipe

`cli-check/release.txt` is the actual text package. Each version keeps previous improvements and makes the following visible change:

| Version | Recipe change |
|---|---|
| 1 | Initial blueberry pie recipe. |
| 2 | Sugar decreases from 150 g to 120 g. |
| 3 | Adds 1 tablespoon lemon juice. |
| 4 | Adds 1 teaspoon lemon zest. |
| 5 | Cornstarch increases from 2 to 3 tablespoons. |
| 6 | Adds 30 minutes of dough chilling. |
| 7 | Oven temperature increases from 180 C to 190 C. |
| 8 | Baking time increases from 40 to 45 minutes. |
| 9 | Adds an egg glaze. |
| 10 | Specifies 2 hours of cooling before slicing. |

`Update-Release N` changes the actual file and prints the full saved recipe, including `Version: N` and `Change:`. Publishing uploads those saved bytes. Downloading an old version should show its old ingredients and instructions, even after the source file has been updated.

## Prepare once

```powershell
# Create the test folder if missing.
New-Item -ItemType Directory -Force cli-check

# Allow local scripts in this PowerShell session only.
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass

# Build the current CLI once.
go build -o cli-check/kadlab.exe ./cmd/simshell

# Create/reuse a publisher key and fake DNS configuration for example.com.
# This action does not start Docker.
.\docker-lab.ps1 -Action Prepare -PublisherKey cli-check/owner.key -OwnersFile cli-check/owners.json

# Load the recipe helper and create version 1 of the SAME recipe file.
. ./cmd/tests/recipe-demo.ps1
Update-Release 1

# Create binary input for the Part 1 test.
[IO.File]::WriteAllBytes("$PWD/cli-check/binary.bin", [byte[]](0,1,127,128,255))

# Create a signer that does not own example.com. Skip if already created.
.\cli-check\kadlab.exe keygen cli-check/other.key
```

Use a fresh package name instead of `demo` if repeating against a network with an existing history.

## Start the Docker network

Use Docker Desktop with its Linux engine. Start rebuilds/redeploys the `kadlab` stack and may restart existing nodes, losing their in-memory values.

```powershell
# Build and start one bootstrap plus 49 UDP servers.
.\docker-lab.ps1 -Action Start -PublisherKey cli-check/owner.key -OwnersFile cli-check/owners.json

# Wait for bootstrap 1/1 and other servers 49/49.
.\docker-lab.ps1 -Action Status

# Find the local bootstrap container.
$bootstrap = docker ps --filter 'label=com.docker.swarm.service.name=kadlab_kademlia-bootstrap' --format '{{.ID}}'

# Prepare the container files.
docker exec $bootstrap mkdir -p /tmp/cli-check
docker cp cli-check/release.txt "${bootstrap}:/tmp/cli-check/release.txt"
docker cp cli-check/binary.bin "${bootstrap}:/tmp/cli-check/binary.bin"

# Supply the signing key to the demonstration client only.
docker cp cli-check/owner.key "${bootstrap}:/tmp/cli-check/owner.key"
docker exec $bootstrap chmod 600 /tmp/cli-check/owner.key

# Each call launches a fresh UDP client on port 9000.
# Save probe messages in the container, keeping command output visible.
function d {
    docker exec -e CLI_PORT=9000 $bootstrap sh -c 'exec /usr/local/bin/kadlab-shell "$@" 2>>/tmp/cli-check/client.log' demo --publisher-key /tmp/cli-check/owner.key @args
    if ($LASTEXITCODE -ne 0) { docker exec $bootstrap tail -n 1 /tmp/cli-check/client.log }
}

# Set container paths and the successful ping target.
$release = '/tmp/cli-check/release.txt'
$download = '/tmp/cli-check/download.txt'
$binary = '/tmp/cli-check/binary.bin'
$binaryCopy = '/tmp/cli-check/binary-copy.bin'
$pingAddress = 'kademlia-bootstrap:8000'

# Rewrite the SAME file and replace its container copy.
. ./cmd/tests/recipe-demo.ps1
function Update-Release([int]$version) {
    Save-Recipe $version
    docker cp cli-check/release.txt "${bootstrap}:/tmp/cli-check/release.txt"
    if ($LASTEXITCODE -ne 0) { throw 'Could not copy the updated recipe into Docker' }
    Write-Host 'Saved container recipe:'
    docker exec $bootstrap cat /tmp/cli-check/release.txt
    if ($LASTEXITCODE -ne 0) { throw 'Could not read the updated container recipe' }
}

# Read a downloaded file from the container.
function Read-DemoFile([string]$path) { (docker exec $bootstrap cat $path) -join "`n" }
```

Each `Update-Release N` prints the full updated host recipe and its hash, copies it into Docker, then prints the container recipe too. Run the helper setup again to replace the old function.

## Terminal 2: independent reader

Run the preparation and network startup above in terminal 1 only. In a second PowerShell terminal in `labs`, run:

```powershell
$bootstrap = docker ps --filter 'label=com.docker.swarm.service.name=kadlab_kademlia-bootstrap' --format '{{.ID}}'
if (@($bootstrap).Count -ne 1) { throw 'Expected one running bootstrap container' }
function d {
    docker exec -e CLI_PORT=9001 $bootstrap sh -c 'exec /usr/local/bin/kadlab-shell "$@" 2>>/tmp/cli-check/reader.log' demo @args
    if ($LASTEXITCODE -ne 0) { docker exec $bootstrap tail -n 1 /tmp/cli-check/reader.log }
}
$release = '/tmp/cli-check/release.txt'
$download = '/tmp/cli-check/reader-download.txt'
$binary = '/tmp/cli-check/binary.bin'
$binaryCopy = '/tmp/cli-check/reader-binary-copy.bin'
$pingAddress = 'kademlia-bootstrap:8000'
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
. ./cmd/tests/recipe-demo.ps1
function Read-DemoFile([string]$path) { (docker exec $bootstrap cat $path) -join "`n" }
```

The clients use different UDP ports (9000 and 9001) and independent node identities. Both join the servers on port 8000. Terminal 2 needs no signing key. Keep the stack running; do not redeploy between writing and reading. These commands run in PowerShell, not at `simnet>`.

## Which terminal runs each test

Run the following sections in order, switching terminals at the indicated commands. Variables and functions belong to each PowerShell session; copy the full hash printed by terminal 1 into terminal 2 as `$key = 'HASH'` (and `$binaryKey = 'HASH'` for binary data).

| Operation in the remaining sections | Terminal |
|---|---|
| `Update-Release`, `put`, `publish`, and wrong-owner publication attempts | 1: writer |
| `get`, `install`, `show example.com:demo`, `show example.com:intruder`, and download checks | 2: reader |
| `ping`, `show rt`, `show dns`, malformed input, and missing-value checks | Either terminal |
| Interactive `show ds` demonstration | 1; it inspects that client's local store |
| Targeted Go tests and final cleanup | 1, after both clients finish |

For example, terminal 1 runs `d put $release`. Copy its printed hash into terminal 2, then run `d get $key $download` and `(Read-DemoFile $download) -ceq (Get-RecipeText 1)` there; expect `True`. Repeat with binary data. For the existing binary verification block, use the reader's container output:

```powershell
# Terminal 2: compare the uploaded and retrieved binary bytes. Expect equal hashes.
docker exec $bootstrap sha256sum $binary $binaryCopy
```

For Part 2, terminal 1 runs each `Update-Release N` and publication. After each publication succeeds, terminal 2 runs:

```powershell
# Replace 1 with the version just published; repeat for all versions 1 through 10.
$version = 1
d show example.com:demo
d install "example.com:demo:$version" --output $download
Read-DemoFile $download
(Read-DemoFile $download) -ceq (Get-RecipeText $version)
d install example.com:demo:latest --output $download
(Read-DemoFile $download) -ceq (Get-RecipeText $version)
```

After all ten publications, run the historical install loop below in terminal 2. After each rejected rollback, fork, forced update, or wrong-owner attempt in terminal 1, check history and latest again in terminal 2: latest must remain 10 and the intruder package must remain absent. Terminal 2 can still retrieve after terminal 1's interactive client exits, because the servers retain the data. `show ds` only shows the current client's store; an empty reader store does not mean the network lost the value.

## Part 1

Run these in PowerShell after setting up both terminals, following the terminal assignment table above. Each download uses a fresh reader client, so it retrieves from the servers. Calculate hashes in terminal 1 and copy them into terminal 2 before `get`.

```powershell
# Ping a real UDP server. Expect pong and round-trip time.
d ping $pingAddress

# Check joining populated the routing table: peer addresses and shortened IDs.
d show rt

# Upload the file. Expect its full SHA-256 key.
d put $release

# Compute the expected key from the exact uploaded bytes.
$key = (Get-FileHash cli-check/release.txt -Algorithm SHA256).Hash.ToLowerInvariant()

# Print the value. Expect the full version 1 blueberry pie recipe and the source server.
d get $key

# Save the retrieved value. Expect destination filename and source server.
d get $key $download

# Check exact contents. Expect True.
(Read-DemoFile $download) -ceq (Get-RecipeText 1)

# Demonstrate show ds in the persistent session below; a fresh client starts empty.

# Upload arbitrary binary bytes and calculate their expected key.
d put $binary
$binaryKey = (Get-FileHash cli-check/binary.bin -Algorithm SHA256).Hash.ToLowerInvariant()

# Download the binary bytes to a file.
d get $binaryKey $binaryCopy

# Bad address: expect IP:PORT validation error.
d ping bad-address

# Unreachable peer: expect failure, not pong.
d ping 127.0.0.1:7999

# Missing file: expect file-not-found error.
d put missing-file.txt

# Malformed key: expect SHA-256 key validation error.
d get bad-key

# Well-formed but absent key: expect not found.
d get ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff
```

Verify the binary download:

Run these in **PowerShell from `labs`**, after `get` reports that the file was saved. First copy the downloaded container file to the host. Then `Get-FileHash` reads each host file and calculates a SHA-256 fingerprint; `.Hash` selects that fingerprint, and `-eq` compares the two. The comparison does not modify either file. `True` verifies that all binary bytes match. `False` means different bytes only if both files exist and were read successfully; a missing-file error means the check did not complete. This tests that `put` and `get` preserve binary data without displaying it as text. Run each command once on its own line.

```powershell
# Copy the downloaded binary back to the host.
docker cp "${bootstrap}:$binaryCopy" cli-check/reader-binary-copy.bin

# Expect True: every byte survived the upload/download.
(Get-FileHash cli-check/binary.bin).Hash -eq (Get-FileHash cli-check/reader-binary-copy.bin).Hash
```

Check the documented file-size limit from PowerShell:

```powershell
# Make a supported 32,768-byte file and an unsupported 32,769-byte file.
[IO.File]::WriteAllBytes("$PWD/cli-check/limit.bin", [byte[]]::new(32768))
[IO.File]::WriteAllBytes("$PWD/cli-check/oversize.bin", [byte[]]::new(32769))
```

```powershell
# Copy both boundary inputs into the container.
docker cp cli-check/limit.bin "${bootstrap}:/tmp/cli-check/limit.bin"
docker cp cli-check/oversize.bin "${bootstrap}:/tmp/cli-check/oversize.bin"

# Exactly 32,768 bytes: must succeed.
d put /tmp/cli-check/limit.bin

# One byte over the limit: must report a size error.
d put /tmp/cli-check/oversize.bin
```

## Part 2: update one file through ten versions

Every valid publication should print replica acceptance and `published`.

Run updates and publications in terminal 1. After each succeeds, run the per-version reader checks above in terminal 2. From `d show example.com:demo` onward in this block, run commands in terminal 2, including the historical install loop.

```powershell
# Show the owner's public key from the configured DNS verifier.
d show dns example.com

# Unknown owner: expect lookup failure.
d show dns missing.example

# Version 1 starts the chain with an automatic genesis predecessor.
Update-Release 1
d publish example.com:demo:1 $release

# Version 2 automatically extends version 1.
Update-Release 2
d publish example.com:demo:2 $release

# Version 3 explicitly names version 2 as predecessor.
Update-Release 3
d publish --prev=2 example.com:demo:3 $release

# Version 4 extends version 3.
Update-Release 4
d publish example.com:demo:4 $release

# Version 5 extends version 4.
Update-Release 5
d publish example.com:demo:5 $release

# Version 6 extends version 5.
Update-Release 6
d publish example.com:demo:6 $release

# Version 7 extends version 6.
Update-Release 7
d publish example.com:demo:7 $release

# Version 8 extends version 7.
Update-Release 8
d publish example.com:demo:8 $release

# Version 9 extends version 8.
Update-Release 9
d publish example.com:demo:9 $release

# Version 10 explicitly extends version 9.
Update-Release 10
d publish --prev=9 example.com:demo:10 $release

# Expect ten verified records: 10 -> 9 -> ... -> 1 -> genesis.
d show example.com:demo

# Install latest. Expect installed version 10.
d install example.com:demo:latest --output $download

# Print the downloaded version 10 recipe so its changes are visible.
Read-DemoFile $download

# Latest must contain version 10's bytes. Expect True.
(Read-DemoFile $download) -ceq (Get-RecipeText 10)

# Test EVERY historical version, not just latest.
# Expect version=N matches=True for each N from 1 through 10.
foreach ($version in 1..10) {
    d install "example.com:demo:$version" --output $download
    # Display this historical recipe before checking it.
    Read-DemoFile $download
    $matches = (Read-DemoFile $download) -ceq (Get-RecipeText $version)
    "version=$version matches=$matches"
    if (-not $matches) { throw "Incorrect bytes for version $version" }
}

# Test install's default filename. Expect demo-1.pkg containing the version 1 recipe.
d install example.com:demo:1
```

Check `docker exec $bootstrap cat /app/demo-1.pkg`: it must print the full version 1 blueberry pie recipe.

## Part 2: invalid updates and --force

Latest must remain **10**. Force skips sender preflight; receivers still validate. Failed attempts may leave immutable blobs/records, but must not change accepted history.

Run publication attempts in terminal 1. Run installs, history checks, and content comparisons in terminal 2 to verify the network still serves version 10.

```powershell
# Rollback without force: sender must reject before upload.
d publish example.com:demo:5 $release

# Duplicate version without force: sender must reject.
d publish example.com:demo:10 $release

# Fork without force: sender must reject the old predecessor.
d publish --prev=3 example.com:demo:11 $release

# Forced rollback: receiving replicas must reject.
d publish --force example.com:demo:5 $release

# Forced fork from version 3: receiving replicas must reject.
d publish --force --prev=3 example.com:demo:11 $release

# Forced competing genesis: receiving replicas must reject.
d publish --force --prev=0 example.com:demo:11 $release

# Forced missing ancestry: receiving replicas must reject.
d publish --force --prev=ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff example.com:demo:11 $release

# Force must not carry into later commands: expect sender rejection again.
d publish --prev=3 example.com:demo:11 $release

# Invalid version: expect positive-integer error.
d publish example.com:demo:0 $release

# Unknown predecessor: expect predecessor-not-found error.
d publish --prev=99 example.com:demo:11 $release

# Missing historical version: expect not found.
d install example.com:demo:99

# Missing package: expect package/pointer not found.
d install example.com:missing:latest

# Check accepted history is still exactly ten versions.
d show example.com:demo

# Check latest remains version 10 with unchanged bytes. Expect True.
d install example.com:demo:latest --output $download
(Read-DemoFile $download) -ceq (Get-RecipeText 10)
```

### Wrong owner

Run both wrong-owner publication attempts in terminal 1; run the final intruder lookup in terminal 2.

```powershell
# Supply the other signer for the attack test.
docker cp cli-check/other.key "${bootstrap}:/tmp/cli-check/other.key"

# Without force: sender must reject ownership.
docker exec $bootstrap sh -c 'exec /usr/local/bin/kadlab-shell --publisher-key /tmp/cli-check/other.key "$@" 2>>/tmp/cli-check/client.log' demo publish example.com:intruder:1 $release
docker exec $bootstrap tail -n 1 /tmp/cli-check/client.log

# With force: receivers must reject ownership/signature.
docker exec $bootstrap sh -c 'exec /usr/local/bin/kadlab-shell --publisher-key /tmp/cli-check/other.key "$@" 2>>/tmp/cli-check/client.log' demo publish --force example.com:intruder:1 $release
docker exec $bootstrap tail -n 1 /tmp/cli-check/client.log

# No accepted intruder package should exist.
d show example.com:intruder
```

## Decoded store and exit

Open a persistent client with `k=51`: 50 servers plus this client. Run `put` and `show ds` in this same session so the local store visibly contains the file. Ensure all 50 servers are healthy. The earlier commands use the default k.


```powershell
# Open the interactive UDP client with terminal input attached.
docker exec -it $bootstrap sh -c 'exec /usr/local/bin/kadlab-shell --k 51 --publisher-key /tmp/cli-check/owner.key 2>>/tmp/cli-check/client.log'
```

At `simnet>`, run each command separately:

| Command | Expected result |
|---|---|
| `put /tmp/cli-check/release.txt` | Attempts storage across all 51 reachable nodes, including this client. Prints the full key. |
| `show ds` | Must show that key shortened and the actual byte count for the current version 10 recipe file. |
| `publish example.com:visible:1 /tmp/cli-check/release.txt` | Creates a separate demonstration package with locally stored metadata. Use a fresh name on repeat runs. |
| `show ds` | Must show the blob, a decoded `version-record example.com:visible version=1`, and a `latest-pointer example.com:visible version=1`. |
| `show example.com:demo` | Retrieves/verifies the ten-version history. |
| `show rt` | Displays populated routing buckets. |
| `exit` | Ends the client and returns to PowerShell. |

In terminal 2's PowerShell, `d install example.com:demo:latest --output $download` must still install version 10 from the servers after terminal 1's interactive client exits.

## Conditions the CLI cannot directly create

These targeted tests cover malicious data and missed updates; no replication tests are included.

```powershell
# Reject wrong-hash stores and corrupted downloaded values.
go test ./kademlia -run 'TestStoreLocalAndHandleStoreRPC|TestLookupDataRejectsCorruptedRemoteValue' -count=1 -v

# Catch up after missing several publications.
go test ./cmd/simshell -run '^TestCLIInstallCatchesUpSeveralMissedPublications$' -count=1 -v

# Reject cyclic history; genuine self-hashing cycles cannot practically be made by publish.
go test ./metadata ./registry -run 'TestHistoryCycle' -count=1 -v
```

Require PASS. This demonstration guide does not replace the complete engineering suite or report.

## Finish

```powershell
# Remove the temporary signing-key copies introduced by this guide.
docker exec $bootstrap rm -f /tmp/cli-check/owner.key /tmp/cli-check/other.key

# Stop/remove the demonstration stack; its in-memory values are lost.
.\docker-lab.ps1 -Action Stop
```
