# EntPay 桌面端集成完整方案

状态：代码实现完成，生产发布门禁尚未全部通过
目标版本：1.5.2 发布候选
适用范围：Windows 10/11 x64、Ubuntu 24.04+ amd64、现有 Wails 桌面端、EntPay v1

## 1. 结论

EntPay 应直接集成进现有 Entcoin 桌面端，普通用户不应再手动安装运行环境、启动 `entpay agent-ui`、配置钱包目录或操作 localhost 页面。

目标体验：

```text
用户在商家页面选择服务并填写请求
                    ↓
点击“Open in Entcoin / 在 Entcoin 中确认”
                    ↓
Entcoin 自动启动或切到前台，并打开 Agent Pay 审批页
                    ↓
用户核对商家、商品、请求、金额、手续费和钱包
                    ↓
点击“Pay 0.001 ENT / 支付 0.001 ENT”
                    ↓
桌面端签名、广播、等待确认、验证 Receipt 和交付哈希
                    ↓
在桌面端预览结果，并可打开或定位已保存文件
```

用户只需安装 Entcoin 桌面端。钱包私钥继续由当前桌面节点管理，商家、网页和 AI Agent 都无法读取私钥。

现有 `entpay agent` 和 `entpay agent-ui` 保留为开发者、服务器和兼容入口，但不再作为普通用户的主流程。

## 2. 产品边界

### 2.1 第一版必须完成

- 在现有桌面端新增一级页面 `Agent Pay / Agent 支付`。
- 商家网页可通过系统链接启动或聚焦 Entcoin。
- 桌面端独立重新获取并校验商家信息、商品条款、签名 Invoice 和请求哈希。
- 付款前展示完整的人类可读确认页，默认必须人工确认。
- 复用当前正在运行的 `node.Service` 创建交易，不再打开第二个节点或钱包数据库。
- 展示广播、确认、履约、Receipt 验证和文件保存全过程。
- 购买会话可在应用崩溃或重启后恢复。
- 提供购买历史、结果预览、打开文件、定位文件和复制交易 ID。
- 英文和简体中文同时交付。
- Windows 安装版和 Ubuntu Deb 自动注册 `entcoin://` 系统协议。

### 2.2 第一版不做

- 不默认自动付款。
- 不宣称存在去中心化全球商店或商家信誉系统。
- 不提供托管、退款、争议仲裁、订阅、支付通道或法币定价。
- 不允许网页嵌入桌面审批界面，也不在桌面 WebView 中加载商家网页。
- 不允许商家指定本地保存路径、执行本地程序或读取本地文件。
- 不因为 Receipt 签名有效就声称商家内容真实、模型正确或服务合法。
- 不改变 Entcoin 共识、交易编码、地址、钱包格式或 P2P 协议。

### 2.3 后续能力

- 受认证的本机 Agent IPC/API。
- 用户定义的单笔、每日、商家和商品策略。
- 低风险请求的可选自动批准。
- 签名商家目录、收藏和信誉信息。
- 硬件钱包、离线签名和系统级生物识别确认。

这些能力必须建立在第一版人工确认闭环之上，不能提前弱化付款边界。

## 3. 当前实现与缺口

当前实现已经具备：

- `entpay.Invoice` 和 `entpay.Receipt` 的 Ed25519 签名与验证。
- 商品、价格、网络、输入哈希、过期时间和确认数校验。
- 普通 ENT 交易付款与 merchant exact-output verification。
- SQLite 幂等履约、claim capability、artifact 授权和哈希验证。
- `RunPreparedAgent`、`InspectPreparedPayment` 和进度回调。
- 只绑定 loopback 的本机审批网页。
- 真实生成照片购买闭环。

实现前的桌面产品化缺口与落地方案：

| 缺口 | 影响 | 方案 |
| --- | --- | --- |
| 用户要单独启动 `agent-ui` | 安装和使用门槛高 | 能力内置到 Entcoin 进程 |
| `localWalletPayment` 会打开第二个节点 | 与已运行桌面的目录锁冲突 | 注入当前 `node.Service` 的付款回调 |
| 浏览器 handoff 依赖 localhost 页面 | 桌面未运行时无法接收 | 系统协议启动 + 一次性 handoff capsule |
| 会话只在内存中 | 崩溃后可能丢失付款后状态 | 独立客户端 SQLite 和加密 capability |
| 只显示当前一次请求 | 无购买记录和结果管理 | Agent Pay 收件箱、历史和详情 |
| 付款前没有准确手续费预览 | 用户不知道总支出上限 | 增加只读交易预览和 fee ceiling |
| 当前进度粒度较粗 | 错误与恢复方向不清楚 | 持久化状态机和错误分类 |
| Claim token 通过页面 fragment 交接 | 不适合系统协议和大 input | 短时一次性兑换码，只在 HTTPS body 返回敏感 capsule |

## 4. 用户场景

### 4.1 用户从商家网站购买

1. 用户通过官网、社区、X、朋友或 Agent 推荐找到商家的 HTTPS 地址。
2. 用户在商家页面选择商品并填写请求。
3. 商家 Gateway 签发绑定输入的 Invoice，同时创建短时 handoff code。
4. 用户点击“在 Entcoin 中确认”。
5. 操作系统打开 `entcoin://pay?...`；若 Entcoin 已运行，单实例回调将链接交给现有进程。
6. Entcoin 通过 HTTPS 用 handoff code 兑换完整请求，重新读取 `/v1/info` 并验证全部确定性条件。
7. 桌面端进入 Agent Pay 审批页。用户拒绝时不创建交易。
8. 用户批准后，当前桌面节点使用当前活动钱包签名并广播普通 ENT 交易。
9. 桌面端等待商家要求的确认数，验证 Receipt、payload 和 artifact。
10. 结果在桌面端预览，文件以安全名称原子保存。

