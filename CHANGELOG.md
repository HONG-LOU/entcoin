# Changelog

All notable changes are documented here. The protocol identity is the network
compatibility boundary; a `mainnet` identity is not a security or audit claim.

## [Unreleased]

## [1.5.0] - 2026-08-06

### Added

- Added `entpay agent-ui`, a loopback-only confirmation application that
  verifies a merchant's signed Invoice and exact request before the user
  approves local-wallet payment.
- Added visible payment, confirmation, delivery-verification, Receipt, and
  artifact states with Chinese and English interfaces.
- Added prepared-Invoice execution so browser handoff does not create a second
  Invoice, plus startup wallet-directory validation and bounded local storage.

### Changed

- Rebuilt the merchant workspace around the actual service, request, terms,
  signed Invoice, and local-Agent handoff instead of exposing raw JSON.
- Reworked the public website into a continuous responsive network narrative
  with live node state and clearer desktop/download choices.
- Kept merchant products and provider credentials in independently deployed
  merchant services; Entcoin publishes only the protocol, SDK, and local Agent.

### Security

- The merchant page no longer displays or copies claim capabilities. Handoff
  uses a URL fragment that the loopback page clears before processing.
- The local Agent re-fetches merchant metadata and validates protocol, network,
  product, price, input hash, Ed25519 signature, expiry, and hard limit both at
  inspection and immediately before payment.
- Rejecting creates no transaction, and concurrent approval requests are
  idempotent.

## [1.4.0] - 2026-08-05

### Added

- Published EntPay as a reusable Go package with dynamic merchant products,
  signed input-bound Invoices and Receipts, confirmation-gated fulfillment,
  claim-token authorization, optional artifact delivery, and Range downloads.
- Added a generic local Agent that applies deterministic policy before Codex
  semantic approval, restores the active wallet, verifies every Receipt and
  artifact binding, and refuses to overwrite an existing output file.
- Added crash-safe fulfillment staging, bounded retry and permanent-failure
  handling, SQLite transaction replay prevention, and concurrent claim
  idempotency.

### Changed

- Moved concrete merchant products, model-provider integrations, deployment
  inventory, and production credentials out of the public Entcoin repository.
  Merchants now own and deploy their services independently.
- Replaced the single-product EntPay page with a dynamic merchant workspace and
  redesigned the public website around a full-width live network topology.
- Reduced the public `entpay` CLI to the generic `agent` and `generate-key`
  commands.

### Security

- Added canonical strict JSON boundaries, signed payload and artifact hashes,
  bearer capability hashing, authorized downloads, and exact ENT output checks.
- Kept wallet seeds, private keys, claim tokens, merchant signing credentials,
  and model credentials outside Codex prompts and merchant source repositories.

## [1.3.0] - 2026-08-05

### Added

- Added EntPay `entpay-v1`, an application-layer Agent payment service with
  Ed25519-signed invoices and receipts, random bearer claim capabilities,
  SQLite-backed idempotency, exact-output validation, transaction replay
  prevention, and one-confirmation resource delivery.
- Added the `entpay` server and local Agent client. The client verifies the
  invoice independently, enforces a hard spending limit, signs with a selected
  local wallet profile, restores the previous profile, and uses the locally
  configured Codex model for approval and paid report analysis.
- Added a responsive EntPay operations surface and production deployment units
  for the two existing public archive-node hosts.
- Added `entpay.exe` and `entpay-linux-amd64` to release builds, checksums, and
  build provenance.

### Security

- Kept wallet-control methods outside the public P2P listener. EntPay receives
  only a signed transaction intended for broadcast and never receives wallet
  keys, recovery words, Codex credentials, or a local wallet-control token.
- Added strict JSON boundaries, request size/depth limits, per-IP rate limits,
  HTTPS-only remote Agent endpoints, signed delivery verification, atomic
  transaction reuse rejection, and persisted retry-safe delivery responses.

### Compatibility

- Preserved `entropy-mainnet-v1`, genesis, consensus rules, transaction
  encoding, wallets, addresses, balances, SQLite ledger schema, and P2P peer
  compatibility. EntPay uses a separate database and HTTP process.

