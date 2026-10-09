# Kademlia, network, and command-shell architecture

| Color | Responsibility |
| --- | --- |
| Purple | Command shell: `cmd/simshell/main.go` |
| Blue | Kademlia operations and RPC handling |
| Teal | Simulated network in one process |
| Orange | Real UDP networking between processes |
| Green | Routing tables and stored data |
| Yellow | Decisions |
| Red | Errors and timeouts |

## 1. General architecture

[cmd/simshell/main.go](cmd/simshell/main.go) · [kademlia.go](kademlia/kademlia.go) · [network.go](kademlia/network.go)

```mermaid
flowchart TB
    USER["User enters a command"]:::shell --> CMD["Command shell<br/>Cobra / interactive REPL"]:::shell
    CMD --> KADE["Kademlia logic<br/>Join · LookupContact · Store · LookupData"]:::logic
    subgraph STATE["Each Kademlia instance owns its state"]
        ROUTE["Routing table<br/>Known peers sorted by XOR distance"]:::data
        STORE["Data store<br/>SHA-256 key to value bytes"]:::data
    end
    KADE <--> ROUTE
    KADE <--> STORE
    KADE --> RPC["JSON requests and replies<br/>Request IDs, response channels, timeouts"]:::logic
    RPC --> API["Node interface in network.go<br/>Listen · SendData · Receive · Close"]:::logic
    API --> SIM["SimulatedNode<br/>In-memory message channels"]:::sim
    API --> UDP["UDPNode<br/>Real UDP sockets"]:::real
    SIM <--> SPEERS["Other Kademlia instances<br/>Same Go process, shared simulation"]:::sim
    UDP <--> RPEERS["Other Kademlia instances<br/>Other processes or Docker containers"]:::real
    API -.-> SAME["Same Kademlia functions<br/>Different transport implementation"]:::logic
    classDef shell fill:#ede9fe,stroke:#7c3aed,color:#2e1065,stroke-width:2px
    classDef logic fill:#dbeafe,stroke:#2563eb,color:#172554,stroke-width:2px
    classDef sim fill:#ccfbf1,stroke:#0d9488,color:#134e4a,stroke-width:2px
    classDef real fill:#ffedd5,stroke:#ea580c,color:#7c2d12,stroke-width:2px
    classDef data fill:#dcfce7,stroke:#16a34a,color:#14532d,stroke-width:2px
    classDef choice fill:#fef3c7,stroke:#d97706,color:#78350f,stroke-width:2px
    classDef failure fill:#ffe4e6,stroke:#e11d48,color:#881337,stroke-width:2px
```

## 2. How Kademlia functions

[kademlia.go](kademlia/kademlia.go) · [routingtable.go](kademlia/routingtable.go) · [bucket.go](kademlia/bucket.go)