### 4.2 AI Agent 为用户准备购买

第一版中，Agent 可以发现服务、整理输入并让商家生成 handoff，但最终仍通过 Entcoin 桌面端显示人工确认。Agent 不持有钱包权限，也不能模拟确认按钮。

后续本机 Agent API 使用操作系统用户级认证或 Unix socket/named pipe，把准备好的请求提交给桌面端。API 只能创建“等待确认”的会话，默认不能直接调用付款。

### 4.3 用户直接在桌面端打开商家

Agent Pay 空状态提供“Open payment link / 打开支付链接”输入。用户可以粘贴商家生成的 EntPay handoff URL，但桌面端不把任意普通网页当作支付请求，也不从剪贴板自动读取。

### 4.4 用户查看历史交付

完成后的会话保留 Invoice、交易 ID、Receipt、结果摘要、artifact 路径和哈希。Claim token 完成后立即从持久化 secret capsule 中删除。用户可以删除历史记录，但删除历史默认不删除已经保存的交付文件；删除文件必须单独确认。

## 5. 总体架构

```text
┌──────────────────── Merchant HTTPS ─────────────────────┐
│ /v1/info  /v1/invoices  /v1/handoffs/redeem  /claim    │
└──────────────────────────┬───────────────────────────────┘
                           │ signed Invoice + one-time capsule
                           ▼
┌──────────────────── Entcoin desktop process ────────────────────┐
│                                                                 │
│  URI receiver ──► EntPay session manager ──► Wails events/UI    │
│                         │                      │                  │
│                         │ approve              │ review/result    │
│                         ▼                      │                  │
│                  active node.Service ◄─────────┘                  │
│                         │                                        │
│                  sign + broadcast                                │
│                         │                                        │
│  entpay-client.db + encrypted capsule + verified artifacts       │
└─────────────────────────┬────────────────────────────────────────┘
                          │ ordinary ENT transaction
                          ▼
                 Entcoin peer-to-peer network
```

信任边界：

- 商家控制商品、描述、价格、签名密钥、履约程序和服务器。
- Entcoin 桌面端控制钱包、付款确认、交易签名、Receipt 验证和文件保存。
- 浏览器只负责展示商家商品并触发 handoff，不获得桌面 API 或钱包访问。
- AI Agent 可以准备意图，不能绕过确定性校验和用户确认。
- 链上交易证明付款，不证明商家内容的真实性或质量。

## 6. 浏览器到桌面的安全 Handoff

### 6.1 为什么不能直接把现有 JSON 放进 `entcoin://`

- 输入最大可达 128 KiB，超过多个操作系统和浏览器对 URL/启动参数的可靠范围。
- Claim token 出现在命令行参数时，可能短暂暴露在进程列表、崩溃报告或诊断日志中。
- URL 编码、Unicode、引号和平台 shell 处理会增加注入与截断风险。
- 完整 Invoice 通过系统链接传递后仍需重新验证，直接携带没有收益。

因此系统链接只携带公开的商家 endpoint。短时随机 handoff code 保留在商家 HTTPS 响应体中，桌面启动后由商家页面 POST 到固定 loopback relay；code、input 和 claim token 都不进入操作系统启动参数。

### 6.2 系统链接格式

```text
entcoin://pay?v=1&merchant=https%3A%2F%2Fmerchant.example%2Fentpay%2F
```

约束：

- scheme 固定为 `entcoin`，host 固定为 `pay`。
- 仅接受 `v=1`。
- 生产 merchant 必须为 HTTPS，localhost 开发例外沿用现有规则。
- URL 总长度限制为 2048 bytes。
- 不接受 userinfo、fragment、非默认危险端口、重复参数或未知参数。
- URI 本身不是 handoff capability 或支付授权，只允许对应 merchant origin 在 2 分钟内连接本机 relay。

### 6.3 桌面 loopback relay

桌面启动后只监听 `127.0.0.1:47833` 的 `POST /v1/handoffs`。商家页面提交：

```json
{"merchant":"https://merchant.example/entpay/","handoff":"<base64url>"}
```

- 仅在同一 merchant bootstrap URI 已通过严格解析且仍在 2 分钟窗口时接受。
- 浏览器 `Origin` 必须与 merchant endpoint 的 scheme/host/port 完全一致；不使用通配 CORS。
- 支持浏览器 Private Network Access preflight，但只为已授权 origin 返回 allow header。
- POST 使用 `application/json`、4 KiB 上限、固定字段、重复/未知字段拒绝和 `Cache-Control: no-store`。
- handoff 为至少 256-bit 随机 base64url；接收后进入原有 8 项有界队列并立即撤销 origin 授权，重放返回拒绝。
- relay 不能付款，只能创建待验证、待人工审批会话。商家签名、金额、input hash、钱包与网络门禁仍由后端重新检查。

用户在已运行桌面的 Agent Pay 页面粘贴完整 handoff URL 时，code 由 Wails 表单直接交给后端，不经过进程参数。系统 handler 明确拒绝任何包含 `handoff` 的 URI。

### 6.4 Gateway 扩展

新增：

```text
POST /v1/handoffs/redeem
Content-Type: application/json

{
  "code": "...",
  "client_nonce": "..."
}
```

成功响应：

```json
{
  "endpoint": "https://merchant.example/entpay/",
  "input": {"prompt": "..."},
  "created": {
    "invoice": {},
    "claim_token": "..."
  }
}
```

