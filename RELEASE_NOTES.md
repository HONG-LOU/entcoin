# Entcoin v1.3.0

English | [简体中文](#简体中文)

Entcoin v1.3.0 adds EntPay Agent payments without changing
`entropy-mainnet-v1`, consensus, genesis, transaction encoding, wallets,
addresses, balances, chain data, or peer compatibility.

## EntPay Agent payments

EntPay lets a local AI agent buy a bounded resource with a normal ENT
transaction:

1. the merchant issues an Ed25519-signed invoice;
2. the client verifies the endpoint, network, merchant, resource, amount,
   expiry, confirmation requirement, and signature;
3. the locally configured Codex model approves or rejects the request while a
   deterministic maximum amount remains authoritative;
4. the selected local Entcoin wallet signs and broadcasts the transaction;
5. the merchant's validating node checks one exact invoice output and waits for
   one confirmation; and
6. the merchant returns a persisted, Ed25519-signed receipt and paid report.

Private keys and Codex credentials never leave the local machine. The public
merchant receives only the signed transaction intended for broadcast. Claim
tokens are random bearer capabilities stored only as SHA-256 digests.

The first paid resource is a live `network-report` comparing the two public
archive nodes. Production endpoints are:

- `https://entcoin.xyz/entpay/`
- `https://template-chat.xyz/entpay/`

## Reliability and security

- SQLite atomically prevents one transaction from paying two invoices.
- Payment submission and delivery are idempotent; retrying after a lost HTTP
  response returns the same signed delivery.
- Unknown or duplicate JSON fields, trailing data, oversized bodies, deep
  nesting, invalid signatures, wrong outputs, expired invoices, and reused
  transactions are rejected.
- Remote Agent endpoints require HTTPS. The Agent can use a dedicated wallet
  profile and restores the prior active profile before closing the ledger.
- EntPay remains a separate loopback service and database. No wallet-control
  method was added to the public Entcoin P2P listener.

## Artifacts

Windows releases now include `entpay.exe`; Linux releases include
`entpay-linux-amd64`. Both are covered by the release checksum files and GitHub
build-provenance attestations. See `docs/entpay.md` for protocol, client,
deployment, and limitation details.

EntPay v1 requires one confirmation and is intended for low-value community and
demonstration services. A one-block reorganization can reverse payment after
delivery. ENT is not a stable unit of account, and EntPay is not escrow,
custody, a bridge, a stablecoin, or a high-frequency payment channel.

## Verification

Release gates cover the complete Go suite and race detector, `go vet`, Windows
and Linux builds, EntPay protocol and replay regressions, frontend and website
tests, npm audit, reachable vulnerability scanning, Ubuntu package installation,
Secret Service wallet smoke tests, artifact SHA-256 checks, and build provenance.

## 简体中文

Entcoin v1.3.0 新增 EntPay Agent 支付，但不改变
`entropy-mainnet-v1`、共识、创世块、交易编码、钱包、地址、余额、链数据或节点兼容性。

## EntPay Agent 支付

EntPay 允许本地 AI Agent 使用普通 ENT 交易购买一个边界明确的资源：

1. 商户签发 Ed25519 签名的 Invoice；
2. 客户端独立核对端点、网络、商户、资源、金额、有效期、确认数和签名；
3. 本机配置的 Codex 模型决定批准或拒绝，但确定性的最大额度始终具有最终约束力；
4. 指定的本地 Entcoin 钱包完成签名和广播；
5. 商户自己的验证节点核对唯一且精确的 Invoice 输出，并等待一次确认；
6. 商户返回持久化保存、可重复领取且带 Ed25519 签名的 Receipt 和付费报告。

私钥和 Codex 凭据不会离开本机。公网商户只接收本来就要广播的签名交易。Claim token
是随机 bearer capability，服务端只保存其 SHA-256 摘要。

第一项付费资源是比较两个公网归档节点的实时 `network-report`。生产端点为：

- `https://entcoin.xyz/entpay/`
- `https://template-chat.xyz/entpay/`

## 可靠性与安全

- SQLite 原子阻止一笔交易支付两张 Invoice。
- 提交与交付都支持幂等重试；HTTP 响应丢失后会返回同一份签名交付结果。
- 未知/重复 JSON 字段、尾随数据、超限请求、深层嵌套、错误签名、错误输出、过期
  Invoice 和交易重放都会被拒绝。
- 远程 Agent 端点强制 HTTPS；Agent 可使用独立钱包 profile，并在关闭账本前恢复原钱包。
- EntPay 使用独立 loopback 进程和数据库；公网 Entcoin P2P 监听器没有新增钱包控制接口。

## 产物与边界

Windows Release 新增 `entpay.exe`，Linux 新增 `entpay-linux-amd64`；两者均进入 checksum
和 GitHub 构建来源证明。协议、客户端、部署和限制详见 `docs/entpay.md`。

EntPay v1 等待一次确认，只面向低价值社区服务和演示。一块深度的重组仍可能在交付后
逆转付款。ENT 不是稳定计价单位；EntPay 也不是托管、桥、稳定币、托管交易或高频支付通道。

## 验证

发布门禁覆盖完整 Go 测试与竞态检测、`go vet`、Windows/Linux 构建、EntPay 协议和
防重放回归、前端与官网测试、npm audit、可达漏洞扫描、Ubuntu 安装、Secret Service
钱包冒烟、附件 SHA-256 校验与构建来源证明。