## [1.2.2] - 2026-07-29

### Fixed

- Replaced the Windows updater's hard-coded `%LOCALAPPDATA%\Programs\Entcoin`
  relaunch target with an exact in-place update of the executable that initiated
  the update, covering installed, portable, renamed, and custom-path copies.
- Added a pre-Wails update helper that waits for the old PID, stages and hashes
  the verified executable on the target volume, preserves a rollback copy,
  atomically replaces the target, and relaunches the same path.
- Restored the previous executable when replacement verification or relaunch
  fails, and recorded a local `.update-error.log` instead of silently opening a
  different or stale Entcoin copy.

### Verification

- Added Windows regressions for exact-target replacement, successful cleanup,
  rollback after relaunch failure, helper-version validation, and Windows asset
  selection.
- Preserved `entropy-mainnet-v1`, consensus rules, wallet formats, chain data,
  SQLite schema, addresses, balances, and peer compatibility.

## [1.2.1] - 2026-07-28

### Consensus

- Moved mandatory rule-version-2 activation from block `160000` to block
  `125555` so the audited ASERT liveness fix no longer waits through more than
  35,000 additional legacy-rule blocks.
- Kept the fixed anchor, integer ASERT formula, rule version, timestamp rules,
  network identity, genesis, issuance, transaction validity, and fork-choice
  rules unchanged from v1.2.0.

### Upgrade

- Superseded v1.2.0. Every desktop, CLI, validating, relay, and mining node must
  install v1.2.1 before block `125555`; v1.2.0 still expects activation at
  `160000` and will reject the earlier v1.2.1 chain.
- Preserved the existing wallet, keys, addresses, balances, history, SQLite
  ledger, peers, and in-place **Update and restart** workflow.
- Updated the public consensus vectors, diagnostics, operator guidance, website,
  update manifest, and bilingual release notes for the earlier boundary.

## [1.2.0] - 2026-07-28

### Consensus

- Scheduled mandatory rule-version-2 activation at block 160000 while retaining
  all historical v1 rules below activation and rejecting unknown/wrong-side
  block versions.
- Replaced the asymmetric 60-block DAA after activation with a fixed-anchor,
  per-block integer ASERT using a 600-second half-life, signed nearest rounding,
  existing difficulty clamps, and published deterministic vectors.
- Required version-2 timestamps to exceed both MTP11 and the immediately
  previous timestamp while retaining the 120-second future bound.
- Centralized height-gated rule selection across replay, mining, direct connect,
  import, HTTP/WebSocket sync, staged validation, and atomic reorganization.

### Changed

- Mining now derives timestamp and difficulty from one snapshot and
  transparently rebuilds post-activation work when elapsed time lowers the
  integer difficulty.
- Added a compatibility-safe `/v2/consensus` endpoint and desktop diagnostic
  display without adding fields to strict legacy peer-status messages.
- Made outbound WebSocket dialing-to-connected slot transitions atomic and
  removed random test-port collisions from the 48-candidate limit regression.
- Preserved genesis, network ID, wallet/key/address format, amount and
  transaction encoding, SQLite schema/data, balances, history, peer records,
  and normal in-app update/restart behavior.

### Verification

- Added fixed DAA/anchor/rounding/clamp/version vectors, hash-rate shock and
  stall simulations, and real SQLite connect, mining, equal-work rejection,
  rollback, reorg, and HTTP sync tests crossing activation.
- Made public-network sync tests replay a fixed validated mainnet block instead
  of depending on stochastic proof-of-work finishing within 30 seconds under
  the Windows race detector.
- Re-audited every consensus and state-transition surface. The DAA liveness
  defect was the only confirmed independent mainnet issue requiring a hard fork.
- Opened and synchronized a read-consistent production archive-ledger backup
  with the v1.2 binary without migration, pruning, wallet creation, or state
  divergence.
- Added detailed English and Simplified Chinese release and operator guidance.

## [1.1.0] - 2026-07-22

### Added