规则：

- Handoff code 只存 SHA-256 digest。
- 默认有效期 2 分钟，不能晚于 Invoice expiry。
- 第一次兑换时绑定桌面随机生成的 `client_nonce`。
- 同一个 code + nonce 在短窗口内可幂等重试，用于处理响应丢失。
- 不同 nonce 再兑换返回 `404`，避免泄露 code 是否存在。
- 响应使用 `Cache-Control: no-store`，code 放 JSON body，不进入 access-log URL。
- Gateway 对兑换接口做 IP 和 invoice 双维度限流。
- 兑换成功不代表付款，桌面端仍执行完整 `InspectPreparedPayment`。

这属于 EntPay 应用层向后兼容扩展，不改变 `entpay-v1` Invoice/Receipt 签名字段。旧 CLI 和现有 claim 路由继续工作。

### 6.5 桌面未运行与已经运行

- 首次启动：`main.go` 在创建 Wails 前只解析不含秘密的 bootstrap URI，授权 merchant origin，不进行商家网络请求。
- 已运行：Wails `SingleInstanceLock.OnSecondInstanceLaunch` 接收 `SecondInstanceData.Args`，校验 bootstrap 后授权同一 relay 并聚焦窗口。
- 商家页在应用启动后把 handoff 作为 loopback POST body 交付；桌面验证并投递有界队列。
- 节点未就绪：先显示启动过程，URI 保留在有界队列；节点就绪后再兑换。
- 同一时间最多排队 8 个不同 handoff；按 `(merchant, handoff digest)` 去重。
- 解析失败只显示本地错误，不把原始 URL 或 handoff 写入日志。

### 6.6 系统注册

Windows 安装版：

- NSIS 在当前用户范围注册 `HKCU\Software\Classes\entcoin`。
- 描述为 `URL:Entcoin payment request`，设置空字符串 `URL Protocol`。
- open command 必须严格引用可执行文件和 `%1`。
- 卸载时只删除仍指向当前安装路径的注册项，不能删除被其他版本接管的值。
- Portable EXE 不静默修改注册表；Diagnostics 提供“Register payment links / 注册支付链接”显式操作。

Ubuntu Deb：

- `entcoin.desktop` 增加 `MimeType=x-scheme-handler/entcoin;`。
- `Exec=/usr/bin/entcoin %u`。
- 安装后更新 desktop database；卸载不删除用户数据。
- 验证 `xdg-mime query default x-scheme-handler/entcoin` 返回 `entcoin.desktop`。

若协议未注册，商家页提供“Entcoin not opening? / 无法打开？”入口，指导安装桌面端或复制短时链接。不得回退为显示 Claim token 原始 JSON。

## 7. 客户端状态机

持久化状态：

```text
received
   ↓
inspecting ───────────────► invalid / expired
   ↓
awaiting_approval ────────► rejected / expired
   ↓ approve
preparing_payment ────────► failed_retryable / failed_terminal
   ↓
broadcast
   ↓
submitting
   ↓
confirming
   ↓
fulfilling
   ↓
verifying
   ↓
complete
```

状态要求：

- 所有状态转换由 Go 后端执行，前端不能直接写状态。
- 每次转换在 SQLite 事务中记录当前状态、时间和必要恢复数据。
- `awaiting_approval` 之前绝不能调用钱包付款。
- `broadcast` 之后必须保存交易 ID 和原始交易，再提交 merchant。
- 重启时只自动恢复 `broadcast` 之后的查询、提交和验证；绝不自动恢复“尚未付款”的确认动作。
- `failed_retryable` 只允许重试网络查询、submit、claim、artifact 下载和验证。
- 已经存在交易 ID 的会话，重试时绝不创建第二笔交易。
- Invoice 过期只阻止新付款，不阻止已经广播交易的 submit、claim 和交付恢复。
- 用户拒绝后删除 capability，不允许把同一会话重新变成待批准；商家需创建新 Invoice。

建议将现有 `RunPreparedAgent` 拆成可恢复步骤，而不是在桌面端外围猜测内部进度。

## 8. Go 后端设计

### 8.1 模块布局

建议新增：

```text
entpay/
  client_manager.go       会话编排与状态机
  client_store.go         SQLite 持久化
  client_secure.go        敏感 capsule 接口
  client_secure_linux.go  Secret Service + XChaCha20-Poly1305
  client_secure_windows.go DPAPI current-user protection
  handoff.go              URI 与兑换协议
  delivery_preview.go     安全预览元数据

app_entpay.go             Wails 绑定和 node.Service 付款适配
app_entpay_test.go
```

不要把业务状态继续堆入已经较大的 `app.go` 和 `frontend/src/main.js`。前端建议同步拆出 `frontend/src/entpay.js`，样式放入 `frontend/src/entpay.css`，通用金额和错误格式化逐步抽到小模块。

### 8.2 App 生命周期

`App` 增加：

```go
entpayManager *entpay.ClientManager
handoffQueue  chan entpay.LaunchRequest
```

启动顺序：

1. 解析初始 URI，只入队。
2. 启动现有 `node.Service`。
3. 节点 ready 后创建 client store 和 manager。
4. 恢复未完成的付款后状态。
5. 消费 handoff 队列并向前端发送事件。

关闭顺序：

1. 拒绝新 handoff。
2. cancel manager context。
3. 等待兑换、claim 和下载 goroutine 有界结束。
4. checkpoint/关闭 client SQLite。
5. 最后关闭 `node.Service`。

