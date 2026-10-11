# System architecture

The command interface connects users to the package registry. The registry verifies ownership and version history, while the DHT stores and retrieves data across peer nodes.

```mermaid
flowchart TB
    USER["User"] -->|publish, install, show| CLI["Command interface"]
    CLI --> REGISTRY["Package registry"]
    REGISTRY <-->|verify ownership and signatures| DNS["Domain ownership<br/>DNS public key"]
    REGISTRY <-->|check and follow version history| RECORDS["Package data<br/>Latest pointer → version records → package blobs"]
    RECORDS <-->|store and retrieve| DHT["Kademlia DHT"]
    CLI -->|put, get, ping| DHT
    DHT <-->|find nearest peers| ROUTING["Routing table"]
    DHT <-->|read and write| STORAGE["Local data store"]
    DHT <-->|requests and replies| RPC["RPC layer"]
    RPC <--> NETWORK["Network transport<br/>In-memory or UDP"]
    NETWORK <--> PEERS["Other nodes<br/>Same registry, DHT, and storage"]
```

Publishing stores the package blob and signed version record, then updates the latest pointer after ownership and history checks. Installing follows the pointer and version records to download the package blob. The DHT finds the peers holding the data; RPC and the network transport carry the requests and replies.
