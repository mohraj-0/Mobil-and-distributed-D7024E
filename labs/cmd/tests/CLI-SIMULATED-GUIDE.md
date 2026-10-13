# Part 1 and Part 2: simulated network

Run PowerShell in `labs`. This uses 50 in-process simulated nodes with fake DNS ownership. Probe logs are saved separately; no replication experiments are included.

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

## Start the simulated network

Use two PowerShell terminals in `labs`. The simulated network exists only while terminal 1's CLI stays open. Keep this interactive session open: one-shot commands would create a new empty network each time.

Terminal 1:

```powershell
# Start 50 simulated nodes in one process; save probe logs to a file.
.\cli-check\kadlab.exe --transport simulated --nodes 50 --owners-file cli-check/owners.json --publisher-key cli-check/owner.key 2>> cli-check/simulated.log
```

Terminal 2:

```powershell
# Update the SAME package file while the simulated CLI stays open.
function Update-Release([int]$version) { [IO.File]::WriteAllText("$PWD/cli-check/release.txt", "release $version") }

# Set its initial contents before running the Part 1 commands.
Update-Release 1

# Print the expected SHA-256 key for the text file.
Get-FileHash cli-check/release.txt -Algorithm SHA256
```

### Simulated Part 1

Run these commands individually at terminal 1's `simnet>` prompt. Replace `KEY` with the hash printed by the preceding `put`. The prompt does not expand PowerShell variables and does not accept comments.

| CLI command | Explanation / expected result |
|---|---|
| `show nodes` | Lists all 50 simulated nodes. |
| `ping 127.0.0.1:8049` | Pings another simulated node; prints pong and round-trip time. |
| `show rt` | Shows populated routing buckets and shortened node IDs. |
| `put cli-check/release.txt` | Uploads `release 1`; printed key must match the PowerShell SHA-256 hash. |
| `get KEY` | Prints `release 1` and the source node. |
| `get KEY cli-check/sim-download.txt` | Saves the downloaded bytes. |
| `show ds` | Shows the current node's local stored values; it can be empty if other nodes hold the replicas. |
| `put cli-check/binary.bin` | Stores the binary input; copy its new key. |
| `get KEY cli-check/sim-binary.bin` | Downloads that binary input using its key. |
| `ping bad-address` | Must reject invalid address syntax. |
| `ping 127.0.0.1:7999` | Must fail because this peer does not exist. |
| `put missing-file.txt` | Must report a missing file. |
| `get bad-key` | Must reject the malformed hash. |
| `get ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff` | Must report not found for the absent key. |

In terminal 2, check the downloaded files:

```powershell
# Expect True: text bytes were preserved.
(Get-FileHash cli-check/release.txt).Hash -eq (Get-FileHash cli-check/sim-download.txt).Hash

# Expect True: binary bytes were preserved.
(Get-FileHash cli-check/binary.bin).Hash -eq (Get-FileHash cli-check/sim-binary.bin).Hash

# Prepare the supported and oversized file-size boundary inputs.
[IO.File]::WriteAllBytes("$PWD/cli-check/limit.bin", [byte[]]::new(32768))
[IO.File]::WriteAllBytes("$PWD/cli-check/oversize.bin", [byte[]]::new(32769))
```

At terminal 1's CLI, `put cli-check/limit.bin` must succeed; `put cli-check/oversize.bin` must report a size error.

### Simulated Part 2: ten updates of one file

For **each row**, run the terminal 2 command first, then the terminal 1 CLI command. Wait for publication to finish before moving to the next row. This ensures each published version has different bytes.

| Terminal 2: PowerShell | Terminal 1: CLI | What is tested |
|---|---|---|
| `Update-Release 1` | `publish example.com:demo:1 cli-check/release.txt` | First version; automatic genesis predecessor. |
| `Update-Release 2` | `publish example.com:demo:2 cli-check/release.txt` | Automatic predecessor version 1. |
| `Update-Release 3` | `publish --prev=2 example.com:demo:3 cli-check/release.txt` | Explicit valid predecessor version 2. |
| `Update-Release 4` | `publish example.com:demo:4 cli-check/release.txt` | Extends version 3. |
| `Update-Release 5` | `publish example.com:demo:5 cli-check/release.txt` | Extends version 4. |
| `Update-Release 6` | `publish example.com:demo:6 cli-check/release.txt` | Extends version 5. |
| `Update-Release 7` | `publish example.com:demo:7 cli-check/release.txt` | Extends version 6. |
| `Update-Release 8` | `publish example.com:demo:8 cli-check/release.txt` | Extends version 7. |
| `Update-Release 9` | `publish example.com:demo:9 cli-check/release.txt` | Extends version 8. |
| `Update-Release 10` | `publish --prev=9 example.com:demo:10 cli-check/release.txt` | Explicitly extends version 9. |

