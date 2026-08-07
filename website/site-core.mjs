export const FALLBACK_RELEASE_URL =
  "https://github.com/HONG-LOU/entcoin/releases/tag/v1.5.3";

const en = {
  "meta.title": "Entcoin - Your node. Your wallet. Your agent.",
  "meta.description":
    "Entcoin is an independent proof-of-work network with a local wallet, a fully validating node, and verifiable Agent payments.",
  "skip.content": "Skip to content",
  "brand.tagline": "Proof-of-work, held locally",
  "nav.about": "The node",
  "nav.technology": "Verification",
  "nav.economics": "Economics",
  "nav.download": "Download",
  "nav.wallet": "Mobile wallet",
  "nav.entpay": "Agent payments",
  "nav.community": "Community",
  "nav.docs": "Docs",
  "nav.language": "中文",
  "nav.open": "Open navigation",
  "nav.close": "Close navigation",
  "hero.eyebrow": "Entcoin mainnet / protocol v1",
  "hero.title": "Entcoin",
  "hero.thesis": "Your node. Your wallet. Your agent.",
  "hero.body":
    "A proof-of-work network you can run and verify yourself, with a local wallet and verifiable Agent payments built on ordinary ENT transactions.",
  "hero.download": "Run a node",
  "hero.entpay": "Explore EntPay",
  "hero.protocol": "Read the protocol",
  "hero.signal": "Public archive seed",
  "hero.online": "Online",
  "hero.unavailable": "Status unavailable",
  "metric.height": "Live height",
  "metric.height.pending": "Connecting",
  "metric.spacing": "Block target",
  "metric.supply": "Maximum ENT",
  "metric.premine": "Premine",
  "metric.release": "Current release",
  "about.eyebrow": "01 / Start here",
  "about.title": "What is Entcoin?",
  "about.body":
    "Entcoin is an open-source cryptocurrency network and ENT is its native unit. People running Entcoin form the network together: they keep their own keys, check the same rules, relay transfers, and may choose to mine new blocks.",
  "about.wallet.title": "A wallet you control",
  "about.wallet.body":
    "No registration or hosted account. Your wallet is created on this computer. Back up the 24-word recovery phrase before receiving or mining ENT.",
  "about.node.title": "A real node, not just a wallet screen",
  "about.node.body":
    "The app downloads and verifies the chain itself. Two public seed nodes help new installations find the network; they do not decide your balance or approve transactions.",
  "about.use.title": "Transfers and mining in one app",
  "about.use.body":
    "Receive and send ENT, inspect confirmations, manage peers, and review diagnostics. Mining is available but remains off until you start it.",
  "about.step.download.label": "Download",
  "about.step.download.body": "Install the desktop app for your system",
  "about.step.backup.label": "Protect",
  "about.step.backup.body": "Create a wallet and record its 24 words",
  "about.step.sync.label": "Connect",
  "about.step.sync.body":
    "Let the node synchronize, then receive, send, or mine",
  "about.steps.label": "Getting started",
  "product.eyebrow": "The desktop node",
  "product.title": "The network becomes real when it runs on your machine.",
  "product.body":
    "Entcoin is not a browser wallet wrapped around a remote account. The desktop app keeps your wallet and ledger local, discovers peers, verifies every accepted block, and can mine without a hosted login.",
  "product.window.status": "Node active",
  "product.window.overview": "Overview",
  "product.window.transactions": "Transactions",
  "product.window.network": "Network",
  "product.window.online": "Online",
  "product.window.synced": "Synced and relaying",
  "product.window.wallet": "Wallet",
  "product.window.diagnostics": "Diagnostics",
  "product.window.confirmed": "Confirmed",
  "product.window.height": "Chain height",
  "product.window.storage": "Storage",
  "product.window.archive": "Archive",
  "product.point.wallet.title": "Self-custodied wallet",
  "product.point.wallet.body":
    "24-word recovery and an encrypted portable backup, protected locally.",
  "product.point.validation.title": "Full validation",
  "product.point.validation.body":
    "UTXOs, signatures, proof of work, issuance, maturity, and reorg rules.",
  "product.point.process.title": "One process",
  "product.point.process.body":
    "No hosted login, browser wallet, external database, or coordinator.",
  "entpay.eyebrow": "New in v1.5.3 / Agent payments",
  "entpay.title": "Software can buy a result. You still approve the payment.",
  "entpay.body":
    "An independent merchant signs a quote. Your local Agent checks the product, request, price, merchant, expiry, and spending limit. Only then can your local wallet pay and verify the signed delivery.",
  "entpay.open": "Open the live merchant",
  "entpay.guide": "Merchant and user guide",
  "entpay.result": "Verified delivery",
  "entpay.local": "Local approval / wallet keys stay here",
  "entpay.step.discover": "Discover",
  "entpay.step.invoice": "Signed invoice",
  "entpay.step.approve": "Approve locally",
  "entpay.step.pay": "Pay on-chain",
  "entpay.step.verify": "Verify delivery",
  "technology.eyebrow": "Verification path",
  "technology.title": "Trust less. Verify the path.",
  "technology.body":
    "Headers arrive first. Candidate work is compared locally. Bodies are fetched only for a viable chain, fully validated, and committed atomically with undo data.",
  "technology.discover.title": "Discover",
  "technology.discover.body":
    "Built-in HTTPS bootstrap seeds and local network discovery connect the node automatically.",
  "technology.validate.title": "Validate",
  "technology.validate.body":
    "Headers, timestamps, difficulty, work, ownership, and signatures.",
  "technology.commit.title": "Commit",
  "technology.commit.body":
    "One SQLite transaction with WAL, full synchronization, and undo records.",
  "technology.relay.title": "Relay",
  "technology.relay.body":
    "Accepted blocks and transactions move over bounded HTTP and WebSocket paths.",
  "technology.architecture": "Explore the architecture",
  "economics.eyebrow": "Monetary rules",
  "economics.title": "The supply is a rule, not a promise on a webpage.",
  "economics.body":
    "No premine, no account balances, and no floating-point supply math. Rewards are determined by block height and sum to an exact terminal amount.",
  "economics.max": "Maximum supply",
  "economics.units": "Atomic units / ENT",
  "economics.spacing": "Target spacing",
  "economics.maturity": "Coinbase maturity",
  "economics.choice": "Fork choice",
  "economics.choice.value": "Cumulative work",
  "economics.blocks": "reward-bearing blocks",
  "economics.schedule": "across an approximately ten-year target schedule",
  "economics.progress":
    "Issuance progress is derived from the validated chain, never a hosted counter.",
  "download.eyebrow": "Run it yourself",
  "download.title": "Choose where your node lives.",
  "download.body":
    "Desktop builds include the wallet and operational interface. The headless CLI runs the same validation, ledger, and peer-to-peer implementation.",
  "download.windows.platform": "Windows 10/11 · x64",
  "download.windows.title": "Desktop node",
  "download.windows.body":
    "Installer or portable build with wallet, mining, peers, history, and diagnostics.",
  "download.windows.action": "Windows downloads",
  "download.ubuntu.platform": "Ubuntu 24.04+ · amd64",
  "download.ubuntu.title": "Desktop node",
  "download.ubuntu.body":
    "Native .deb with a Secret Service protected local wallet.",
  "download.ubuntu.action": "Download .deb",
  "download.cli.platform": "Linux / Windows",
  "download.cli.title": "Headless CLI",
  "download.cli.body":
    "Archive, pruned, wallet, or public seed deployments from one binary.",
  "download.cli.action": "CLI downloads",
  "download.checksums": "Checksums",
  "download.release": "View release notes",
  "download.boundary":
    "v1.2.0 is superseded. Upgrade to v1.5.3 before block 125555 and verify the published checksums.",
  "infrastructure.eyebrow": "06 / Live infrastructure",
  "infrastructure.title": "Inspect the public node.",
  "infrastructure.body":
    "This page reads the same bounded status endpoint other nodes use. No market ticker and no fabricated activity.",
  "infrastructure.endpoint": "Open status endpoint",
  "infrastructure.protocol": "protocol",
  "infrastructure.height": "height",
  "infrastructure.tip": "tip hash",
  "infrastructure.transport": "transport",
  "infrastructure.storage": "storage",
  "infrastructure.storage.value": "archive seed",
  "source.eyebrow": "07 / Open source",
  "source.title": "The implementation is the claim.",
  "source.body":
    "Entcoin is MIT-licensed. Consensus, wallet, ledger, networking, tests, operations, and known limitations are public in the repository.",
  "source.repository": "Browse source",
  "source.operations": "Node operations",
  "source.roadmap": "Maturity roadmap",
  "source.security": "Security policy",
  "security.label": "Open network",
  "security.body":
    "Two public archive seeds in separate regions keep Entcoin reachable across the internet. Every desktop node independently validates the chain and can also discover peers on its local network.",
  "footer.description": "A compact, independent proof-of-work network.",
  "footer.github": "GitHub",
  "footer.releases": "Releases",
  "footer.protocol": "Protocol",
  "footer.operations": "Operations",
  "footer.security": "Security",
  "footer.license": "MIT License",
  "status.connected": "Connected",
  "status.unavailable": "Temporarily unavailable",
  "status.loading": "Reading the public node",
  "menu.windows.installer": "Windows installer",
  "menu.windows.portable": "Windows portable",
  "menu.ubuntu": "Ubuntu .deb",
  "menu.linux.cli": "Linux CLI",
  "menu.windows.cli": "Windows CLI",
};

