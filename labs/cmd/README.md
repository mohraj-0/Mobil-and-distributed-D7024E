# CLI Testing

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
