<p align="center">
  <img src="build/appicon.png" width="112" alt="Entcoin 标志">
</p>

<h1 align="center">Entcoin</h1>

<p align="center">
  面向 Windows 与 Ubuntu 的轻量级工作量证明全节点、本地钱包与矿工。
</p>

<p align="center">
  <a href="README.md">English</a> · <a href="README.zh-CN.md">简体中文</a> ·
  <a href="https://entcoin.xyz">官网</a> ·
  <a href="https://github.com/HONG-LOU/entcoin/releases/latest">下载</a> ·
  <a href="SECURITY.md">安全策略</a> · <a href="docs/README.md">技术文档</a>
</p>

---

Entcoin v1.5.0 是一个紧凑、可独立验证的 PoW 主网实现。启动一个桌面程序，
即可同时运行钱包、SQLite 账本、完整区块与交易验证、节点同步、实时中继和可选矿工；
无需外部数据库、后台守护进程或浏览器页面。

> `entropy-mainnet-v1` 是主网的永久兼容标识。产品名称升级为 Entcoin 不改变创世块、
> 地址、钱包派生方式或链数据；现有用户可原地升级到 v1.5.0。

> **强制更新：**v1.2.0 已被替代。它仍认为共识 v2 会在高度 160000 激活，因此会拒绝
> 已在高度 125555 启用规则 v2 的正式主链。所有 v1.2.0 节点和矿工都必须安装 v1.5.0。

## 核心能力

- Windows 10/11 与 Ubuntu 24.04+ 桌面全节点，以及使用相同核心代码的无头 CLI。
- 基于 SQLite WAL 的 UTXO 账本，启用 `synchronous=FULL`、外键、原子重组、
  每区块撤销记录、持久化内存池与启动完整性检查。
- 先同步区块头、再按受限批次下载区块体；按累计工作量而不是高度选择主链。
- HTTP 与 WebSocket 双通道中继，支持仅出站/NAT 节点沿同一 WebSocket 反向协调。
- 24 词 BIP39 恢复短语；Windows DPAPI 或 Linux Secret Service 本机保护；
  Argon2id + XChaCha20-Poly1305 加密的跨平台 `.entwallet` 备份。
- 归档与裁剪存储模式。两者都完整验证新区块；归档节点额外保留并提供全部历史区块体。
- 内置 HTTPS 启动清单、局域网发现、受限公共节点交换、连接上限与指数退避。
- 桌面更新器支持断点续传；镜像仅负责分发，预期 SHA-256 只取自官方 GitHub Release。
  Windows 会在旧进程退出后备份并替换当前实际运行的 EXE，成功后从同一路径重启；失败则恢复旧文件。
- EntPay 应用层 Agent 支付：Ed25519 签名 Invoice/Receipt、本地钱包签名、硬额度限制，
  以及由本机 Codex 驱动的付款客户端。

## 下载与校验

