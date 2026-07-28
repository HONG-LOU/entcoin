# Entcoin node operations

This guide covers the v1.2.1 Windows/Ubuntu desktop node, headless CLI, and
optional public-seed deployment. The network identity is
`entropy-mainnet-v1`; this compatibility label keeps existing Entcoin nodes,
wallets, and chain data on the same network.

## Install and launch

Release builds provide these Windows artifacts:

- `Entcoin.exe`: portable Wails desktop application;
- `*installer*.exe`: NSIS per-user installer;
- `entcoin-cli.exe`: headless node and wallet utility;
- `entcoin-windows-seed-deploy.zip`: archive-seed deployment package.

Ubuntu 24.04+ amd64 releases additionally provide:

- `entcoin_1.2.1_amd64.deb`: desktop application and CLI installer;
- `entcoin-linux-amd64`: unpackaged desktop binary;
- `entcoin-cli-linux-amd64`: unpackaged headless node;
- `SHA256SUMS-linux.txt`: Linux artifact checksums.

Verify Windows artifacts against `SHA256SUMS.txt` and Linux artifacts against
`SHA256SUMS-linux.txt` from the same release before running them. Current
v1.2.1 binaries may be unsigned, so a checksum proves only that the file matches
the published release artifact, not that a trusted certificate authority
verified its publisher.

Double-click the installed shortcut or portable EXE. A clean first launch
creates the wallet and SQLite ledger under
`%LOCALAPPDATA%\Entcoin\mainnet-v1`, starts the TCP listener, loads the built-in
HTTPS bootstrap manifests, and searches the LAN for peers. It is immediately a
full validating and relaying node; mining remains off until explicitly enabled.
The fresh desktop ledger retains the newest 20,000 complete block bodies. Later
launches respect the storage mode already persisted in the database.

Historical testnet state under `%APPDATA%\Entropy` or
`%LOCALAPPDATA%\Entropy` is never reused as mainnet state. Existing legacy
mainnet directories are detected and reused so the product rename cannot hide a
wallet. Testnet chains and databases must stay outside `mainnet-v1`.

The desktop prefers TCP `47821`. If another process already owns that port it
starts on a free dynamic port instead and shows the actual listener plus a
diagnostic warning. LAN discovery advertises the selected port. CLI nodes keep
strict `--listen` behavior so operator mistakes fail visibly.

Microsoft WebView2 Runtime is required. It is normally present on current
Windows 10/11 systems; the NSIS build can install the bootstrapper when needed.

Install Ubuntu packages with `sudo apt install ./entcoin_1.2.1_amd64.deb`, then
launch **Entcoin** from the desktop menu or run `entcoin`. Ubuntu stores mainnet
state under `~/.config/Entcoin/mainnet-v1`. The logged-in desktop session must
provide an unlocked Secret Service keyring; the standard Ubuntu Desktop session
does so automatically.

### Desktop update flow

Open **Diagnostics → Software update** to check the bounded
`https://entcoin.xyz/update.json` manifest first and the official stable GitHub
Release feed on failure. Entcoin selects the platform installer, limits
metadata and download sizes, and gives metadata and checksum requests a bounded
timeout. The checksum manifest comes only from the matching official GitHub
Release. Installers may then come from exact versioned Alibaba Cloud and US
website mirror URLs or GitHub, but they are accepted only when their bytes match
that GitHub digest. Interrupted files resume from the protected update cache
with HTTP Range requests; a server that does not honor Range causes a safe
restart from byte zero. **Update and restart**
installs the verified package, closes the old process, and relaunches Entcoin.
Ubuntu requests normal Polkit authorization. Windows uses the per-user NSIS
installer and may show SmartScreen. The updater does not bypass either
operating-system trust prompt.

The first release mirror is the Alibaba Cloud endpoint at
`https://template-chat.xyz/downloads/`, followed by
`https://entcoin.xyz/downloads/` and the matching GitHub Release. Both mirrors
serve immutable version directories with byte-range support, but neither is a
checksum trust root. Mirror operators must stage a complete release, validate
the GitHub `SHA256SUMS.txt` and `SHA256SUMS-linux.txt`, and only then publish the
version directory. The Asia
mirror shares Caddy with the archive seed but uses a separate read-only
`/downloads/*` handler; `/v2/*` continues to proxy only to the loopback node.