const zh = {
  "meta.title": "Entcoin - 节点、钱包与 Agent，都在自己手里",
  "meta.description":
    "Entcoin 是一条独立的工作量证明网络，提供本地钱包、完整验证节点与可验证的 Agent 支付。",
  "skip.content": "跳到正文",
  "brand.tagline": "钱包与节点，本地掌控",
  "nav.about": "本机节点",
  "nav.technology": "验证路径",
  "nav.economics": "发行规则",
  "nav.download": "下载",
  "nav.wallet": "移动钱包",
  "nav.entpay": "Agent 支付",
  "nav.community": "社区",
  "nav.docs": "文档",
  "nav.language": "EN",
  "nav.open": "打开导航",
  "nav.close": "关闭导航",
  "hero.eyebrow": "Entcoin 主网 / 协议 v1",
  "hero.title": "Entcoin",
  "hero.thesis": "你的节点，你的钱包，你的 Agent。",
  "hero.body":
    "一条可以自己运行、自己验证的工作量证明网络；本地钱包与可验证的 Agent 支付，都建立在普通 ENT 交易之上。",
  "hero.download": "下载桌面端",
  "hero.entpay": "体验 EntPay",
  "hero.protocol": "查看技术协议",
  "hero.signal": "公共节点",
  "hero.online": "在线",
  "hero.unavailable": "状态不可用",
  "metric.height": "当前区块高度",
  "metric.height.pending": "连接中",
  "metric.spacing": "目标出块时间",
  "metric.supply": "ENT 发行上限",
  "metric.premine": "预挖数量",
  "metric.release": "当前版本",
  "about.eyebrow": "01 / 先从这里了解",
  "about.title": "Entcoin 到底是什么？",
  "about.body":
    "Entcoin 是一条开源的数字货币网络，ENT 是网络里的原生单位。运行 Entcoin 的用户共同组成这张网络：各自保管钱包、按照同一套规则核对数据、转发交易，也可以选择参与挖矿。",
  "about.wallet.title": "钱包由你自己保管",
  "about.wallet.body":
    "不用注册，也没有托管账户。钱包直接创建在这台电脑上。接收或挖到 ENT 之前，请先抄好 24 个恢复词；电脑损坏时，它们是找回钱包的关键。",
  "about.node.title": "不只是一个钱包界面",
  "about.node.body":
    "桌面端会自己下载并核对区块链。国内外两个公共节点帮助新安装找到网络，但它们不能修改你的余额，也不能替你批准交易。",
  "about.use.title": "转账、挖矿和节点管理都在一起",
  "about.use.body":
    "你可以收发 ENT、查看确认进度、管理连接和检查运行状态。挖矿默认关闭，只有你主动开启后才会使用计算资源。",
  "about.step.download.label": "第一步：下载",
  "about.step.download.body": "选择 Windows 或 Ubuntu 桌面端",
  "about.step.backup.label": "第二步：备份",
  "about.step.backup.body": "创建钱包，妥善记录 24 个恢复词",
  "about.step.sync.label": "第三步：联网",
  "about.step.sync.body": "等待同步完成，再收款、转账或挖矿",
  "about.steps.label": "开始使用 Entcoin",
  "product.eyebrow": "本机完整节点",
  "product.title": "当网络运行在你的电脑上，它才真正属于你。",
  "product.body":
    "Entcoin 不是套在远程账户外面的浏览器钱包。桌面端在本机保存钱包和账本，发现其他节点，验证每一个接受的区块，也可以在不登录托管账户的情况下挖矿。",
  "product.window.status": "节点运行中",
  "product.window.overview": "概览",
  "product.window.transactions": "交易",
  "product.window.network": "网络",
  "product.window.online": "在线",
  "product.window.synced": "同步完成，正在转发数据",
  "product.window.wallet": "钱包",
  "product.window.diagnostics": "诊断",
  "product.window.confirmed": "已确认",
  "product.window.height": "链高度",
  "product.window.storage": "存储",
  "product.window.archive": "归档",
  "product.point.wallet.title": "钱包在本机",
  "product.point.wallet.body":
    "支持 24 个恢复词和密码加密备份，不依赖网站账户。",
  "product.point.validation.title": "数据自己核对",
  "product.point.validation.body":
    "节点会检查余额来源、交易签名、挖矿工作量和 ENT 发行规则。",
  "product.point.process.title": "一个应用即可运行",
  "product.point.process.body":
    "钱包、账本、节点连接和挖矿都在桌面端里，不需要另装数据库。",
  "entpay.eyebrow": "v1.5.3 新能力 / Agent 支付",
  "entpay.title": "软件可以买到结果，但付款仍由你确认。",
  "entpay.body":
    "独立商家先签发报价。本机 Agent 核对商品、请求、价格、商家、有效期和付款上限，确认无误后，本机钱包才会付款并验证签名交付。",
  "entpay.open": "打开在线商家",
  "entpay.guide": "查看商家与用户指南",
  "entpay.result": "已验证交付",
  "entpay.local": "本机确认 / 钱包密钥留在这里",
  "entpay.step.discover": "发现服务",
  "entpay.step.invoice": "签名账单",
  "entpay.step.approve": "本机确认",
  "entpay.step.pay": "链上付款",
  "entpay.step.verify": "验证交付",
  "technology.eyebrow": "验证路径",
  "technology.title": "少一点信任，多一条可核对的路径。",
  "technology.body":
    "节点先了解其他节点有哪些新区块，比较哪条链投入的总工作量更多，再下载需要的完整数据。所有规则检查通过后，区块和回退记录才会一起写入本地账本。",
  "technology.discover.title": "找到网络",
  "technology.discover.body":
    "通过内置公共节点和局域网发现自动建立连接，也支持手动添加节点。",
  "technology.validate.title": "核对数据",
  "technology.validate.body": "检查时间、难度、工作量、资金来源和交易签名。",
  "technology.commit.title": "写入账本",
  "technology.commit.body":
    "验证完整批次后再写入 SQLite，失败时不会留下半套数据。",
  "technology.relay.title": "继续转发",
  "technology.relay.body": "把已经通过验证的区块和交易转发给其他节点。",
  "technology.architecture": "查看完整技术架构",
  "economics.eyebrow": "发行规则",
  "economics.title": "供应量是一条规则，不是网页上的承诺。",
  "economics.body":
    "ENT 没有预挖，创世区块也没有奖励。新 ENT 按区块高度通过挖矿产生，发行上限固定为 2,000,000 ENT；转账手续费只是在用户之间流转，不会增加总量。",
  "economics.max": "最大供应量",
  "economics.units": "每个 ENT 的最小单位数",
  "economics.spacing": "目标出块时间",
  "economics.maturity": "挖矿奖励可用时间",
  "economics.choice": "节点如何选择主链",
  "economics.choice.value": "总工作量更多的链",
  "economics.blocks": "个奖励区块",
  "economics.schedule": "按 10 秒目标出块时间计算，发行期约为十年",
  "economics.progress": "实际进度以各节点验证过的区块链为准，不依赖官网统计。",
  "download.eyebrow": "自己运行",
  "download.title": "选择你的节点运行在哪里。",
  "download.body":
    "普通用户选择桌面端即可，里面已经包含钱包和完整节点。CLI 适合需要长期运行服务器、公共节点或自定义存储方式的用户。",
  "download.windows.platform": "Windows 10/11 · x64",
  "download.windows.title": "Windows 桌面端",
  "download.windows.body":
    "建议普通用户下载安装版；便携版可直接运行。两者都包含钱包、转账、挖矿、节点连接和诊断。",
  "download.windows.action": "选择 Windows 版本",
  "download.ubuntu.platform": "Ubuntu 24.04+ · amd64",
  "download.ubuntu.title": "Ubuntu 桌面端",
  "download.ubuntu.body":
    "安装 .deb 后可从应用菜单打开，钱包密钥由系统钥匙串保护。",
  "download.ubuntu.action": "下载 Ubuntu 安装包",
  "download.cli.platform": "Linux / Windows",
  "download.cli.title": "命令行节点",
  "download.cli.body":
    "没有桌面界面，适合服务器和高级用户，可运行归档节点、精简存储节点或公共接入节点。",
  "download.cli.action": "选择命令行版本",
  "download.checksums": "核对文件完整性",
  "download.release": "查看版本说明",
  "download.boundary":
    "v1.2.0 已被替代。请在高度 125555 前升级到 v1.5.3，并核对官方 SHA-256。",
  "infrastructure.eyebrow": "06 / 网络现状",
  "infrastructure.title": "公共节点现在是否在线？",
  "infrastructure.body":
    "这里直接读取公共节点的实时状态，显示它当前同步到的区块高度和最新区块摘要。公共节点用于帮助其他节点联网和同步，不托管用户钱包。",
  "infrastructure.endpoint": "查看节点原始状态",
  "infrastructure.protocol": "协议",
  "infrastructure.height": "高度",
  "infrastructure.tip": "最新区块摘要",
  "infrastructure.transport": "传输",
  "infrastructure.storage": "存储",
  "infrastructure.storage.value": "保留完整历史的公共节点",
  "source.eyebrow": "07 / 开源透明",
  "source.title": "规则和代码都可以公开检查。",
  "source.body":
    "Entcoin 采用 MIT 开源许可证。共识规则、钱包、账本、网络通信、自动化测试、节点运维方式和已知限制都在 GitHub 仓库中公开。",
  "source.repository": "查看 GitHub 源码",
  "source.operations": "运行和维护节点",
  "source.roadmap": "查看后续计划",
  "source.security": "报告安全问题",
  "security.label": "当前网络",
  "security.body":
    "Entcoin 主网已经有两个位于不同地区的公共节点，帮助桌面端跨互联网发现网络和同步数据。每台电脑仍会独立验证收到的区块和交易；同一局域网内的节点也能自动发现彼此。",
  "footer.description": "一条由用户共同运行、各自验证的工作量证明网络。",
  "footer.github": "GitHub",
  "footer.releases": "发布版本",
  "footer.protocol": "协议",
  "footer.operations": "运维",
  "footer.security": "安全",
  "footer.license": "MIT 许可证",
  "status.connected": "公共节点在线",
  "status.unavailable": "暂时不可用",
  "status.loading": "正在连接公共节点",
  "menu.windows.installer": "Windows 安装版",
  "menu.windows.portable": "Windows 便携版",
  "menu.ubuntu": "Ubuntu .deb",
  "menu.linux.cli": "Linux CLI",
  "menu.windows.cli": "Windows CLI",
};

