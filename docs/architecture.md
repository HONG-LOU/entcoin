# Entcoin v1.2.2 architecture

## Scope

Entcoin v1.2.2 implements the `entropy-mainnet-v1` network. It is designed so
one Windows or Ubuntu application can be a wallet, a full validator, a relaying
peer, and an optional proof-of-work miner without an external database service.

The phrase "full node" means that the node independently verifies consensus
rules before changing its active ledger. The retained `entropy-mainnet-v1`
identifier is the compatibility boundary for the Entcoin genesis and consensus
rules.

## Process layout

The desktop and CLI use the same Go service and consensus code:

```text
Wails desktop UI                     cmd/entcoin CLI
        |                                  |
        +----------------+-----------------+
                         |
                  internal/node
          lifecycle, mining, peers, sync,
           HTTP/WebSocket, LAN discovery
                  /       |       \
                 /        |        \
        internal/core  internal/ledger  internal/vault
        consensus and  SQLite ledger,   OS-protected wallet,
        cryptography    UTXO and undo    backup/recovery
                            |
                     internal/store
              lock and wallet migration
```

The browser UI never receives the private key. Wails calls enter Go, and the Go
node signs locally. The public P2P listener exposes chain and mempool messages,
not wallet-control methods.

## Consensus identity and objects

The network identity is `entropy-mainnet-v1`. Nodes reject a peer status,
discovery message, or WebSocket message with another protocol identity.

The fixed mainnet anchor is:

```text
Genesis height       0
Genesis timestamp    1783983600 (2026-07-13 23:00:00 UTC)
Genesis reward       0 ENT
Genesis hash         f58101a2332dbffff670b4b2f8d08deea08883e0719df9b008b7eb1c8d5b2f0e
```

Consensus hashes are also domain-separated, rather than relying only on the
genesis check. The deterministic transaction encoder begins with
`entropy-mainnet-v1`; therefore each input signing digest, regular transaction
ID, and coinbase transaction ID binds the network. The block-header hash begins
with the same identity, while its Merkle root commits to already separated
transaction IDs. A testnet signature, transaction ID, coinbase ID, or block hash
cannot be replayed as an otherwise identical mainnet object.

A regular transaction contains a random nonce, inputs, and outputs. Each input
references a previous output and carries a SEC1 P-256 public key and an ASN.1
DER ECDSA signature. Signatures use low-S normalization. The signing digest
uses deterministic big-endian encoding and binds:

- every input outpoint and public key;
- every output address and amount;
- the index of the input being signed;
- the amount and address of that input's referenced output.

The transaction ID hashes the complete signed transaction. An address is a
checksummed `ent1...` encoding derived from the first 20 bytes of the public
key's SHA-256 hash plus a four-byte checksum.

A block header binds version, height, timestamp, previous hash, Merkle root,
difficulty, nonce, and resulting hash. Proof of work requires the SHA-256 header
hash to have at least `difficulty` leading zero bits. A block contributes
`2^difficulty` work. Fork choice compares cumulative work, never height alone.

Below height `125555`, timestamps must exceed median-time-past over the previous
11 blocks and remain no more than 120 seconds ahead of local time. Difficulty
begins at 22 leading zero bits, first adjusts at height 120, and then adjusts
every 60 blocks. At height `125555`, centralized `RulesAtHeight` selection
requires block version 2, timestamps greater than both MTP11 and the previous
block, and the fixed-anchor 600-second integer ASERT rule. Historical blocks
always replay with version-1 rules; unknown or wrong-side versions fail closed.
The v1.2 audit documents the formula and fixed vectors. Neither DAA has received
the independent economic review expected of a mature production network.

Deterministic resource limits include a 1 MiB block, 64 KiB transaction, 2,000
transactions per block, 256 inputs and outputs per transaction, and 5,000
pending transactions. Local relay policy further caps the mempool at 32 MiB
and 20,000 total inputs and requires at least 1,000 atomic units (0.00001000
ENT) of fee per started KiB. The desktop builds the transaction, measures its
encoded size, and automatically selects this minimum; CLI callers may
voluntarily pay more. Miners prioritize fee rate while preserving
pending parent-before-child dependencies. Those mempool limits and ordering
rules are policy, not block-consensus rules.

## Issuance and coinbase maturity

Consensus selects subsidy by height rather than local time:

