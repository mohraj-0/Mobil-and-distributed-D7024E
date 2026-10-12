# Simple CLI demonstration: 12 versions of one folder

Run PowerShell blocks in PowerShell and CLI blocks inside `simnet>`, one line at a time.

## Prepare (PowerShell)

```powershell
cd C:\Users\natan\Desktop\Mobile\Mobil-and-distributed-D7024E\labs
go build -o kadlab.exe ./cmd/simshell
.\docker-lab.ps1 -Action Prepare
New-Item -ItemType Directory -Force demo-folder, demo-releases | Out-Null
1..12 | ForEach-Object {
    Set-Content -Encoding ASCII demo-folder\version.txt "This is version $_"
    Compress-Archive -Path demo-folder -DestinationPath "demo-releases\v$_.zip" -Force
}
.\kadlab.exe --transport simulated --nodes 3 --k 3 --owners-file owners.json --publisher-key publisher.key
```

Each ZIP contains a snapshot of the same folder. The CLI publishes files, so use
ZIPs for folders. Keep the CLI open throughout; exiting destroys simulated data.
Use a fresh package name if `example.com:demo` already exists.

## Part 1 (CLI)

```text
ping 127.0.0.1:8001
show rt
put demo-folder/version.txt
```

Copy the full 64-character hash printed by `put` and replace `HASH` below.
Do not type the literal word `HASH`.

```text
get HASH
get HASH cli-downloaded.txt
show ds
```

## Publish versions 1 through 11 (CLI)

No `--prev` is needed: publishing automatically extends current latest.

```text
show dns example.com
publish example.com:demo:1 demo-releases/v1.zip
publish example.com:demo:2 demo-releases/v2.zip
publish example.com:demo:3 demo-releases/v3.zip
publish example.com:demo:4 demo-releases/v4.zip
publish example.com:demo:5 demo-releases/v5.zip
publish example.com:demo:6 demo-releases/v6.zip
publish example.com:demo:7 demo-releases/v7.zip
publish example.com:demo:8 demo-releases/v8.zip
publish example.com:demo:9 demo-releases/v9.zip
publish example.com:demo:10 demo-releases/v10.zip
publish example.com:demo:11 demo-releases/v11.zip
show example.com:demo
install --output=latest-before.zip example.com:demo:latest
```

Expect history `11 -> 10 -> ... -> 1`; latest downloads version 11.

## Show latest changing to version 12 (CLI)

```text
publish --prev=11 example.com:demo:12 demo-releases/v12.zip
install --output=latest-after.zip example.com:demo:latest
install --output=version-1.zip example.com:demo:1
show example.com:demo
```

Expect history `12 -> 11 -> ... -> 1`. The same `:latest` name now downloads
version 12, while `:1` still downloads version 1. The latest pointer is mutable;
published versions and their contents are immutable. `--prev=11` demonstrates
an explicit predecessor; omitting it has the same effect here.

## Demonstrate --force (CLI)

```text
publish --prev=5 example.com:demo:13 demo-releases/v12.zip
publish --force --prev=5 example.com:demo:13 demo-releases/v12.zip
publish --force --prev=0 example.com:demo:13 demo-releases/v12.zip
publish --force example.com:demo:1 demo-releases/v1.zip
show example.com:demo
install example.com:demo:latest
```

- First: publisher rejects the fork before uploading.
- Second: force bypasses publisher checks; receivers reject the fork.
- Third: receivers reject a competing genesis (`--prev=0` means no predecessor).
- Fourth: receivers reject a rollback to version 1.
- Latest remains version 12, with the accepted history unchanged.

`--force` and `--prev` apply only to their individual command. Force lets you
attempt invalid updates; receivers still validate them.

## Inspect downloaded folders (PowerShell)

Run `exit` in the CLI, then:

```powershell
Expand-Archive latest-before.zip -DestinationPath check-before -Force
Expand-Archive latest-after.zip -DestinationPath check-after -Force
Expand-Archive version-1.zip -DestinationPath check-v1 -Force
Get-Content check-before/demo-folder/version.txt
Get-Content check-after/demo-folder/version.txt
Get-Content check-v1/demo-folder/version.txt
```

Expect version 11, version 12, and version 1 respectively.

## Optional: use 50 Docker nodes

Prepare the ZIPs above, then run in PowerShell:

```powershell
.\docker-lab.ps1 -Action Start
.\docker-lab.ps1 -Action Status
```

Wait for 1/1 bootstrap and 49/49 replicas, then:

```powershell
$bootstrap = @(docker ps --filter 'label=com.docker.swarm.service.name=kadlab_kademlia-bootstrap' --format '{{.ID}}')[0]
docker cp demo-releases "${bootstrap}:/app/demo-releases"
docker cp demo-folder "${bootstrap}:/app/demo-folder"
.\docker-lab.ps1 -Action Shell
```

Run the same CLI commands, using `ping kademlia-bootstrap:8000` for the ping.
Relative paths work from `/app`. Use a fresh package name if `demo` already exists.
After exiting the CLI, copy the downloads to Windows for extraction:

```powershell
docker cp "${bootstrap}:/app/latest-before.zip" latest-before.zip
docker cp "${bootstrap}:/app/latest-after.zip" latest-after.zip
docker cp "${bootstrap}:/app/version-1.zip" version-1.zip
.\docker-lab.ps1 -Action Stop
```

Exiting the Docker CLI leaves the servers running. Stopping or updating the stack
can lose their in-memory data.
