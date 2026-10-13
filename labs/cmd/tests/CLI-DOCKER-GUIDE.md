# Part 1 and Part 2: UDP with 50 Docker servers

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
    docker exec $bootstrap sh -c 'exec /usr/local/bin/kadlab-shell "$@" 2>>/tmp/cli-check/client.log' demo --publisher-key /tmp/cli-check/owner.key @args
    if ($LASTEXITCODE -ne 0) { docker exec $bootstrap tail -n 1 /tmp/cli-check/client.log }
}

# Set container paths and the successful ping target.
$release = '/tmp/cli-check/release.txt'
$download = '/tmp/cli-check/download.txt'
$binary = '/tmp/cli-check/binary.bin'
$binaryCopy = '/tmp/cli-check/binary-copy.bin'
$pingAddress = 'kademlia-bootstrap:8000'

# Rewrite the SAME file and replace its container copy.
function Update-Release([int]$version) {
    [IO.File]::WriteAllText("$PWD/cli-check/release.txt", "release $version")
    docker cp cli-check/release.txt "${bootstrap}:/tmp/cli-check/release.txt"
}

# Read a downloaded file from the container.
function Read-DemoFile([string]$path) { docker exec $bootstrap cat $path }
```

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
# Copy the downloaded binary back to the host.
docker cp "${bootstrap}:/tmp/cli-check/binary-copy.bin" cli-check/binary-copy.bin

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

Check `docker exec $bootstrap cat /app/demo-1.pkg`: it must print `release 1`.

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

Open a persistent client so `show ds` can display metadata fetched by the preceding command.


```powershell
# Open the interactive UDP client with terminal input attached.
docker exec -it $bootstrap sh -c 'exec /usr/local/bin/kadlab-shell --publisher-key /tmp/cli-check/owner.key 2>>/tmp/cli-check/client.log'
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

```powershell
# Remove the temporary signing-key copies introduced by this guide.
docker exec $bootstrap rm -f /tmp/cli-check/owner.key /tmp/cli-check/other.key

# Stop/remove the demonstration stack; its in-memory values are lost.
.\docker-lab.ps1 -Action Stop
```