不能使用脱离 App 生命周期的 `context.Background()` 长任务。每个购买会话拥有 child context，应用退出时统一取消。

### 8.3 Wails API

建议只暴露结构化方法：

```text
GetEntPayOverview()
GetEntPaySession(id)
ApproveEntPaySession(id, expectedRevision)
RejectEntPaySession(id, expectedRevision)
RetryEntPaySession(id, expectedRevision)
DeleteEntPaySession(id)
ChooseEntPayArtifactDirectory()
OpenEntPayArtifact(id)
RevealEntPayArtifact(id)
Copy 不需要后端，仍使用受控 clipboard
GetEntPaySettings()
SaveEntPaySettings(settings, expectedRevision)
RegisterEntPayLinks()       Windows portable 显式调用
GetEntPayLinkStatus()
```

所有写操作带 `expectedRevision`，避免用户在旧页面上重复批准或覆盖更新后的状态。

事件：

```text
entcoin:entpay-session      单个会话状态变化
entcoin:entpay-incoming     新请求，需要切换页面并聚焦
entcoin:entpay-settings     设置变化
```

事件只携带可展示 DTO，不携带 claim token、handoff code、原始交易私密材料或本地加密 key。

### 8.4 复用当前钱包

桌面端不能调用当前 `localWalletPayment(dataDirectory, walletAddress)`，因为它会尝试再次锁定同一个数据目录。

新增节点接口：

```go
PreviewRecommendedPayment(expectedWallet, to string, amount uint64) (PaymentPreview, error)
SendRecommendedFrom(expectedWallet, to string, amount uint64, maximumFee uint64) (core.Transaction, uint64, error)
```

要求：

- 两个方法都使用现有 `walletMutationMu` 和 service lock。
- `expectedWallet` 必须等于审批页展示的钱包；钱包改变时付款失败并要求重新审核。
- Preview 返回 amount、预计 fee、total debit、spendable balance 和是否有足够成熟 UTXO。
- Approve 时使用 preview 生成的 fee ceiling；实际最低 fee 高于 ceiling 时停止并要求重新确认。
- 不允许 EntPay 在后台切换活动钱包。
- 挖矿可继续运行，但钱包切换、恢复、删除和 EntPay 签名必须通过同一 mutation boundary 串行化。
- 钱包未确认备份时，第一版阻止 Agent Pay，并引导用户先完成备份。

### 8.5 付款前网络门禁

审批页可在节点同步时打开，但 Pay 按钮必须满足：

- 节点已 ready，非 seed mode。
- 当前钱包与预览钱包一致。
- 钱包已完成恢复材料确认。
- spendable balance >= amount + maximum fee。
- Invoice 未过期，签名和 input hash 仍有效。
- 至少连接一个 peer，且本地链没有明确落后于 best peer。
- 当前没有同 invoice 的已广播交易。

网络暂时不可用时显示“等待节点同步”，不要把按钮伪装成可点击后再报通用错误。

### 8.6 客户端持久化

在现有 data directory 中新增 `entpay-client.db`，与主链 `entropy.db` 分离，权限为用户私有。建议字段：

```text
sessions
  id, revision, endpoint, merchant, signing_key, signing_key_fingerprint
  merchant_trust_state
  invoice_id, resource, product_name, amount, confirmations
  invoice_json, input_sha256, encrypted_capsule
  wallet_address, fee_ceiling, transaction_id, transaction_json
  stage, error_code, error_message
  receipt_json, payload_json
  artifact_path, artifact_sha256, artifact_media_type, artifact_bytes
  created_at, updated_at, completed_at
```

敏感项：

- `encrypted_capsule` 包含 input、claim token 和恢复所需的 handoff 数据。
- Linux 使用新的 Secret Service account `mainnet-v1-entpay-session-key` 保护随机 key，再以 XChaCha20-Poly1305 加密；AAD 绑定 network、session ID 和 schema version。
- Windows 使用 current-user DPAPI，optional entropy 同样绑定 session ID 和 schema version。
- Claim 完成、拒绝或确定失败后，从 capsule 中删除 claim token。
- 日志、Wails DTO、崩溃错误和诊断导出永远不包含 capsule 明文。
- 数据库 schema 版本独立迁移；迁移失败不影响主节点和钱包启动，Agent Pay 页面进入只读故障状态。

默认保留 Invoice、Receipt、交易和结果记录；原始 input 加密保留 30 天后清理。用户可立即清理某条请求内容或全部 Agent Pay 历史。

### 8.7 Artifact 保存

默认目录：

- Windows：`%USERPROFILE%\Downloads\EntPay`
- Linux：XDG Downloads 下 `EntPay`，无法解析时使用用户 home 下 `Downloads/EntPay`

规则：

- 只接受 merchant Receipt 中已验证的文件名、media type、size 和 SHA-256。
- 文件名经过 basename、长度、保留名和平台字符校验。
- 商家文件名只作为建议名；冲突时添加 invoice ID 短后缀，不覆盖已有文件。
- 下载到同目录随机 `.part`，校验后 `fsync + atomic rename`。
- 最大 artifact 继续为 20 MiB；未来调整需要协议与 UI 同步。
- 图片只允许通过受控 blob/object URL 在桌面 WebView 预览；HTML、SVG、脚本和未知格式只显示文件信息，不内联执行。
- `Open` 和 `Reveal in folder` 由平台适配器对已验证的绝对路径执行，拒绝 symlink、目录穿越和已被替换的文件。

## 9. 桌面 UI/UX 设计

### 9.1 页面位置

左侧/顶部现有一级导航调整为：