```text
N    = 10 * 365 * 24 * 60 * 60 / 10 = 31,536,000 blocks
MAX  = 2,000,000 * 100,000,000       = 200,000,000,000,000 units
BASE = floor(MAX / N)                 = 6,341,958 units
REM  = MAX mod N                      = 12,512,000 units

subsidy(h) = 0                    when h == 0 or h > N
             BASE + 1             when 1 <= h <= REM
             BASE                 when REM < h <= N
```

The fixed genesis block has no reward and there is no premine. Fees are
existing value transferred into coinbase and cannot increase total issuance.
The terminal reward height sums exactly to 2,000,000 ENT.

Coinbase maturity is 100 blocks beginning with the first reward block at height
1. At every reward-bearing height, spending a coinbase requires:

```text
spending_height - coinbase_height >= 100
```

For example, the height-1 coinbase is rejected at spending height 100 and
accepted at height 101. A coinbase created at height 100 becomes spendable at
height 200.

## Validation pipeline

The ledger validates before committing. For a new block the important order is:

1. Check chain identity, expected height, previous hash, expected difficulty,
   timestamp, Merkle root, header hash, proof of work, and size limits.
2. Begin one SQLite write transaction and read the active UTXO records needed
   by each regular transaction.
3. Reject missing, duplicate, already spent, immature, or conflicting inputs.
4. Verify ownership, signature, transaction ID, address, positive outputs,
   integer overflow, and `outputs <= inputs`.
5. Accumulate fees and verify exactly one coinbase paying subsidy plus fees.
6. Remove spent UTXOs, insert created UTXOs, indexes, bodies, cumulative work,
   and an undo record.
7. Remove confirmed mempool entries, revalidate remaining entries, and commit.

Any failure rolls back the whole SQLite transaction. Mining and P2P use this
same commit path; neither can bypass validation.

## SQLite ledger

The ledger is `%LOCALAPPDATA%\Entcoin\mainnet-v1\entropy.db` on Windows and
`~/.config/Entcoin/mainnet-v1/entropy.db` on Ubuntu. It uses the pure-Go
`modernc.org/sqlite` driver. Connections set:

```text
journal_mode = WAL
synchronous  = FULL
foreign_keys = ON
busy_timeout = 5000 ms
trusted_schema = OFF
```

Startup creates or migrates the schema and verifies the fixed genesis block.
It runs `PRAGMA quick_check` on first or unclean startup; clean shutdown records
a checkpointed session marker so routine starts do not rescan a ten-year file.
The database stores:

- active-chain headers, bodies, encoded size, and cumulative work;
- transaction bodies and per-address sent/received indexes;
- the current UTXO set with creation height and coinbase flag;
- one undo record for every retained non-genesis block;
- the validated mempool and its spent/created output indexes;
- configured and discovered peers, retry state, and errors;
- health events and storage-policy metadata.

Consensus work and nonces use explicit big-endian byte encodings where SQLite's
signed integer range is unsuitable. Amount validation prevents values above the
signed range from entering SQL columns.

One operating-system `node.lock` protects each data directory. A desktop app,
CLI command, or second process cannot concurrently write the same ledger.
Clean shutdown checkpoints and truncates the WAL before closing.

## Reorganization and undo

Each connected block records both the UTXOs it consumed and the outpoints it
created. A higher-work replacement is applied in one database transaction:

```text
remote block locator
        |
        v
find common active-chain ancestor
        |
        v
validate candidate headers and cumulative work
        |
        v
fetch missing bodies in bounded batches
        |
        v
BEGIN SQLite transaction
  disconnect old tip to ancestor using undo records
  connect candidate blocks in forward order
  require candidate cumulative work > old cumulative work
  revalidate orphaned and existing mempool transactions
COMMIT, or ROLLBACK everything
```

Transactions orphaned from the old branch return to the mempool only if they
remain valid against the replacement UTXO set. The candidate never becomes
visible as a partially applied chain.

## Mining consistency

A mining job snapshots the expected tip, pending transactions, fee total,
timestamp rule, and target difficulty. CPU workers search nonces concurrently.
Receiving or committing a new tip cancels every job built on the old tip so
continuous mining immediately builds a fresh template. After proof of work is
found, `CommitMinedBlock` still requires the active tip to equal the snapshot
tip and then runs normal block validation.

