# Kademlia value sizes

## 1,000-node simulated-network test

`TestThousandNodesCommunicateOverSimulatedNetwork` creates 1,000 actual Kademlia instances using `NewSimulatedNetwork` and `NewSimulatedNodeWithNetwork` from `network.go`. All instances remain live while each completes a FIND_NODE request/response exchange with the next node in a ring. The test validates reply contents, then stores a value and retrieves it from a non-replica through FIND_VALUE. Cleanup closes every transport and waits for listener loops to exit.

This test runs in the normal test suite and CI's race-detector step; it is not gated behind a benchmark or an optional flag. Run it from `labs` with:

```powershell
go test ./kademlia -run TestThousandNodesCommunicateOverSimulatedNetwork -v
```

## Supported values

This implementation supports **1 through 32,768 raw bytes (32 KiB) per value**, inclusive. The exported `kademlia.MaxValueSize` constant defines the upper limit. The same limit applies to UDP and simulated networks; values may contain arbitrary binary bytes, including zero bytes. Sizes are byte counts, not character counts.

| Raw value size | Supported |
| --- | --- |
| Empty / 0 bytes | No |
| 1 byte | Yes |
| 255 and 256 bytes | Yes |
| 1,024 bytes | Yes |
| 32,768 bytes | Yes, maximum |
| 32,769 bytes or larger | No |

`Store` rejects unsupported sizes before sending requests or changing the data store. Incoming `STORE` RPCs reject them with `Stored: false`, even when the SHA-256 key matches. Outbound STORE RPCs also validate size. Lookup ignores unsupported values received from peers or inserted directly into the public data-store map, and nodes do not serve them in `FIND_VALUE` replies.

Values are encoded as base64 inside JSON RPCs. The maximum raw value expands to 43,692 base64 bytes, plus JSON fields such as the key, sender, and request ID. The conservative 32 KiB limit leaves room for ordinary RPC metadata and fits the UDP transport's 64 KiB receive buffer. Each STORE request and successful FIND_VALUE reply carries the entire value in one datagram; there is no application-level chunking or reassembly. Large datagrams can require IP fragmentation, so support at this size does not guarantee delivery on every network. Packet loss and RPC timeouts still apply.

Replication factor `K` changes how many peers hold copies, not the maximum value size. At most `K` peers store a value when enough peers are available. Stored data is held in memory; there is no fixed total storage quota or persistence across node restarts. Larger objects require caller-managed chunking or a different transport/protocol.

Run the size tests from `labs`:

```powershell
go test ./kademlia -run 'Test(StoreValueSizeBoundaries|IncomingStoreValueSizeBoundaries|ValueSizeRemoteRoundTrip|LookupRejectsOversizedRemoteValue)' -v
```

The tests cover local storage and lookup, incoming STORE acceptance/rejection, binary payloads, and remote STORE/FIND_VALUE round trips over both real loopback UDP and simulation at boundary sizes. The maximum-size UDP case uses a remote replica, so it cannot pass through a local cache hit.
