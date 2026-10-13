# Part 1 and Part 2: UDP without Docker

Run PowerShell in `labs`. Uses fake DNS ownership. Probe logs are saved separately; replication experiments are omitted.

`d` runs a fresh real UDP client for each command; the independent servers keep the data.

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

## Start the UDP network

Use three PowerShell terminals in `labs`. Keep the two servers running.

Terminal 1:

```powershell
# Start the UDP bootstrap; save probe logs.
.\cli-check\kadlab.exe --transport udp --addr 127.0.0.1:8000 --owners-file cli-check/owners.json serve 2>> cli-check/server1.log
```

Terminal 2:

```powershell
# Start another independent UDP server and join the bootstrap.
.\cli-check\kadlab.exe --transport udp --addr 127.0.0.1:8001 --bootstrap-addr 127.0.0.1:8000 --owners-file cli-check/owners.json serve 2>> cli-check/server2.log
```

Terminal 3:

```powershell
# Configure every client to join the real UDP network with the correct signer.
$nodeArgs = @('--transport','udp','--addr','127.0.0.1:9000','--bootstrap-addr','127.0.0.1:8000','--owners-file','cli-check/owners.json','--publisher-key','cli-check/owner.key')

# Define the short CLI command; save probe logs separately.
function d {
    & .\cli-check\kadlab.exe @nodeArgs @args 2>> cli-check/client.log
    if ($LASTEXITCODE -ne 0) { Get-Content cli-check/client.log -Tail 1 }
}

# Set the file paths and successful ping target.
$release = 'cli-check/release.txt'
$download = 'cli-check/download.txt'
$binary = 'cli-check/binary.bin'
$binaryCopy = 'cli-check/binary-copy.bin'
$pingAddress = '127.0.0.1:8000'

# Rewrite the SAME package file with the next version's contents.
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
. ./cmd/tests/recipe-demo.ps1

# Read a downloaded file for checking its contents.
function Read-DemoFile([string]$path) { [IO.File]::ReadAllText((Join-Path $PWD $path)) }
```

Continue with the Part 1 tests in terminal 3. Ports 8000, 8001 and 9000 must be available.

Each `Update-Release N` prints the full saved blueberry pie recipe, its change description and hash before publication. Run the helper setup again to replace the old function. In this UDP setup, updates appear in **terminal 3**; terminal 2 runs a server.

## Part 1

Run these in PowerShell after the setup above. Each download uses a fresh client, so it retrieves from the servers.

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

Run this in **PowerShell from `labs`**, after `get` reports that the file was saved. `Get-FileHash` reads each file and calculates a SHA-256 fingerprint; `.Hash` selects that fingerprint, and `-eq` compares the two. It does not modify either file. `True` verifies that the original and downloaded binary bytes match. `False` means different bytes only if both files exist and were read successfully; a missing-file error means the check did not complete. This tests that `put` and `get` preserve binary data without needing to display it as text. Run only the command below, once on its own line.

```powershell
# Expect True: every byte survived the upload/download.
(Get-FileHash cli-check/binary.bin).Hash -eq (Get-FileHash cli-check/binary-copy.bin).Hash
```

Check the documented file-size limit from PowerShell:

```powershell
# Make a supported 32,768-byte file and an unsupported 32,769-byte file.
[IO.File]::WriteAllBytes("$PWD/cli-check/limit.bin", [byte[]]::new(32768))
[IO.File]::WriteAllBytes("$PWD/cli-check/oversize.bin", [byte[]]::new(32769))
```

```powershell
# Exactly 32,768 bytes: must succeed.
d put cli-check/limit.bin

# One byte over the limit: must report a size error.
d put cli-check/oversize.bin
```

## Part 2: update one file through ten versions

Every valid publication should print replica acceptance and `published`.

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

Check `[IO.File]::ReadAllText("$PWD/demo-1.pkg") -ceq (Get-RecipeText 1)`: it must print `True`.

## Part 2: invalid updates and --force

Latest must remain **10**. Force skips sender preflight; receivers still validate. Failed attempts may leave immutable blobs/records, but must not change accepted history.

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

```powershell
# Configure the same network with the other signer.
$wrongArgs = @('--transport','udp','--addr','127.0.0.1:9000','--bootstrap-addr','127.0.0.1:8000','--owners-file','cli-check/owners.json','--publisher-key','cli-check/other.key')

# Without force: sender must reject the non-owner key.
& .\cli-check\kadlab.exe @wrongArgs publish example.com:intruder:1 $release 2>> cli-check/client.log
Get-Content cli-check/client.log -Tail 1

# With force: receivers must reject the non-owner signature.
& .\cli-check\kadlab.exe @wrongArgs publish --force example.com:intruder:1 $release 2>> cli-check/client.log
Get-Content cli-check/client.log -Tail 1

# No accepted intruder package should exist.
d show example.com:intruder
```

## Decoded store and exit

Open a persistent client with `k=3`: two servers plus this client. Run `put` and `show ds` in this same session so the local store visibly contains the file. This replication setting is for demonstrating storage; the earlier commands use the default k.


```powershell
# Open the interactive UDP client; save probe logs.
& .\cli-check\kadlab.exe @nodeArgs --k 3 2>> cli-check/client.log
```

At `simnet>`, run each command separately:

| Command | Expected result |
|---|---|
| `put cli-check/release.txt` | Stores the current file across the three reachable nodes, including this client. Prints the full key. |
| `show ds` | Must show that key shortened and the actual byte count for the current version 10 recipe file. |
| `publish example.com:visible:1 cli-check/release.txt` | Creates a separate demonstration package so version records and its latest pointer are stored locally too. Use a fresh name on repeat runs. |
| `show ds` | Must show the blob, a decoded `version-record example.com:visible version=1`, and a `latest-pointer example.com:visible version=1`. |
| `show example.com:demo` | Retrieves/verifies the ten-version history. |
| `show rt` | Displays populated routing buckets. |
| `exit` | Ends the client and returns to PowerShell. |

Back in PowerShell, `d install example.com:demo:latest --output $download` must still install version 10 from the servers after the interactive client exits.

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

Press Ctrl+C in both server terminals to stop the network.