After activation, a mining job checks every five seconds whether advancing its
candidate timestamp would lower ASERT difficulty. It cancels and transparently
rebuilds only at an actual integer difficulty boundary. A chain-tip change still
cancels the candidate immediately, and a found block commits only against its
captured tip. Pre-activation mining behavior is unchanged.

Mining is always opt-in. An outbound-only node validates and relays without
mining, and a miner is not a network coordinator.

## Incremental peer synchronization

The mainnet protocol separates catch-up from real-time relay:

- HTTP provides status, block locators, header batches, requested body batches,
  bounded mempool catch-up, and a fallback transaction/block submission path.
- WebSocket `/v2/p2p` carries hello/status, transaction, block, ping, and pong
  messages without waiting for the next sync poll. Peers that negotiate the
  reconcile capability also tunnel bounded header, body, and mempool requests
  through this already-established connection.
- LAN multicast announces the node ID and TCP listen port on
  `239.255.78.21:47822/UDP`.

The synchronizer requests headers first, validates them locally, computes the
candidate work, and downloads bodies only for a chain that can beat local work.
Bodies must match the requested headers and are fully validated during the
atomic reorg. The removed v0.1 `/v1/state` endpoint cannot replace local state.

The reverse reconcile path fixes the asymmetric NAT case. After an outbound-only
node reconnects, the inbound peer uses that same WebSocket to request the
outbound node's headers, selected block bodies, and paged mempool. It therefore
converges on offline transactions and a stronger remote branch without dialing
the advertised listen port. This is not NAT traversal: an initially isolated
node still needs the address of at least one reachable peer.
An incomplete but progressing round schedules a bounded follow-up. Successful
outbound sync polls also send a current status over the live socket, so large
paged backlogs continue until convergence.

A scheduled HTTP peer-sync session has a two-minute context. A direct extension
requests 128 headers, validates the page once, then reuses it across sixteen
requests of at most eight block bodies before one atomic commit. One session
attempts at most 32 such chunks and stops starting chunks when less than five
seconds remain. The 512 MiB staging ceiling remains independent of peer input.
One WebSocket reconcile round has the same 30-second bound, at most two pending
correlated requests, at most 64 mempool transactions, and no more than one
active round per socket. The internal downloader groups up to eight bodies, then
splits them into socket requests of at most two hashes and returns one complete
block per bounded frame. The receive burst covers the full internal body group,
while queued encoded output is capped at about two maximum frames per socket.
Invalid reconcile responses close the connection; transient busy, unavailable,
and pruned responses back off. The existing 20,000-header and 512 MiB staged
fork ceilings still apply.

Peer failures persist in SQLite. Retry starts at one second, doubles after each
failure, and caps at five minutes. Success resets the failure count. Global and
per-IP request/WebSocket limits, cross-reconnect invalid-message scoring,
strict JSON decoding, bounded response/staging bytes, timeouts, and a 64-peer
cap bound common resource-exhaustion paths. These controls reduce risk but are
not a substitute for hostile-network auditing.

Public peer exchange is deliberately narrower than manual or manifest
configuration. `GET /v2/peers` returns at most 16 recently successful, online,
active outbound peers. Untrusted exchanged candidates must be globally routable
IP literals with explicit ports; DNS names, private/link-local/multicast and
reserved/special-use addresses are rejected. The node retains at most 24 such
public discovered candidates, 48 discovered peers in total, and activates eight
outbound peers by default. An HTTPS bootstrap manifest is operator-controlled
and may name a validated public FQDN. A peer returning `404` or `405` for the
optional exchange endpoint remains compatible and is not failed for that reason.

At startup, desktop and default CLI configurations fetch the versioned mainnet
bootstrap manifest over HTTPS from the public repository and a CDN mirror. A
manifest is only a bounded peer-location hint: every peer and every received
object is still validated locally, and a manifest cannot change consensus. The
mainnet manifest publishes `https://template-chat.xyz` and
`https://node.entcoin.xyz` as archive seeds. It refreshes every six hours, so
seed changes do not require an application release. Startup continues with
LAN/manual peers when every manifest source is empty or unavailable.

See [Mainnet protocol](protocol.md) for endpoint and message details.

## Archive and pruned nodes

Both modes retain all headers, cumulative work, the current UTXO set, address
indexes, mempool, and peers. The difference is historical body and undo
retention.

An archive node retains all block and transaction bodies and all undo records.
It can serve a new peer from genesis and can reorganize anywhere in its stored
history.