- Added complete English and Simplified Chinese README entry points, a
  documentation index, controlled release notes, and a project-led v1.1.0
  security audit with mathematical checks, evidence, and residual risks.
- Added immutable GitHub build-provenance attestations for published release
  artifacts.

### Security

- Removed the Alibaba Cloud and website mirrors from the update checksum trust
  path. Artifacts may still use either mirror, but the expected SHA-256 now
  comes only from the official GitHub Release.
- Added an adversarial updater regression test proving that an artifact and
  matching checksum supplied by a compromised mirror are not trusted.
- Upgraded `golang.org/x/crypto` to v0.52.0 and `golang.org/x/sys` to v0.45.0;
  reachable-code vulnerability scanning reports zero vulnerabilities.
- Pinned all GitHub Actions to immutable commit SHAs, moved JavaScript Actions
  to maintained Node 24 runtimes, reduced release workflow permissions, and
  added Go race, `govulncheck`, and npm audit CI gates.
- Fixed Linux package directories to explicit non-writable `0755` modes instead
  of inheriting the build host's umask, and added a pre-package permission gate.

### Changed

- Unified desktop, updater, package, website, build, operations, architecture,
  protocol, and security metadata on v1.1.0.
- Changed GitHub Release publishing to use the reviewed `RELEASE_NOTES.md`
  instead of a generic fixed warning as the release body.
- Added a tag-aware release metadata verifier and stripped unnecessary debug
  symbols from release CLI binaries.
- Preserved `entropy-mainnet-v1`, its genesis, consensus parameters, wallet
  derivation, address format, database schema, and P2P compatibility. No chain
  reset or wallet migration is required.

### Verification

- Passed the full Go test suite, race detector, `go vet`, CLI build, frontend
  and website tests, production frontend build, npm audit, and `govulncheck`
  with Go 1.26.5.

## [1.0.15] - 2026-07-21

### Changed

- Desktop updates now prefer the Alibaba Cloud Asia mirror for both checksum
  manifests and installers, with the US website mirror and GitHub Release as
  verified fallbacks.
- The Asia mirror serves immutable version directories with HTTP Range support
  alongside the existing archive seed without changing node traffic.

## [1.0.14] - 2026-07-21

### Changed

- Maintenance release used to verify the bounded in-app update path introduced
  in v1.0.13 from an already-fixed desktop client.

## [1.0.13] - 2026-07-21

### Fixed

- Update checks now prefer the official website manifest and use GitHub as a
  bounded fallback instead of making every install wait for GitHub first.
- Versioned checksum manifests now use the official mirror first, fall back to
  the matching GitHub Release, reject invalid source content, and enforce a
  short timeout per source instead of leaving the UI in Preparing for minutes.
- The desktop reports a distinct integrity-check phase before artifact download.

## [1.0.12] - 2026-07-21

### Changed

- Published a maintenance release so installed v1.0.11 clients can exercise the
  official mirror, resumable download, checksum verification, installation,
  and restart path end to end.

## [1.0.11] - 2026-07-21

### Changed

- Desktop updates now prefer the official `entcoin.xyz` release mirror and
  automatically fall back to GitHub while keeping the expected SHA-256 digest
  anchored to the matching GitHub Release checksum manifest.
- Interrupted installer downloads remain in the protected update cache and
  resume with bounded HTTP Range requests. Servers that reject or ignore a
  range request safely restart the artifact from byte zero.
- Website download actions now use the same versioned release mirror.

## [1.0.10] - 2026-07-21

### Changed

- Desktop transaction filters now query up to 100 matching wallet records from
  SQLite instead of filtering only the latest 100 mixed transaction records.
- The update button now shows real byte-based download progress with a filled
  progress state, followed by explicit verification and installation phases.

## [1.0.9] - 2026-07-21

### Added

- Added complete English and Simplified Chinese desktop interfaces for Windows
  and Linux, including startup, wallet, transactions, mining, diagnostics,
  storage, updates, dialogs, validation, and operation feedback.
- Added a compact language control that follows the operating-system language
  on first launch and remembers the user's selection afterward.

### Changed