```text
Overview | Transactions | Agent Pay | Wallet | Diagnostics
```

Agent Pay 使用 `Bot` 或 `BadgeCheck` 类 Lucide 图标，标签始终带文字，不使用陌生的纯图标导航。

新 handoff 到达时：

- 应用窗口恢复并前置。
- 自动切换到 Agent Pay 请求详情。
- 导航显示一个珊瑚色小圆点，但不闪烁。
- 不播放声音，不使用系统级高频通知。

### 9.2 视觉方向

主题：`Proof desk / 验证工作台`。它不是商店，也不是 AI 聊天框，而是用户签署一笔机器交易前的证据桌面。

配色沿用新官网，但适配高频桌面操作：

| Token | Hex | 用途 |
| --- | --- | --- |
| Ink | `#111413` | 主文字、完成态深色区域 |
| Cool paper | `#F4F6F2` | 主背景 |
| Mineral | `#58D6BD` | 已验证、完成、连接 |
| Coral | `#FF6B5F` | 待确认、付款主操作、风险焦点 |
| Steel | `#AEB9B5` | 次级文字、边界和时间轴 |
| Warning | `#D69A3A` | 过期、同步、余额和可恢复错误 |

禁止渐变、发光球、玻璃拟态和大圆角卡片。沿用现有桌面字体，数字、哈希和金额使用等宽字体。卡片圆角不超过 6px。

唯一标志性元素是 `Verification rail / 验证轨`：一条贯穿请求详情顶部的细线，依次连接 Invoice、User approval、On-chain、Receipt、Delivery。它编码真实状态，不是装饰。状态前进时只有节点和短线发生一次 180ms 位移动效；减少动效模式直接切换。

### 9.3 Agent Pay 首页

```text
┌──────────────────────────────────────────────────────────────────┐
│ Agent payments                              [Settings icon]       │
│ Review what software wants to buy with your wallet.              │
├──────────────────────────────────────────────────────────────────┤
│ PENDING 1                                                        │
│ ● Image generation     0.00100000 ENT      Waiting for approval  │
│   merchant.example     expires in 08:42                  [Open →] │
├──────────────────────────────────────────────────────────────────┤
│ RECENT                                                           │
│ ✓ Generated photo      0.00100000 ENT      Delivered       09:04 │
│ ✓ Data report          0.00250000 ENT       Verified       Aug 5 │
│ × Document conversion  0.00080000 ENT       Rejected       Aug 4 │
├──────────────────────────────────────────────────────────────────┤
│ [Open payment link]                                              │
└──────────────────────────────────────────────────────────────────┘
```

列表使用行，不使用卡片墙。默认排序：待批准、进行中、可重试失败、最近完成。金额与状态列有稳定宽度，不因文案变化导致布局跳动。

空状态只显示：

- `No Agent payments yet / 还没有 Agent 支付`
- `Purchases you review in Entcoin will appear here.`
- 一个 `Open payment link` 次要操作。

不在应用内放营销说明或教程大段落。

### 9.4 付款审批页

```text
┌──────────────────────────────────────────────────────────────────┐
│ ← Agent Pay       PAYMENT REQUEST          Expires in 08:42      │
│                                                                  │
│  Invoice ●──────── Approval ○──────── Chain ○──── Receipt ○────○ │
│                                                                  │
│  Generate a photo                         0.00100000 ENT          │
│  merchant.example                         + network fee 0.000...  │
│                                            ───────────────────   │
│  REQUEST                                   Max debit 0.001... ENT │
│  “Two cats discussing a plan...”                                  │
│                                                                  │
│  VERIFIED                                                         │
│  ✓ Signed invoice        ✓ Request hash       ✓ Price             │
│  ✓ entropy-mainnet-v1    ✓ 1 confirmation     ✓ HTTPS merchant    │
│                                                                  │
│  PAYING FROM                                                     │
│  ent1abc...xyz                 Spendable 84.21000000 ENT          │
├──────────────────────────────────────────────────────────────────┤
│ [Reject]                                  [Pay 0.00100000 ENT]    │
└──────────────────────────────────────────────────────────────────┘
```

信息优先级：

1. 用户买什么。
2. 商家是谁。
3. 请求内容是什么。
4. 商品金额、预计网络费和最大总支出。
5. 从哪个钱包付款、可用余额。
6. Invoice expiry 和确认数。
7. 技术证据折叠区。

主按钮必须写精确动作和金额，例如 `Pay 0.00100000 ENT`，不能写 `Confirm`、`Continue` 或 `Submit`。拒绝按钮写 `Reject`，完成提示写 `Payment rejected; no transaction was created`，动作名称前后一致。

技术证据区默认展示通过/失败摘要，可展开查看完整 merchant address、signing key fingerprint、invoice ID、input SHA-256、expiry、network 和 resource ID。Claim token 永不展示或复制。

商家提供的名称、描述、请求和 payload 一律按不可信文本渲染，只能写入 `textContent`，不能作为 HTML。桌面端不自动翻译商家内容；协议后续可以增加有界的本地化字段，但缺少中文时应显示商家原文，不能让本机 AI 在付款页悄悄改写商品条款。

### 9.5 进行中页面

```text
Invoice ✓ ─ Approval ✓ ─ On-chain ● ─ Receipt ○ ─ Delivery ○

Waiting for 1 confirmation
Transaction 4a91...e8c2
Current confirmations 0 / 1

[Copy transaction ID]                         [Run in background]
```