export const translations = Object.freeze({
  en: Object.freeze(en),
  zh: Object.freeze(zh),
});

export function validateNodeStatus(value) {
  if (!value || typeof value !== "object")
    throw new TypeError("Invalid node status");
  if (value.protocol !== "entropy-mainnet-v1")
    throw new TypeError("Unexpected node protocol");
  if (!Number.isSafeInteger(value.height) || value.height < 0)
    throw new TypeError("Invalid node height");
  if (
    typeof value.tip_hash !== "string" ||
    !/^[0-9a-f]{64}$/i.test(value.tip_hash)
  ) {
    throw new TypeError("Invalid tip hash");
  }
  if (
    typeof value.chain_work !== "string" ||
    !/^[0-9]{1,128}$/.test(value.chain_work)
  ) {
    throw new TypeError("Invalid chain work");
  }

  return {
    protocol: value.protocol,
    name: value.name === "Entcoin" ? value.name : "Entcoin",
    symbol: value.symbol === "ENT" ? value.symbol : "ENT",
    height: value.height,
    tip_hash: value.tip_hash.toLowerCase(),
    chain_work: value.chain_work,
    listen_port: value.listen_port === 47_821 ? 47_821 : 47_821,
  };
}

export function formatNodeStatus(value, language = "en") {
  const status = validateNodeStatus(value);
  const locale = language === "zh" ? "zh-CN" : "en-US";
  return {
    ...status,
    height: new Intl.NumberFormat(locale).format(status.height),
    tip: `${status.tip_hash.slice(0, 12)}...${status.tip_hash.slice(-4)}`,
  };
}

