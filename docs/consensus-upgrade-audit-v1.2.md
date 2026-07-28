# Entcoin v1.2.0 consensus upgrade audit

Date: 2026-07-28

## Executive conclusion

The current difficulty value of 35 is valid under the implemented mainnet
rules. The implementation and the documented step-function DAA agree. The
defect is in the rule design: a rapid hash-rate increase can raise difficulty
much faster than the remaining miners can lower it after that hash rate leaves.
At the observed pre-event hash rate, returning from difficulty 35 to 27 can take
about 83 hours.

The DAA replacement is the only confirmed current defect that requires old
nodes to accept blocks they presently reject, and therefore the only confirmed
independent hard-fork fix. This audit found no second live-mainnet defect in
issuance, transaction validation, coinbase maturity, proof-of-work accounting,
fork choice, or reorganization that must be repaired by a hard fork.

The v1.2.0 fork includes a centralized, height-gated rule selection mechanism.
That mechanism is not a second chain defect; it is required
upgrade infrastructure. Without it, this fork and every later consensus change
would remain scattered across constants, mining, header validation, sync, and
status reporting.

Timestamp behavior is changed and tested together with the new DAA.
The current rule permits a block timestamp to be lower than its immediate
predecessor as long as it exceeds median-time-past, and a miner controlling six
of eleven timestamps controls the median. This is an input weakness of the DAA,
not evidence of an existing invalid block. A monotonic timestamp restriction is
a tightening of validity and could be deployed as a soft fork in isolation, but
there is no reason to separate it from the already-required DAA activation.

## Evidence for the incident

The dominant miner appeared near height 119717 on 2026-07-27 21:48:52 Beijing
time. Subsequent adjustments were `27 -> 29 -> 31 -> 33 -> 34 -> 35`. The
60-height median-time spans that drove those changes were approximately 10, 10,
63, 173, and 266 seconds. Each result follows the current consensus thresholds.

Before the event, a representative difficulty-27 window spanned 882 seconds.
Scaling that observed rate gives the following approximate recovery times if
the dominant hash rate disappears:

| Window | Approximate time |
| --- | ---: |
| difficulty 35 | 62.72 hours |
| difficulty 33 | 15.68 hours |
| difficulty 31 | 3.92 hours |
| difficulty 29 | 0.98 hours |
| total to difficulty 27 | 83.30 hours |

The asymmetry comes from fixed 60-block epochs and a maximum decrease of two
bits per epoch. A slow network must first produce the expensive epoch before it
receives relief.

## Consensus surface review

| Rule surface | Authoritative implementation | Validation paths reviewed | Result |
| --- | --- | --- | --- |
| Chain identity and genesis | `core.NetworkID`, `GenesisBlock` | state replay, ledger open/import, peer protocol | No defect found |
| Header continuity and PoW | `validateHeader` | direct extension, staged sync, reorg, mined commit | No divergence found |
| Difficulty | `expectedDifficulty` | state replay, ledger, mining, header-first sync | Liveness defect; hard fork required |
| Timestamp | MTP11 and local `now + 120` | mining, direct extension, sync, reorg | Miner influence must be bounded in new DAA design |
| Chain work and fork choice | `2^difficulty`, strict greater work | state, ledger, header sync, atomic reorg | No defect found |
| Transaction and signature validity | deterministic encoding and P-256 ECDSA | state, ledger, mempool, mining, reorg rebuild | No bypass or path divergence found |
| Issuance and fees | `Subsidy`, exact coinbase equality | state replay and ledger connect | Exact 2,000,000 ENT cap; no defect found |
| Coinbase maturity | `IsCoinbaseMature` | state, ledger, mempool, mining, reorg | No divergence found |
| Size and count limits | core consensus constants | full block validation on every body path | No divergence found |
| Storage transition | SQLite transaction plus hash-bound undo | direct extension, import, mined commit, reorg | Atomic; no consensus defect found |
| Pruning | retained-body horizon | sync and reorg refusal below horizon | Operational liveness limit, not consensus |

All live block-body entry points converge on `connectBlock`. Replacement chains
are connected through the same function inside one database transaction. Header
sync validates continuity, expected difficulty, timestamps, hashes, and work
before downloading bodies; each body must match the requested header and is then
fully validated during connection. The legacy in-memory replay applies the same
header, transaction, fee, reward, maturity, and ordering rules.

Passing these checks does not prove economic safety or replace an independent
implementation. It does rule out the currently suspected class of path-specific
acceptance differences in the reviewed code.

## Activated rule specification