- 付款后不再显示 Reject。
- 关闭详情或切换页面不会取消链上交易和交付恢复。
- `Run in background` 只是返回列表，不停止任务。
- 预计时间只能基于协议目标说明为“通常约一个或多个区块”，不能承诺准确秒数。
- 网络中断时保留 transaction ID，明确显示“Payment was broadcast; delivery checking will resume automatically”。

### 9.6 完成交付页

图片结果使用大面积真实预览，不放进多层卡片：

```text
┌──────────────────────────────────────────┬───────────────────────┐
│                                          │ VERIFIED DELIVERY     │
│            generated image               │ Receipt signature  ✓ │
│                                          │ Payload hash       ✓ │
│                                          │ Artifact hash      ✓ │
│                                          │ 0.00100000 ENT        │
│                                          │ 1 confirmation        │
├──────────────────────────────────────────┴───────────────────────┤
│ [Open file] [Show in folder] [Copy transaction ID]              │
└──────────────────────────────────────────────────────────────────┘
```

JSON-only结果使用可读摘要优先，原始 JSON 放入展开区。未知二进制只显示文件名、类型、大小与 SHA-256，不尝试预览。

### 9.7 设置

Agent Pay 设置使用 unframed 表单区，不做设置卡片：

- `Require approval for every payment`：固定开启，第一版不可关闭。
- `Maximum per purchase`：默认 `0.01000000 ENT`，范围大于 0 且不超过协议金额上限。
- `Artifact folder`：显示路径，按钮为文件夹图标 + `Choose folder`。
- `Keep encrypted request details`：默认 30 天，可选完成后立即清理、7 天、30 天、永久。
- `Register payment links`：仅 Windows portable 需要显示。
- `Clear Agent Pay history`：危险操作，说明不会自动删除已经交付的文件。

第一版不提供一个看似可用但实际危险的“Auto approve”开关。

### 9.8 窄窗口与可访问性

- 现有最小宽度 860px 下，审批页改为单列，付款摘要放在请求内容之后，不叠加浮层。
- Verification rail 保持五个等宽节点；长中文换行到节点下方，不缩放字体。
- 底部付款操作区可 sticky，但必须占据正常布局高度，不能遮挡最后一行内容。
- 所有图标按钮有 tooltip 和 aria-label。
- 焦点顺序按返回、证据、请求、钱包、Reject、Pay 排列。
- 新请求前置窗口后，焦点落在页面标题，不直接落到 Pay。
- 使用 `aria-live=polite` 报告确认数与交付变化，不每两秒重复朗读相同内容。
- 完整支持 `prefers-reduced-motion`、键盘操作和高对比焦点。

## 10. 安全策略

### 10.1 永远由确定性代码检查

- Endpoint 规范化和 HTTPS。
- `entpay-v1` 与 `entropy-mainnet-v1`。
- Merchant address 和 signing key。
- Product ID、价格、确认数和 capability 集合。
- Invoice 签名、expiry 和 input SHA-256。
- 本地单笔最大金额、fee ceiling、余额和钱包地址。
- 付款交易 ID、自计算 ID、exact merchant output。
- Receipt 签名及 invoice、transaction、resource、input、payload、artifact 绑定。
- Artifact size、media type、SHA-256 和本地保存边界。

AI 推理不能代替上述检查，也不能获得 claim token、钱包材料或商家 signing private key。

### 10.2 商家身份连续性

当前 EntPay 可以证明 Invoice 由 `/v1/info` 当时提供的 signing key 签发，但没有公共 PKI 或链上商家身份注册。因此桌面端必须准确表达信任层级：

- 第一次访问某个 HTTPS endpoint 时显示 `First purchase from this merchant / 首次向此商家购买`。
- 保存规范化 endpoint、merchant address 和 signing key fingerprint 的对应关系。
- 后续完全一致时显示 `Previously used merchant / 曾使用过的商家`，这只是本机历史，不是官方认证。
- Endpoint 的 merchant address 或 signing key 改变时显示高优先级警告并阻止普通 Pay 流程，用户必须展开新旧 fingerprint 后显式重新建立信任。
- 商家 display name、accent 和商品描述不是身份凭证，不能掩盖 host、address 或 fingerprint 变化。
- 第一版不允许“记住并自动批准”首次商家，也不把 HTTPS 证书等同于 EntPay signing key 认证。
- 后续若支持密钥轮换，应由旧 key 签名新 key 和生效时间；没有旧 key 证明时继续按身份改变处理。

本地信任记录不上传，不构成全网信誉分，也不能证明商家提供的外部内容正确。

### 10.3 人工确认边界

- 每个新 Invoice 都需要一次可见确认。
- URI、网页点击、Agent 调用或恢复任务都不能生成 approval。
- 前端按钮只调用后端 `ApproveEntPaySession(id, revision)`。
- 后端再次读取数据库并重复检查 stage、revision、expiry、wallet、fee 和 node readiness 后才付款。
- 双击、重复 Wails 调用和多个窗口事件必须幂等。

### 10.4 日志与诊断

允许记录：session ID、invoice ID、resource、stage、错误代码、transaction ID 和非敏感 merchant host。

禁止记录：claim token、handoff code、完整 input、payload 原文、钱包私钥、助记词、加密 key、Authorization header 和 artifact 内容。

用户导出诊断时默认对 merchant address、transaction ID 和本地路径做可选脱敏，并明确提示可能包含交易标识。

## 11. 错误与恢复设计