```mermaid
flowchart TB
    CALL["Shell calls a Kademlia operation"]:::shell --> OP{"Which operation?"}:::choice
    OP -->|Join| JOIN["Add bootstrap contact<br/>Look up own ID<br/>Refresh 256 XOR-distance ranges"]:::logic
    OP -->|LookupContact| FIND["Select nearest unqueried peers<br/>FIND_NODE probes in parallel batches"]:::logic
    OP -->|Store| PUT["Validate 1-32768 bytes<br/>Compute SHA-256<br/>Discover up to K replicas"]:::logic
    OP -->|LookupData| GET["Check local store<br/>Otherwise probe peers with FIND_VALUE"]:::logic
    JOIN --> FIND
    JOIN --> ROUTE["Routing table<br/>Learn contacts from replies"]:::data
    FIND <--> ROUTE
    PUT --> SAVE["Save locally if selected<br/>Send STORE to remote replicas"]:::logic
    GET <--> LOCAL["Local data store"]:::data
    FIND --> RPC["RPC engine<br/>Register request ID before sending"]:::logic
    SAVE --> RPC
    GET --> RPC
    RPC --> NET["Node transport<br/>SimulatedNode or UDPNode"]:::logic
    NET --> REPLY{"Reply received before timeout?"}:::choice
    REPLY -->|Yes| HANDLE["Match request ID<br/>Learn contacts and process result"]:::logic
    REPLY -->|No| FAIL["RPC error<br/>Lookup can continue with other candidates"]:::failure
    HANDLE --> VALIDATE["Value replies: verify size and SHA-256<br/>STORE replies: count accepted replicas"]:::logic
    VALIDATE --> RESULT["Return contacts, verified bytes, or store key<br/>Store requires at least one accepting replica"]:::logic
    HANDLE --> ROUTE
    HANDLE --> LOCAL
    RESULT --> OUT["Shell prints result or writes file"]:::shell
    classDef shell fill:#ede9fe,stroke:#7c3aed,color:#2e1065,stroke-width:2px
    classDef logic fill:#dbeafe,stroke:#2563eb,color:#172554,stroke-width:2px
    classDef sim fill:#ccfbf1,stroke:#0d9488,color:#134e4a,stroke-width:2px
    classDef real fill:#ffedd5,stroke:#ea580c,color:#7c2d12,stroke-width:2px
    classDef data fill:#dcfce7,stroke:#16a34a,color:#14532d,stroke-width:2px
    classDef choice fill:#fef3c7,stroke:#d97706,color:#78350f,stroke-width:2px
    classDef failure fill:#ffe4e6,stroke:#e11d48,color:#881337,stroke-width:2px
```

## 3. How the simulated network functions

[network.go](kademlia/network.go): SimulatedNode, SimulatedNetwork, and SimulatedConnection

```mermaid
flowchart LR
    subgraph A["Node A — same Go process"]
        KA["Kademlia A<br/>Creates JSON RPC"]:::logic
        SA["SimulatedNode A<br/>SendData(destination, bytes)"]:::sim
        KA --> SA
    end
    subgraph BUS["Shared SimulatedNetwork"]
        REG["Listen registers addresses<br/>Map protected by mutex"]:::sim
        DIAL["Dial destination address<br/>Create sending connection"]:::sim
        SEND["Connection.Send<br/>Message: From, To, Data"]:::sim
        QUEUE["Destination buffered channel<br/>1024 message slots"]:::sim
        REG -.-> QUEUE
        DIAL --> SEND --> QUEUE
    end
    subgraph B["Node B — same Go process"]
        SB["SimulatedNode B<br/>Receive / Connection.Recv"]:::sim
        KB["Kademlia B<br/>ListenForRPC dispatches handler"]:::logic
        SB --> KB
    end
    SA --> DIAL
    QUEUE --> SB
    KB --> REPLY["Reply uses the same path<br/>Back to node A's channel"]:::sim
    REPLY --> KA
    SEND -.-> ERROR["Unknown address or full queue<br/>Return send error"]:::failure
    REG -.-> CLOSE["Close listener<br/>Unregister address, close channel<br/>Receive loop exits"]:::sim
    BUS -.-> NOTE["No UDP port is bound<br/>1000 nodes can share this transport fabric"]:::sim
    classDef shell fill:#ede9fe,stroke:#7c3aed,color:#2e1065,stroke-width:2px
    classDef logic fill:#dbeafe,stroke:#2563eb,color:#172554,stroke-width:2px
    classDef sim fill:#ccfbf1,stroke:#0d9488,color:#134e4a,stroke-width:2px
    classDef real fill:#ffedd5,stroke:#ea580c,color:#7c2d12,stroke-width:2px
    classDef data fill:#dcfce7,stroke:#16a34a,color:#14532d,stroke-width:2px
    classDef choice fill:#fef3c7,stroke:#d97706,color:#78350f,stroke-width:2px
    classDef failure fill:#ffe4e6,stroke:#e11d48,color:#881337,stroke-width:2px
```

## 4. How the real UDP network functions

[network.go](kademlia/network.go): UDPNode