## Uninstall and retained data

The NSIS uninstaller removes the installed application and shortcuts. It does
not delete `%LOCALAPPDATA%\Entcoin\mainnet-v1`; wallet keys, the recovery
marker, peer records, and the chain database remain available for a later
reinstall. Remove active data manually only after confirming that the recovery
phrase or a portable `.entwallet` backup works.

On Ubuntu, `sudo apt remove entcoin` removes the application and menu entry but
retains `~/.config/Entcoin/mainnet-v1` and the Secret Service key. Confirm the
recovery phrase or portable backup before deleting either one.

Do not configure profile replication for the live mainnet directory. Copying
SQLite WAL files between computers is not a supported backup method.

## v1.2.1 consensus activation

v1.2.0 is superseded and must not remain in service: it still expects rule 1 at
block `125555` and will reject the v1.2.1 main chain. Upgrade v1.2.0, v1.1, and
all earlier validating or mining nodes before activation.

Rule version 2 activates at block `125555`. Upgrade every validating or mining
node before that height. Desktop users use **Update and restart**; the process
reopens the same `mainnet-v1` directory, wallet vault, SQLite ledger, peer set,
balances, and history without a migration step. Headless operators replace only
the binary and restart with the existing flags and data directory.

Before activation, rollback to v1.1 is technically possible but should be used
only to diagnose a v1.2 startup failure. At or after activation, v1.1 cannot
validate version-2 blocks and will stop at height `125554`; reinstall v1.2.1 and
restart against the same ledger. Do not delete or rebuild the database merely
because an obsolete node stopped syncing.

Verify readiness on a node's private or public HTTP endpoint with
`GET /v2/consensus`. The response must show activation `125555`, anchor
`123265`, algorithm `integer-asert`, half-life `600`, and supported rules
`[1,2]`. Public Seeds must remain archive nodes and be rolled one at a time so
one remains reachable.

## Node count and topologies

### One computer, one node

One node is a complete isolated network. It can mine blocks and create locally
confirmed transactions, but there is no independent peer to observe relay,
synchronization, or forks.

### One computer, two nodes

Use separate data directories and TCP ports. Never point two processes at the
same data directory.

```powershell
go build -o .\build\bin\entcoin-cli.exe .\cmd\entcoin

.\build\bin\entcoin-cli.exe node `
  --data .\data\node-a `
  --listen 127.0.0.1:47821 `
  --mine `
  --no-discovery
```

In another terminal:

```powershell
.\build\bin\entcoin-cli.exe node `
  --data .\data\node-b `
  --listen 127.0.0.1:47822 `
  --peer http://127.0.0.1:47821 `
  --no-discovery
```

Node B validates and adopts node A's higher-work chain. Mining both nodes is a
useful reorg test but increases short forks.

### LAN nodes

The desktop listener defaults to `0.0.0.0:47821`. LAN discovery sends a small
announcement every five seconds to `239.255.78.21:47822/UDP`. Nodes on the same
multicast-capable subnet store discovered peer URLs and begin synchronization.

LAN discovery can fail on guest Wi-Fi, networks with client isolation,
multicast-disabled switches, VPN adapters, or restrictive firewalls. Add the
peer manually in that case:

```text
http://192.168.1.20:47821
```

The peer URL is the base URL only. Do not include `/v2/status`, credentials, a
query string, or fragment.

### Public nodes

At least one node must accept internet connections for nodes behind NAT to join
across the public internet. Desktop and CLI nodes fetch built-in HTTPS bootstrap
manifest sources. The published `https://template-chat.xyz` and
`https://node.entcoin.xyz` archive seeds let new nodes join without exchanging
peer URLs manually. The manifest refreshes every six hours, so seed changes do
not require a desktop release. Operators may still add manual peers from the
CLI when recovering from a manifest outage.

