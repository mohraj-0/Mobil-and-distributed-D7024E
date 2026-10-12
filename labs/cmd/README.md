# Unified Part 1 and Part 2 CLI

`go run ./cmd/simshell` is the single CLI for the Kademlia DHT and package
registry. Run without a subcommand for the interactive prompt, or pass the same
commands directly to Cobra. Both modes use the same handlers and node. For
registry demonstrations, use the interactive prompt so the in-memory simulated
network survives between publications. One-shot commands can also join a running
UDP network using the existing bootstrap flags.

```text
ping IP:PORT
put FILENAME                 (alias: store)
get KEY [FILENAME]
show rt
show ds
show nodes
publish [--force] [--prev=VERSION] DOMAIN:PACKAGE:VERSION FILENAME
install DOMAIN:PACKAGE:VERSION [--output FILE]
show DOMAIN:PACKAGE
show dns DOMAIN
exit                         (alias: quit)
```

Versions are positive integers; `install` also accepts `latest`. History output
is verified and newest first, showing abbreviated record/blob hashes and
predecessors (`genesis` for the first record). `show ds` prints sorted local
snapshot entries as blobs, version records, or latest pointers. It is a diagnostic
decode, not proof of ownership; malformed tagged records are marked explicitly.
`show dns` prints the full owner public key from the node's configured verifier,
or reports a missing owner/lookup failure.

For real DNS, publish a TXT record `d7024e-ed25519=PUBLIC_KEY_HEX` on your domain.
Generate a signing key with `go run ./cmd/simshell keygen publisher.key`; it prints
only the public DNS TXT value and refuses to overwrite an existing key file.
Launch with `--publisher-key publisher.key` to sign publications. Private keys
stay local and are never sent through the DHT.

For a repeatable simulated demonstration, create a JSON owners file such as
`{"example.com":"PUBLIC_KEY_HEX"}` using that printed public key, then run:

```powershell
go run ./cmd/simshell --nodes 3 --owners-file owners.json --publisher-key publisher.key
```

All three nodes use the same fake DNS ownership file. At the prompt:

```text
show dns example.com
publish example.com:demo:1 go.mod
publish example.com:demo:2 go.mod
show example.com:demo
show ds
install example.com:demo:1 --output demo-first.pkg
install example.com:demo:latest
publish --force --prev=0 example.com:demo:3 go.mod
show example.com:demo
show rt
show nodes
exit
```

The forced competing genesis is rejected by receivers. Without `--force`, the
publisher checks ownership, increasing version, and extension of the latest head
before uploading. `--prev` selects a published predecessor version; `0` selects
genesis. With `--force`, a 64-character record hash may also be supplied to test
missing/invalid ancestry. Force bypasses sender preflight, while every receiving
node retains authenticated update validation. Replica acceptance/rejection is
printed individually. Publication flags apply only to the current command.
`install` verifies history and the selected blob before writing
`PACKAGE-VERSION.pkg` or the explicit `--output` destination.

Ownership and transport flags configure nodes at startup. Data remains in memory,
package size remains limited to 32,768 bytes, and installs retrieve package bytes
rather than running an installer or resolving dependencies. Interactive input is
split on whitespace, so use paths without spaces at the prompt.

## CLI Testing

The normal test suite includes independent-replica authenticity and catch-up
tests over `network.go`'s simulated network and loopback UDP, CLI receiver-state
regressions, real stdin/quit subprocess tests, remote latest/historical installs,
and tampered-blob rejection. The existing 1,000-node simulated test remains part
of the full suite. From `labs`:

