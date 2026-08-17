# Entcoin v1.5.5

English | [简体中文](#简体中文)

Entcoin v1.5.5 makes a busy Agent Pay inbox easier to manage while preserving
payment recovery records. It does not change `entropy-mainnet-v1`, consensus,
transaction encoding, wallet formats, addresses, balances, chain data, EntPay
Invoice or Receipt signatures, or peer compatibility.

## Agent Pay request management

The desktop now shows at most five Agent Pay requests per inbox page. Previous
and next controls keep long histories inside the workspace instead of extending
the page indefinitely, and selecting a new page opens its first request.

Every safely removable row has a persistent trash action. Awaiting-approval
requests, terminal history, and retryable requests that have not broadcast a
transaction can be removed directly from the inbox or detail view. Completed
delivery files remain on disk when their history entry is removed.

## Payment safety boundary

The desktop and SQLite store enforce the same deletion policy. Active payments,
post-broadcast sessions, and retryable sessions with a transaction ID cannot be
deleted. Their records remain available for submit, confirmation, Receipt, and
artifact recovery after a restart.

Opening or deleting a request never authorizes payment. Every new Invoice still
requires the user to review the verified merchant, request, amount, wallet,
expiry, and fee ceiling before clicking the exact-amount payment button.

## Compatibility and artifacts

Windows and Ubuntu users can upgrade in place without migrating wallet or chain
data. The stable release includes the Windows desktop, installer, CLI, EntPay
binary and seed deployment package, plus the Linux desktop, CLI, EntPay binary
and Ubuntu amd64 package. All published files are covered by platform SHA-256
manifests and GitHub build-provenance attestations. Windows artifacts may remain
unsigned when no Authenticode certificate is configured and can therefore show
a SmartScreen warning. Release builds use Go 1.26.6, which removes six reachable
standard-library vulnerabilities present in the previous Go 1.26.5 toolchain.

## 简体中文

Entcoin v1.5.5 改善了桌面 Agent 支付请求较多时的管理体验，同时保留付款恢复所需记录。
本版本不改变 `entropy-mainnet-v1`、共识、交易编码、钱包格式、地址、余额、链数据、
EntPay Invoice/Receipt 签名或节点兼容性。

## Agent 支付请求管理

桌面端收件箱现在每页最多显示 5 条 Agent 支付请求。上一页、下一页和页码会把较长历史
固定在工作区内，不再让页面无限向下延伸；切换页面时会自动打开该页第一条请求。

每条可安全移除的请求都会常驻显示垃圾桶按钮。待批准请求、终态历史，以及尚未广播交易的
可重试请求，可以直接从收件箱或详情页删除。删除已完成记录时，已交付文件仍保留在磁盘上。

## 付款安全边界

桌面与 SQLite 存储执行相同删除策略。正在付款、已经广播，以及带 transaction ID 的可重试
会话不能删除，因此重启后仍可继续提交、确认、Receipt 验证和文件交付恢复。

打开或删除请求都不会授权付款。每张新 Invoice 仍要求用户检查已验证商家、请求、金额、钱包、
有效期和手续费上限，并亲自点击带准确金额的支付按钮。

## 兼容性与产物

Windows 与 Ubuntu 用户可以原地升级，无需迁移钱包或链数据。稳定 Release 包含 Windows
桌面程序、安装器、CLI、EntPay 和 seed 部署包，以及 Linux 桌面程序、CLI、EntPay 和
Ubuntu amd64 包。所有文件均由平台 SHA-256 清单与 GitHub build provenance 覆盖。
如果发布环境没有配置 Authenticode 证书，Windows 产物仍可能未签名并触发 SmartScreen。
发布构建升级到 Go 1.26.6，移除了旧 Go 1.26.5 工具链中的 6 个可达标准库漏洞。