An outbound-only node still downloads and independently validates headers,
blocks, transactions, signatures, proof of work, and monetary rules. It also
relays to its established outbound peers. It simply cannot accept a new inbound
connection initiated from the internet.

The release's `entcoin-windows-seed-deploy.zip` can install an always-on archive
node behind Caddy on HTTPS/WSS port 443. Its archive policy is intentional: a
new peer cannot synchronize from genesis through a pruned seed. Follow
[the public-seed guide](public-seed.md), verify the endpoint externally, and
only then add it to `network/mainnet.json`.

After a successful status check, nodes optionally request `/v2/peers`. A node
advertises at most 16 public peers that are active outbound, currently healthy,
and seen within two minutes. Exchanged candidates must be globally routable IP
literals with explicit ports; DNS names and private or special-use addresses
from this untrusted path are discarded. A controlled bootstrap manifest may use
a public FQDN, but it must be HTTPS on port 443. Older nodes that return `404` or
`405` for `/v2/peers` remain usable.

Automatic peer exchange retains at most 24 public candidates and 48 discovered
peers overall; eight outbound peers are active by default. During catch-up, one
direct-extension chunk validates 128 headers once and reuses them across
sixteen bounded 8-block body requests. A two-minute scheduled HTTP sync session
attempts no more than 32 chunks and stops starting chunks when less than five
seconds remain. Later sessions resume from the committed tip.

## Windows Firewall, router, and NAT

The two default ports have different purposes:

| Port | Transport | Scope | Purpose |
| --- | --- | --- | --- |
| `47821` | TCP | LAN or internet | HTTP synchronization and WebSocket relay |
| `47822` | UDP multicast | LAN only | Automatic local peer discovery |

To make a home node publicly reachable:

1. Give the computer a stable LAN address or DHCP reservation.
2. Allow inbound TCP `47821` for Entcoin in Windows Defender Firewall.
3. Forward router TCP `47821` to that computer's TCP `47821`.
4. Test from a different internet connection, not from the same LAN.
5. Give peers `http://<public-ip-or-dns>:47821`.

Do not forward the multicast UDP port. Public discovery does not use it.

| Connection type | Can accept inbound? | What to do |
| --- | --- | --- |
| Public IPv4 directly on host | Yes | Open TCP firewall rule |
| Home NAT with port forwarding | Yes | Forward TCP `47821` and open firewall |
| Carrier-grade NAT | Usually no | Connect outbound to a reachable public peer |
| Corporate/hotel network | Usually no | Use outbound peers if policy allows |
| Public VPS | Yes | Bind TCP listener and restrict unrelated services |

Use an external status check against:

```text
http://<public-host>:47821/v2/status
```

The direct node server uses plain HTTP and WebSocket and has no peer
authentication. A TLS reverse proxy may provide HTTPS/WSS if it passes WebSocket
upgrades and preserves the bounded endpoint behavior. Do not expose unrelated
administrative services on the same port.

Bootstrap manifests accept public peer endpoints only over HTTPS on TCP 443.
The Windows seed package therefore binds the node to loopback on `47821` and
opens only Caddy's TCP 443 firewall rule; do not expose that seed's loopback
listener directly.

## CLI reference

Build once:

```powershell
go build -o .\build\bin\entcoin-cli.exe .\cmd\entcoin
```

Each command accepts `--data <directory>`. When omitted, Windows uses
`%LOCALAPPDATA%\Entcoin\mainnet-v1`. Each data directory has an exclusive lock,
so stop the desktop app or existing CLI node before opening the same directory.

### `node`

```text
entcoin-cli node [--data DIR] [--listen HOST:PORT] [--peer URL]
                 [--mine] [--seed-mode] [--prune-depth BLOCKS] [--no-discovery]
                 [--bootstrap-manifest HTTPS_URL] [--no-bootstrap]
                 [--trust-loopback-proxy]
```