| 场景 | 付款 | UI 结果 | 恢复 |
| --- | --- | --- | --- |
| URI 无效 | 未发生 | 无法识别支付链接 | 返回商家重新创建 |
| Handoff 过期/已兑换 | 未发生 | 支付链接已过期 | 商家重新签发 |
| Invoice 签名或价格不符 | 未发生 | 请求未通过验证 | 不允许继续 |
| 节点未同步 | 未发生 | 等待节点同步 | 自动刷新 readiness |
| 余额不足 | 未发生 | 显示需要与可用金额 | 收款后重新预览 |
| Invoice 在审批时过期 | 未发生 | 请求已过期 | 商家重新签发 |
| 用户拒绝 | 未发生 | 已拒绝，没有交易 | 不可对同会话重试 |
| 广播前构建失败 | 未发生 | 付款未创建 | 修复条件后重新审批 |
| 已广播但 submit 失败 | 已发生 | 付款已广播，正在恢复 | 使用同一交易重试 submit |
| 等待确认超时 | 已发生 | 付款存在，交付尚未完成 | 后台继续或用户手动重试 |
| 商家履约失败 | 已发生 | 商家未交付，显示 merchant 状态 | 不自动再次付款 |
| Receipt/哈希无效 | 已发生 | 交付验证失败，不打开文件 | 保留证据，允许重新 claim |
| Artifact 下载中断 | 已发生 | 下载未完成 | Range/重新下载后再校验 |
| 应用崩溃 | 取决于阶段 | 重启恢复 | 只恢复已广播后的动作 |

错误文案必须说明“钱是否已经发出”。这是 Agent Pay 错误页的第一句话。

## 12. Merchant 兼容改动

商家 SDK 新增 handoff capsule，但保持原 API 可用：

- 旧 `CreateInvoiceResponse` 可增加可选 `launch` 对象，JSON 客户端需允许已知版本字段或通过新 endpoint 获取。
- 推荐新结构：`launch.url`、`launch.handoff`、`launch.expires_at`；URL 只负责无秘密启动，商家页面把 handoff POST 到桌面 relay。
- Gateway Web UI 的主按钮改为 `Open in Entcoin`。
- 当前 localhost Agent 按钮保留在开发模式或高级入口。
- 商家仍独立部署，不进入 Entcoin 官方仓库，不上传模型 key、商家 signing key、数据库或生成结果。

上线顺序必须是：先发布能识别 handoff 的桌面端，再让商家页面默认展示新按钮。旧桌面用户看到兼容下载提示，不能进入死链接。

## 13. 测试计划

### 13.1 Go 单元与集成测试

- URI parser：合法、重复字段、未知字段、超长、Unicode、userinfo、fragment、危险端口和 shell 字符。
- Handoff：过期、digest、nonce 绑定、幂等重试、竞争兑换、限流和日志脱敏。
- Invoice 重验：价格、merchant、key、input、expiry、network、capability 和确认数漂移。
- 商家连续性：首次使用、重复使用、address/key 改变、显式重新信任和伪造 display name。
- 状态机：每个合法转换与所有非法跳转。
- 人工确认：未批准不得调用 Pay；重复批准只产生一笔交易。
- 钱包竞争：审批后切换钱包、挖矿、恢复、删除和退出。
- Fee：preview、ceiling、余额变化和 UTXO 竞争。
- 崩溃点：广播前、广播后、submit 后、确认后、Receipt 后、下载 rename 前后。
- Secret capsule：DPAPI/Secret Service round-trip、AAD 错误、篡改、权限和删除。
- Artifact：文件名、symlink、覆盖、Range、size、media type、hash 和 atomic rename。
- 全仓 `go test -race ./...` 与 `go vet ./...`。

### 13.2 前端测试

- 英中词典 key 完全一致且非空。
- 列表排序、金额格式、状态文案、expiry countdown 和按钮门禁。
- 批准、拒绝、重试的 revision 防重。
- 交易已广播后的所有错误都明确显示“付款已发出”。
- JSON、图片和未知 artifact 三种结果渲染。
- 页面切换不丢失进行中状态。

### 13.3 Playwright/Wails 真实验收

Windows 和 Ubuntu 至少覆盖：

1. Entcoin 未运行，商家按钮启动应用并打开审批页。
2. Entcoin 已运行，第二实例参数聚焦现有窗口。
3. 英文和中文完整购买。
4. 拒绝后链上没有交易。
5. 真实付款、1 次确认、Receipt 和图片交付。
6. 付款后断网、恢复网络并继续交付。
7. 付款后强制退出、重启并恢复同一交易。
8. 余额不足、未同步、钱包未备份、Invoice 过期。
9. 860×640 最小窗口与 1180×780 默认窗口无重叠、无横向溢出。
10. keyboard-only、reduced motion 和 screen-reader live region。

### 13.4 安全验收

- 进程参数、日志、Wails event、SQLite 明文字段和 crash report 中搜索不到 claim token/input。
- 恶意网页不能调用 Wails bindings、localhost API 或跳过审批。
- 自定义 URI 不能执行命令、读文件或覆盖本地路径。
- CSP 不允许外部 script、frame 或商家内容进入桌面 WebView。
- 同一 Invoice/transaction 的幂等性在并发和重启后成立。

## 14. 实施阶段

### Phase 0：冻结契约和威胁模型

- [x] 确认 system URI、handoff redeem API 和 nonce 重试语义。
- [x] 写协议测试向量和 threat model。
- [x] 确认 Windows NSIS 与 Ubuntu desktop registration 做法。
- [x] 确认数据库和 secret capsule schema。

### Phase 1：可恢复客户端引擎

- [x] 拆分 `RunPreparedAgent` 为状态步骤。
- [x] 实现 client store、加密 capsule 和恢复任务。
- [x] 增加 node payment preview 与 expected-wallet send。
- [x] 覆盖广播前后崩溃恢复边界和幂等测试。

