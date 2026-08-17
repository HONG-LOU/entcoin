const LANGUAGE_KEY = "entcoin-desktop-language";
const SUPPORTED_LANGUAGES = new Set(["en", "zh"]);

const zh = Object.freeze({
  "ENT desktop node": "ENT 桌面节点",
  "Update available": "有新版本",
  "Starting": "正在启动",
  "Node views": "节点页面",
  "Overview": "概览",
  "Transactions": "交易",
  "Agent Pay": "Agent 支付",
  "Verified commerce": "可验证交易",
  "Payment link": "支付链接",
  "Payment link or manual payment link": "支付链接或手工支付链接",
  "Paste entcoin:// payment link": "粘贴 entcoin:// 支付链接",
  "Open payment link": "打开支付链接",
  "Loading payment requests": "正在读取支付请求",
  "Payment requests": "支付请求",
  "Requests": "请求",
  "Payment request pages": "支付请求分页",
  "Previous page": "上一页",
  "Next page": "下一页",
  "No payment selected": "尚未选择支付请求",
  "Verified requests appear here before any transaction can be created.": "所有请求会先在这里完成验证，之后才可能创建交易。",
  "No Agent Pay requests yet": "还没有 Agent 支付请求",
  "Payment request": "支付请求",
  "Received": "已收到",
  "Verifying request": "正在验证请求",
  "Awaiting approval": "等待批准",
  "Preparing payment": "正在准备付款",
  "Payment broadcast": "付款已广播",
  "Submitting proof": "正在提交付款证明",
  "Confirming": "等待确认",
  "Merchant fulfilling": "商家正在交付",
  "Verifying delivery": "正在验证交付",
  "Complete": "已完成",
  "Invalid request": "请求无效",
  "Expired": "已过期",
  "Rejected": "已拒绝",
  "Action required": "需要处理",
  "Verification failed": "验证失败",
  "Approve": "批准",
  "Broadcast": "广播",
  "Confirm": "确认",
  "Verify": "验证",
  "Done": "完成",
  "Payment was sent": "付款已发出",
  "Payment has not been sent": "付款尚未发出",
  "Merchant": "商家",
  "Merchant endpoint": "商家端点",
  "Signing key fingerprint": "签名密钥指纹",
  "Trust": "信任状态",
  "Fee ceiling": "手续费上限",
  "Expires": "有效期",
  "Confirmations": "确认数",
  "Verified request": "已验证请求",
  "Verified result": "已验证结果",
  "Reject without paying": "拒绝且不付款",
  "Trust this changed merchant": "信任这个已变更的商家身份",
  "Retry": "重试",
  "This request will continue automatically.": "此请求会自动继续。",
  "Requests are verified locally before payment": "所有请求均在付款前由本机验证",
  "Payment request queued": "支付请求已加入队列",
  "The EntPay payment link was invalid and was not opened.": "EntPay 支付链接无效，未执行打开操作。",
  "Agent Pay settings": "Agent 支付设置",
  "Require approval for every payment": "每笔付款都必须批准",
  "Maximum per purchase": "单笔最高金额",
  "Artifact folder": "交付文件夹",
  "Choose folder": "选择文件夹",
  "Keep encrypted request details": "保留加密请求详情",
  "Clear after completion": "完成后立即清理",
  "7 days": "7 天",
  "30 days": "30 天",
  "Forever": "永久",
  "Register payment links": "注册支付链接",
  "Save settings": "保存设置",
  "Open file": "打开文件",
  "Show in folder": "在文件夹中显示",
  "Delete history": "删除历史记录",
  "Delete request": "删除请求",
  "Request deleted": "请求已删除",
  "Agent Pay history deleted; delivered files were not removed": "Agent 支付记录已删除；已交付的文件仍保留",
  "Agent Pay settings saved": "Agent 支付设置已保存",
  "Loading verified image": "正在加载已验证图片",
  "Verified image preview unavailable": "已验证图片暂时无法预览",
  "Verified EntPay delivery": "已验证的 EntPay 交付",
  "Wallet": "钱包",
  "Diagnostics": "诊断",
  "Secure your wallet recovery now": "请立即备份钱包",
  "Your wallet has not been marked as backed up.": "这个钱包还没有完成备份确认。",
  "View recovery phrase": "查看助记词",
  "Wallet balance": "钱包余额",
  "Spendable": "可用余额",
  "Receive address": "收款地址",
  "Copy address": "复制地址",
  "Starting...": "正在启动...",
  "Chain sync status": "区块同步状态",
  "Checking chain": "正在检查区块链",
  "Waiting for node status": "正在等待节点状态",
  "Network metrics": "网络指标",
  "Local height": "本地区块高度",
  "Network height": "网络区块高度",
  "Network": "网络",
  "Connecting": "正在连接",
  "Pending": "待确认",
  "Difficulty": "难度",
  "Block target": "目标出块时间",
  "Recipient": "收款地址",
  "Amount": "金额",
  "Network fee": "网络手续费",
  "Automatic minimum": "自动使用最低手续费",
  "Send transaction": "发送交易",
  "Send ENT": "发送 ENT",
  "Proof of work": "工作量证明",
  "Mining": "挖矿",
  "Stopped": "已停止",
  "Next reward": "下一块奖励",
  "Start mining": "开始挖矿",
  "Mine one block": "挖一个区块",
  "Issued": "已发行",
  "Ledger": "账本",
  "Recent blocks": "最近区块",
  "Height": "高度",
  "Hash": "哈希",
  "Time": "时间",
  "Work": "累计工作量",
  "Waiting for ledger": "正在读取账本",
  "Wallet ledger": "钱包账本",
  "Transaction history": "交易记录",
  "Refresh history": "刷新交易记录",
  "Filter transactions": "筛选交易",
  "All": "全部",
  "Received": "收款",
  "Sent": "转出",
  "Not loaded": "尚未加载",
  "of": "/",
  "transactions": "笔交易",
  "Transaction": "交易",
  "Status": "状态",
  "Loading transactions": "正在加载交易记录",
  "Local identity": "本机钱包",
  "Wallet security": "钱包安全",
  "Checking": "正在检查",
  "Create wallet": "新建钱包",
  "Active receive address": "当前收款地址",
  "On this device": "这台设备",
  "Wallets": "钱包",
  "Loading wallets": "正在加载钱包",
  "Wallet actions": "钱包操作",
  "Recovery phrase": "助记词",
  "24-word offline recovery": "用于离线恢复的 24 个单词",
  "View": "查看",
  "Encrypted backup": "加密备份",
  "Portable .entwallet file": "可携带的 .entwallet 文件",
  "Export": "导出",
  "Add encrypted backup": "导入加密备份",
  "Keep existing wallets and activate the imported wallet": "保留现有钱包，并启用导入的钱包",
  "Import": "导入",
  "Add recovery phrase": "通过助记词添加钱包",
  "Keep existing wallets and activate the restored wallet": "保留现有钱包，并启用恢复的钱包",
  "Local node": "本地节点",
  "Desktop application": "桌面应用",
  "Software update": "软件更新",
  "Checking for updates": "正在检查更新",
  "Release notes": "更新说明",
  "Check again": "重新检查",
  "Update and restart": "更新并重启",
  "Database": "数据库",
  "Copy database path": "复制数据库路径",
  "Database size": "数据库大小",
  "Storage mode": "存储模式",
  "Retention depth": "保留深度",
  "Pruned through": "已裁剪到",
  "Not pruned": "未裁剪",
  "Protocol": "协议",
  "Listener": "监听地址",
  "Chain tip": "最新区块",
  "Emission schedule": "发行计划",
  "Last node error": "最近一次节点错误",
  "None": "无",
  "Ledger storage": "账本存储",
  "Prune historical data": "清理历史数据",
  "Old block bodies and undo records are deleted. Headers, the current UTXO set, and transaction/address indexes remain. Archive mode stops future pruning but does not restore deleted data.": "清理后会删除旧区块内容和回滚记录，但保留区块头、当前 UTXO 以及交易和地址索引。切回归档模式只能停止后续清理，已经删除的数据不会恢复。",
  "Recent blocks to retain (120 - 31,536,000)": "要保留的最近区块数（120 - 31,536,000）",
  "Review pruning": "检查清理范围",
  "Switch to archive": "切换为归档模式",
  "Entcoin Node": "Entcoin 节点",
  "Secure desktop startup": "安全启动桌面节点",
  "Starting local node": "正在启动本地节点",
  "Opening wallet and ledger...": "正在打开钱包和账本...",
  "Protect legacy wallet": "保护旧版钱包",
  "Your existing wallet must be encrypted before this node can start. Select a backup destination in the next step.": "启动节点前，需要先加密现有钱包。下一步请选择备份文件的保存位置。",
  "Backup password": "备份密码",
  "Confirm password": "再次输入密码",
  "12-1024 UTF-8 bytes": "12-1024 个 UTF-8 字节",
  "Encrypt wallet and start node": "加密钱包并启动节点",
  "Node could not start": "节点启动失败",
  "Unknown startup error": "发生未知的启动错误",
  "Wallet recovery": "钱包恢复",
  "Close": "关闭",
  "Anyone with these words can spend this wallet. Keep them offline and private.": "任何拿到这 24 个单词的人都能使用这个钱包。请离线保存，不要发送给任何人。",
  "Reveal phrase": "显示助记词",
  "Copy words": "复制助记词",
  "I have stored these words offline": "我已经离线保存这些单词",
  "Mark as secured": "确认已安全备份",
  "Portable backup": "可携带备份",
  "Export encrypted wallet": "导出加密钱包",
  "Choose destination": "选择保存位置",
  "Add wallet": "添加钱包",
  "Import encrypted backup": "导入加密备份",
  "The imported wallet becomes active. Existing wallets remain on this device.": "导入的钱包将立即启用，设备上的现有钱包会继续保留。",
  "Add and activate this wallet": "添加并启用这个钱包",
  "Choose backup and import": "选择备份文件并导入",
  "Import recovery phrase": "通过助记词导入",
  "Enter all 24 words. Existing wallets remain on this device.": "请输入完整的 24 个单词。设备上的现有钱包会继续保留。",
  "Expected phrase length": "助记词长度",
  "Import wallet": "导入钱包",
  "New identity": "新钱包",
  "A new wallet becomes active. The current wallet remains available on this device.": "新钱包创建后会立即启用，当前钱包仍会保留在这台设备上。",
  "Create and activate a new wallet": "创建并启用新钱包",
  "Device storage": "设备存储",
  "Remove wallet": "移除钱包",
  "Removal deletes this wallet vault from this device. Recovery requires its 24 words or encrypted backup.": "移除后，这台设备会删除该钱包文件。以后只能通过 24 个助记词或加密备份恢复。",
  "I can recover this wallet": "我确认自己可以恢复这个钱包",
  "Remove from device": "从设备中移除",
  "Permanent storage change": "不可撤销的存储操作",
  "Confirm ledger pruning": "确认清理账本",
  "Reviewing prune horizon...": "正在计算清理范围...",
  "Deleted": "将删除",
  "Old block bodies, transaction bodies, and undo records": "旧区块内容、交易内容和回滚记录",
  "Retained": "将保留",
  "All headers, current UTXOs, and transaction/address indexes": "全部区块头、当前 UTXO 以及交易和地址索引",
  "I understand old bodies and undo data cannot be restored without resyncing": "我明白这些数据删除后，只能重新同步才能找回",
  "Prune ledger": "清理账本",
  "Transaction details": "交易详情",
  "No blocks are available": "暂无区块",
  "No wallet profiles available": "这台设备上还没有钱包",
  "Unknown wallet": "未知钱包",
  "Active wallet": "当前钱包",
  "Backup needed": "需要备份",
  "Ready to activate": "可以启用",
  "Copy wallet address": "复制钱包地址",
  "Stop mining before switching wallets": "请先停止挖矿，再切换钱包",
  "Activate wallet": "启用钱包",
  "Secure this wallet before removing it": "请先备份这个钱包，再将它移除",
  "Remove wallet from device": "从设备中移除钱包",
  "Stop mining": "停止挖矿",
  "Chain synchronized": "区块链已同步",
  "Validating peer data": "正在验证节点数据",
  "Connecting to the network": "正在连接网络",
  "Public seeds are being retried": "正在重新连接公共节点",
  "Discovering public peers": "正在寻找公共节点",
  "Synchronizing chain": "正在同步区块链",
  "Syncing": "同步中",
  "Peer chain is ahead": "网络上有更新的区块",
  "Behind": "需要同步",
  "Online": "在线",
  "Offline": "离线",
  "Unavailable": "不可用",
  "Node warning": "节点有警告",
  "Finding network": "正在寻找网络",
  "Synchronizing": "正在同步",
  "Node active": "节点运行正常",
  "Archive": "归档",
  "Archive going forward / previously pruned": "当前为归档模式 / 历史数据曾被清理",
  "No future pruning": "以后不再清理",
  "Retention enabled; no eligible blocks yet": "已启用保留策略，暂时没有可清理的区块",
  "Not listening": "未监听",
  "Warning": "有警告",
  "Healthy": "正常",
  "Recovery secured": "已安全备份",
  "Stop mining before restoring": "请先停止挖矿，再恢复钱包",
  "Restore encrypted backup": "恢复加密备份",
  "Restore recovery phrase": "通过助记词恢复",
  "Stop mining before creating a wallet": "请先停止挖矿，再创建钱包",
  "Archive policy is active; previously deleted data remains unavailable": "已经是归档模式；之前删除的数据仍无法恢复",
  "Archive policy is already active": "已经是归档模式",
  "Stop future pruning; previously deleted data will not be restored": "停止后续清理；之前删除的数据不会恢复",
  "Mining reward": "挖矿奖励",
  "Sent transaction": "转出交易",
  "Received transaction": "收款交易",
  "No wallet transactions yet": "还没有钱包交易",
  "No received transactions yet": "还没有收款记录",
  "No sent transactions yet": "还没有转出记录",
  "No mining rewards yet": "还没有挖矿奖励",
  "Open transaction details": "打开交易详情",
  "Node offline": "节点离线",
  "Entcoin is up to date": "Entcoin 已是最新版本",
  "Checking for updates": "正在检查更新",
  "Update check unavailable": "暂时无法检查更新",
  "Downloading": "正在下载",
  "Downloading and verifying the update": "正在下载并校验更新",
  "Preparing": "正在准备",
  "Preparing update": "正在准备更新",
  "Checking update integrity": "正在检查安装包",
  "Verifying": "正在校验",
  "Verifying downloaded update": "正在校验下载文件",
  "Installing": "正在安装",
  "Waiting for system approval": "等待系统授权",
  "Update ready; restarting": "更新已准备好，正在重启",
  "Update installation did not start": "未能启动更新安装",
  "Transaction ID": "交易 ID",
  "Type": "类型",
  "Block height": "区块高度",
  "Block hash": "区块哈希",
  "Position": "区块内位置",
  "Body": "交易内容",
  "Pruned locally; indexed wallet totals remain available": "本机已清理交易内容，但钱包统计仍然可用",
  "Transaction body is unavailable": "交易内容不可用",
  "Inputs": "输入",
  "Coinbase has no inputs": "挖矿奖励没有输入",
  "No inputs": "没有输入",
  "Outputs": "输出",
  "No outputs": "没有输出",
  "Transaction history is unavailable": "暂时无法读取交易记录",
  "Nothing is available to copy": "没有可复制的内容",
  "Address copied": "地址已复制",
  "Database path copied": "数据库路径已复制",
  "Recovery phrase copied": "助记词已复制",
  "Clipboard access failed": "无法访问剪贴板",
  "The operation failed": "操作失败",
  "Done": "已完成",
  "Working": "处理中",
  "Password must contain at least 12 UTF-8 bytes": "密码至少需要 12 个 UTF-8 字节",
  "Password must not exceed 1024 UTF-8 bytes": "密码不能超过 1024 个 UTF-8 字节",
  "Recipient address is required": "请输入收款地址",
  "Amount must be positive with no more than 8 decimal places": "金额必须大于 0，且最多保留 8 位小数",
  "Broadcasting": "正在发送",
  "Transaction submitted": "交易已提交",
  "Stopping": "正在停止",
  "Mining started": "挖矿已开始",
  "Mining stopping": "正在停止挖矿",
  "Mining block": "正在挖矿",
  "Block mined": "新区块已挖出",
  "Creating": "正在创建",
  "Wallet created": "钱包已创建",
  "Removing": "正在移除",
  "Wallet removed": "钱包已移除",
  "Retention must be a whole number": "保留区块数必须是整数",
  "Retention must be between 120 and 31,536,000 blocks": "保留区块数必须在 120 到 31,536,000 之间",
  "Switching": "正在切换",
  "Archive policy enabled": "已切换为归档模式",
  "Previously pruned data remains unavailable.": "之前清理的数据仍无法恢复。",
  "Pruning": "正在清理",
  "Ledger pruning completed": "账本清理完成",
  "Decrypting": "正在解密",
  "Backend returned an invalid recovery phrase": "节点返回的助记词无效",
  "Saving": "正在保存",
  "Wallet recovery confirmed": "已确认钱包备份",
  "Password confirmation does not match": "两次输入的密码不一致",
  "Encrypting": "正在加密",
  "Wallet backup exported": "钱包备份已导出",
  "Confirm wallet import": "请确认导入这个钱包",
  "Restoring": "正在恢复",
  "Recovery phrase must contain exactly 24 words": "助记词必须正好包含 24 个单词",
  "Wallet restored": "钱包已恢复",
  "Legacy wallet migrated": "旧版钱包已迁移",
  "Switching": "正在切换",
  "Wallet activated": "钱包已启用",
  "legacy wallet migration is not required": "不需要迁移旧版钱包",
  "legacy wallet requires encrypted migration": "旧版钱包需要先完成加密迁移",
  "Entcoin node is running": "Entcoin 节点正在运行",
  "Entcoin node is starting": "Entcoin 节点正在启动",
  "application is shutting down": "应用正在退出",
  "an update is already being prepared": "更新已经在准备中",
  "node is still starting": "节点仍在启动",
  "Peer added": "节点已添加",
  "Peer removed": "节点已移除",
  "Health event resolved": "问题已标记为已处理",
  "Wallet recovery phrase confirmed": "已确认助记词备份",
  "Backup cancelled": "已取消备份",
  "Encrypted wallet backup exported": "加密钱包备份已导出",
  "Restore cancelled": "已取消恢复",
  "Wallet imported and activated": "钱包已导入并启用",
  "New wallet created and activated": "新钱包已创建并启用",
  "Wallet removed from this device": "钱包已从这台设备移除",
  "Migration cancelled": "已取消迁移",
  "Legacy wallet encrypted and node started": "旧版钱包已加密，节点已经启动",
});