| Option | Meaning |
| --- | --- |
| `--listen` | TCP P2P bind address; default `0.0.0.0:47821` |
| `--peer` | Add one persistent HTTP(S) peer at startup |
| `--mine` | Begin CPU mining immediately |
| `--seed-mode` | Run a non-financial archive relay with an ephemeral in-memory identity and no wallet file |
| `--prune-depth` | Explicitly persist `0` for archive, or retain `120..31,536,000` recent bodies |
| `--no-discovery` | Disable LAN multicast send and receive for this run |
| `--bootstrap-manifest` | Replace built-in manifest sources; repeat for fallback HTTPS URLs |
| `--no-bootstrap` | Disable HTTPS manifest fetching for an isolated/manual topology |
| `--trust-loopback-proxy` | Trust the seed proxy's client-IP header only from a loopback TCP peer |

When `--prune-depth` is omitted, the node keeps the storage policy already
recorded in its SQLite database. A fresh database defaults to archive mode.
Seed mode always enforces prune depth `0`, rejects wallet artifacts or a
previously pruned ledger, and disables sending, mining, wallet recovery,
backup, and restore. It is intended for public infrastructure, not desktop
wallets.

### `status`

```powershell
.\build\bin\entcoin-cli.exe status --data .\data\node-a
```

Prints address, confirmed balance, height, pending count, peers, and issued
supply. It opens the data directory directly and cannot inspect a concurrently
running node.

### `mine-one`

```powershell
.\build\bin\entcoin-cli.exe mine-one --data .\data\node-a
```

Builds one candidate, searches proof of work, commits through normal consensus
validation, prints the block height/hash, and exits.

### `history`

```powershell
.\build\bin\entcoin-cli.exe history --data .\data\node-a --limit 50
```

Prints pending and confirmed wallet transactions with received/sent amounts and
confirmation counts.

### `wallet-backup`

The backup password is read from `ENTCOIN_WALLET_PASSWORD` so it does not appear
as a command-line argument. Passwords must contain 12 to 1024 UTF-8 bytes.

```powershell
$env:ENTCOIN_WALLET_PASSWORD = Read-Host "Backup password"
.\build\bin\entcoin-cli.exe wallet-backup `
  --data .\data\node-a `
  --output E:\Backups\entcoin-wallet.entwallet
Remove-Item Env:\ENTCOIN_WALLET_PASSWORD
```

The command appends `.entwallet` when the destination has another extension.
It refuses to silently replace malformed wallet state.

### `wallet-migrate`

Use this only to convert a v0.1 plaintext `wallet.json` in a copied historical
data directory into a portable encrypted backup:

```powershell
$env:ENTCOIN_WALLET_PASSWORD = Read-Host "Backup password"
.\build\bin\entcoin-cli.exe wallet-migrate `
  --data D:\Backups\entcoin-testnet-v01 `
  --output E:\Backups\entcoin-legacy-wallet.entwallet
Remove-Item Env:\ENTCOIN_WALLET_PASSWORD
```

The migration preserves the old P-256 key and address but does not migrate its
testnet chain. There is intentionally no CLI restore command in v1.2.1; start
the mainnet desktop app and restore the backup from the Wallet view.

## Data directory

The default Windows layout is:

```text
%LOCALAPPDATA%\Entcoin\mainnet-v1\
  entropy.db                       SQLite ledger and indexes
  entropy.db-wal                   live WAL, normally checkpointed on shutdown
  entropy.db-shm                   live SQLite shared-memory file
  wallet.vault                     OS-user-protected active wallet
  wallet.recovery-confirmed        local backup acknowledgement marker
  node.lock                        exclusive process lock
```

Peer records, mempool, UTXOs, history indexes, health events, prune policy, and
prune horizon are inside `entropy.db`.

A seed-mode directory omits `wallet.vault` and
`wallet.recovery-confirmed`. Its P-256 relay identity exists only in process
memory and changes on restart; it has no recoverable or spendable wallet.

The database is reconstructable from archive peers. `wallet.vault`, the
24-word phrase, or a `.entwallet` file is required to recover spending control.
Do not treat a database-only copy as a wallet backup.