### Phase 2：桌面 API 与页面

- [x] 新增 Agent Pay 一级导航、列表、审批、进行中、结果和设置页。
- [x] 新增 Wails DTO、revision 写操作和 session events。
- [x] 完成中英文、键盘、最小窗口和 reduced-motion。
- [x] 完成 JPEG/PNG 结果预览和文件操作验证。

### Phase 3：系统唤起与 Merchant SDK

- [x] Gateway 增加 handoff capsule/redeem。
- [x] Windows 和 Ubuntu 注册 `entcoin://`。
- [x] 处理初始启动、第二实例、重复链接和未 ready 队列。
- [x] 商家页面切换为 `Open in Entcoin` 主流程。

### Phase 4：生产验收和发布

- [x] 全仓 test/race/vet、frontend test/build、npm audit、govulncheck。
- [ ] Windows/Linux 正式构建、安装、协议注册与卸载测试。
- [ ] 用测试商品完成真实小额主网购买和重启恢复。
- [ ] 发布 checksum、provenance、双镜像和双语 Release Notes。
- [ ] 桌面新版覆盖率足够后，再更新生产商家默认按钮。

### 14.1 2026-08-06 发布候选验证记录

已取得的证据：

- Go 1.26.5 全仓 `test`、`test -race`、`vet` 均通过；`govulncheck` 为 0 个可达漏洞。
- 前端 10 项测试、production build 和 `npm audit` 通过；商家脚本通过 Node 语法解析。
- Windows amd64 应用与 EntPay 测试目标成功交叉编译为 PE32+ x86-64。
- Ubuntu amd64 `.deb` 构建与 checksum 通过；包根和安装文件权限已检查。
- Ubuntu 实机确认无秘密 bootstrap URI 启动参数、单实例转发、固定 loopback 监听、错误 Origin 拒绝、一次接受和重放拒绝。
- Chromium 从公开 HTTPS origin 向 relay POST 得到 `202`，无控制台错误；Gateway CSP 只额外允许固定 relay origin。
- Ubuntu 卸载后 `entpay-client.db`、wallet 和 chain DB 哈希保持不变；重装后二进制匹配且协议 handler 恢复。

尚未通过、因此不能发布为正式完成：

- Windows 10/11 NSIS 实机安装、URI、单实例和卸载保留性。
- 使用受控测试商家与资金完成真实小额主网付款、断网恢复和强制退出恢复。
- 发布产物签名、provenance、双镜像上传、双语 Release Notes 和生产 merchant 分阶段 rollout。

### Phase 5：本机 Agent 自动化

- [ ] 设计用户级 authenticated IPC，不复用公开 HTTP。
- [ ] Agent 只能创建待审批 intent；自动批准需单独 policy engine。
- [ ] 策略包含单笔上限、每日上限、merchant key、resource 和 expiry。
- [ ] 所有自动批准提供可撤销授权、审计记录和 kill switch。

## 15. 发布与回滚

- 该功能需要桌面应用新版本，但不需要主网硬分叉。
- `entpay-v1` Invoice/Receipt 保持兼容；handoff 是 SDK/transport 扩展。
- 旧 `entpay` CLI artifact 继续发布，避免破坏服务器和开发者流程。
- 客户端数据库使用 forward-only schema，但实现版本不支持时应禁用 Agent Pay，不影响节点、钱包和普通转账。
- Windows 卸载/回滚不能删除 `entpay-client.db`、加密 capsule 或 artifacts。
- 回滚桌面版本后，进行中的会话由兼容 CLI 或重新升级后的桌面恢复；绝不能因旧版无法识别而自动重新付款。
- Merchant Gateway 在新版桌面普及前同时保留 localhost/CLI 高级入口。

## 16. 第一版验收标准

只有同时满足以下条件，才算“桌面端 Agent Pay 完成”：

- 普通用户只安装 Entcoin 桌面端即可完成购买。
- 商家按钮可以启动未运行的 Entcoin，也能聚焦已运行实例。
- 用户付款前能看到商品、商家、请求、金额、手续费上限、钱包和 expiry。
- 未点击精确金额 Pay 按钮时，任何路径都不能创建交易。
- 桌面端复用现有节点，不发生数据目录锁冲突。
- 付款后退出并重启，仍能以同一 transaction 恢复交付，不重复付款。
- Receipt 和 artifact 验证失败时不会把结果标记为完成。
- Claim token、handoff code 和原始 input 不出现在日志、启动参数、前端事件或明文数据库中。
- Windows、Ubuntu、英文、中文、默认窗口和最小窗口全部通过真实浏览器/Wails 验收。
- 旧 CLI、现有商家和普通 ENT 转账行为保持兼容。

## 17. 已确定的设计决定

1. 集成到现有 Entcoin 桌面端，不另做一个面向普通用户的独立应用。
2. 现有 CLI 保留，但从普通用户主流程移除。
3. 第一版每笔付款都需要人工确认。
4. 桌面端复用当前 `node.Service`，不打开第二个节点。
5. 系统 URI 只携带公开 merchant endpoint 并授权短时 origin 窗口，不携带 handoff、支付 JSON 或 claim token。
6. 付款会话必须持久化并支持广播后恢复。
7. 商家发现继续通过官网、社区、X、Agent 配置或直接 URL，不虚构官方全球商店。
8. Agent Pay 页面是“验证与授权工作台”，不是聊天页或电商首页。
9. 错误信息首先说明资金是否已经发出。
10. 自动付款、商家信誉和目录属于后续独立能力，不能混入第一版。
