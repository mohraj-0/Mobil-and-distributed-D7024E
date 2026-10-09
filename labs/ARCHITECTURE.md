# Implementation architecture

```mermaid
flowchart LR
    GUIDE["Implementation architecture"] --> CURRENT["Implemented: Part 1<br/>DHT, CLI, transports, and experiments"]
    GUIDE --> PLANNED["Design only: Part 2<br/>Signatures, DNS ownership, version chains, latest pointers"]
    CURRENT --> ORDER["Read general sequence first<br/>Then inspect file diagrams"]
    ORDER --> FLOW["Flowchart arrows<br/>Calls or data flow"]
    ORDER --> SEQ["Sequence arrows<br/>Runtime message order"]
    CURRENT --> PEER["Every peer is another Kademlia instance<br/>Own listener, routing table, and data store"]
```

[Part 2 design](PART2-DESIGN.md)

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

[CLI](cmd/simshell/main.go) · [Kademlia operations](kademlia/kademlia.go) · [Transports](kademlia/network.go)

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
    Note over Local,Peer: Remote STORE requests run concurrently; wait for all attempts
    Note over Local,Peer: Store succeeds when at least one selected replica accepts
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

[kademlia.go](kademlia/kademlia.go)

```mermaid
flowchart LR
    API["Kademlia operations"]
    SETTINGS["Defaults<br/>Alpha = 3, K = 10<br/>RPC timeout = 2 s<br/>Replication interval = 30 s"] -.-> API
    API --> JOIN["Join / Refresh<br/>Bootstrap and discover peers"]
    API --> LOOK["LookupContact<br/>Iterative FIND_NODE"]
    API --> GET["LookupData<br/>Local check, then FIND_VALUE"]
    API --> STORE["Store<br/>Validate 1-32768 raw bytes<br/>Hash bytes and select K replicas"]
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
    LISTEN --> HANDLERS["Request handlers: answer FIND_NODE, FIND_VALUE, STORE<br/>Reply handlers: learn contacts and signal waiting request"]
    HANDLERS --> DS
    HANDLERS --> RT
    HANDLERS --> PENDING
```

```mermaid
sequenceDiagram
    participant Operation as Lookup / Store
    participant Pending as Pending-response map
    participant Transport as Node transport
    participant Handler as Local reply handler

    Operation->>Operation: Generate random request ID
    Operation->>Pending: Register buffered response channel
    Operation->>Transport: Send JSON request to peer
    alt Matching reply arrives
        Transport->>Handler: Receive reply
        Handler->>Handler: Learn sender and returned contacts
        Handler->>Pending: Find channel for request ID
        Pending-->>Operation: Deliver response
    else No matching reply before deadline
        Operation->>Operation: Return RPC timeout error
    end
    Operation->>Pending: Remove pending request
```

## kademlia.go: parallel lookup

[kademlia.go](kademlia/kademlia.go)

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
    Note over Lookup,C: Timed-out peers can remain known candidates; returned contact does not prove liveness
```

```mermaid
flowchart TD
    GET["LookupData(key)"] --> LOCAL{"Valid local value?"}
    LOCAL -->|Yes| VALUE["Return verified bytes"]
    LOCAL -->|No| BATCH["Send up to alpha FIND_VALUE probes concurrently"]
    BATCH --> RESPONSE{"Next probe result"}
    RESPONSE -->|RPC error| MORE{"Unprocessed results in this round?"}
    RESPONSE -->|Contacts| LEARN["Merge, deduplicate, and sort candidates"]
    LEARN --> MORE
    RESPONSE -->|Value| CHECK{"Supported size and correct SHA-256?"}
    CHECK -->|Yes| VALUE
    CHECK -->|No| MORE
    MORE -->|Yes| RESPONSE
    MORE -->|No| LEFT{"Unqueried candidates remain?"}
    LEFT -->|Yes| BATCH
    LEFT -->|No| FAIL["Return value-not-found error"]
    VALUE -.-> EARLY["May return before remaining probes finish"]
```

## kademlia.go: joining and refresh

[kademlia.go](kademlia/kademlia.go)

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
        Note over Joiner,RT: Range indices differ from dynamically allocated routing-tree leaf indices
        Joiner->>Joiner: Refresh(index)
        Joiner->>Joiner: Copy local ID and flip the indexed bit
        Joiner->>Joiner: Randomize less significant bits
        Joiner->>Peers: LookupContact(random target in that range)
        Peers-->>Joiner: Contacts or probe timeout
        Joiner->>RT: Learn replying peers and returned contacts
    end
    Joiner-->>Startup: Join returns
    Note over Startup,Peers: Lookup failures are not propagated; nil does not guarantee a responsive bootstrap
```

## kademlia.go: replication

[kademlia.go](kademlia/kademlia.go)

