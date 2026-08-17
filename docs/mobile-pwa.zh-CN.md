# Entcoin 移动 PWA 钱包

本文说明 `mobile/` 中可安装网页钱包的实现、安全边界、构建方式和上线顺序。当前代码尚未发布；只有节点与网页均完成部署后，线上钱包才能查询余额和发送交易。

## 结论

这套方案可直接通过 iPhone Safari 的“添加到主屏幕”或 Android 浏览器的“安装应用”使用，不需要 App Store、Apple Developer 账号或 Google Play 账号。它是完整的轻钱包，不在手机上保存全链，也不在手机上挖矿。

已实现：

- 创建或通过 24 个英文恢复词恢复钱包；
- 使用 Argon2id 和 XChaCha20-Poly1305 加密钱包，将密文保存到 IndexedDB；
- 在 Web Worker 中运行 Go/WASM，复用桌面钱包的 P-256 派生与交易签名实现；
- 锁定、密码解锁、后台两分钟自动锁定和 `.entwallet` 加密备份导出；
- 双节点查询余额、UTXO、交易记录和链尖，链尖不一致时禁止发送；
- 收款二维码、相机扫码、交易核对、本地签名和节点广播；
- PWA manifest、离线应用外壳、iOS 安全区域和手机/桌面响应式界面。

## 安全边界

PWA 的发布源是钱包信任根。网站账号、DNS、CDN 或发布流程被攻破后，恶意新版 JavaScript 可能在用户解锁时窃取恢复词。因此它适合作为日常小额热钱包，不应替代离线冷钱包。

明文恢复词和私钥不会写入 IndexedDB，也不会发给节点。它们只存在于本机 WASM 内存中；创建时恢复词会短暂显示一次。浏览器和 Go 字符串无法保证物理内存被可靠擦除，锁定只能清除程序仍持有的引用。

必须保留以下发布要求：

1. 当前钱包使用独立路径 `https://entcoin.xyz/wallet/` 和独立 Service Worker scope，不加载任何第三方运行时脚本、统计脚本或广告；条件具备后可进一步迁移到独立域名。
2. CDN/反向代理设置与 `mobile/index.html` 一致的 CSP，并额外通过 HTTP 响应头设置 `frame-ancestors 'none'`、`X-Content-Type-Options: nosniff`、`Referrer-Policy: no-referrer` 和 HSTS。
3. 构建产物经过代码审查后按内容哈希发布；保留上一个完整版本，异常时整包回滚。
4. 节点只对官网、钱包域名和本机开发来源开放 CORS。不要改成 `Access-Control-Allow-Origin: *`。
5. 每次恢复词、加密格式、签名或 Service Worker 变化都要重新执行完整测试和真机验证。

Safari PWA 无法把当前由恢复词派生的任意 P-256 私钥可靠地放入 Secure Enclave。Face ID、Passkey 或 WebAuthn 不能被描述成与硬件钱包等价。删除 Safari 网站数据或卸载 PWA 可能清除 IndexedDB，最终恢复手段始终是 24 个恢复词或另行保存的加密备份。

## 节点接口

PWA 使用两个官方节点：

- `https://node.entcoin.xyz`
- `https://template-chat.xyz`

新增 `GET /v2/wallet/{address}`，一次返回同一数据库快照中的链尖、确认余额、可用余额、最多 256 个可花费输出和最多 50 条最近记录。响应上限为 4 MiB，并复用节点重请求并发槽和按 IP 限流。

如果一个地址有超过 256 个可花费输出，接口返回 `utxos_truncated: true`，移动钱包禁止发送并提示先在桌面钱包归集，避免把截断误判成余额不足。交易仍通过现有 `POST /v2/transactions` 广播。

## 本地构建

前置条件为 Go 1.26.6、Node.js 和 npm：

```bash
cd mobile
npm install
npm test
npm audit --audit-level=high
npm run build
npm run preview
```

`npm run build` 先执行 `scripts/build-mobile-wasm.sh`，生成 `mobile/public/wasm/entcoin.wasm` 和对应 Go 版本的 `wasm_exec.js`，再由 Vite 输出 `mobile/dist/`。不要混用其他 Go 版本生成的 `wasm_exec.js`。

开发服务器：

```bash
cd mobile
npm run dev
```

## 发布顺序

1. 先升级两个官方节点，确认新钱包接口、CORS、限流和交易广播均正常。
2. 从干净提交构建 PWA，执行 Go 全量测试、竞态测试、`go vet`、npm 测试、审计和生产构建。
3. 在预发布 HTTPS 域名完成 iPhone Safari、Android Chrome 和桌面浏览器测试，包括创建、恢复、锁定、重载解锁、扫码、签名与广播。
4. 发布静态产物和安全响应头，不立即删除旧版本。
5. 检查两个节点链尖一致、manifest 可安装、Service Worker 离线启动和真实小额转账，再开放入口。

节点未升级时，当前 PWA 仍可创建、恢复和解锁本地钱包，但线上余额查询会失败。因此不能只发布 `mobile/dist/`。

## iPhone 安装

用 Safari 打开钱包 HTTPS 地址，点击分享按钮，选择“添加到主屏幕”。首次打开后先创建或恢复钱包，再离线抄写恢复词。不要截图、复制到聊天软件或存入普通云笔记。

PWA 更新由 Service Worker 管理。用户重新联网打开时会获取新版资源；涉及安全修复时应同时更新版本说明，并建议用户完全关闭后重新打开主屏幕应用。