- Dates and numeric values now follow the selected desktop language, while
  protocol identifiers, addresses, hashes, file types, and currency symbols
  retain their exact technical representation.

## [1.0.8] - 2026-07-21

### Added

- Added `All`, `Received`, `Sent`, and `Mining` filters to the desktop
  transaction history without changing the loaded history or detail view.
- Added an HTTPS update-manifest fallback at `entcoin.xyz/update.json` for
  desktop clients that cannot reach the GitHub release feed.

### Changed

- Desktop updates now install after checksum verification, close the current
  process, and relaunch Entcoin automatically. Linux still requires the normal
  Polkit authorization prompt, and unsigned Windows builds may show SmartScreen.
- Update metadata and checksum downloads retry temporary network failures.

## [1.0.7] - 2026-07-21

### Changed

- Unified the website favicon and Windows desktop package icon with the custom
  Entcoin E used by Linux packages, the PWA, and website branding.
- Replaced obsolete test-network/value disclaimers in current product surfaces
  with the live two-region public seed topology and local-validation model.
- Kept `entropy-mainnet-v1`, `entropy.db`, and legacy Entropy data-directory
  detection unchanged as protocol and wallet compatibility identifiers.

### Security

- v1.0.7 Windows artifacts remain unsigned and may trigger Microsoft Defender
  SmartScreen; published SHA-256 manifests cover every release executable.

## [1.0.6] - 2026-07-21

### Added

- A desktop software-update surface that checks the official GitHub Release,
  selects the current platform installer, downloads it into the user cache,
  verifies it against the matching SHA-256 release manifest, and opens the
  operating-system installer for explicit user approval.

### Fixed

- Recoverable WebSocket peer reconcile failures remain attached to the failed
  peer instead of permanently changing the desktop node state to `Node warning`.

### Security

- Update metadata and artifacts are bounded, accepted only from trusted GitHub
  HTTPS hosts, matched to exact release asset names, and verified before launch.
- v1.0.6 Windows artifacts remain unsigned and may trigger Microsoft Defender
  SmartScreen; the updater does not bypass operating-system trust prompts.

### Compatibility

- Consensus identity `entropy-mainnet-v1`, blocks, transactions, addresses,
  wallets, vaults, database layout, and peer protocol are unchanged from
  v1.0.0-v1.0.5.

## [1.0.5] - 2026-07-21

### Added

- Clickable and keyboard-accessible desktop transaction details with status,
  confirmations, block metadata, inputs, outputs, and pruned-body state.
- Optional SHA-256 Authenticode signing with RFC 3161 timestamps for Windows
  desktop, installer, and CLI artifacts when a CA certificate is configured.

### Changed

- Product branding, executables, installers, packages, CLI commands, service
  names, repository links, and website assets now use Entcoin.
- The source repository moved to `HONG-LOU/entcoin`.
- New installations use Entcoin application-data and Linux Secret Service
  names while existing Entropy mainnet data and wallet keys remain available.

### Security

- v1.0.5 Windows artifacts are published unsigned and may trigger Microsoft
  Defender SmartScreen. SHA-256 checksums provide integrity, not publisher
  identity or reputation.

### Compatibility

- Consensus identity `entropy-mainnet-v1`, genesis hash domains, transaction
  signatures, wallet formats and derivation, addresses, and `entropy.db` are
  unchanged from v1.0.0-v1.0.4.

## [1.0.4] - 2026-07-19

### Fixed

- Desktop chain status now reports synchronization only when an active peer
  sync targets a height above the local validated tip. Polling an equal or
  behind peer no longer leaves an up-to-date node labelled as synchronizing.
- Website icon URLs are versioned so browsers refresh the Entcoin E mark
  instead of retaining an older cached application or favicon asset.

### Compatibility

- Consensus, `entropy-mainnet-v1`, blocks, transactions, addresses, wallets,
  vaults, and the wire protocol are unchanged from v1.0.0-v1.0.3.

## [1.0.3] - 2026-07-19

### Added

- A second public archive seed, `https://node.entcoin.xyz`, in the remotely
  refreshed mainnet bootstrap manifest.