const patterns = [
  [/^Entcoin v(.+) is available$/, (_, version) => `Entcoin v${version} 可以更新`],
  [/^Update v(.+)$/, (_, version) => `更新到 v${version}`],
  [/^Downloading (\d+)%$/, (_, percent) => `正在下载 ${percent}%`],
  [/^Downloaded (.+) \/ (.+)$/, (_, downloaded, total) => `已下载 ${downloaded} / ${total}`],
  [/^(.+) downloaded$/, (_, downloaded) => `已下载 ${downloaded}`],
  [/^Local #(\S+) of #(\S+)$/, (_, local, remote) => `本地 #${local} / 网络 #${remote}`],
  [/^Local #(\S+) \| peer #(\S+)$/, (_, local, remote) => `本地 #${local} / 节点 #${remote}`],
  [/^Validated through block #(.*)$/, (_, height) => `已验证到区块 #${height}`],
  [/^Pruned \| keep (.*)$/, (_, count) => `裁剪模式 / 保留 ${count} 个区块`],
  [/^(.*) recent blocks$/, (_, count) => `保留最近 ${count} 个区块`],
  [/^Block #(.*)$/, (_, height) => `区块 #${height}`],
  [/^(\d+) confirmations?$/, (_, count) => `${count} 次确认`],
  [/^Updated (.*)$/, (_, time) => `更新于 ${time}`],
  [/^(\d+) \/ 24 words$/, (_, count) => `${count} / 24 个单词`],
  [/^(\d+) bytes$/, (_, count) => `${count} 字节`],
  [/^(.*) blocks at (.*) seconds$/, (_, blocks, seconds) => `${blocks} 个区块，目标间隔 ${seconds} 秒`],
  [/^Pending in local pool$/, () => "正在本地交易池等待确认"],
  [/^The ledger is already pruned through block #(.*)\. Deleted data will not be restored; future pruning will retain the newest (.*) blocks\.$/, (_, height, retain) => `账本已经清理到区块 #${height}。已删除的数据不会恢复；以后将保留最新的 ${retain} 个区块。`],
  [/^Current height is #(.*)\. No existing body is eligible yet; future pruning will retain the newest (.*) blocks\.$/, (_, height, retain) => `当前高度是 #${height}，暂时没有可清理的数据；以后将保留最新的 ${retain} 个区块。`],
  [/^Block and transaction bodies plus undo records through block #(.*) will be permanently removed\. The newest (.*) blocks remain complete\.$/, (_, height, retain) => `将永久删除区块 #${height} 及以前的区块内容、交易内容和回滚记录，最新的 ${retain} 个区块会完整保留。`],
  [/^Entcoin (.+) update ready; restarting$/, (_, version) => `Entcoin ${version} 已安装，正在重启`],
  [/^Transaction added to local pending pool with (.+) ENT fee$/, (_, fee) => `交易已加入本地待确认池，手续费 ${fee} ENT`],
  [/^Block (.+) mined$/, (_, height) => `区块 #${height} 已挖出`],
  [/^Ledger pruned through block (.+)$/, (_, height) => `账本已清理到区块 #${height}`],
  [/^Clipboard access failed: (.*)$/, (_, reason) => `无法访问剪贴板：${reason}`],
];

const textState = new WeakMap();
const attributeState = new WeakMap();
const listeners = new Set();
let language = initialLanguage();
let observer;

function initialLanguage() {
  const saved = window.localStorage.getItem(LANGUAGE_KEY);
  if (SUPPORTED_LANGUAGES.has(saved)) return saved;
  return navigator.language?.toLowerCase().startsWith("zh") ? "zh" : "en";
}

export function translate(source) {
  const text = String(source ?? "");
  if (language !== "zh" || text === "") return text;
  if (zh[text]) return zh[text];
  for (const [pattern, replacement] of patterns) {
    if (pattern.test(text)) return text.replace(pattern, replacement);
  }
  return text;
}

export function translateError(source) {
  const text = String(source ?? "").trim();
  if (language !== "zh" || text === "") return text;
  const translated = translate(text);
  if (translated !== text) return translated;
  const normalized = text.toLowerCase();
  if (/update|download|checksum|release|http|network/.test(normalized)) {
    return "更新没有完成，请检查网络连接后重试。";
  }
  if (/wallet|recovery|backup|mnemonic|vault|keyring|secret service|password/.test(normalized)) {
    return "钱包操作没有完成，请检查输入和系统密钥服务后重试。";
  }
  if (/transaction|address|amount|fee|balance|utxo/.test(normalized)) {
    return "交易没有完成，请检查地址、金额和可用余额。";
  }
  if (/mining|mine|block/.test(normalized)) {
    return "挖矿操作没有完成，请稍后重试。";
  }
  if (/database|ledger|chain|node|start|listen|peer/.test(normalized)) {
    return "节点暂时无法完成操作，请打开“诊断”查看状态后重试。";
  }
  return "操作没有完成，请稍后重试。";
}

function translateTextNode(node) {
  const value = node.nodeValue ?? "";
  let state = textState.get(node);
  if (!state || value !== state.rendered) state = { source: value, rendered: value };
  const match = state.source.match(/^(\s*)(.*?)(\s*)$/s);
  const rendered = match ? `${match[1]}${translate(match[2])}${match[3]}` : translate(state.source);
  state.rendered = rendered;
  textState.set(node, state);
  if (value !== rendered) node.nodeValue = rendered;
}

function translateAttribute(element, name) {
  if (!element.hasAttribute(name)) return;
  let states = attributeState.get(element);
  if (!states) {
    states = new Map();
    attributeState.set(element, states);
  }
  const value = element.getAttribute(name) ?? "";
  let state = states.get(name);
  if (!state || value !== state.rendered) state = { source: value, rendered: value };
  const rendered = translate(state.source);
  state.rendered = rendered;
  states.set(name, state);
  if (value !== rendered) element.setAttribute(name, rendered);
}

function translateTree(root) {
  if (root.nodeType === Node.TEXT_NODE) {
    translateTextNode(root);
    return;
  }
  if (!(root instanceof Element) && root !== document) return;
  const elements = root === document ? [document.documentElement, ...document.querySelectorAll("*")] : [root, ...root.querySelectorAll("*")];
  for (const element of elements) {
    if (element.closest?.("[data-no-translate]")) continue;
    for (const node of element.childNodes) {
      if (node.nodeType === Node.TEXT_NODE) translateTextNode(node);
    }
    for (const name of ["title", "aria-label", "placeholder"]) translateAttribute(element, name);
  }
}

function updateLanguageButton() {
  const button = document.querySelector("#language-toggle");
  if (!button) return;
  const label = button.querySelector("span");
  if (label) label.textContent = language === "zh" ? "EN" : "中文";
  const description = language === "zh" ? "Switch to English" : "切换到中文";
  button.title = description;
  button.setAttribute("aria-label", description);
  button.setAttribute("aria-pressed", String(language === "zh"));
}

export function setLanguage(nextLanguage, { persist = true } = {}) {
  language = SUPPORTED_LANGUAGES.has(nextLanguage) ? nextLanguage : "en";
  document.documentElement.lang = language === "zh" ? "zh-CN" : "en";
  document.documentElement.dataset.language = language;
  document.title = language === "zh" ? "Entcoin 节点" : "Entcoin Node";
  if (persist) window.localStorage.setItem(LANGUAGE_KEY, language);
  translateTree(document);
  updateLanguageButton();
  for (const listener of listeners) listener(language);
}

export function toggleLanguage() {
  setLanguage(language === "zh" ? "en" : "zh");
}

export function currentLanguage() {
  return language;
}

export function currentLocale() {
  return language === "zh" ? "zh-CN" : "en-GB";
}

export function onLanguageChange(listener) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function initializeI18n() {
  translateTree(document);
  updateLanguageButton();
  document.documentElement.lang = language === "zh" ? "zh-CN" : "en";
  document.documentElement.dataset.language = language;
  observer = new MutationObserver((mutations) => {
    observer.disconnect();
    for (const mutation of mutations) {
      if (mutation.type === "characterData") translateTextNode(mutation.target);
      if (mutation.type === "attributes") translateAttribute(mutation.target, mutation.attributeName);
      for (const node of mutation.addedNodes) translateTree(node);
    }
    observer.observe(document.body, { childList: true, characterData: true, attributes: true, attributeFilter: ["title", "aria-label", "placeholder"], subtree: true });
  });
  observer.observe(document.body, { childList: true, characterData: true, attributes: true, attributeFilter: ["title", "aria-label", "placeholder"], subtree: true });
}