A pruned node permanently clears old block and transaction bodies and deletes
old undo records while retaining the most recent configured window. The prune
depth is persisted in SQLite: `0` means archive for future blocks, while
`120..31,536,000` is the retained complete-body horizon. Deleted data does not
reappear merely by switching to archive mode.

A pruned node continues to validate and relay all new data, but it returns HTTP
`410 Gone` for deleted bodies and rejects a reorg crossing its prune horizon.
It must resynchronize from an archive peer to recover from such a deep fork or
to regain historical bodies. Public bootstrap nodes should therefore be
archives. The public-seed deployment package enforces archive mode. A fresh
desktop ledger instead starts with a 20,000-block prune depth and then respects
its persisted storage policy; fresh CLI ledgers default to archive unless
configured otherwise.

Public infrastructure can run the CLI in explicit seed mode. Seed mode forces
archive retention and refuses a ledger whose historical bodies were already
pruned. It uses a fresh in-memory P-256 identity for protocol fields on every
start, writes no wallet artifact, and rejects transaction creation, mining,
recovery, backup, and restore operations. Validation, synchronization, peer
exchange, transaction relay, and block relay use the same code paths as a
wallet node.

## Wallet vault and recovery

New wallets use 256 bits of entropy encoded as 24 English BIP39 words. Entcoin
then deterministically derives a P-256 private scalar using the versioned
`entropy-p256-bip39-hmac-sha256-v1` derivation. This is not BIP32, SLIP-0010, or
a Bitcoin wallet path; importing the phrase into unrelated wallet software
will not produce the Entcoin address.

Each local wallet profile uses Windows user-scope DPAPI or Linux Secret Service
plus XChaCha20-Poly1305. `wallet.vault` remains the active restart pointer while
the `wallets` directory retains protected profiles. They open automatically
only for the same OS account and are not portable backups. The Linux envelope
authenticates its public wallet descriptor and random nonce; its 256-bit master
key remains in the desktop keyring. A `.entwallet` backup uses:

```text
KDF       Argon2id v1.3, time=3, memory=64 MiB, threads=2
Cipher    XChaCha20-Poly1305
Password  12 to 1024 UTF-8 bytes
```

Envelope metadata is authenticated, imports reject hostile KDF parameters
before allocating memory, and one process permits only one password KDF at a
time. Local and portable writes use temporary files, flush, and atomic replace
semantics. A corrupt or missing vault never causes silent wallet regeneration.

Creating or importing a wallet preserves existing profiles and activates the
new address. Switching is blocked while mining or before the current profile's
recovery is confirmed. The ledger is shared and remains open, so profile
changes do not resynchronize chain state. Local OS protection applies to wallet
nodes; seed mode deliberately has no persistent wallet or recovery secret and
is therefore portable to supported servers.

## Testnet isolation and wallet recovery

Mainnet uses both a new reward-free genesis and the isolated
`%LOCALAPPDATA%\Entcoin\mainnet-v1` directory. The ledger rejects a published
testnet protocol or genesis before migration or schema work. Testnet
`chain.json`, SQLite databases, mempools, peers, balances, and histories are
never imported or replayed into mainnet.

Wallet material is recoverable independently of chain state. A user may restore
a known 24-word Entcoin phrase or verified `.entwallet` backup in the mainnet
desktop application. This recovers the P-256 key and address, while mainnet
balances and history come only from mainnet blocks. A v0.1 plaintext wallet can
first be converted with `wallet-migrate` against a copy of its old directory;
its resulting encrypted portable backup is the only application-level recovery
copy because legacy keys have no BIP39 phrase.

## Security boundary

The implementation verifies signatures, proof of work, monetary policy,
coinbase maturity, timestamps, size limits, UTXO ownership, and cumulative-work
fork choice. Private keys stay outside P2P and frontend bindings.

It does not yet provide independent audit evidence, authenticated peers,
built-in node-side transport encryption, automatic NAT traversal, signed
binaries, reproducible builds, or a mature protocol-upgrade process. HTTPS
manifest delivery and the optional reverse-proxied seed package help discovery
and transport deployment; they are not audit evidence or consensus authorities,
and the two published archive seeds do not override local validation. P-256 and
the compact fast-chain DAA are project-specific design choices, not claims of
Bitcoin compatibility. Read [SECURITY.md](../SECURITY.md) before exposing a node
or handling wallet material.