```text
go test ./...
go test -race ./...
go test -covermode=atomic -coverpkg=./... -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

CI runs both full suites and enforces at least 80% aggregate module statement
coverage, uploading test logs and the coverage profile. Detailed setup, assertions,
coverage assessment and limitations are in the
[implementation report](../registry/imp-report.md).

The CLI can be tested in two different ways:

- A simulated network, where all nodes run inside one process.
- A real Docker network, where the nodes run as separate containers and communicate over UDP.

The required user interface commands are:

```text
ping IP:PORT
put FILENAME
get KEY [FILENAME]
exit
show rt
show ds
```

## Simulated CLI Test

Run this from the `labs` directory:

```powershell
cd C:\Users\natan\Desktop\Mobile\Mobil-and-distributed-D7024E\labs
go run ./cmd/simshell
```

This starts one simulated node and opens the interactive prompt:

```text
simnet>
```

Create or use an existing file, then test the commands:

```text
put go.mod
show ds
show rt
get KEY_PRINTED_BY_PUT
exit
```

For a multi-node simulated test, start 50 in-process nodes:

```powershell
go run ./cmd/simshell --nodes 50
```

Then use the prompt:

```text
show nodes
ping 127.0.0.1:8049
put go.mod
get KEY_PRINTED_BY_PUT
show rt
show ds
exit
```

There is also an automated simulated CLI demo:

```powershell
go run ./cmd/simshell test --nodes 50
```

This checks `ping`, `put`, `get`, `show rt`, and `show ds` in the simulated transport.

## Docker Image Test

Build the Docker image from the `labs` directory:

```powershell
docker build --no-cache -t kadlab:latest .
```

Check that the image contains the real CLI:

```powershell
docker run --rm kadlab:latest ./kadlab --help
```

The help output should list commands such as `serve`, `ping`, `put`, `get`, `show`, `exit`, and `test`.

You can also run the simulated CLI inside Docker:

```powershell
docker run --rm -it kadlab:latest ./kadlab
```

At the prompt:

```text
put /app/kadlab
show ds
get KEY_PRINTED_BY_PUT
exit
```

## Real 50-Node Docker Network Test

The Docker Compose file starts one bootstrap node and 49 additional nodes, for 50 total nodes.

Initialize Docker swarm if it is not already active:

```powershell
docker swarm init
```

Deploy the stack from the `labs` directory:

```powershell
cd C:\Users\natan\Desktop\Mobile\Mobil-and-distributed-D7024E\labs
docker stack deploy -c docker-compose.yml kadlab
```

Check that all nodes are running:

```powershell
docker service ls
```

Expected replicas:

```text
kadlab_kademlia-bootstrap   1/1
kadlab_kademlia-nodes       49/49
```

If an older service named `kadlab_kademliaNodes` is still running from a previous compose file, remove it:

```powershell
docker service rm kadlab_kademliaNodes
```

Open a shell inside the bootstrap container:

```powershell
docker ps --filter name=kadlab_kademlia-bootstrap --format "{{.ID}} {{.Names}}"
docker exec -it CONTAINER_ID sh
```

Inside the container, create a test file:

```sh
echo "hello kademlia" > /tmp/hello.txt
```

Start an interactive CLI node connected to the real Docker network:

```sh
./kadlab --transport udp \
  --listen-addr 0.0.0.0:9000 \
  --addr $(hostname -i):9000 \
  --bootstrap-addr kademlia-bootstrap:8000
```

At the prompt, test the required UI commands:

```text
ping kademlia-bootstrap:8000
put /tmp/hello.txt
get KEY_PRINTED_BY_PUT
get KEY_PRINTED_BY_PUT /tmp/out.txt
show rt
show ds
exit
```

After exiting the CLI, verify the downloaded file:

```sh
cat /tmp/out.txt
```

Expected output:

```text
hello kademlia
```

This is a real network test because the stack uses `--transport udp` and each node runs in a separate Docker container.

Note that `show ds` prints the data store of the currently running CLI node only. In a Kademlia network, `put` stores data on the nodes closest to the key, so `show ds` can be empty on one node even when `put` and `get` work correctly.

## Useful Docker Debugging Commands

Show swarm services:

```powershell
docker service ls
```

Show individual node tasks:

```powershell
docker service ps kadlab_kademlia-bootstrap
docker service ps kadlab_kademlia-nodes
```

Show service logs:

```powershell
docker service logs kadlab_kademlia-bootstrap --tail 50
docker service logs kadlab_kademlia-nodes --tail 50
```

Stop the stack:

```powershell
docker stack rm kadlab
```
