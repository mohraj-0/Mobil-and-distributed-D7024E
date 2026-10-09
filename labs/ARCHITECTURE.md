# Implementation architecture

These diagrams describe the current Part 1 implementation. Read the general sequence first, then use the file diagrams to locate each responsibility. Arrows in flowcharts mean calls or data flow; arrows in sequence diagrams show runtime message order. A peer represents another instance of the same Kademlia implementation.

Part 2 publication signatures, DNS ownership, version records, and mutable latest pointers are described in [PART2-DESIGN.md](PART2-DESIGN.md) but are not implemented in the current Go code.

## Diagram index

- [General functionality](#general-functionality)
- [File relationships](#file-relationships)
- [kademlia.go: operations and state](#kademliago-operations-and-state)
- [kademlia.go: parallel lookup](#kademliago-parallel-lookup)
- [kademlia.go: joining and refresh](#kademliago-joining-and-refresh)
- [kademlia.go: replication](#kademliago-replication)
- [network.go: transport implementations](#networkgo-transport-implementations)
- [network.go: simulated delivery](#networkgo-simulated-delivery)
- [routingtable.go: routing tree](#routingtablego-routing-tree)
- [bucket.go: contact recency](#bucketgo-contact-recency)
- [contact.go and kademliaid.go: identity and distance](#contactgo-and-kademliaidgo-identity-and-distance)
- [CLI: cmd/simshell/main.go](#cli-cmdsimshellmaingo)
- [Experiments: main.go and resilience.go](#experiments-maingo-and-resiliencego)
- [Deployment and tests](#deployment-and-tests)

## General functionality

Sources: [CLI](cmd/simshell/main.go), [Kademlia operations](kademlia/kademlia.go), [transports](kademlia/network.go). This example stores a file and then retrieves its value by hash. Discovery can require several rounds; each replica has its own listener, routing table, and data store.

```mermaid
sequenceDiagram
    actor User
    participant CLI as CLI / shell
    participant Local as Local Kademlia node
    participant RT as Local routing table
    participant Transport as Node transport
    participant Peer as Remote Kademlia peer
    participant DS as Peer data store

    Note over Local,Peer: Nodes already have running ListenForRPC loops
    User->>CLI: put FILENAME
    CLI->>Local: Store(file bytes)
    Local->>Local: Check size and compute SHA-256 key
    Local->>RT: Find contacts closest to key
    RT-->>Local: Initial shortlist
    loop Discovery rounds with up to alpha parallel probes
        Local->>Transport: Send FIND_NODE with request ID
        Transport->>Peer: Deliver JSON RPC
        Peer-->>Transport: FIND_NODE_REPLY with contacts
        Transport-->>Local: Match request ID and deliver contacts
        Local->>RT: Learn discovered contacts
    end
    loop Up to K selected replicas
        alt Replica is the local node
            Local->>Local: Store a copy under its hash
        else Replica is a remote peer
            Local->>Transport: Send STORE with key and bytes
            Transport->>Peer: Deliver STORE
            Peer->>Peer: Check value size and hash
            Peer->>DS: Save valid value
            Peer-->>Transport: STORE_REPLY accepted or rejected
            Transport-->>Local: Complete pending STORE request
        end
    end
    Note over Local,Peer: Remote STORE requests run concurrently
    Local-->>CLI: Key, or error if no replica accepted
    CLI-->>User: Print key

    User->>CLI: get KEY FILENAME
    CLI->>Local: LookupData(key)
    alt Valid value exists locally
        Local-->>CLI: Value bytes
    else Remote lookup is needed
        loop Probe nearest candidates in parallel rounds
            Local->>Transport: Send FIND_VALUE with request ID
            Transport->>Peer: Deliver FIND_VALUE
            Peer->>DS: Look up key
            DS-->>Peer: Value or missing
            Peer-->>Transport: FIND_VALUE_REPLY with value or contacts
            Transport-->>Local: Complete matching pending request
            Local->>Local: Verify returned size and SHA-256
        end
        Local-->>CLI: First valid value, or not-found error
    end
    CLI-->>User: Write verified bytes to file
```

## File relationships

This is a component map rather than a struct-by-struct UML diagram.

```mermaid
flowchart TD
    CLI["cmd/simshell/main.go<br/>Commands and node startup"]
    EXP["experiments/main.go and resilience.go<br/>Scenarios and measurements"]
    K["kademlia.go<br/>Lookup, store, join, RPC handling"]
    RT["routingtable.go<br/>Prefix tree and nearest contacts"]
    B["bucket.go<br/>Contact lists"]
    C["contact.go<br/>Peer ID, address, and distance"]
    ID["kademliaid.go<br/>256-bit IDs and XOR distance"]
    N["network.go<br/>Node interface and transports"]
    CLI --> K
    EXP --> K
    CLI --> N
    EXP --> N
    K --> RT
    K --> N
    RT --> B
    RT --> C
    B --> C
    C --> ID
    K --> ID
```

## kademlia.go: operations and state

Source: [kademlia.go](kademlia/kademlia.go). Defaults are `Alpha=3`, `K=10`, RPC timeout 2 seconds, and replication interval 30 seconds. Supported raw value sizes are 1–32,768 bytes.

```mermaid
flowchart LR
    API["Kademlia operations"]
    API --> JOIN["Join / Refresh<br/>Bootstrap and discover peers"]
    API --> LOOK["LookupContact<br/>Iterative FIND_NODE"]
    API --> GET["LookupData<br/>Local check, then FIND_VALUE"]
    API --> STORE["Store<br/>Hash bytes and select K replicas"]
    API --> REP["ReplicateData<br/>Store copies again"]
    JOIN --> LOOK
    STORE --> LOOK
    REP --> STORE
    GET --> DS["DataStore<br/>Protected by dataMu"]
    STORE --> DS
    LOOK --> RT["RoutingTable<br/>Protected by routingMu in Kademlia helpers"]
    GET --> RT
    LOOK --> SEND["sendFindNodeRPC"]
    GET --> FV["sendFindValueRPC"]
    STORE --> SR["sendStoreRPC"]
    SEND --> PENDING["Pending responses<br/>Request-ID maps, channels, and mutexes"]
    FV --> PENDING
    SR --> PENDING
    PENDING --> NET["Network.SendData / Receive"]
    NET --> LISTEN["ListenForRPC<br/>Dispatch each message in a goroutine"]
    LISTEN --> HANDLERS["Request handlers and reply handlers"]
    HANDLERS --> DS
    HANDLERS --> RT
    HANDLERS --> PENDING
```

Request handlers answer FIND_NODE, FIND_VALUE, and STORE. Reply handlers update routing knowledge and signal the waiting request channel. `Store` waits for its remote replication attempts and succeeds if at least one selected replica accepted the value; it does not require all K replicas to succeed.

## kademlia.go: parallel lookup

Sources: `lookupContactParallel`, `queryContactsParallel`, and the three `send...RPC` methods in [kademlia.go](kademlia/kademlia.go). Each outgoing request registers its response channel before sending, then waits for the matching reply or a timeout.

```mermaid
sequenceDiagram
    participant Lookup as LookupContact
    participant RT as Routing table
    participant A as Peer A
    participant B as Peer B
    participant C as Peer C

    Lookup->>RT: Initial contacts nearest target by XOR distance
    RT-->>Lookup: Shortlist
    loop While shortlist has unqueried contacts
        Note over Lookup,C: Example batch with alpha = 3
        par Probe A
            Lookup->>A: FIND_NODE target, request ID A
            A-->>Lookup: Matching reply with contacts
        and Probe B
            Lookup->>B: FIND_NODE target, request ID B
            B-->>Lookup: Matching reply with contacts
        and Probe C
            Lookup->>C: FIND_NODE target, request ID C
            Note over Lookup,C: If no matching reply arrives, this probe times out
        end
        Lookup->>Lookup: Wait for every probe in this round
        Lookup->>Lookup: Merge replies, remove duplicates, sort by XOR distance
        Lookup->>Lookup: Keep nearest candidates and mark queried IDs
    end
    Lookup-->>Lookup: Return nearest known contacts
```

Contact lookup uses strict parallel batches. Failed probes contribute no new contacts, but known candidates can remain in the returned shortlist; a returned contact alone is not proof of a successful liveness check. Value lookup uses FIND_VALUE batches and can return as soon as a valid value is received rather than waiting for every remaining response.

## kademlia.go: joining and refresh

Source: `Join` and `Refresh` in [kademlia.go](kademlia/kademlia.go).

```mermaid
sequenceDiagram
    participant Startup as CLI startup
    participant Joiner as Joining node
    participant RT as Joining node routing table
    participant Peers as Bootstrap and discovered peers

    Startup->>Joiner: Start ListenForRPC
    Startup->>Joiner: Join(bootstrap contact)
    Joiner->>Joiner: Validate local setup and bootstrap contact
    Joiner->>RT: Add bootstrap contact
    Joiner->>Peers: LookupContact(local node ID)
    Peers-->>Joiner: FIND_NODE replies
    Joiner->>RT: Learn neighbors from replies
    loop XOR-distance ranges 0 through 255
        Joiner->>Joiner: Refresh(index)
        Joiner->>Joiner: Copy local ID and flip the indexed bit
        Joiner->>Joiner: Randomize less significant bits
        Joiner->>Peers: LookupContact(random target in that range)
        Peers-->>Joiner: Contacts or probe timeout
        Joiner->>RT: Learn replying peers and returned contacts
    end
    Joiner-->>Startup: Join returns
```

Refresh indices here identify 256 XOR-distance ranges. They are not the dynamically allocated leaf indices used by the routing tree's inspection methods. `Join` validates configuration but currently does not propagate failed lookup probes, so its nil result does not guarantee a responsive bootstrap.

## kademlia.go: replication

Source: `StartReplication` and `ReplicateData` in [kademlia.go](kademlia/kademlia.go).

```mermaid
flowchart TD
    START["Caller starts StartReplication in a goroutine"] --> WAIT["Wait for ticker or stop channel"]
    WAIT --> EVENT{"Which event?"}
    EVENT -->|Stop| EXIT["Stop ticker and return"]
    EVENT -->|Tick| SNAP["Copy local values under read lock"]
    SNAP --> UNLOCK["Release data-store lock"]
    UNLOCK --> EACH["For each copied value, call Store"]
    EACH --> SELECT["Discover currently closest peers"]
    SELECT --> COPY["Re-store on up to K replicas"]
    COPY --> WAIT
```

The replication functions exist, but the current CLI and experiment startup paths do not call `StartReplication`. The diagram shows how an explicit caller activates it. Values do not expire.

## network.go: transport implementations

Source: [network.go](kademlia/network.go). Kademlia uses the `Node` interface to exchange JSON RPC bytes without depending on a particular transport.

```mermaid
flowchart TD
    K["Kademlia JSON RPC handling"] --> API["Node interface<br/>Listen, Close, Receive, SendData"]
    API --> SIM["SimulatedNode"]
    API --> UDP["UDPNode"]
    SIM --> DIAL["SimulatedNetwork.Listen / Dial"]
    DIAL --> CONN["SimulatedConnection.Send / Recv"]
    CONN --> QUEUE["Address-indexed channels<br/>1024 messages per listener"]
    QUEUE --> PEER["Another SimulatedNode in this process"]
    UDP --> SOCKET["net.UDPConn<br/>One datagram per SendData"]
    SOCKET --> RECEIVE["Receive copies datagram bytes<br/>64 KiB receive buffer"]
    RECEIVE --> REMOTE["Another UDPNode / process"]
    LEGACY["Legacy Network helpers<br/>PING, FIND_CONTACT, FIND_DATA, STORE text"] --> TEXT["Plain-text UDP messages<br/>Separate from JSON request/reply RPCs"]
```

`SimulatedNetwork` protects its listener map with a mutex. Delivery to a full queue returns an error instead of blocking. Closing a listener unregisters its address and closes its channel, allowing the receive loop to terminate. `UDPNode` binds sockets and uses read/write operations guarded by its connection-state mutex. The legacy standalone `Listen` helper has a 1 KiB buffer and prints text; the JSON RPC listener uses `UDPNode.Receive` instead.

## network.go: simulated delivery

This is the same simulation used by the 1,000-node test; addresses identify in-memory listeners rather than real bound UDP ports.

```mermaid
sequenceDiagram
    participant KA as Kademlia node A
    participant A as SimulatedNode A
    participant Bus as Shared SimulatedNetwork
    participant Q as Node B channel
    participant B as SimulatedNode B
    participant KB as Kademlia node B

    B->>Bus: Listen(address B)
    Bus->>Q: Register buffered listener channel
    KA->>A: SendData(address B, JSON bytes)
    A->>Bus: Dial(address B)
    Bus-->>A: Sending connection
    A->>Q: Connection.Send(Message with From, To, Data)
    A->>Bus: Close sending connection
    B->>Q: Receive / Connection.Recv
    Q-->>B: Message
    B-->>KB: Receive returns message
    KB->>KB: Dispatch RPC handler in a goroutine
    KB->>B: SendData(address A, reply bytes)
    Note over A,B: Reply follows the same channel-routing path in reverse
    A-->>KA: Receive reply and match request ID
```

## routingtable.go: routing tree

Source: [routingtable.go](kademlia/routingtable.go). The default branching parameter is `b=1`. Only a full leaf on the local node's prefix path may split. Leaf capacity is fixed at 20, independently of Kademlia's configurable replication factor `K`.

```mermaid
flowchart TD
    ADD["AddContact"] --> VALID{"Contact ID present and not the local ID?"}
    VALID -->|No| IGNORE["Return without insertion"]
    VALID -->|Yes| WALK["Follow ID prefix bits from root"]
    WALK --> LEAF{"Reached a leaf?"}
    LEAF -->|No| CHILD["Select child from next b bits"]
    CHILD --> WALK
    LEAF -->|Yes| SPACE{"Space available or contact already known?"}
    SPACE -->|Yes| BUCKET["Delegate to bucket.AddContact"]
    SPACE -->|No| SPLIT{"Leaf contains local ID and can split?"}
    SPLIT -->|Yes| CHILDREN["Create children and redistribute contacts"]
    CHILDREN --> WALK
    SPLIT -->|No| FULL["Delegate to full bucket<br/>New contact is dropped"]

    FIND["FindClosestContacts(target, count)"] --> COLLECT["Collect contacts from all leaves"]
    COLLECT --> DIST["Calculate XOR distances"]
    DIST --> SORT["Sort nearest first"]
    SORT --> TAKE["Return up to count contacts"]
```

Nearest-contact queries currently collect and sort all stored contacts rather than performing a pruned nearest-neighbor tree traversal. Kademlia's routing helpers provide synchronization; `RoutingTable` itself does not acquire locks.

## bucket.go: contact recency

Source: [bucket.go](kademlia/bucket.go). The front of the linked list is the most recently seen contact.

```mermaid
flowchart TD
    CONTACT["bucket.AddContact(contact)"] --> SCAN["Search list for matching node ID"]
    SCAN --> KNOWN{"Already present?"}
    KNOWN -->|Yes| MOVE["Move existing entry to front"]
    KNOWN -->|No| ROOM{"Fewer than 20 contacts?"}
    ROOM -->|Yes| PUSH["Insert contact at front"]
    ROOM -->|No| DROP["Drop new contact"]
    MOVE --> DONE["Return"]
    PUSH --> DONE
    DROP --> DONE
```

The current bucket implementation does not ping or evict the least recently seen contact when full. Moving an existing entry updates its recency but does not replace its stored address.

## contact.go and kademliaid.go: identity and distance

Sources: [contact.go](kademlia/contact.go), [kademliaid.go](kademlia/kademliaid.go), and the CLI's `hashID` helper. A `Contact` pairs a 256-bit ID with a network address and a calculated distance.

```mermaid
flowchart LR
    ADDRESS["CLI node address"] --> MATERIAL["Normalized host and port<br/>host|port"]
    MATERIAL --> HASH["SHA-256"]
    HASH --> ID["KademliaID<br/>32 bytes / 64 hex characters"]
    VALUE["Stored value bytes"] --> VH["SHA-256 content key"]
    VH --> TARGET["Lookup target ID"]
    ID --> CONTACT["Contact<br/>ID and address"]
    CONTACT --> XOR["CalcDistance<br/>contact ID XOR target ID"]
    TARGET --> XOR
    XOR --> COMPARE["Compare distance bytes<br/>Nearest first"]
    COMPARE --> CAND["ContactCandidates.Sort / GetContacts"]
```

ID hashing is chosen by the caller: the CLI uses `host|port`, while the experiment helper hashes its generated address string. `NewKademliaID` validates and decodes a 64-character hex identifier; it does not hash its input.

## CLI: cmd/simshell/main.go

Source: [cmd/simshell/main.go](cmd/simshell/main.go). Cobra handles both direct invocation and commands entered in the interactive shell.

```mermaid
flowchart TD
    USER["Command line or REPL input"] --> COBRA["Cobra command dispatch"]
    COBRA --> CONFIG["configure once"]
    CONFIG --> KIND{"Transport?"}
    KIND -->|Simulated| SIM["Create shared simulation<br/>Create configured number of nodes"]
    SIM --> START["Start each RPC listener<br/>Additional nodes Join first node"]
    KIND -->|UDP| UDP["Bind local socket<br/>Set advertised address"]
    UDP --> BOOT["Start RPC listener<br/>Join bootstrap if configured"]
    START --> COMMAND{"Requested command"}
    BOOT --> COMMAND
    COMMAND -->|put / store| PUT["Read file and call Store"]
    COMMAND -->|get| GET["LookupData<br/>Print bytes or write file"]
    COMMAND -->|ping| PING["LookupContact for target address"]
    COMMAND -->|show| SHOW["Display routing table, data store, or nodes"]
    COMMAND -->|serve| SERVE["Keep process alive"]
    COMMAND -->|test| TEST["Run Part 1 CLI demonstration"]
    COMMAND -->|exit / quit / EOF| CLOSE["Close local and peer transports"]
```

The `test` command creates its own demonstration network and bypasses normal startup configuration. `ping` currently checks the contact-lookup result rather than exchanging a dedicated JSON PING/PONG RPC. The REPL tokenizes input with `strings.Fields`, so it does not interpret shell quoting for filenames containing spaces.

## Experiments: main.go and resilience.go

Sources: [experiments/main.go](experiments/main.go), [experiments/resilience.go](experiments/resilience.go), and [experiment documentation](experiments/README.md).

```mermaid
flowchart TD
    FLAGS["Parse experiment options"] --> MODE{"Resilience mode?"}
    MODE -->|No| GRID["Sweep node counts, alpha, K, and seeds"]
    GRID --> BUILD["Build simulated network<br/>Seed routing knowledge"]
    BUILD --> STORE["Store seeded random values"]
    STORE --> LOOK["Run node and value lookups"]
    LOOK --> OBS["observedNode records outbound probes"]
    OBS --> STATS["Aggregate success, probes, estimated hops, variance"]
    STATS --> OUTPUT["Raw CSV, summary CSV, report"]

    MODE -->|Yes| SCENARIO["Loss/latency, alpha, and churn/K scenarios"]
    SCENARIO --> FRESH["Fresh network for each trial"]
    FRESH --> HEALTHY["Store value under healthy conditions"]
    HEALTHY --> CHURN["Controlled departures and empty replacement nodes"]
    CHURN --> IMPAIR["Enable per-packet loss and one-way latency"]
    IMPAIR --> MEASURE["Lookup and record probes, duration, request RTT"]
    MEASURE --> RESULTS["trials.csv, requests.csv, probes.csv, report.md"]
```

The wrappers delegate to the existing `network.go` simulation. `observedNode` counts relevant FIND_NODE/FIND_VALUE sends. `impairedNode` silently drops selected packets or schedules delayed delivery, including replies. Churn is a simulated exposure between storage and lookup, not continuous node departures during the lookup. Estimated hops are `ceil(probes/alpha)`; they are not measured routing path lengths.

## Deployment and tests

Sources: [docker-compose.yml](docker-compose.yml), [kademlia_test.go](kademlia/kademlia_test.go), [value_size_test.go](kademlia/value_size_test.go), and [.github/workflows/ci.yml](../.github/workflows/ci.yml).

```mermaid
flowchart LR
    subgraph Simulation["One Go process: simulation or tests"]
        A["Kademlia instance A"]
        B["Kademlia instance B"]
        MANY["Additional instances<br/>1000 total in the large-network test"]
        BUS["Shared SimulatedNetwork<br/>Address-indexed channels"]
        A <--> BUS
        B <--> BUS
        MANY <--> BUS
    end
    subgraph Docker["Docker Swarm deployment"]
        BOOT["Bootstrap container<br/>1 replica"]
        PEERS["Peer containers<br/>49 replicas"]
        OVERLAY["Overlay network<br/>UDP RPCs"]
        BOOT <--> OVERLAY
        PEERS <--> OVERLAY
    end
    TEST["Normal test suite"] --> Simulation
    TEST --> LOOPBACK["Loopback UDP tests<br/>Remote value-size round trips"]
    CI["GitHub Actions"] --> TEST
    CI --> RACE["Repeat tests with -race"]
```

Each node owns its state; the simulation shares the transport fabric, not the routing tables or data stores. The 1,000-node test keeps all instances live, verifies a request/reply ring, then stores and retrieves a value from a non-replica. Docker's replica count is configured for `docker stack deploy`; ordinary Compose does not establish the Swarm topology described here.