```mermaid
flowchart LR
    subgraph A["Process or container A"]
        KA["Kademlia A<br/>JSON RPC and request ID"]:::logic
        UA["UDPNode A<br/>Listen binds a UDP socket"]:::real
        SEND["SendData<br/>WriteToUDP / DialUDP"]:::real
        KA --> UA --> SEND
    end
    WIRE["Real network<br/>One UDP datagram per message"]:::real
    SEND --> WIRE
    subgraph B["Process or container B"]
        UB["UDPNode B<br/>Bound UDP socket"]:::real
        RECV["Receive / ReadFromUDP<br/>64 KiB receive buffer<br/>Copy received bytes"]:::real
        KB["Kademlia B<br/>ListenForRPC dispatches JSON handler"]:::logic
        UB --> RECV --> KB
    end
    WIRE --> UB
    KB --> BACK["Reply datagram<br/>SendData back to A"]:::real
    BACK --> RECEIVEA["UDPNode A receives reply"]:::real
    RECEIVEA --> MATCH["Kademlia A matches request ID<br/>Signals pending response channel"]:::logic
    MATCH --> KA
    WIRE -.-> LOSS["Packet loss / excessive delay"]:::failure
    LOSS --> TIMEOUT["Kademlia RPC deadline expires"]:::failure
    UA -.-> ADDR["Bind address: local socket<br/>Advertised address: how peers reach this node"]:::real
    classDef shell fill:#ede9fe,stroke:#7c3aed,color:#2e1065,stroke-width:2px
    classDef logic fill:#dbeafe,stroke:#2563eb,color:#172554,stroke-width:2px
    classDef sim fill:#ccfbf1,stroke:#0d9488,color:#134e4a,stroke-width:2px
    classDef real fill:#ffedd5,stroke:#ea580c,color:#7c2d12,stroke-width:2px
    classDef data fill:#dcfce7,stroke:#16a34a,color:#14532d,stroke-width:2px
    classDef choice fill:#fef3c7,stroke:#d97706,color:#78350f,stroke-width:2px
    classDef failure fill:#ffe4e6,stroke:#e11d48,color:#881337,stroke-width:2px
```

## 5. How the command shell starts each mode

[cmd/simshell/main.go](cmd/simshell/main.go): configure, configureSimulated, configureUDP, and newNode

```mermaid
flowchart TB
    INPUT["Run simshell or enter a REPL command"]:::shell --> PARSE["Cobra parses command and flags"]:::shell
    PARSE --> MODE{"--transport"}:::choice
    subgraph SIMMODE["SIMULATED — many instances in one process"]
        SC["configureSimulated"]:::shell
        BUS["NewSimulatedNetwork<br/>One shared channel-based network"]:::sim
        SN["Create --nodes instances<br/>Each gets NewSimulatedNodeWithNetwork"]:::sim
        SK["newNode<br/>Create contact, routing table, Kademlia instance"]:::logic
        SL["Start ListenForRPC for every instance"]:::logic
        SJ["First instance becomes the shell node<br/>Other instances Join the first node"]:::logic
        SC --> BUS --> SN --> SK --> SL --> SJ
    end
    subgraph REALMODE["REAL UDP — one instance per process"]
        RC["configureUDP"]:::shell
        UN["NewUDPNode"]:::real
        BIND["newNode<br/>Listen on --listen-addr or --addr"]:::real
        UK["Create contact from advertised --addr<br/>Create routing table and Kademlia instance"]:::logic
        UL["Start ListenForRPC"]:::logic
        HAS{"--bootstrap-addr configured?"}:::choice
        JOIN["Join bootstrap node<br/>Self lookup and range refresh"]:::logic
        FIRST["Run without bootstrap<br/>Can serve as bootstrap for other nodes"]:::logic
        RC --> UN --> BIND --> UK --> UL --> HAS
        HAS -->|Yes| JOIN
        HAS -->|No| FIRST
    end
    MODE -->|simulated| SC
    MODE -->|udp| RC
    SJ --> READY["Shell keeps its selected Kademlia node<br/>Execute the requested command"]:::shell
    JOIN --> READY
    FIRST --> READY
    classDef shell fill:#ede9fe,stroke:#7c3aed,color:#2e1065,stroke-width:2px
    classDef logic fill:#dbeafe,stroke:#2563eb,color:#172554,stroke-width:2px
    classDef sim fill:#ccfbf1,stroke:#0d9488,color:#134e4a,stroke-width:2px
    classDef real fill:#ffedd5,stroke:#ea580c,color:#7c2d12,stroke-width:2px
    classDef data fill:#dcfce7,stroke:#16a34a,color:#14532d,stroke-width:2px
    classDef choice fill:#fef3c7,stroke:#d97706,color:#78350f,stroke-width:2px
    classDef failure fill:#ffe4e6,stroke:#e11d48,color:#881337,stroke-width:2px
```