export function selectReleaseAssets(value) {
  const fallback = fallbackRelease();
  if (!isStableRelease(value)) return fallback;

  const prefix = `/HONG-LOU/entcoin/releases/download/${value.tag_name}/`;
  const known = new Map();
  for (const asset of Array.isArray(value.assets) ? value.assets : []) {
    if (
      !asset ||
      typeof asset.name !== "string" ||
      typeof asset.browser_download_url !== "string"
    )
      continue;
    let url;
    try {
      url = new URL(asset.browser_download_url);
    } catch {
      continue;
    }
    if (
      url.protocol !== "https:" ||
      url.hostname !== "github.com" ||
      !url.pathname.startsWith(prefix)
    )
      continue;
    known.set(asset.name, asset.browser_download_url);
  }

  return {
    version: value.tag_name,
    release: value.html_url,
    windowsPortable:
      findMirrorAsset(known, /^Entcoin\.exe$/i, value.tag_name) ??
      fallback.release,
    windowsInstaller:
      findMirrorAsset(
        known,
        /^entcoin-amd64-installer\.exe$/i,
        value.tag_name,
      ) ?? fallback.release,
    ubuntu:
      findMirrorAsset(
        known,
        /^entcoin_\d+\.\d+\.\d+_amd64\.deb$/i,
        value.tag_name,
      ) ?? fallback.release,
    linuxCli:
      findMirrorAsset(known, /^entcoin-cli-linux-amd64$/i, value.tag_name) ??
      fallback.release,
    windowsCli:
      findMirrorAsset(known, /^entcoin-cli\.exe$/i, value.tag_name) ??
      fallback.release,
    linuxChecksums:
      findGitHubAsset(known, /^SHA256SUMS-linux\.txt$/i) ?? fallback.release,
    windowsChecksums:
      findGitHubAsset(known, /^SHA256SUMS\.txt$/i) ?? fallback.release,
  };
}

