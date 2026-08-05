# Entcoin mainnet protocol

This document describes the v1.3.0 implementation. The consensus/network
identity is `entropy-mainnet-v1`. The HTTP path prefix remains `/v2` because it
denotes the second transport design; endpoint numbering and network identity
are not the same version counter.

The network identity is the genesis and consensus compatibility boundary. This
protocol may change incompatibly in a later release, is not an authenticated
payment API, and has not received an independent security audit.

## Consensus anchor and domain separation

```text
Network identity     entropy-mainnet-v1
Genesis height       0
Genesis timestamp    1783983600 (2026-07-13 23:00:00 UTC)
Genesis reward       0 ENT
Genesis hash         f58101a2332dbffff670b4b2f8d08deea08883e0719df9b008b7eb1c8d5b2f0e
```

The deterministic binary encoding for every transaction starts with the network
identity. Both the input signing digest and the final transaction ID use that
encoding; coinbase IDs use the same transaction-ID path. Block-header hashing
also starts with the network identity, and the Merkle root commits to the
domain-separated transaction IDs. Network separation therefore covers
signatures, regular and coinbase transaction IDs, and block hashes in addition
to peer protocol checks and the fixed genesis.

## Height-gated consensus rules

Blocks below height `125555` retain version 1, MTP11 plus 120-second future
timestamp validation, and the legacy 60-block epoch DAA. Blocks at height
`125555` and later require version 2, additionally require a timestamp greater
than the immediately preceding block, and use the integer ASERT rule specified
in [the v1.2.2 consensus audit](consensus-upgrade-audit-v1.2.md). Unknown or
wrong-side block versions are rejected.

The v2 rule uses anchor height `123265`, anchor timestamp `1785201853`, anchor
difficulty 35, and a 600-second half-life. Its activation-height on-schedule
timestamp is `1785224753`. Network identity, genesis, block hashing, transaction
encoding, issuance, and cumulative-work fork choice remain unchanged.

## Transport overview

```text
LAN discovery       UDP multicast announcement
Catch-up sync       bounded HTTP JSON requests
Live relay          WebSocket JSON messages
Fallback relay      HTTP transaction/block POST
Peer candidates     bounded HTTP GET
```

Desktop nodes and default CLI nodes also fetch a bounded, versioned bootstrap
manifest from built-in HTTPS repository and CDN URLs:

```json
{
  "version": 1,
  "protocol": "entropy-mainnet-v1",
  "peers": ["https://template-chat.xyz"]
}
```

Manifest URLs locate candidate peers; they do not distribute chain state or
alter consensus. Public manifest peers must use HTTPS on port 443, and received
objects are validated exactly like data from manual peers. The checked-in
manifest publishes an externally verified archive seed, allowing automatic
cross-internet discovery. Empty or unavailable manifests do not prevent local
startup.

The built-in server listens on plain TCP, normally port `47821`. A peer base URL
is `http://host:port` or `https://host:port` without a path. HTTPS peers map to
WSS for the real-time channel, but TLS must be supplied by an operator's reverse
proxy because the node does not terminate TLS itself.

All JSON is size-limited and decoded against explicit structures. Unknown,
duplicate, trailing, and excessively nested data is rejected. Amounts and
nonces are JSON integers; hashes and cumulative work are encoded as strings in
the fields shown below.

## Chain status

`GET /v2/status` returns:

```json
{
  "protocol": "entropy-mainnet-v1",
  "name": "Entcoin",
  "symbol": "ENT",
  "height": 123,
  "tip_hash": "...",
  "chain_work": "...",
  "listen_port": 47821
}
```

`chain_work` is an unsigned base-10 integer. A node rejects status with a
different protocol/name/symbol, empty tip hash, or malformed work. Status is
informational; it never causes direct state replacement.

`GET /v2/consensus` separately reports supported block rules, the next block's
rule, activation height, anchor, algorithm, and half-life. Consensus fields are
intentionally absent from `/v2/status` and WebSocket status messages because
v1.1 peers strictly reject unknown JSON fields.

```json
{
  "protocol": "entropy-mainnet-v1",
  "supported_rules": [1, 2],
  "next_block_rule": 1,
  "activation_height": 125555,
  "anchor_height": 123265,
  "anchor_hash": "000000000b6ffc20400cafe308ae13a73cead4c5a7cb232214d714ff2949dead",
  "algorithm": "integer-asert",
  "half_life_seconds": 600
}
```

## Block locator and headers

`POST /v2/headers` accepts one to 64 active-chain hashes, newest first, plus a
batch limit from 1 to 2,000:

```json
{
  "locator": ["newest-known-hash", "older-hash", "genesis-hash"],
  "limit": 2000
}
```

The normal locator contains the newest ten consecutive ancestors, then steps
back exponentially, and always ends with genesis. The server selects the first
hash it has on its active chain and returns:

```json
{
  "protocol": "entropy-mainnet-v1",
  "common_height": 120,
  "common_hash": "...",
  "headers": []
}
```