```mermaid
flowchart TD
    START["Caller starts StartReplication in a goroutine"] --> WAIT["Wait for ticker or stop channel"]
    INACTIVE["Current CLI and experiments<br/>Do not start this loop automatically"] -.-> START
    WAIT --> EVENT{"Which event?"}
    EVENT -->|Stop| EXIT["Stop ticker and return"]
    EVENT -->|Tick| SNAP["Copy local values under read lock"]
    SNAP --> UNLOCK["Release data-store lock"]
    UNLOCK --> EACH["For each copied value, call Store"]
    EACH --> SELECT["Discover currently closest peers"]
    SELECT --> COPY["Re-store on up to K replicas"]
    COPY --> WAIT
    COPY -.-> KEEP["Values remain stored<br/>No expiration"]
```

## network.go: transport implementations

[network.go](kademlia/network.go)

```mermaid
flowchart TD
    K["Kademlia JSON RPC handling"] --> API["Node interface<br/>Listen, Close, Receive, SendData"]
    API --> SIM["SimulatedNode"]
    API --> UDP["UDPNode"]
    SIM --> DIAL["SimulatedNetwork.Listen / Dial"]
    DIAL --> CONN["SimulatedConnection.Send / Recv<br/>Listener map protected by mutex"]
    CONN --> QUEUE["Address-indexed channels<br/>1024 messages per listener"]
    QUEUE --> PEER["Another SimulatedNode in this process"]
    QUEUE --> FULL["Full queue: return error<br/>No blocking send"]
    SIM --> CLOSE["Close listener<br/>Unregister address and close channel"]
    CLOSE --> STOP["Receive loop exits"]
    UDP --> SOCKET["net.UDPConn<br/>One datagram per SendData<br/>Mutex protects connection state"]
    SOCKET --> RECEIVE["Receive copies datagram bytes<br/>64 KiB receive buffer"]
    RECEIVE --> REMOTE["Another UDPNode / process"]
    LEGACY["Legacy Network helpers<br/>PING, FIND_CONTACT, FIND_DATA, STORE text"] --> TEXT["Plain-text UDP messages<br/>Separate from JSON request/reply RPCs"]
    TEXT --> PRINT["Standalone Listen helper<br/>1 KiB buffer; print received text"]
```

## network.go: simulated delivery

[network.go](kademlia/network.go)

```mermaid
sequenceDiagram
    participant KA as Kademlia node A
    participant A as SimulatedNode A
    participant Bus as Shared SimulatedNetwork
    participant Q as Node B channel
    participant B as SimulatedNode B
    participant KB as Kademlia node B
    Note over A,Bus: Addresses identify in-memory listeners; no UDP ports are bound
    Note over KA,KB: Same transport used by the 1000-node test

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

[routingtable.go](kademlia/routingtable.go)

```mermaid
flowchart TD
    SETTINGS["Prefix tree<br/>Default b = 1<br/>20 contacts per leaf, independent of replication K"] -.-> ADD
    LOCK["Kademlia helpers acquire routingMu<br/>RoutingTable methods do not acquire locks"] -.-> ADD
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

## bucket.go: contact recency

[bucket.go](kademlia/bucket.go)

```mermaid
flowchart TD
    CONTACT["bucket.AddContact(contact)"] --> SCAN["Search list for matching node ID"]
    SCAN --> KNOWN{"Already present?"}
    KNOWN -->|Yes| MOVE["Move existing entry to front<br/>Most recently seen; stored address unchanged"]
    KNOWN -->|No| ROOM{"Fewer than 20 contacts?"}
    ROOM -->|Yes| PUSH["Insert contact at front"]
    ROOM -->|No| DROP["Drop new contact<br/>No ping or eviction of oldest peer"]
    MOVE --> DONE["Return"]
    PUSH --> DONE
    DROP --> DONE
```

## contact.go and kademliaid.go: identity and distance

[contact.go](kademlia/contact.go) · [kademliaid.go](kademlia/kademliaid.go) · [CLI](cmd/simshell/main.go)

```mermaid
flowchart LR
    ADDRESS["CLI node address"] --> MATERIAL["Normalized host and port<br/>host|port"]
    MATERIAL --> HASH["SHA-256"]
    EXP["Experiment-generated address string"] --> HASH
    HASH --> ID["KademliaID<br/>32 bytes / 64 hex characters"]
    VALUE["Stored value bytes"] --> VH["SHA-256 content key"]
    VH --> TARGET["Lookup target ID"]
    HEX["Existing 64-character hex ID"] --> DECODE["NewKademliaID<br/>Validate and decode; no hashing"]
    DECODE --> ID
    ID --> CONTACT["Contact<br/>ID, address, calculated distance"]
    CONTACT --> XOR["CalcDistance<br/>contact ID XOR target ID"]
    TARGET --> XOR
    XOR --> COMPARE["Compare distance bytes<br/>Nearest first"]
    COMPARE --> CAND["ContactCandidates.Sort / GetContacts"]
```

