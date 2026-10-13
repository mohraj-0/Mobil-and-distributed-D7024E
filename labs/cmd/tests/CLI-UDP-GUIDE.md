# Part 1 and Part 2: UDP without Docker

Run PowerShell in `labs`. Uses fake DNS ownership. Probe logs are saved separately; replication experiments are omitted.

`d` runs a fresh real UDP client for each command; the independent servers keep the data.

## Prepare once

```powershell
# Create the test folder if missing.
New-Item -ItemType Directory -Force cli-check

# Build the current CLI once.
go build -o cli-check/kadlab.exe ./cmd/simshell

# Create/reuse a publisher key and fake DNS configuration for example.com.
# This action does not start Docker.
.\docker-lab.ps1 -Action Prepare -PublisherKey cli-check/owner.key -OwnersFile cli-check/owners.json

# Create one package file; all ten versions will update this SAME file.
[IO.File]::WriteAllText("$PWD/cli-check/release.txt", 'release 1')

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
function Update-Release([int]$version) { [IO.File]::WriteAllText("$PWD/cli-check/release.txt", "release $version") }

# Read a downloaded file for checking its contents.
function Read-DemoFile([string]$path) { [IO.File]::ReadAllText((Join-Path $PWD $path)) }
```

Continue with the Part 1 tests in terminal 3. Ports 8000, 8001 and 9000 must be available.

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

# Print the value. Expect release 1 and the source server.
d get $key

# Save the retrieved value. Expect destination filename and source server.
d get $key $download

# Check exact contents. Expect True.
(Read-DemoFile $download) -ceq 'release 1'

# Print the fresh client's local store; it may be empty.
d show ds

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

# Latest must contain version 10's bytes. Expect True.
(Read-DemoFile $download) -ceq 'release 10'

# Test EVERY historical version, not just latest.
# Expect version=N matches=True for each N from 1 through 10.
foreach ($version in 1..10) {
    d install "example.com:demo:$version" --output $download
    $matches = (Read-DemoFile $download) -ceq "release $version"
    "version=$version matches=$matches"
    if (-not $matches) { throw "Incorrect bytes for version $version" }
}

# Test install's default filename. Expect demo-1.pkg containing release 1.
d install example.com:demo:1
```

Check `[IO.File]::ReadAllText("$PWD/demo-1.pkg") -ceq 'release 1'`: it must print `True`.

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
(Read-DemoFile $download) -ceq 'release 10'
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

Open a persistent client so `show ds` can display metadata fetched by the preceding command.


```powershell
# Open the interactive UDP client; save probe logs.
& .\cli-check\kadlab.exe @nodeArgs 2>> cli-check/client.log
```

At `simnet>`, run each command separately:

| Command | Expected result |
|---|---|
| `show example.com:demo` | Retrieves/verifies the ten-version history. |
| `show ds` | Decodes locally cached version records/latest pointers with package/version fields and short hashes. |
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
