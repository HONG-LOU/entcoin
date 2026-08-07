# Entcoin v1.5.3

English | [简体中文](#简体中文)

Entcoin v1.5.3 makes EntPay delivery a merchant-independent desktop contract
without changing `entropy-mainnet-v1`, consensus, transaction encoding, wallet
formats, addresses, balances, chain data, or peer compatibility.

## Agent Pay desktop

The desktop now includes an Agent Pay workspace for incoming requests, review,
payment progress, verified results, history, and settings. Before payment it
shows the merchant identity, product, human-readable input, exact amount, fee
ceiling, active wallet, expiry, and verification evidence. Every new Invoice
requires a visible click on the exact-amount Pay button; opening a link never
authorizes payment.

Payment reuses the desktop's active `node.Service` instead of opening a second
wallet database. The client journals the transaction before broadcast and
persists a revision-checked state machine in a separate `entpay-client.db`, so
post-broadcast submit, confirmation, claim, Receipt verification, and artifact
delivery can resume without creating another payment.

Every new merchant returns the same `entpay-result-v1` envelope: a verified
summary plus opaque JSON data. The desktop does not branch on merchant or
product IDs. Verified artifacts of any allowed media type use bounded downloads,
safe names, atomic saves, hash evidence, and explicit open/reveal actions; media
is not embedded in the payment UI. New services can deploy without a desktop
release. Merchant launch also uses same-page protocol navigation, avoiding the
empty `about:blank` tab created by browser popup handling.

## Secure browser handoff

Windows installers and Ubuntu packages register `entcoin://`. The operating
system receives only a public merchant endpoint:

```text
entcoin://pay?v=1&merchant=https%3A%2F%2Fmerchant.example%2Fentpay%2F
```

The short-lived 256-bit handoff code stays in the merchant HTTPS response and
is POSTed separately to the desktop's fixed `127.0.0.1:47833` relay. The relay
requires an exact, recently bootstrapped merchant Origin, strict JSON and PNA
preflight, accepts one bounded request, and cannot approve or send a payment.
Secret-bearing system launch arguments are rejected.

Gateway handoff capsules are encrypted at rest, stored under digest-only codes,
bound to a client nonce, short-lived, rate-limited, and redeemed with generic
not-found responses. The desktop encrypts recovery capabilities with Windows
DPAPI or Linux Secret Service backed XChaCha20-Poly1305. Merchant address or
signing-key changes block payment until the user explicitly establishes trust
again.

## Compatibility and artifacts

The existing `entpay agent` and `entpay agent-ui` commands remain available for
automation, development, and compatibility. Invoice and Receipt signatures are
unchanged. Windows artifacts include `entpay.exe`; Linux artifacts include
`entpay-linux-amd64` and the Ubuntu amd64 package. Published artifacts are
covered by SHA-256 checksums and GitHub build-provenance attestations.

## 简体中文

Entcoin v1.5.3 将 EntPay 交付统一为与商家无关的桌面协议，同时不改变
`entropy-mainnet-v1`、共识、交易编码、钱包格式、地址、余额、链数据或节点兼容性。

## 桌面 Agent 支付

桌面端新增 Agent 支付工作台，包含待处理请求、付款审核、进度、已验证结果、历史和设置。
付款前会显示商家身份、商品、人类可读请求、准确金额、手续费上限、当前钱包、有效期和验证
证据。每张新账单都必须由用户点击带准确金额的支付按钮；打开链接永远不会授权付款。

付款复用桌面正在运行的 `node.Service`，不会再打开第二个钱包数据库。客户端在广播前记录
交易，并把带 revision 校验的状态机持久化到独立 `entpay-client.db`。广播后的提交、确认、
领取、Receipt 验证和文件交付可以在重启后继续，且不会创建第二笔付款。

所有新商家统一返回 `entpay-result-v1`：一段已验证摘要和不透明 JSON 数据。桌面端不再按
商家或商品 ID 分支。任何允许类型的已验证附件都采用有界下载、安全文件名、原子保存和
哈希证据，只提供打开与定位操作，不在支付界面内嵌媒体。新增服务无需发布桌面新版。
商家启动桌面时改为当前页面协议导航，不再因弹窗机制留下空白 `about:blank` 标签页。

## 安全浏览器交接

Windows 安装包和 Ubuntu 包会注册 `entcoin://`。操作系统启动参数只接收公开 merchant
endpoint，不接收 handoff code。短时 256-bit code 保留在商家 HTTPS 响应体中，再单独 POST
到桌面的固定 `127.0.0.1:47833` relay。Relay 要求最近通过 bootstrap 的完全匹配 Origin、
严格 JSON 和 PNA 预检，只接受一次有界请求，而且不能批准或发送付款。任何含秘密 handoff
的系统启动参数都会被拒绝。

Gateway capsule 静态加密保存，只存 code digest，绑定客户端 nonce，并执行短时有效期、限流
和统一 404。桌面恢复 capability 使用 Windows DPAPI，或 Linux Secret Service 配合
XChaCha20-Poly1305 加密。商家地址或签名密钥变化时会阻止付款，直到用户显式重新建立信任。

## 兼容性与产物

现有 `entpay agent` 和 `entpay agent-ui` 继续作为自动化、开发和兼容入口；Invoice/Receipt
签名字段保持不变。Windows 产物包含 `entpay.exe`，Linux 产物包含 `entpay-linux-amd64` 和
Ubuntu amd64 包。正式发布产物由 SHA-256 checksum 与 GitHub build provenance 覆盖。