正式构建位于 [GitHub Releases](https://github.com/HONG-LOU/entcoin/releases/latest)：

| 平台 | 推荐产物 |
| --- | --- |
| Windows 10/11 x64 | `entcoin-amd64-installer.exe` |
| Windows 便携版 | `Entcoin.exe` |
| Ubuntu 24.04+ amd64 | `entcoin_1.5.0_amd64.deb` |
| Windows / Linux 无头节点 | `entcoin-cli.exe` / `entcoin-cli-linux-amd64` |
| Windows / Linux Agent 支付 | `entpay.exe` / `entpay-linux-amd64` |

下载后应使用同一 Release 中的 `SHA256SUMS.txt` 或
`SHA256SUMS-linux.txt` 校验。GitHub 还为 v1.5.0 产物发布构建来源证明。

Windows 构建只有在发布环境配置可信 Authenticode 证书时才会签名；未签名构建可能触发
SmartScreen。SHA-256 能证明文件与发布清单一致，但不能替代代码签名、独立审计或主机安全。

Ubuntu 安装：

```bash
sudo apt install ./entcoin_1.5.0_amd64.deb
entcoin
```

## 第一次启动

首次运行钱包节点时，Entcoin 会：

1. 创建 P-256 钱包并生成 24 词恢复短语；
2. 使用当前操作系统账户的 DPAPI 或 Secret Service 保护本机钱包；
3. 创建并验证 SQLite 主网账本；
4. 监听 TCP `47821`，发现并同步可用节点；
5. 保持挖矿关闭，直到用户明确选择“开始挖矿”或“挖一个区块”。

主网数据目录：

```text
Windows  %LOCALAPPDATA%\Entcoin\mainnet-v1
Ubuntu   ~/.config/Entcoin/mainnet-v1
```

卸载程序不会删除钱包和链数据。收款或挖矿前，必须离线抄录恢复短语，并导出使用独立强密码
保护的 `.entwallet`。链可以重新下载，丢失的钱包密钥无法由项目方恢复。

## 货币与共识

创世高度 0 固定且无奖励；不存在预挖。所有金额均使用 8 位小数的整数原子单位计算。

| 参数 | 数值 |
| --- | ---: |
| 网络标识 | `entropy-mainnet-v1` |
| 最大发行量 | `2,000,000.00000000 ENT` |
| 每 ENT 原子单位 | `100,000,000` |
| 目标出块时间 | 10 秒 |
| 奖励高度 | 1 至 31,536,000 |
| 高度 1..12,512,000 | `0.06341959 ENT` |
| 高度 12,512,001..31,536,000 | `0.06341958 ENT` |
| 后续高度 | 仅交易手续费 |
| Coinbase 成熟期 | 100 个区块 |
| 最大区块体 | 1 MiB |
| 分叉选择 | 最大累计工作量 |
| 共识 v2 激活高度 | 125,555 |
| 共识 v2 DAA 半衰期 | 600 秒 |

发行公式为：

```text
N    = 31,536,000
MAX  = 200,000,000,000,000 原子单位
BASE = floor(MAX / N) = 6,341,958
REM  = MAX mod N      = 12,512,000

subsidy(h) = 0          , h = 0 或 h > N
             BASE + 1   , 1 <= h <= REM
             BASE       , REM < h <= N
```

因此最后一个奖励高度的累计发行量严格等于 `MAX`，不存在浮点误差。手续费只是已有 ENT 的转移，
不会增加总量。高度 1 的 Coinbase 在消费高度 100 仍不成熟，到高度 101 才可消费。

难度以区块哈希的前导零位数表示。高度 125555 前沿用 v1 的 60 区块调整规则；从该高度
开始强制区块版本 2，并使用半衰期 600 秒的逐区块整数 ASERT。新时间规则同时要求大于
前 11 块中位时间和上一块时间戳，并继续拒绝超过本机时间 120 秒的区块。每个区块贡献
`2^difficulty` 工作量。详细确定性编码与验证规则见[协议文档](docs/protocol.md)。

## 运行 CLI

```bash
go build -trimpath -o build/bin/entcoin-cli ./cmd/entcoin

# 归档节点并启用局域网发现
./build/bin/entcoin-cli node \
  --data ./data/node-a \
  --listen 0.0.0.0:47821 \
  --mine

# 同机第二个节点
./build/bin/entcoin-cli node \
  --data ./data/node-b \
  --listen 127.0.0.1:47822 \
  --peer http://127.0.0.1:47821

# 保留最近 20,000 个完整区块体的裁剪节点
./build/bin/entcoin-cli node \
  --data ./data/node-c \
  --listen 0.0.0.0:47823 \
  --peer http://127.0.0.1:47821 \
  --prune-depth 20000
```

常用命令包括 `node`、`status`、`mine-one`、`send`、`wallet-list`、
`wallet-backup`、`wallet-migrate`、`db-info`、`db-check` 与 `db-prune`。
完整参数、Windows PowerShell 示例和公网 Seed 部署见[运维手册](docs/operations.md)。

## 节点与确认

一个节点足以创建钱包、挖矿、自转账并验证本地链。两个节点是网络实验的实际最低配置，
它们可以中继交易、同步区块并按累计工作量解决分叉。没有协调者，也不存在固定法定人数。

健康局域网中的交易通常在一秒内进入对端内存池，但中继不等于确认：

```text
内存池中继      健康局域网通常低于 1 秒
1 次确认         目标约 10 秒
6 次确认         目标约 1 分钟
```

实际时间取决于算力、连接和分叉。10 秒链的孤块风险较高，6 次 ENT 确认不等同于比特币级别的
经济安全性。公网网络应由多个独立运营者维护归档节点，至少一个节点可从互联网访问。

## 钱包恢复

- 同平台重装：保留整个 `mainnet-v1` 目录并在节点停止后备份。
- 跨平台恢复：使用 24 词 Entcoin 短语或验证过的 `.entwallet`，不要复制 DPAPI/Secret
  Service 本机保险库。
- 旧测试网钱包：只迁移已验证的钱包密钥或备份，绝不复制测试网链、数据库或内存池。
- 恢复短语采用 Entcoin 专有的版本化 P-256 派生，不是 BIP32/Bitcoin 路径；导入其他钱包
  软件不会得到相同地址。

## 安全边界

v1.5.0 继承 v1.2.1 已审计的共识规则，并通过全量测试、竞态检测、
静态分析和可达漏洞扫描。基础安全审计与本次共识升级证据分别记录在
[v1.1.0 安全审计报告](docs/security-audit-v1.1.0.md)和
[v1.2.1 共识升级审计](docs/consensus-upgrade-audit-v1.2.md)。

仍需清楚理解以下边界：

- 这不是独立第三方审计，也不是对未来漏洞的保证。
- P2P 默认没有身份认证和内置加密；公网 Seed 应由 HTTPS/WSS 反向代理保护传输。
- 节点发现不具备成熟的抗女巫/抗日食能力，少量固定 Seed 不能提供去中心化保证。
- GitHub Release 账户与 TLS 仍是更新校验和的信任根；Windows 是否签名取决于发布证书配置。
- 钱包解锁后密钥存在于进程内存中，尚无硬件签名、多签或内存锁定。
- 项目特有的快速链难度算法尚缺少长期对抗性网络和独立实现验证。

在完成独立审计、运营者多样化和成熟应急治理前，不应把 ENT 用于无法承受损失的价值。
安全问题请按 [SECURITY.md](SECURITY.md) 私下报告，不要在公开 Issue 中粘贴敏感细节。

## 从源码验证

要求 Go 1.26.5 与 Node.js 22：

```bash
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...

cd frontend
npm ci
npm audit --audit-level=high
npm test
npm run build
```

Linux 发布构建还需要 Wails v2.13.0、GTK3、WebKitGTK 4.1 与 `dpkg-deb`：

```bash
./scripts/build-linux.sh 1.5.0
```

## 文档

- [文档索引](docs/README.md)
- [架构](docs/architecture.md)
- [主网协议](docs/protocol.md)
- [节点运维](docs/operations.md)
- [公网 Seed](docs/public-seed.md)
- [EntPay Agent 支付](docs/entpay.md)
- [安全策略](SECURITY.md)
- [v1.5.0 中英文发布说明](RELEASE_NOTES.md)
- [后续路线图](docs/next-step.md)

Entcoin 采用 [MIT License](LICENSE) 开源。