Each item in `headers` uses the normal block JSON fields but has no transaction
body. The receiver checks continuity, expected difficulty, median-time-past,
future-time bound, Merkle commitment, header hash, proof of work, and cumulative
work before requesting bodies. A response that changes its common point during
pagination is rejected. One synchronization attempt validates at most 20,000
headers. A peer that starts from an unnecessarily old locator cannot force a
redownload: headers identical to the local active prefix are folded into the
effective common ancestor.

## Block bodies

`POST /v2/blocks` accepts one to 8 unique hashes:

```json
{
  "hashes": ["block-hash-1", "block-hash-2"]
}
```

The response preserves request order:

```json
{
  "protocol": "entropy-mainnet-v1",
  "blocks": []
}
```

Each body must exactly match its requested/validated header. A missing hash
returns `404 Not Found`; a body deleted by pruning returns `410 Gone`. Archive
nodes are therefore required for genesis-to-tip bootstrap.

The synchronizer fetches body batches only after the candidate header chain can
beat local cumulative work. It then disconnects the old branch, validates and
connects the candidate bodies, checks higher cumulative work, rebuilds the
mempool, and commits in one SQLite transaction. Any invalid body or database
error rolls back the entire replacement.

A direct extension commits at most one 8-block body chunk per chain-sync call,
so a slow peer still makes resumable progress. A scheduled sync session uses a
30-second context and attempts at most 32 chunks, for at most 256 directly
extending blocks. It stops starting chunks when less than five seconds remain.
A non-direct candidate is staged in a single protected temporary file with a
512 MiB ceiling before the atomic reorg. Candidates beyond the 20,000-header or
512 MiB safety boundary are rejected; operators must use a closer archive peer
or rebuild the local database.

## Mempool catch-up

`GET /v2/mempool?limit=N&offset=M` returns up to 64 validated pending transactions.
Clients advance the zero-based offset across bounded sync rounds and reset it
when a short page is returned:

```json
{
  "protocol": "entropy-mainnet-v1",
  "transactions": []
}
```

The receiver runs each transaction through its own UTXO, signature, maturity,
fee, conflict, and resource validation. A sender cannot force an entry into the
remote mempool by including it in this response. Local relay policy limits the
entire mempool to 5,000 transactions, 32 MiB, and 20,000 total inputs. The
minimum relay fee is 1,000 atomic units per started KiB; miners may still include
otherwise consensus-valid transactions under their own policy.

## HTTP relay

`POST /v2/transactions` accepts one signed transaction. Success returns HTTP
`202 Accepted` and its transaction ID. Invalid, duplicate, or conflicting data
returns `409 Conflict`.

`POST /v2/block` accepts one complete block. Success returns HTTP `202 Accepted`
and its block hash. Invalid or nonconnecting data returns `409 Conflict`.

These endpoints are the fallback and fan-out path. They call the same ledger
validation used by local wallet submissions, mining, and synchronization.

The v0.1 `GET/POST /v1/state` endpoint is intentionally absent. A mainnet peer
never accepts another node's serialized whole-chain/whole-mempool state.

## WebSocket live relay

`GET /v2/p2p` upgrades to WebSocket. Direct peer clients omit the browser
`Origin` header. The connection begins with a `hello` message:

```json
{
  "type": "hello",
  "protocol": "entropy-mainnet-v1",
  "node_id": "ent1...",
  "listen_port": 47821,
  "status": {
    "protocol": "entropy-mainnet-v1",
    "name": "Entcoin",
    "symbol": "ENT",
    "height": 123,
    "tip_hash": "...",
    "chain_work": "...",
    "listen_port": 47821
  }
}
```

`node_id` is currently the node wallet address and is not an authenticated peer
identity. A receiver rejects an empty/self node ID or an incompatible protocol.
For an inbound connection, the observed remote IP plus advertised listen port
may be recorded as a discovered peer.

Supported message types are:

| Type | Payload | Behavior |
| --- | --- | --- |
| `hello` | node ID, listen port, optional status | establish compatibility and peer address |
| `status` | status object | update peer height and liveness |
| `transaction` | complete transaction | validate, persist, and relay if accepted |
| `block` | complete block | validate and connect; trigger catch-up if needed |
| `ping` | optional capability marker | respond with protocol `pong` |
| `pong` | optional capability marker | keepalive acknowledgement or capability confirmation |
| `reconcile_headers_request` | request ID and bounded locator request | request headers through the existing socket |
| `reconcile_headers_response` | request ID and header response | return the common point and headers |
| `reconcile_blocks_request` | request ID and up to 2 hashes | request candidate block bodies |
| `reconcile_block` | request ID, part metadata, and one block | return one requested body per bounded frame |
| `reconcile_mempool_request` | request ID, limit, and offset | request at most 8 pending transactions |
| `reconcile_mempool_response` | request ID and transactions | return one bounded mempool page |
| `reconcile_error` | request ID and bounded error | fail one request without accepting partial state |