- Automatic desktop transaction fees based on the transaction's encoded size
  and the node's minimum relay policy.

### Changed

- Direct-extension synchronization now reuses each validated 128-header page
  across sixteen bounded 8-block body requests instead of discarding all but
  eight headers. Scheduled HTTP sync rounds may run for up to two minutes.
- The desktop replaces the detailed Network page and manual peer form with a
  compact Online, Syncing, Connecting, Behind, or Offline status.

### Compatibility

- Consensus, `entropy-mainnet-v1`, blocks, transactions, addresses, wallets,
  vaults, and the wire protocol are unchanged from v1.0.0-v1.0.2.
- Older nodes discover both archive seeds when they next refresh the public
  manifest; installing v1.0.3 is required only for the faster synchronizer and
  desktop changes.

## [1.0.2] - 2026-07-18

### Added

- Lightweight multi-wallet profiles over one chain database, including create,
  recovery/backup import, switching, export, and guarded removal in the desktop
  application.
- A prioritized maturity roadmap covering audit, hash-power, release,
  bootstrap/eclipse, wallet, fee-market, sync, privacy, and operations gaps.

### Changed

- Inbound WebSocket source addresses are no longer persisted as dialable peers.
  Bidirectional reconciliation continues on the established socket without
  requiring a NAT callback.
- Startup removes old automatically discovered peers that have never succeeded
  and accumulated at least eight failures. Manual and bootstrap peers remain.
- Wallet import preserves the existing wallet instead of replacing it. The
  active wallet must be backed up before switching, and active or unsecured
  profiles cannot be removed.
- Portable backups cannot be written inside the live node data directory.

### Compatibility

- `entropy-mainnet-v1`, genesis, consensus, transaction encoding, addresses,
  wallet derivation, and `.entwallet` format are unchanged from v1.0.0/v1.0.1.
- Existing `wallet.vault` files are registered as the first profile on startup;
  no chain resynchronization or wallet re-import is required.

## [1.0.1] - 2026-07-14

### Added

- Ubuntu 24.04+ amd64 desktop wallet/node package with the same mainnet,
  recovery phrase, encrypted backup, transaction, synchronization, and mining
  behavior as the Windows application.
- Linux local-wallet protection using Secret Service for a user-scoped random
  master key and XChaCha20-Poly1305 for authenticated `wallet.vault`
  encryption.
- Native Linux desktop and CLI binaries, `.deb` packaging, menu integration,
  and Linux release checksums.

### Changed

- Release automation now builds Windows and Ubuntu artifacts independently and
  publishes them together only after both platform jobs succeed.

### Compatibility

- `entropy-mainnet-v1`, genesis, consensus, wallet derivation, addresses, and
  portable `.entwallet` backups are unchanged from v1.0.0.
- Windows DPAPI vaults remain Windows-local. Move a wallet between Windows and
  Ubuntu with its 24-word phrase or an encrypted `.entwallet` backup.

## [1.0.0] - 2026-07-14

### Added

- Public `entropy-mainnet-v1` network with a new reward-free genesis block.
- Built-in HTTPS bootstrap-manifest discovery through the public repository and
  its CDN mirror, with a verified public archive seed at
  `https://template-chat.xyz`.
- Optional Windows archive-seed deployment package with Caddy HTTPS/WSS
  termination, service accounts, scheduled startup, firewall setup, health
  checks, and uninstall support.
- Explicit Linux/Windows seed mode with archive-only storage, an ephemeral
  non-financial identity, no persistent wallet, and disabled wallet/mining
  operations.
- Release artifacts for the desktop app, installer, headless CLI, public-seed
  deployment package, and SHA-256 checksums.

### Changed

- Desktop and package metadata now report version `1.0.0`.
- Fresh Windows mainnet state is isolated under
  `%LOCALAPPDATA%\Entropy\mainnet-v1`; published testnet directories are never
  selected automatically.
- A fresh desktop database starts pruned with a 20,000-block body horizon and
  then respects the persisted operator choice. CLI nodes remain archive by
  default, and the public-seed deployment enforces archive mode.