For a consistent full-directory maintenance copy, stop the node first and copy
the directory while no `node.lock` process is active. Copying only `entropy.db`
while the node is live can omit committed data still present in its WAL.

## Wallet backup and recovery

### New v1.2.1 wallet

1. Open **Wallet** and reveal the 24-word recovery phrase.
2. Record the words in order on offline media. Do not store a screenshot or
   paste them into chat, email, or an unencrypted cloud note.
3. Confirm recovery in the app so the warning clears.
4. Also export a `.entwallet` file with a long, unique password.
5. Store the phrase, backup file, and backup password in separate locations.

The BIP39 words use an Entcoin-specific P-256 derivation and an empty BIP39
passphrase. Generic Bitcoin or Ethereum wallets will not derive this address.

### Local operating-system vault

`wallet.vault` unlocks automatically through Windows user-scope DPAPI. It is
bound to the Windows protection context and is not a portable recovery method.
Changing computers, reinstalling Windows, deleting the Windows profile, or
losing account protection may make the copied local vault unusable.

On Ubuntu, `wallet.vault` is authenticated with XChaCha20-Poly1305 and its
random master key is held by Secret Service for the current Linux user. The
vault file and keyring entry are both required. Losing either one, resetting the
login keyring, or copying only the file makes the local vault unusable.

### Portable `.entwallet` backup

Portable backups encrypt authenticated wallet material with Argon2id and
XChaCha20-Poly1305. They support mnemonic wallets and migrated legacy
wallets. Keep the file and password separate. A forgotten password cannot be
reset by the project.

### Restore from backup

1. Stop mining.
2. Open **Wallet** and choose **Add encrypted backup**.
3. Select the `.entwallet` file, enter its password, and confirm import.
4. Verify the displayed address against your known address.
5. Allow the existing ledger to refresh history and balances.

Import adds or updates that wallet profile and makes it active. Existing wallet
profiles and the chain database remain in place.

### Restore from 24 words

1. Stop mining.
2. Open **Wallet** and choose **Add recovery phrase**.
3. Enter exactly 24 words and confirm wallet import.
4. Verify the restored address.
5. Export a fresh encrypted backup.

Migrated v0.1 wallets do not have a phrase. Do not invent one for them; restore
their `.entwallet` backup instead.

### Multiple local wallets

All wallet profiles share one validated chain database. Creating, importing,
or switching a wallet changes only the active signing identity, balance view,
history filter, and mining reward address. It does not restart synchronization.

The current wallet must have a confirmed phrase or encrypted backup before the
application permits switching away. Only a non-active, recovery-confirmed
profile can be removed. Removal deletes its local OS-protected vault, not its
on-chain history; the phrase or `.entwallet` remains sufficient to add it back.
Entcoin has no hosted account login or password-reset service.

## Recover a wallet from a testnet release

Before leaving v0.1 or v0.2, stop it and copy the whole old data directory to
offline storage. Never place its `chain.json`, `entropy.db`, WAL, mempool, or
peer files under `%LOCALAPPDATA%\Entcoin\mainnet-v1`. Mainnet has a different
genesis and rejects testnet protocol state rather than importing or replaying
it.

For a v0.2 mnemonic wallet, record the known 24 words or export and verify an
`.entwallet` backup. For a v0.1 plaintext wallet, run `wallet-migrate` against a
copy of the old directory; the command validates the key and creates verified
local OS protection and portable encrypted copies before removing plaintext.

Then start v1.2.1 normally and restore the phrase or `.entwallet` from the
desktop Wallet view. This recovers only the P-256 key and address. Mainnet
balances, spendable outputs, confirmations, and history are derived solely from
the mainnet chain and begin independently of every testnet balance.

## Archive and pruning operations

Archive is the fresh CLI default and is required for a node intended to serve
new peers from genesis. The public-seed package enforces it. A fresh desktop
database instead starts with a 20,000-block retention depth and then respects
the persisted operator choice. Archive consumes increasing disk space over the
approximately ten-year schedule but preserves maximum reorg and diagnostic
history.

