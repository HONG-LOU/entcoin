# Security policy

## Mainnet scope

Entcoin v1.4.0 uses the compatibility identity `entropy-mainnet-v1`. The word
`mainnet` identifies which genesis and consensus rules a node accepts; it does
not mean the implementation has received an independent security audit.

Cryptographic primitives and proof of work do not by themselves make a new
blockchain economically or operationally secure. Independent review, sustained
adversarial operation, trustworthy releases, and a mature response process are
still required.

## Supported versions

| Version | Status |
| --- | --- |
| `1.4.0` | Current release; includes EntPay without changing mainnet consensus |
| `1.2.1` - `1.2.2` | Consensus-compatible; upgrade for current application fixes |
| `1.2.0` | Superseded; activation-height mismatch at block 125555 |
| `1.1.x` | Superseded; cannot validate consensus v2 after block 125554 |
| `1.0.x` | Superseded; upgrade to v1.4.0 |
| `0.2.x` | Historical public testnet; unsupported |
| `0.1.x` | Educational testnet MVP; unsupported |

Protocol identity `entropy-mainnet-v1` and its genesis are intentionally
incompatible with every published testnet. Testnet chains are not migrated or
replayed. Restore only a verified wallet backup or recovery phrase into the
isolated mainnet data directory.

## Report a vulnerability

For a vulnerability that could expose wallet secrets, bypass consensus,
inflate supply, cause an invalid reorg, corrupt persistent state, or remotely
exhaust/crash nodes, use GitHub's private vulnerability report for this
repository:

<https://github.com/HONG-LOU/entcoin/security/advisories/new>

Do not include wallet phrases, private keys, live host credentials, or a
weaponized public exploit. If private vulnerability reporting is unavailable,
open a minimal issue requesting a private reporting channel without disclosing
technical exploit details.

Include when possible:

- affected commit, version, operating system, and build source;
- whether the desktop, CLI, wallet, ledger, consensus, or P2P path is affected;
- exact preconditions and minimal reproduction steps;
- expected versus observed behavior and security impact;
- logs or test fixtures with all secrets removed;
- whether the issue is already public or actively exploited.

Ordinary bugs, feature requests, documentation problems, and performance
questions can use public GitHub Issues. There is currently no bug-bounty or
guaranteed response-time program.

## Security boundaries

### Wallet

- The active local wallet is protected by Windows user-scope DPAPI and is
  decrypted inside the node process when running.
- On Linux, the active wallet is protected by XChaCha20-Poly1305 with a random
  master key stored in the current user's Secret Service keyring.
- A process or user with equivalent Windows-account access may be able to invoke
  DPAPI or inspect process memory. DPAPI is not protection from a compromised
  account or administrator.
- A process in the same unlocked Linux desktop session may request the Secret
  Service key or inspect process memory. Secret Service is not protection from
  a compromised account or administrator.
- New wallets have a 24-word BIP39 phrase with an Entcoin-specific P-256
  derivation. The phrase is the key; anyone who learns it controls the wallet.
- Portable `.entwallet` backups use Argon2id and XChaCha20-Poly1305. Their
  security still depends on a strong, secret password and uncompromised host.
- Migrated v0.1 wallets retain their original key and have no recovery phrase.
  Their verified encrypted backup is indispensable.
- A missing/corrupt vault fails closed and never triggers automatic generation
  of a replacement address.

### P2P listener

- TCP `47821` accepts untrusted HTTP and WebSocket input. Requests have size,
  time, concurrency, peer, and per-IP limits and received objects are locally
  validated.
- Direct P2P is neither encrypted nor authenticated. Peer URL, node ID, status,
  height, and chain-work claims are untrusted metadata.
- The public P2P endpoints do not expose send, restore, recovery phrase, mining,
  or private-key methods. Those are local desktop/CLI actions.
- LAN discovery is unauthenticated multicast and must be treated as a hint, not
  identity proof.
- Rate limits reduce obvious abuse but have not been independently tested as a
  complete denial-of-service defense.

### Consensus and persistence

- Blocks and transactions are validated against local UTXOs, signatures,
  maturity, size/timestamp rules, proof of work, and exact issuance.
- Fork choice uses cumulative work and reorg application is atomic in SQLite.
- SQLite uses WAL and full synchronization, per-block undo data, a process lock,
  and startup integrity checks. Hardware, filesystem, kernel, or storage-driver
  failures remain outside the application's control.
- Pruning intentionally removes historical bodies and undo records. A pruned
  node cannot serve deleted history or accept a reorg below its prune horizon.
- The 10-second target and compact leading-zero difficulty adjustment have not
  received the economic, game-theoretic, or long-duration review of established
  production networks.

## Known security limitations

- No independent consensus, cryptography, wallet, P2P, database, installer, or
  desktop-boundary audit has been completed.
- The node does not terminate TLS or authenticate peer identity. The optional
  public-seed deployment package supplies a reverse proxy, but there is no
  eclipse-resistant peer selection or automatic NAT traversal.
- Built-in HTTPS manifest delivery is a discovery mechanism, not a trust root or
  consensus authority. A published seed cannot change locally validated rules.
- Desktop update metadata is accepted only from exact `entcoin.xyz/update.json`
  or the official GitHub release feed. Installers may use exact versioned
  `template-chat.xyz/downloads/` and `entcoin.xyz/downloads/` mirrors, but the
  expected SHA-256 is fetched only from the matching official GitHub Release.
  Mirror compromise alone therefore cannot authorize altered bytes. The GitHub
  repository account, Actions platform, and TLS PKI remain trust roots.
- v1.4.0 Windows binaries may be unsigned and trigger SmartScreen. Release CI
  signs and verifies every EXE when a CA-issued certificate is configured;
  builds are not yet reproducible.
- P-256 addresses and mnemonic derivation are Entcoin-specific and not Bitcoin
  wallet compatible.
- The node has no hardware-wallet integration, multisignature policy, wallet
  passphrase unlock mode, or process sandbox.
- Network privacy is not a goal of v1.4.0. Peers observe IP addresses, timing, and
  the wallet address currently used as node ID.
- A single node or a network controlled by one miner/operator provides little
  independent failure or censorship resistance.
- Protocol upgrades and emergency governance are not mature enough for a
  real-value network.

## Operator practices

1. Run as a normal OS user on a patched Windows or Ubuntu system.
2. Record the recovery phrase offline and export a separate encrypted backup
   before receiving or mining ENT.
3. Never paste a phrase, private key, or backup password into an issue, log,
   screenshot, chat, or diagnostic report.
4. Stop the node before copying its full data directory; do not copy only the
   live SQLite main file while WAL data may exist.
5. Verify release checksums and download only from the public repository's
   release page.
6. Expose only the P2P TCP port. Do not expose file sharing, remote desktop,
   development servers, or data directories with it.
7. Use multiple independent archive peers for public experiments and monitor
   unexpected tip, peer, database, and reorg behavior.
8. Keep real funds and real credentials completely separate from ENT until the
   implementation has received appropriate independent audits.

See [Node operations](docs/operations.md) for backup, restore, firewall, NAT,
testnet-wallet recovery, and pruning procedures.