- Coinbase maturity is 100 blocks beginning with the first reward block at
  height 1. The target remains one block per 10 seconds, exactly 2,000,000 ENT
  over 31,536,000 reward-bearing heights, approximately ten years.
- HTTP and WebSocket endpoints retain the `/v2` transport path even though the
  network identity is `entropy-mainnet-v1`.

### Compatibility

- `entropy-mainnet-v1` rejects every testnet protocol identity and uses a new
  genesis, so old testnet chains are neither migrated nor replayed.
- Wallet control can be restored on mainnet from a known 24-word recovery phrase
  or verified `.entwallet` backup. Chain/database files must not be copied into
  the mainnet directory.
- The published HTTPS archive seed enables automatic cross-internet discovery;
  manually configured peers remain available for manifest or seed outages.

### Security status

- The source repository is public, but v1.0.0 has not received an independent
  consensus, cryptography, wallet, P2P, persistence, or desktop audit.

## [0.2.0] - 2026-07-13

### Added

- SQLite WAL ledger with indexed UTXOs, wallet history, persistent mempool and
  peers, health records, full-synchronous writes, and startup integrity checks.
- Per-block undo records and atomic cumulative-work chain reorganization with
  orphaned-transaction mempool revalidation.
- Persistent archive/pruned storage policy and irreversible body/undo pruning.
- Header-first incremental HTTP synchronization and requested body batches.
- WebSocket transaction/block relay with keepalive, connection, message, and
  per-IP limits.
- Automatic LAN multicast discovery and persistent exponential peer backoff.
- Windows user-scope DPAPI wallet vault.
- New-wallet 24-word BIP39 recovery using a versioned Entcoin P-256 derivation.
- Portable Argon2id/XChaCha20-Poly1305 `.entwallet` export and restore.
- Desktop transaction history, confirmation/spendable state, peer management,
  wallet recovery, database diagnostics, and pruning workflow.
- Headless `history`, `wallet-backup`, and `wallet-migrate` commands.
- NSIS installer build and release SHA-256 checksum generation.
- Single-instance desktop activation, automatic occupied-port fallback, and a
  pinned tag-to-GitHub-Release workflow.

### Changed

- Network identity is now `entropy-testnet-v3`.
- Chain state moved from whole-file JSON replay to incremental SQLite commits.
- Synchronization compares and validates headers before downloading candidate
  bodies and never accepts a serialized remote state wholesale.
- Coinbase outputs require 100-block maturity beginning at spending height 100,
  preserving the earlier published testnet history.
- Retry failures, manual/discovered peer state, and storage policy survive node
  restart.
- Desktop startup treats missing/corrupt backend, wallet, and ledger state as a
  fatal error instead of showing placeholder data.
- Clean Windows installs now store live data under `%LOCALAPPDATA%`; existing
  `%APPDATA%` wallets and chains are detected and reused.
- Mempool relay now enforces aggregate byte/input budgets and a minimum fee,
  while mixed-case hexadecimal addresses remain visible and spendable.
- Peer synchronization now rejects redirects, rate-limits validation work,
  folds replayed local prefixes, bounds disk staging, and cancels stale mining
  templates as soon as the active tip changes.

### Migration

- Valid `chain.json` and `peers.json` are imported and verified before becoming
  `.migrated.bak` files.
- Plaintext `wallet.json` requires an explicit migration that creates and
  verifies both local DPAPI and portable password-encrypted copies before
  deleting plaintext.
- A disagreement between legacy and SQLite chain tips stops migration without
  replacing either copy.

### Removed

- `GET/POST /v1/state` whole-state synchronization.
- New plaintext wallet storage.
- Production frontend mock data and simulated backend success paths.

### Security status

- v0.2 is a public testnet and has not received an independent audit.
- Release binaries are not Authenticode-signed and builds are not yet
  reproducible.

## [0.1.0]

- Initial educational MVP with a Wails desktop wallet/node, P-256 signed UTXO
  transfers, proof of work, exact 2,000,000 ENT height-based emission, manual
  HTTP peers, and atomic JSON persistence.
- Superseded by v0.2 and no longer supported.