## 6. How shell commands invoke Kademlia

[cmd/simshell/main.go](cmd/simshell/main.go): root, repl, put, get, ping, show, and close

```mermaid
flowchart TB
    USER["Direct command or interactive input"]:::shell --> DISPATCH["Cobra command dispatch<br/>Uses the configured shell node"]:::shell
    DISPATCH --> CMD{"Command"}:::choice
    CMD -->|put / store FILENAME| READ["Read file bytes"]:::shell
    READ --> STORE["Kademlia.Store<br/>Hash value and replicate"]:::logic
    STORE --> KEY["Print SHA-256 key"]:::shell
    CMD -->|get KEY| LOOKUP["Kademlia.LookupData<br/>Local check or remote FIND_VALUE"]:::logic
    LOOKUP --> VERIFY["Verify returned hash<br/>Print bytes or write output file"]:::shell
    CMD -->|ping ADDRESS| FIND["Kademlia.LookupContact<br/>Check returned contact for address"]:::logic
    FIND --> PONG["Print result<br/>Not a dedicated JSON PING/PONG exchange"]:::shell
    CMD -->|show rt / ds / nodes| SHOW["Inspect routing table, data store, or peers"]:::data
    CMD -->|serve| SERVE["Keep process alive<br/>RPC listener serves incoming requests"]:::logic
    CMD -->|exit / quit / EOF| CLOSE["Close local and peer transports"]:::shell
    STORE -.-> TRANSPORT["Same calls in both modes<br/>The node's Network field selects transport"]:::logic
    LOOKUP -.-> TRANSPORT
    FIND -.-> TRANSPORT
    TRANSPORT --> SIM["SimulatedNode<br/>Channels in this process"]:::sim
    TRANSPORT --> UDP["UDPNode<br/>Datagrams to other processes"]:::real
    classDef shell fill:#ede9fe,stroke:#7c3aed,color:#2e1065,stroke-width:2px
    classDef logic fill:#dbeafe,stroke:#2563eb,color:#172554,stroke-width:2px
    classDef sim fill:#ccfbf1,stroke:#0d9488,color:#134e4a,stroke-width:2px
    classDef real fill:#ffedd5,stroke:#ea580c,color:#7c2d12,stroke-width:2px
    classDef data fill:#dcfce7,stroke:#16a34a,color:#14532d,stroke-width:2px
    classDef choice fill:#fef3c7,stroke:#d97706,color:#78350f,stroke-width:2px
    classDef failure fill:#ffe4e6,stroke:#e11d48,color:#881337,stroke-width:2px
```

## Run either mode

From `labs`:

```powershell
# Simulated: three nodes sharing one in-memory network
go run ./cmd/simshell --transport simulated --nodes 3

# Real: bootstrap node in one terminal
go run ./cmd/simshell --transport udp --addr 127.0.0.1:8000

# Real: second node in another terminal
go run ./cmd/simshell --transport udp --addr 127.0.0.1:8001 --bootstrap-addr 127.0.0.1:8000
```

Open [architecture.html](architecture.html) in a browser to view the colored flowcharts. The browser viewer requires internet access to load Mermaid. GitHub also renders the diagrams directly in this Markdown file.