## CLI: cmd/simshell/main.go

[cmd/simshell/main.go](cmd/simshell/main.go)

```mermaid
flowchart TD
    USER["Command line or REPL input"] --> COBRA["Cobra command dispatch"]
    REPL["REPL: strings.Fields tokenization<br/>No shell quoting for filenames with spaces"] --> COBRA
    COBRA --> DEMO{"test command?"}
    DEMO -->|Yes| TEST["Create own network<br/>Run Part 1 CLI demonstration"]
    DEMO -->|No| CONFIG["configure once"]
    CONFIG --> KIND{"Transport?"}
    KIND -->|Simulated| SIM["Create shared simulation<br/>Create configured number of nodes"]
    SIM --> START["Start each RPC listener<br/>Additional nodes Join first node"]
    KIND -->|UDP| UDP["Bind local socket<br/>Set advertised address"]
    UDP --> BOOT["Start RPC listener<br/>Join bootstrap if configured"]
    START --> COMMAND{"Requested command"}
    BOOT --> COMMAND
    COMMAND -->|put / store| PUT["Read file and call Store"]
    COMMAND -->|get| GET["LookupData<br/>Print bytes or write file"]
    COMMAND -->|ping| PING["LookupContact for target address<br/>No dedicated JSON PING/PONG"]
    COMMAND -->|show| SHOW["Display routing table, data store, or nodes"]
    COMMAND -->|serve| SERVE["Keep process alive"]
    COMMAND -->|exit / quit / EOF| CLOSE["Close local and peer transports"]
```

## Experiments: main.go and resilience.go

[experiments/main.go](experiments/main.go) · [experiments/resilience.go](experiments/resilience.go) · [Experiment documentation](experiments/README.md)

```mermaid
flowchart TD
    FLAGS["Parse experiment options"] --> MODE{"Resilience mode?"}
    MODE -->|No| GRID["Sweep node counts, alpha, K, and seeds"]
    GRID --> BUILD["Build simulated network<br/>Seed routing knowledge"]
    BUILD --> STORE["Store seeded random values"]
    STORE --> LOOK["Run node and value lookups"]
    LOOK --> OBS["observedNode counts FIND_NODE / FIND_VALUE sends<br/>Delegates to network.go simulation"]
    OBS --> STATS["Aggregate success, probes, estimated hops, variance"]
    STATS --> OUTPUT["Raw CSV, summary CSV, report"]

    MODE -->|Yes| SCENARIO["Loss/latency, alpha, and churn/K scenarios"]
    SCENARIO --> FRESH["Fresh network for each trial"]
    FRESH --> HEALTHY["Store value under healthy conditions"]
    HEALTHY --> CHURN["Simulated exposure before lookup<br/>Departures and empty replacement nodes"]
    CHURN --> IMPAIR["impairedNode wraps simulated transport<br/>Silently drop packets or delay delivery<br/>Applies to requests and replies"]
    IMPAIR --> MEASURE["Lookup and record probes, duration, request RTT"]
    MEASURE --> RESULTS["trials.csv, requests.csv, probes.csv, report.md"]
    STATS -.-> HOPS["Estimated hops = ceil(probes / alpha)<br/>Not measured routing path length"]
```

## Deployment and tests

[docker-compose.yml](docker-compose.yml) · [kademlia_test.go](kademlia/kademlia_test.go) · [value_size_test.go](kademlia/value_size_test.go) · [CI workflow](../.github/workflows/ci.yml)

```mermaid
flowchart LR
    subgraph Simulation["One Go process: simulation or tests"]
        A["Kademlia instance A<br/>Own routing table and data store"]
        B["Kademlia instance B<br/>Own routing table and data store"]
        MANY["Additional instances<br/>1000 total in the large-network test"]
        BUS["Shared SimulatedNetwork<br/>Address-indexed channels"]
        A <--> BUS
        B <--> BUS
        MANY <--> BUS
    end
    subgraph Docker["Docker Swarm: docker stack deploy"]
        BOOT["Bootstrap container<br/>1 replica"]
        PEERS["Peer containers<br/>49 replicas"]
        OVERLAY["Overlay network<br/>UDP RPCs"]
        BOOT <--> OVERLAY
        PEERS <--> OVERLAY
    end
    TEST["Normal test suite"] --> Simulation
    Simulation --> RING["1000 live instances<br/>1000 request/reply exchanges<br/>Store and retrieve from non-replica"]
    TEST --> LOOPBACK["Loopback UDP tests<br/>Remote value-size round trips"]
    CI["GitHub Actions"] --> TEST
    CI --> RACE["Repeat tests with -race"]
```