function isStableRelease(value) {
  if (!value || typeof value !== "object" || value.draft || value.prerelease)
    return false;
  if (
    typeof value.tag_name !== "string" ||
    !/^v\d+\.\d+\.\d+$/.test(value.tag_name)
  )
    return false;
  if (typeof value.html_url !== "string") return false;
  try {
    const url = new URL(value.html_url);
    return (
      url.protocol === "https:" &&
      url.hostname === "github.com" &&
      url.pathname === `/HONG-LOU/entcoin/releases/tag/${value.tag_name}`
    );
  } catch {
    return false;
  }
}

function findMirrorAsset(assets, pattern, version) {
  for (const name of assets.keys()) {
    if (pattern.test(name))
      return `https://template-chat.xyz/downloads/${version}/${encodeURIComponent(name)}`;
  }
  return undefined;
}

function findGitHubAsset(assets, pattern) {
  for (const [name, address] of assets) {
    if (pattern.test(name)) return address;
  }
  return undefined;
}

function fallbackRelease() {
  return {
    version: "v1.5.3",
    release: FALLBACK_RELEASE_URL,
    windowsPortable: FALLBACK_RELEASE_URL,
    windowsInstaller: FALLBACK_RELEASE_URL,
    ubuntu: FALLBACK_RELEASE_URL,
    linuxCli: FALLBACK_RELEASE_URL,
    windowsCli: FALLBACK_RELEASE_URL,
    linuxChecksums: FALLBACK_RELEASE_URL,
    windowsChecksums: FALLBACK_RELEASE_URL,
  };
}
