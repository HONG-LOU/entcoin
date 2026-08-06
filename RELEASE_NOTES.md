# Entcoin v1.5.1

English | [简体中文](#简体中文)

Entcoin v1.5.1 completes the interactive EntPay Agent payment flow without
changing `entropy-mainnet-v1`, consensus, genesis, transaction encoding,
wallets, addresses, balances, chain data, or peer compatibility.

This patch release fixes the production browser handoff from an HTTPS merchant
to the loopback confirmation page. The local Agent now permits only the initial
cross-site top-level `GET /` document navigation; all cross-site API requests,
approval calls, subresources, and non-loopback hosts remain blocked.

## Local Agent confirmation

The new `entpay agent-ui` command runs a confirmation application only on
`127.0.0.1`. A merchant page creates a signed, input-bound Invoice and opens it
in the local Agent. Before showing approval, deterministic code independently
checks the merchant key, signature, protocol, network, product, exact price,
request hash, expiry, confirmation policy, and the user's hard spending limit.

The user sees the merchant, request, amount, expiry, and confirmation count in
Chinese or English. Rejecting creates no transaction. Approving signs with the
selected local wallet, broadcasts the ordinary ENT transaction, waits for the
merchant's required confirmation, verifies the signed Receipt and delivery
hashes, and saves any artifact locally. Concurrent approval clicks still create
only one payment.

The merchant origin never receives wallet access. Claim capabilities are no
longer displayed or copied by the merchant page. Browser handoff uses a URL
fragment, which is not sent in HTTP requests and is immediately removed by the
loopback page.

## Interface and deployment boundary

The EntPay merchant interface is now an operational workspace: service catalog,
request form, declared terms, signed Invoice, payment rail, and one clear local
confirmation action. It defaults to Chinese for Chinese browser locales and
retains the current request and Invoice when switching languages.

The public Entcoin website also receives the continuous responsive redesign.
Concrete merchant products, image-provider credentials, signing credentials,
databases, logs, and generated artifacts remain outside this public repository
and are deployed independently by each merchant.

## Artifacts and verification

Windows releases include `entpay.exe`; Linux releases include
`entpay-linux-amd64`. Release checks cover the Go suite and race detector,
`go vet`, strict JSON and Invoice tampering regressions, rejection and
concurrent approval, verified artifact delivery, frontend and website tests,
desktop/mobile browser handoff, checksums, and build provenance.

## 简体中文

Entcoin v1.5.1 补齐 EntPay 的可交互 Agent 支付流程，但不改变
`entropy-mainnet-v1`、共识、创世块、交易编码、钱包、地址、余额、链数据或节点兼容性。

本补丁修复 HTTPS 商家页面打开本机确认页时的生产浏览器交接。Agent 只额外允许首次跨站、
顶层文档形式的 `GET /` 导航；跨站 API、确认请求、子资源和非 loopback 主机仍全部拒绝。

## 本机 Agent 确认

新增的 `entpay agent-ui` 只监听 `127.0.0.1`。商家页面创建与原始请求绑定的签名
Invoice，再把它交给本机 Agent。显示确认按钮前，确定性代码会重新核对商家公钥与签名、
协议、网络、商品、准确价格、请求哈希、有效期、确认策略和用户设置的硬支付上限。

用户可以用中文或英文核对收款商家、请求内容、金额、有效期和确认数。拒绝不会创建交易；
确认后由选定的本机钱包签名和广播普通 ENT 交易，等待商家要求的链上确认，验证签名
Receipt 与交付哈希，并把图片等产物保存到本机。并发重复点击确认也只会付款一次。

商家网页永远拿不到钱包控制权。页面不再显示或复制 claim capability。浏览器通过不会
进入 HTTP 请求的 URL fragment 交接，本机页面读取后立即从地址栏清除。

## 界面与部署边界

EntPay 商家页现在直接呈现服务目录、请求表单、声明条款、签名 Invoice、付款轨迹和唯一的
本机确认入口。中文浏览器默认显示中文，切换语言不会丢失当前请求或已经创建的账单。

公开官网同步上线连续场景响应式设计。具体商品、图片模型凭据、商家签名密钥、数据库、
日志与生成文件仍由各商家独立部署，不进入 Entcoin 公共仓库。

## 产物与验证

Windows Release 提供 `entpay.exe`，Linux 提供 `entpay-linux-amd64`。发布门禁覆盖完整 Go
测试与竞态检测、`go vet`、严格 JSON 与 Invoice 篡改回归、拒绝和并发确认、产物验收、
前端/官网测试、桌面与手机浏览器交接、checksum 和构建来源证明。