Each publication must show replica acceptance and `published`. Then run these commands at the CLI:

| CLI command | Explanation / expected result |
|---|---|
| `show dns example.com` | Shows the trusted owner's public key. |
| `show dns missing.example` | Reports unknown ownership. |
| `show example.com:demo` | Shows exactly ten verified records: `10 -> ... -> 1 -> genesis`. |
| `show ds` | Decodes locally stored version records/latest pointers where present. |
| `install example.com:demo:latest --output cli-check/sim-latest.txt` | Downloads version 10. |
| `install example.com:demo:1` | Tests the default filename `demo-1.pkg`, containing `release 1`. |

Paste this block at the CLI to download **all ten historical versions**:

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
```

In terminal 2:

```powershell
# Latest must contain release 10. Expect True.
[IO.File]::ReadAllText("$PWD/cli-check/sim-latest.txt") -ceq 'release 10'

# Default install must contain release 1. Expect True.
[IO.File]::ReadAllText("$PWD/demo-1.pkg") -ceq 'release 1'

# Check every historical download against its original contents.
# Expect version=N matches=True for all ten versions.
foreach ($version in 1..10) {
    $matches = [IO.File]::ReadAllText("$PWD/cli-check/sim-$version.txt") -ceq "release $version"
    "version=$version matches=$matches"
    if (-not $matches) { throw "Incorrect bytes for version $version" }
}
```

### Simulated rejection tests

Run at terminal 1's CLI. Latest must stay **10**.

| CLI command | Explanation / expected result |
|---|---|
| `publish example.com:demo:5 cli-check/release.txt` | Sender rejects rollback. |
| `publish example.com:demo:10 cli-check/release.txt` | Sender rejects duplicate version. |
| `publish --prev=3 example.com:demo:11 cli-check/release.txt` | Sender rejects a fork from an old version. |
| `publish --force example.com:demo:5 cli-check/release.txt` | Receivers reject forced rollback. |
| `publish --force --prev=3 example.com:demo:11 cli-check/release.txt` | Receivers reject forced fork. |
| `publish --force --prev=0 example.com:demo:11 cli-check/release.txt` | Receivers reject competing genesis. |
| `publish --force --prev=ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff example.com:demo:11 cli-check/release.txt` | Receivers reject missing ancestry. |
| `publish --prev=3 example.com:demo:11 cli-check/release.txt` | Sender rejects again: force must not persist. |
| `publish example.com:demo:0 cli-check/release.txt` | Rejects zero version. |
| `publish --prev=99 example.com:demo:11 cli-check/release.txt` | Rejects unknown predecessor. |
| `install example.com:demo:99` | Rejects missing version. |
| `install example.com:missing:latest` | Reports missing package. |
| `show example.com:demo` | Still shows ten accepted versions with latest 10. |
| `install example.com:demo:latest --output cli-check/sim-latest.txt` | Latest still downloads version 10. |
| `exit` | Ends the client and all 50 simulated nodes. |

Repeat the latest-content PowerShell check above after the rejected updates; it must still print `True`.

### Simulated wrong-owner test

After exiting the original session, start a fresh simulated network with the **other** signer but the unchanged trusted owner:

```powershell
# Receivers still trust owner.key's public key; the publisher uses other.key.
.\cli-check\kadlab.exe --transport simulated --nodes 3 --owners-file cli-check/owners.json --publisher-key cli-check/other.key 2>> cli-check/simulated.log
```

| CLI command | Explanation / expected result |
|---|---|
| `publish example.com:intruder:1 cli-check/release.txt` | Sender rejects the non-owner key. |
| `publish --force example.com:intruder:1 cli-check/release.txt` | Receivers reject the non-owner signature. |
| `show example.com:intruder` | No accepted intruder package exists. |
| `exit` | Ends the test network. |

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

-------------------------------------------------------
For a clear simulated demonstration:

# Start 3 simulated nodes and store each file on all 3.
.\cli-check\kadlab.exe --transport simulated --nodes 3 --k 3 2>> cli-check/simulated.log

At simnet>:

put cli-check/release.txt
show ds

show ds should show the uploaded file’s shortened hash and byte count. It displays only the current node’s store, so this proves that node holds the file.

For your 50-node simulated setup, use --nodes 50 --k 50. For Docker’s 50 servers plus the demonstration client, launch the client with --k 51, then run put and show ds in the same interactive session.

There is no put --force option. Increasing k requests more copies, but does not guarantee every node accepted them if some are unreachable. A successful put alone proves at least one copy was stored.