- Activation height is `160000`. Heights below it use block version 1 and the
  unchanged 60-block epoch DAA. Height `160000` and later use block version 2.
- The fixed numerical anchor is height `123265`, timestamp `1785201853`,
  difficulty 35, and hash
  `000000000b6ffc20400cafe308ae13a73cead4c5a7cb232214d714ff2949dead`.
- For candidate height `h` and timestamp `t`, compute
  `deviation = (h - 123265) * 10 - (t - 1785201853)`. The signed correction is
  `deviation / 600`, rounded to the nearest integer with exact halves away from
  zero. Difficulty is `35 + correction`, clamped to the existing range 8..48.
- Version-2 timestamps must exceed both MTP11 and the immediately preceding
  block timestamp. The existing local acceptance limit of validation time plus
  120 seconds remains. The exact candidate timestamp is the DAA time input.
- Version 1 remains valid only below activation, version 2 is required at and
  above activation, and every other version fails closed.
- `NetworkID`, genesis, transaction encoding, wallet derivation, addresses,
  amounts, ledger schema, and cumulative-work fork choice are unchanged.

The 600-second half-life lowers difficulty by one bit per ten minutes of
schedule delay. A complete stall from difficulty 35 reaches difficulty 27 after
4,800 seconds (80 minutes), without waiting for any block or epoch. In the
opposite direction, 600 consecutive minimum-timestamp blocks raise difficulty
by at least eight bits, bounding a sudden high-hash-rate arrival.

### Published vectors

| Height | Candidate timestamp | Expected difficulty |
| ---: | ---: | ---: |
| 160000 | 1785568903 | 36 |
| 160000 | 1785569083 | 35 |
| 160000 | 1785569203 | 35 |
| 160000 | 1785569323 | 35 |
| 160000 | 1785569503 | 34 |
| 160000 | 1785574003 | 27 |
| 160060 | 1785569203 | 36 |

The table deliberately includes activation-time candidate drift of minus and
plus 120 seconds. It cannot change difficulty on the on-schedule vector because
the nearest integer boundary is 300 seconds away.

## Deployment gates

- Consensus capabilities use a separate `GET /v2/consensus` endpoint. Existing
  `/v2/status` and WebSocket documents are byte-contract compatible with v1.1
  because old peers reject unknown JSON fields.
- Both public archive Seeds must be upgraded one at a time before activation and
  must agree on height, tip hash, and cumulative work after each restart.
- Release artifacts, checksums, provenance, mirrors, and the in-app update path
  must be complete before the public update manifest advertises v1.2.0.
- Operators who miss activation must stop the obsolete node, install v1.2.0,
  and restart against the same data directory. No wallet or ledger migration is
  required.

## Changes that are not currently required

The following would require a hard fork if chosen, but this audit found no
present defect requiring them:

- adding a UTXO or state root to the block header;
- protocol-level fee burning or a special burn address;
- changing the 10-second target, emission schedule, supply cap, block limits,
  signature scheme, address format, or transaction encoding;
- relaxing exact coinbase reward collection to allow miners to underclaim fees.

A trustless state snapshot commitment is useful future work, but a fixed trusted
snapshot hash plus background validation can be implemented without changing
historical block validity.

## Historical hard-fork question

Coinbase maturity was changed during testnet development, then set to activation
height 1 in commit `9a4150f` before the current mainnet genesis and first public
mainnet release. Every mainnet block has therefore been governed by the same
maturity rule. It is not unresolved mainnet fork debt and does not need to be
included in this activation.

## Verification completed

- Manual equivalence review of in-memory replay, SQLite block connection,
  mining candidate construction and commit, import, HTTP/WebSocket sync,
  staged body matching, reorganization, and mempool rebuilding.
- Exact regression coverage for DAA spans 150, 151, 300, 1200, 1201, and 2400
  seconds.
- Fixed anchor hash/PoW and published ASERT vectors, signed rounding boundaries,
  extreme timestamp clamps, version boundaries, monotonicity, rapid hash-rate
  arrival, and stalled-network recovery.
- Real SQLite connection, mining commit, equal-work rejection, rollback, and
  stronger-work reorganization across activation, plus HTTP header/body sync.
- A production archive-ledger online backup opened without migration in v1.2
  Seed mode, synchronized to the same public tip/work, passed SQLite checks,
  remained walletless, and shut down cleanly.
- `go test -count=1 ./...`
- `go test -race -count=1 ./internal/core ./internal/ledger ./internal/node`
- `go vet ./...`

Release publication, in-app upgrade, mirror validation, and rolling production
Seed deployment remain operational gates until the v1.2.0 rollout is complete.
