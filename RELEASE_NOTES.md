# Entcoin v1.2.0

English | [简体中文](#简体中文)

Entcoin v1.2.0 is a mandatory consensus upgrade for
`entropy-mainnet-v1`. Rule version 2 activates at block **160000**. Upgrade all
desktop, CLI, validating, relay, and mining nodes before that height. Nodes on
v1.1 or earlier remain compatible below activation but cannot validate the
version-2 chain after activation.

This release does not reset the chain and does not migrate wallets or storage.
Genesis, `NetworkID`, private keys, recovery phrases, wallet derivation,
addresses, amounts, transaction encoding, balances, history, SQLite schema,
peer records, and the existing `mainnet-v1` data directory are unchanged.

## Why this release is required

The difficulty value 35 observed on mainnet was valid under the v1 rule. The
defect was the rule itself: difficulty could rise quickly when a large miner
arrived, but after that miner left the remaining network had to finish expensive
60-block epochs before difficulty could fall. At the observed pre-event rate,
returning from difficulty 35 to 27 could take about 83 hours.

The audit found no arithmetic or validation bug that could be fixed locally
while remaining compatible with v1. Old nodes reject blocks using any different
difficulty result, so replacing the DAA necessarily changes block validity and
requires a scheduled hard fork.

## Consensus changes at block 160000

- Blocks below `160000` remain version 1 and replay under the unchanged legacy
  60-block DAA. Blocks at and above `160000` must be version 2. Unknown and
  wrong-side versions fail closed.
- Version 2 uses a fixed numerical anchor at height `123265`, timestamp
  `1785201853`, difficulty 35, and hash
  `000000000b6ffc20400cafe308ae13a73cead4c5a7cb232214d714ff2949dead`.
- The new per-block integer ASERT rule compares candidate height and timestamp
  with the anchor's ten-second schedule. Its half-life is 600 seconds and signed
  halves round away from zero. Difficulty remains clamped to the existing 8..48
  range.
- A schedule delay of ten minutes lowers difficulty by one bit. A complete stall
  can move from 35 to 27 after 80 minutes without waiting for a block or epoch.
  In the other direction, a rapid stream of minimum-timestamp blocks raises
  difficulty promptly as excess hash rate arrives.
- Version-2 timestamps must exceed both median-time-past over 11 blocks and the
  immediately previous block timestamp. The existing local future limit of 120
  seconds remains.
- Rule selection is centralized by height and is shared by replay, direct block
  connection, mining, import, HTTP/WebSocket synchronization, staged body
  validation, and atomic reorganization.

The exact formula and published vectors are in
[`docs/consensus-upgrade-audit-v1.2.md`](docs/consensus-upgrade-audit-v1.2.md).

## Mining and node behavior

- Mining candidates take one timestamp snapshot and derive version and
  difficulty from that exact timestamp.
- After activation, a long-running mining job checks every five seconds for an
  actual lower integer difficulty boundary. It transparently rebuilds the
  template when relief becomes available; a single **Mine one block** action
  continues instead of returning a stale-difficulty error.
- Tip changes still cancel stale mining work immediately. A solved candidate is
  committed only if its captured tip remains current.
- `GET /v2/consensus` reports supported rules, next-block rule, activation,
  anchor, algorithm, and half-life. Existing `/v2/status` and WebSocket status
  messages are unchanged so strict v1.1 peers remain connected before
  activation.
- Desktop diagnostics show the next consensus rule and activation height.

## User upgrade experience

Existing desktop users only need to choose **Update and restart**. Entcoin
closes, installs v1.2.0, relaunches, and opens the same wallet and ledger. No
address changes, seed import, rescan, balance migration, or manual file copying
is required. As with every release, retain a verified 24-word phrase or
`.entwallet` backup before upgrading.

Headless operators replace the CLI binary and restart with the same service
arguments and data directory. Public Seeds must remain archive nodes and should
be rolled one at a time. If an old node reaches activation, it stops following
the chain at height `159999`; install v1.2.0 and restart against the same data.
Do not delete the ledger.

## Audit and verification

The project reviewed every identified consensus surface: chain identity and
genesis, issuance and the exact supply cap, regular transactions and P-256
signatures, fees, coinbase reward and maturity, timestamp validation, proof of
work, cumulative-work fork choice, direct extension, import, mining, HTTP and
WebSocket synchronization, reorganization, pruning boundaries, mempool rebuild,
and atomic SQLite transitions. The DAA liveness defect was the only confirmed
independent mainnet defect requiring a hard fork. The earlier coinbase-maturity
change was finalized before mainnet genesis and is not unresolved fork debt.

Automated coverage includes fixed anchor hash/PoW and DAA vectors, rounding and
clamp boundaries, extreme timestamps, wrong versions on both sides of
activation, hash-rate arrival and departure, mining-template refresh, real
SQLite connect/mine/reorg/rollback across activation, equal-work rejection,
HTTP header/body sync across activation, and legacy peer-status compatibility.
A read-consistent backup of a production archive Seed opened directly in the
new binary, synchronized to the same public tip and work, remained archive and
walletless, passed SQLite checks, and shut down cleanly.

Release gates include the complete Go suite and race detector, `go vet`,
reachable-code vulnerability scanning, frontend and website tests/builds,
dependency audits, Linux package and wallet smoke tests, Windows/Linux artifact
checksums, and GitHub build-provenance attestations.

## Known boundaries

This remains a project-led audit, not an independent third-party consensus or
economic review. The custom ten-second network and integer DAA need sustained
adversarial operation and independent implementations. P2P transport is not
authenticated or encrypted without an operator TLS proxy, peer discovery is
not Sybil-resistant, Windows binaries may be unsigned, and release integrity is
still rooted in the official GitHub account and CI. Do not use Entcoin for value
you cannot afford to lose.

---

## 简体中文

Entcoin v1.2.0 是 `entropy-mainnet-v1` 的强制共识升级。规则版本 2 将在区块
**160000** 激活。所有桌面端、CLI、验证、转发和挖矿节点都必须在该高度前升级。
v1.1 及更早版本在激活前仍可兼容运行，但激活后无法验证版本 2 主链。

本次升级不重置区块链，也不迁移钱包或存储。创世块、`NetworkID`、私钥、恢复短语、
钱包派生、地址、金额、交易编码、余额、历史记录、SQLite 结构、节点记录以及现有
`mainnet-v1` 数据目录均保持不变。

## 为什么必须升级

主网出现的难度 35 符合 v1 规则，问题不在计算错误，而在规则设计：大算力进入时难度能
快速上升，但该算力离开后，剩余矿工必须先完成高难度的 60 区块周期，难度才会下降。
按事件前观测到的算力，从 35 回落到 27 可能需要约 83 小时。

审计没有发现能在保持 v1 兼容的前提下单机修复的算术或验证漏洞。旧节点会拒绝任何使用
不同难度结果的区块，因此替换 DAA 必然改变区块有效性，必须通过预定高度硬分叉完成。

## 高度 160000 的共识改动

- 高度 `160000` 之前继续使用版本 1 和原有 60 区块 DAA；从 `160000` 开始必须使用
  版本 2。未知版本以及出现在错误高度一侧的版本都会被拒绝。
- 版本 2 使用固定数值锚点：高度 `123265`、时间戳 `1785201853`、难度 35、哈希
  `000000000b6ffc20400cafe308ae13a73cead4c5a7cb232214d714ff2949dead`。
- 新的逐区块整数 ASERT 将候选区块高度和时间戳与锚点的 10 秒计划比较，半衰期为
  600 秒；有符号数恰好位于半数时向远离零方向取整，难度仍限制在原有 8..48 范围。
- 相对计划每延迟 10 分钟，难度下降 1 bit。即使完全停块，也能在 80 分钟后从 35
  降到 27，无需先挖出一个区块或完成一个周期；大算力突然进入时，连续快速区块也会让
  难度及时上升。
- 版本 2 时间戳必须同时大于前 11 个区块的中位时间和上一个区块时间戳；原有“不得超过
  本机验证时间 120 秒”的限制保持不变。
- 按高度选择规则的逻辑已集中管理，并由历史重放、直连区块、挖矿、导入、HTTP/WebSocket
  同步、分阶段区块体校验和原子重组共同使用。

精确公式和公开测试向量见
[`docs/consensus-upgrade-audit-v1.2.md`](docs/consensus-upgrade-audit-v1.2.md)。

## 挖矿和节点行为

- 挖矿模板只取一次时间快照，并用同一个时间戳计算版本和难度。
- 激活后，长时间运行的挖矿任务每 5 秒检查一次是否真正跨入更低的整数难度；出现缓解时
  自动重建模板。“挖一个区块”也会继续执行，不会把内部模板过期错误抛给用户。
- 链 tip 改变仍会立即取消旧工作；找到的区块只有在快照 tip 仍然有效时才会提交。
- 新增 `GET /v2/consensus`，报告支持规则、下一块规则、激活高度、锚点、算法和半衰期。
  原有 `/v2/status` 与 WebSocket 状态消息保持不变，避免严格解析 JSON 的 v1.1 节点在
  激活前断开。
- 桌面诊断页会显示下一块共识规则和激活高度。

## 用户升级体验

现有桌面用户只需点击“更新并重启”。Entcoin 会退出、安装 v1.2.0、重新启动，并直接打开
原钱包和账本。不需要更换地址、重新导入短语、扫描链、迁移余额或手工复制文件。与任何
升级一样，操作前仍应保留已验证的 24 词恢复短语或 `.entwallet` 备份。

无头节点只需替换 CLI 二进制，并使用原 systemd 参数和数据目录重启。公网 Seed 必须保持
归档模式，并逐台滚动升级。如果旧节点运行到激活点，它会停在高度 `159999`；安装 v1.2.0
后使用同一数据目录重启即可，不要删除账本。

## 审计与验证

项目逐项审查了链标识和创世块、发行与精确总量上限、普通交易与 P-256 签名、手续费、
Coinbase 奖励和成熟期、时间戳、工作量证明、累计工作量分叉选择、直连、导入、挖矿、
HTTP/WebSocket 同步、重组、裁剪边界、内存池重建以及 SQLite 原子状态转换。DAA 活性缺陷
是目前唯一确认、且独立需要硬分叉修复的主网问题。此前的 Coinbase 成熟期修改在主网创世
前已经定稿，不是遗留分叉债务。

自动化测试覆盖固定锚点哈希/PoW 与 DAA 向量、舍入和上下限、极端时间戳、激活点两侧的
错误版本、算力进入与离开、挖矿模板刷新、真实 SQLite 跨激活连接/挖矿/重组/回滚、等工作量
拒绝、跨激活 HTTP 区块头与区块体同步，以及旧节点状态消息兼容。新二进制还直接打开了
线上归档 Seed 的一致性备份，无迁移追到相同公网 tip 和累计工作量，保持归档且无钱包，
通过 SQLite 检查并正常关闭。

发布门禁包括完整 Go 测试和竞态检测、`go vet`、可达代码漏洞扫描、前端与网站测试/构建、
依赖审计、Linux 安装包与钱包冒烟测试、Windows/Linux 产物校验和，以及 GitHub 构建来源
证明。

## 已知边界

本次仍是项目方内部审计，不是独立第三方共识或经济安全审计。自定义 10 秒网络和整数 DAA
仍需要长期对抗性运行和独立实现验证。P2P 在没有运营者 TLS 反向代理时不提供认证或加密，
节点发现不抗女巫攻击，Windows 二进制可能没有签名，发布完整性仍依赖官方 GitHub 账户和
CI。请勿投入无法承受损失的价值。