Pruned mode keeps all headers, UTXOs, address summaries, and current operation
state while deleting old bodies and undo records. The retention setting is
persistent. The minimum is 120 blocks so the retained window covers timestamp
and initial difficulty context.

The desktop **Database** panel previews the irreversible cutoff before applying
pruning. The equivalent CLI startup policy is:

```powershell
.\build\bin\entcoin-cli.exe node --prune-depth 20000
```

Set `--prune-depth 0` explicitly to stop future pruning. This changes policy
only; it cannot reconstruct data already deleted.

Do not use a pruned node as the only public bootstrap peer. It responds with
`410 Gone` when another node requests a deleted block body. A reorg whose common
ancestor is below the prune horizon is also rejected rather than guessed.

### Recover historical bodies or a deep reorg

1. Stop the node and verify a wallet phrase or `.entwallet` backup.
2. Move `entropy.db`, `entropy.db-wal`, and `entropy.db-shm` to a quarantine
   directory. Do not delete `wallet.vault`.
3. Start a fresh archive ledger and add a trusted archive peer.
4. Wait for full synchronization and verify the wallet address, height, tip,
   balance, and history.
5. Retain the quarantined database until verification is complete.

## Mining and confirmations

The built-in miner uses CPU workers. It is suitable for this compact network,
not an efficient production mining deployment. Closing the app or stopping
mining cancels active work. A found block is committed only if its snapshot tip
is still current.

Treat mempool receipt as propagation, not settlement. The 10-second interval is
a target, not a service guarantee. Wait more confirmations for larger
experiments and expect occasional short reorganizations.

Coinbase maturity is 100 blocks starting with the first reward at height 1. The
height-1 output becomes spendable at height 101. The app can therefore show a
confirmed balance larger than the currently spendable balance while recent
mining rewards mature.

## Troubleshooting

### `listen tcp ... address already in use`

Another process owns the port. Stop it or choose another `--listen` port. Every
node on one computer needs a unique TCP port.

### Data directory is locked

Close the desktop and CLI process using that directory. Do not delete
`node.lock` while a process is alive. If every process is stopped, relaunch and
let the operating-system lock determine whether the stale file is reusable.

### `legacy wallet requires encrypted migration`

Do not move the old testnet chain into `mainnet-v1`. Run `wallet-migrate` against
a copy of the historical directory, keep the resulting `.entwallet` and
password, then restore that backup in the mainnet desktop app. A migrated legacy
wallet has no recovery phrase.

### Protected wallet is missing or cannot decrypt

The node deliberately does not create a replacement wallet. Restore the known
24-word phrase or `.entwallet` backup. Silent replacement would display a new,
empty address and risk permanent loss.

### Peer remains offline

Check the base URL, remote TCP firewall, router forwarding, and `/v2/status`.
Retry delay grows exponentially to five minutes after repeated failures and
resets after a successful contact.

### Sync cannot fetch old blocks

The selected peer may be pruned and return `410 Gone`. Add an archive peer. If
your own prune horizon blocks a required reorg, follow the database resync
procedure above.

### LAN peers are not discovered

Confirm both nodes are on the same subnet, permit UDP multicast `47822`, and
disable Wi-Fi client isolation. Otherwise add peers manually. Use
`--no-discovery` when multicast is intentionally prohibited.

### Desktop window is blank or does not start

Install or repair Microsoft WebView2 Runtime. The node backend treats startup
failures as fatal and does not show fake balances or simulated success.

On Ubuntu, verify `libwebkit2gtk-4.1-0` is installed and that the graphical
login session exposes `DBUS_SESSION_BUS_ADDRESS`. A Secret Service error means
the login keyring is absent or locked; unlock it rather than deleting
`wallet.vault`.

## Operational security boundary

Opening TCP `47821` exposes a deliberately public P2P parser to untrusted input.
The implementation applies message, connection, and timeout limits, but has not
been independently audited. Keep the operating system patched, run as a normal
user, keep wallet backups offline, and read [the security policy](../SECURITY.md).
