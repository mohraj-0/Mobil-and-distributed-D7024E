# System architecture

Read the main path from left to right: commands enter the registry or DHT, the DHT uses RPC, and the transport connects peer nodes. Each component shows one level of detail.

```mermaid
flowchart LR
    USER["User"] --> CLI["Command interface<br/>Parse commands and read/write files"]

    subgraph REGISTRY["Package registry"]
        direction TB
        PACKAGES["Publish / install / show<br/>Sign publications and resolve versions"]
        CHECKS["Validation<br/>Check ownership, signatures, and history<br/>Reject forks and rollback; fetch missing history"]
        PACKAGES <--> CHECKS
    end

    CLI -->|publish, install, show| PACKAGES
    CHECKS <-->|owner public key| DNS["Ownership verifier<br/>DNS TXT or fake DNS"]

    subgraph RECORDS["Package data"]
        direction TB
        HEAD["Latest pointer<br/>Name-derived key; signed mutable value"]
        VERSION["Version records<br/>Signed records linked to previous versions"]
        BLOB["Package blobs<br/>Immutable bytes addressed by content hash"]
        HEAD -->|record hash| VERSION
        VERSION -->|blob hash| BLOB
        VERSION -->|previous record hash| VERSION
    end

    PACKAGES <-->|resolve and create| HEAD
    PACKAGES <-->|follow history| VERSION
    PACKAGES <-->|upload and download| BLOB

    subgraph DHT["Kademlia DHT"]
        direction TB
        OPERATIONS["Join / lookup / store / replicate<br/>Find closest peers and distribute values"]
        ROUTING["Routing table<br/>Contacts in k-buckets; XOR distance"]
        STORAGE["Local storage<br/>Content-addressed values and validated latest pointers"]
        OPERATIONS <-->|select and learn peers| ROUTING
        OPERATIONS <-->|read and write| STORAGE
    end

    CLI -->|put, get, ping| OPERATIONS
    PACKAGES <-->|store and retrieve| OPERATIONS
    CHECKS -->|validate registry updates| STORAGE

    OPERATIONS <-->|requests and results| RPC["RPC layer<br/>JSON requests and replies<br/>Request IDs, response channels, timeouts"]

    subgraph NETWORK["Network transport"]
        direction TB
        SIM["SimulatedNode<br/>In-memory message channels"]
        UDP["UDPNode<br/>UDP datagrams"]
    end

    RPC <--> SIM
    RPC <--> UDP
    SIM <-->|same process| PEERS["Other nodes<br/>Same registry, DHT, and storage"]
    UDP <-->|processes or containers| PEERS
```

Publishing stores the blob and signed version record, then advances the latest pointer after validation. Installing follows the pointer and version chain to retrieve a verified blob. The DHT locates replicas, RPC matches requests with replies, and the selected transport carries messages between nodes.