Reconcile messages are capability-gated for compatibility. After `hello`, a
A v1 peer sends an ordinary `ping` whose existing `node_id` field contains
`entropy-reconcile-v1`. A supporting peer echoes that marker in `pong`. Older
implementations accept the known `ping` fields and return an unmarked `pong`, so
the new node continues using HTTP and live gossip and never sends them an
unknown reconcile message.

The implementation initiates reverse reconciliation on an inbound socket. This
lets a reachable seed request an outbound-only/NAT peer's chain and mempool over
the connection that peer already opened. Headers still pass the normal locator,
proof-of-work, continuity, difficulty, and cumulative-work checks; bodies still
pass exact header matching and atomic ledger replacement; transactions still
pass normal mempool policy. A remote status is only a trigger and never changes
the ledger by itself.
An incomplete but progressing round schedules another after the minimum
interval. Successful outbound sync polls also send a fresh `status`, allowing
later bounded rounds to continue a large paged backlog until both sides
converge.

Request IDs are numeric and at most 20 characters. A socket permits at most two
pending requests, one active reverse round no more often than every five
seconds, a 20-second request timeout, and a 30-second round timeout. One round
requests at most eight mempool pages of eight transactions. Header sync retains
the 2,000-header page, 20,000-header total, 32-chunk round, and 512 MiB staged
fork limits. The internal downloader handles up to eight bodies at a time but
splits them into WebSocket requests of at most two unique hashes; each response
frame carries one block. The receive burst covers one complete eight-body
internal batch, while each socket's queued encoded output is capped at roughly
two maximum protocol frames plus 64 KiB. Reconcile errors carry a bounded code;
invalid responses close the connection, while busy, unavailable, and pruned
responses use bounded exponential backoff. The realtime validation budget,
global heavy-request slots, and byte-bounded send queue apply in addition to
these reconcile-specific limits.

Unknown types, binary frames, oversized messages, malformed JSON, and protocol
mismatches close the connection. The implementation sends WebSocket control
pings every 20 seconds and requires activity/pong within the read deadline.
The bounded send queue may drop a live relay message for a slow peer instead of
blocking consensus or wallet actions; periodic HTTP synchronization and, when
negotiated, same-connection reverse reconciliation repair missed chain and
mempool data.

## LAN discovery

Nodes listen and announce on:

```text
239.255.78.21:47822/UDP
```

An announcement is at most 2 KiB:

```json
{
  "protocol": "entropy-mainnet-v1",
  "node_id": "ent1...",
  "port": 47821
}
```

The receiver uses the datagram's source IP, not an advertised host string, and
combines it with the validated port. It ignores self, incompatible, malformed,
and out-of-range announcements. Discovery is limited to multicast-capable LANs
and is disabled by `--no-discovery`.

## Public peer exchange

`GET /v2/peers` returns a bounded candidate set:

```json
{
  "protocol": "entropy-mainnet-v1",
  "peers": []
}
```

The response contains at most 16 peers. The server includes only globally
routable public peers that are online, have no current failure, were successful
within the last two minutes, and are selected as active outbound connections.
It excludes candidates with the requester's source IP.

This endpoint is untrusted discovery. Each received URL must use HTTP or HTTPS,
contain a globally routable IP literal and explicit port, and pass the normal
URL parser. DNS names, private, loopback, link-local, multicast, documentation,
benchmark, transition, unspecified, and other reserved/special-use address
ranges are rejected. A controlled HTTPS bootstrap manifest may instead use a
validated public FQDN on port 443.

Nodes retain at most 24 automatically exchanged public candidates and 48
discovered peers overall. Eight outbound peers are active by default. Peer
exchange is optional for compatibility: `404 Not Found` and `405 Method Not
Allowed` from older nodes are treated as absence of the endpoint, not as a peer
failure.

## Peer lifecycle and limits

Manual and discovered peers persist in SQLite. Manual status survives later LAN
rediscovery. A failed peer becomes offline and uses exponential retry:

```text
1s, 2s, 4s, 8s, ... up to 5 minutes
```

A successful request resets failures, last error, and next-attempt time. The
current node caps the stored peer set and live WebSocket set at 64 each.

Inbound HTTP work is limited to 32 concurrent requests globally and eight per
source IP. WebSockets are limited to 32 globally and four per source IP. Server
header/read/write/idle timeouts and per-message limits bound stalled clients.
The limits are implementation policy, not consensus, and may change without a
network reset.

## Security and privacy properties

- Every received transaction, body, and header is locally validated.
- Peer status and node ID are untrusted hints.
- P2P has no bearer token, mutual authentication, or built-in encryption.
- The wallet private key and recovery phrase never appear in protocol messages.
- The node wallet address is exposed as `node_id`, so peers can correlate node
  connectivity with that address.
- IP addresses, manually configured URLs, failure times, and errors persist in
  the local database.
- HTTPS manifest support, an active public archive seed, and an optional
  Windows seed deployment package are included. No automatic NAT traversal or
  anonymity layer is included.

Connection limits and strict decoding are defensive controls, not audit
evidence. Report sensitive issues through the process in
[SECURITY.md](../SECURITY.md).
