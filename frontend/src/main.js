import {
  Activity,
  ArrowDownLeft,
  ArrowRightLeft,
  ArrowUpRight,
  BadgeCheck,
  CircleAlert,
  CircleCheck,
  ChevronLeft,
  ChevronRight,
  Clock3,
  Copy,
  Cpu,
  Database,
  Download,
  Eye,
  EyeOff,
  FolderOpen,
  FileKey,
  History,
  KeyRound,
  Link,
  Languages,
  LayoutDashboard,
  LoaderCircle,
  LockKeyhole,
  Pickaxe,
  Play,
  Plus,
  RefreshCw,
  ReceiptText,
  RotateCcw,
  Save,
  Send,
  ShieldCheck,
  Square,
  SquareDot,
  Trash2,
  TriangleAlert,
  Upload,
  Wifi,
  WifiOff,
  WalletCards,
  X,
  createIcons,
} from "lucide";
import "./style.css";
import "./entpay.css";
import { transactionKind } from "./transaction-filter.js";
import { currentLocale, initializeI18n, onLanguageChange, toggleLanguage, translate, translateError } from "./i18n.js";
import { entpayViewActivated, initializeEntPay, refreshEntPay } from "./entpay.js";

const appIcons = {
  Activity,
  ArrowDownLeft,
  ArrowRightLeft,
  ArrowUpRight,
  BadgeCheck,
  CircleAlert,
  CircleCheck,
  ChevronLeft,
  ChevronRight,
  Clock3,
  Copy,
  Cpu,
  Database,
  Download,
  Eye,
  EyeOff,
  FolderOpen,
  FileKey,
  History,
  KeyRound,
  Link,
  Languages,
  LayoutDashboard,
  LoaderCircle,
  LockKeyhole,
  Pickaxe,
  Play,
  Plus,
  RefreshCw,
  ReceiptText,
  RotateCcw,
  Save,
  Send,
  ShieldCheck,
  Square,
  SquareDot,
  Trash2,
  TriangleAlert,
  Upload,
  Wifi,
  WifiOff,
  WalletCards,
  X,
};

const state = {
  dashboard: null,
  history: [],
  historyFilter: "all",
  ready: false,
  startupCode: "starting",
  startupChecking: false,
  dashboardRefreshing: false,
  historyRefreshing: false,
  historyRefreshQueued: false,
  recoveryPhrase: "",
  pendingPruneRetain: 0,
  pendingRemoveWallet: "",
  lastDashboardError: "",
  lastHistoryRefresh: 0,
  updateStatus: null,
  updateChecked: false,
  updateChecking: false,
  updateInstalling: false,
};

const $ = (id) => document.getElementById(id);
const encoder = new TextEncoder();
let toastTimer;

function backend() {
  return window.go?.main?.App;
}

async function invoke(method, ...args) {
  const api = backend();
  if (!api?.[method]) throw new Error(`Entcoin desktop backend does not expose ${method}`);
  return api[method](...args);
}

function icon(name) {
  const element = document.createElement("i");
  element.dataset.lucide = name;
  return element;
}

function setText(id, value) {
  const element = $(id);
  if (element) element.textContent = value ?? "--";
}

function asArray(value) {
  return Array.isArray(value) ? value : [];
}

function asNumber(value, fallback = 0) {
  const number = Number(value);
  return Number.isFinite(number) ? number : fallback;
}

function shortHash(value, length = 12) {
  const text = String(value || "");
  if (!text) return "--";
  return text.length <= length ? text : `${text.slice(0, length)}...`;
}

function formatAmount(value) {
  const text = String(value ?? "0");
  const [whole, fraction] = text.split(".");
  try {
    const grouped = BigInt(whole || "0").toLocaleString(currentLocale());
    return fraction === undefined ? grouped : `${grouped}.${fraction}`;
  } catch {
    return text;
  }
}

function formatTimestamp(timestamp) {
  const seconds = asNumber(timestamp, 0);
  if (seconds <= 0) return "--";
  const date = new Date(seconds * 1000);
  if (Number.isNaN(date.getTime())) return "--";
  return new Intl.DateTimeFormat(currentLocale(), {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(date);
}

function formatBytes(value) {
  let bytes = asNumber(value, -1);
  if (bytes < 0) return "--";
  if (bytes < 1024) return `${bytes.toLocaleString(currentLocale())} B`;
  const units = ["KiB", "MiB", "GiB", "TiB"];
  let unit = -1;
  do {
    bytes /= 1024;
    unit += 1;
  } while (bytes >= 1024 && unit < units.length - 1);
  return `${bytes.toFixed(bytes >= 100 ? 0 : bytes >= 10 ? 1 : 2)} ${units[unit]}`;
}

function errorMessage(error) {
  if (typeof error === "string") return translateError(error);
  if (error?.message) return translateError(error.message);
  return translate("The operation failed");
}

function showToast(message, type = "success") {
  clearTimeout(toastTimer);
  const toast = $("toast");
  toast.textContent = String(message || "Done");
  toast.className = `toast visible ${type}`;
  toastTimer = setTimeout(() => {
    toast.className = "toast";
  }, 3600);
}

function setButtonBusy(button, busy, busyLabel = "Working") {
  if (!button) return;
  const label = button.querySelector("span");
  if (busy) {
    button.dataset.originalLabel = label?.textContent || "";
    button.dataset.busyLabel = busyLabel;
    if (label) label.textContent = busyLabel;
    button.classList.add("button-busy");
    button.disabled = true;
  } else {
    if (label && button.dataset.originalLabel) label.textContent = button.dataset.originalLabel;
    delete button.dataset.originalLabel;
    delete button.dataset.busyLabel;
    button.classList.remove("button-busy");
    button.disabled = false;
  }
}

function resetUpdateProgress() {
  const button = $("install-update");
  button?.classList.remove("update-progress");
  button?.style.removeProperty("--update-progress");
}

function renderUpdateProgress(progress) {
  if (!state.updateInstalling) return;
  const button = $("install-update");
  const label = button?.querySelector("span");
  const phase = String(progress?.phase || "");
  if (phase === "downloading") {
    const percent = Math.min(100, Math.max(0, asNumber(progress.percent)));
    button.classList.add("update-progress");
    button.style.setProperty("--update-progress", `${percent}%`);
    if (label) label.textContent = progress.total > 0 ? `Downloading ${percent}%` : "Downloading";
    setText("update-status", progress.total > 0
      ? `Downloaded ${formatBytes(progress.downloaded)} / ${formatBytes(progress.total)}`
      : `${formatBytes(progress.downloaded)} downloaded`);
    return;
  }
  resetUpdateProgress();
  if (phase === "checking") {
    if (label) label.textContent = "Checking";
    setText("update-status", "Checking update integrity");
  } else if (phase === "verifying") {
    if (label) label.textContent = "Verifying";
    setText("update-status", "Verifying downloaded update");
  } else if (phase === "installing") {
    if (label) label.textContent = "Installing";
    setText("update-status", "Waiting for system approval");
  } else {
    if (label) label.textContent = "Preparing";
    setText("update-status", "Preparing update");
  }
}

function passwordBytes(password) {
  return encoder.encode(password).byteLength;
}

function validatePassword(password) {
  const bytes = passwordBytes(password);
  if (bytes < 12) throw new Error("Password must contain at least 12 UTF-8 bytes");
  if (bytes > 1024) throw new Error("Password must not exceed 1024 UTF-8 bytes");
}

function updatePasswordCount(inputID, outputID) {
  const bytes = passwordBytes($(inputID).value);
  const output = $(outputID);
  output.textContent = `${bytes.toLocaleString(currentLocale())} bytes`;
  output.classList.toggle("invalid", bytes > 0 && (bytes < 12 || bytes > 1024));
}

function isAmount(value, allowZero) {
  if (!/^(?:0|[1-9]\d*)(?:\.\d{1,8})?$/.test(value)) return false;
  return allowZero || !/^0(?:\.0+)?$/.test(value);
}

async function copyText(value, successMessage) {
  if (!value) {
    showToast("Nothing is available to copy", "error");
    return;
  }
  try {
    await navigator.clipboard.writeText(value);
    showToast(successMessage);
  } catch (error) {
    showToast(`Clipboard access failed: ${errorMessage(error)}`, "error");
  }
}

function renderBlocks(blocks) {
  const body = $("blocks-body");
  body.replaceChildren();
  if (blocks.length === 0) {
    const row = body.insertRow();
    const cell = row.insertCell();
    cell.colSpan = 5;
    cell.className = "empty-cell";
    cell.textContent = "No blocks are available";
    return;
  }

  for (const block of blocks) {
    const row = body.insertRow();
    const height = row.insertCell();
    const heightValue = document.createElement("strong");
    heightValue.textContent = `#${asNumber(block.height).toLocaleString(currentLocale())}`;
    height.append(heightValue);

    const hash = row.insertCell();
    const hashValue = document.createElement("code");
    hashValue.textContent = shortHash(block.hash);
    hashValue.title = String(block.hash || "");
    hash.append(hashValue);

    row.insertCell().textContent = formatTimestamp(block.timestamp);
    row.insertCell().textContent = asNumber(block.transactions).toLocaleString(currentLocale());
    row.insertCell().textContent = asNumber(block.difficulty).toLocaleString(currentLocale());
  }
}

function renderWalletProfiles(profiles, mining) {
  const list = $("wallet-profile-list");
  list.replaceChildren();
  setText("wallet-count", profiles.length.toLocaleString(currentLocale()));
  if (profiles.length === 0) {
    const empty = document.createElement("li");
    empty.className = "empty-row";
    empty.textContent = "No wallet profiles available";
    list.append(empty);
    return;
  }

  for (const profile of profiles) {
    const row = document.createElement("li");
    row.classList.toggle("active", Boolean(profile.active));
    const mark = document.createElement("span");
    mark.className = "wallet-profile-mark";
    mark.append(icon(profile.active ? "wallet-cards" : "key-round"));

    const main = document.createElement("div");
    main.className = "wallet-profile-main";
    const address = document.createElement("code");
    address.textContent = String(profile.address || "Unknown wallet");
    address.title = String(profile.address || "");
    const status = document.createElement("span");
    status.textContent = profile.active ? "Active wallet" : profile.needs_backup ? "Backup needed" : "Ready to activate";
    main.append(address, status);

    const actions = document.createElement("div");
    actions.className = "wallet-profile-actions";
    const copy = document.createElement("button");
    copy.type = "button";
    copy.className = "icon-button";
    copy.title = "Copy wallet address";
    copy.setAttribute("aria-label", "Copy wallet address");
    copy.append(icon("copy"));
    copy.addEventListener("click", () => copyText(String(profile.address || ""), "Address copied"));
    actions.append(copy);

    if (!profile.active) {
      const activate = document.createElement("button");
      activate.type = "button";
      activate.className = "icon-button outlined";
      activate.title = mining ? "Stop mining before switching wallets" : "Activate wallet";
      activate.setAttribute("aria-label", "Activate wallet");
      activate.disabled = mining;
      activate.append(icon("arrow-right-left"));
      activate.addEventListener("click", () => switchWallet(String(profile.address || ""), activate));

      const remove = document.createElement("button");
      remove.type = "button";
      remove.className = "icon-button remove-wallet";
      remove.title = profile.needs_backup ? "Secure this wallet before removing it" : "Remove wallet from device";
      remove.setAttribute("aria-label", "Remove wallet from device");
      remove.disabled = Boolean(profile.needs_backup);
      remove.append(icon("trash-2"));
      remove.addEventListener("click", () => openRemoveWallet(String(profile.address || "")));
      actions.append(activate, remove);
    }

    row.append(mark, main, actions);
    list.append(row);
  }
  createIcons({ icons: appIcons });
}

function renderMining(data) {
  const toggle = $("toggle-mining");
  toggle.replaceChildren(icon(data.mining ? "square" : "play"), document.createElement("span"));
  toggle.querySelector("span").textContent = data.mining ? "Stop mining" : "Start mining";
  toggle.classList.toggle("danger", Boolean(data.mining));
  $("mine-once").disabled = Boolean(data.mining);
  $("mining-indicator").classList.toggle("active", Boolean(data.mining));
  setText("mining-label", data.mining ? "Mining" : "Stopped");
}

function renderSync(data) {
  const localHeight = asNumber(data.height);
  const reportedBest = asNumber(data.best_peer_height);
  const bestHeight = Math.max(localHeight, reportedBest);
  setText("best-peer-height", bestHeight.toLocaleString(currentLocale()));

  const progress = $("sync-progress");
  const syncIcon = $("sync-icon");
  progress.classList.remove("indeterminate");
  syncIcon.classList.toggle("active", Boolean(data.syncing));

  const onlinePeers = asNumber(data.peer_count);
  const networkHealth = $("network-health");
  networkHealth.className = "network-health";
  if (data.bootstrap_enabled && onlinePeers === 0) {
    setText("sync-label", "Connecting to the network");
    setText("sync-detail", data.bootstrap_error ? "Public seeds are being retried" : "Discovering public peers");
    progress.style.width = "35%";
    progress.classList.add("indeterminate");
    syncIcon.classList.add("active");
    networkHealth.textContent = "Connecting";
    networkHealth.classList.add("connecting");
  } else if (data.syncing) {
    setText("sync-label", "Synchronizing chain");
    setText("sync-detail", reportedBest > localHeight
      ? `Local #${localHeight.toLocaleString(currentLocale())} of #${reportedBest.toLocaleString(currentLocale())}`
      : "Validating peer data");
    if (reportedBest > 0) {
      progress.style.width = `${Math.min(100, (localHeight / reportedBest) * 100)}%`;
    } else {
      progress.style.width = "35%";
      progress.classList.add("indeterminate");
    }
    networkHealth.textContent = "Syncing";
    networkHealth.classList.add("syncing");
  } else if (reportedBest > localHeight) {
    setText("sync-label", "Peer chain is ahead");
    setText("sync-detail", `Local #${localHeight.toLocaleString(currentLocale())} | peer #${reportedBest.toLocaleString(currentLocale())}`);
    progress.style.width = `${Math.min(100, (localHeight / reportedBest) * 100)}%`;
    networkHealth.textContent = "Behind";
    networkHealth.classList.add("syncing");
  } else {
    setText("sync-label", "Chain synchronized");
    setText("sync-detail", `Validated through block #${localHeight.toLocaleString(currentLocale())}`);
    progress.style.width = "100%";
    networkHealth.textContent = onlinePeers > 0 ? "Online" : "Offline";
    networkHealth.classList.add(onlinePeers > 0 ? "online" : "offline");
  }
}

function renderDashboard(data) {
  state.dashboard = data;
  const blocks = asArray(data.recent_blocks);
  const localHeight = asNumber(data.height);

  setText("confirmed-balance", formatAmount(data.confirmed_balance));
  setText("spendable-balance", formatAmount(data.spendable_balance));
  setText("wallet-address", data.address || "Unavailable");
  setText("wallet-page-address", data.address || "Unavailable");
  setText("height", localHeight.toLocaleString(currentLocale()));
  setText("pending-count", asNumber(data.pending_count).toLocaleString(currentLocale()));
  setText("difficulty", asNumber(data.difficulty).toLocaleString(currentLocale()));
  setText("target-seconds", asNumber(data.target_block_seconds).toLocaleString(currentLocale()));
  setText("issued", formatAmount(data.issued));
  setText("max-supply", formatAmount(data.max_supply));
  setText("next-reward", formatAmount(data.next_subsidy));
  setText("tip-hash", shortHash(data.tip_hash, 18));
  $("tip-hash").title = String(data.tip_hash || "");

  const issued = asNumber(data.issued);
  const maximum = asNumber(data.max_supply);
  const supplyPercent = maximum > 0 ? Math.min(100, (issued / maximum) * 100) : 0;
  $("supply-progress").style.width = `${Math.max(supplyPercent, localHeight > 0 ? 0.35 : 0)}%`;

  const statusDot = $("status-dot");
  statusDot.classList.toggle("error", Boolean(data.last_error));
  statusDot.classList.toggle("syncing", !data.last_error && Boolean(data.syncing));
  setText("node-state-label", data.last_error ? "Node warning" : asNumber(data.peer_count) === 0 && data.bootstrap_enabled ? "Finding network" : data.syncing ? "Synchronizing" : "Node active");

  renderSync(data);
  renderMining(data);
  renderBlocks(blocks);
  renderWalletProfiles(asArray(data.wallets), Boolean(data.mining));
  setText("database-path", data.database_path || "Unavailable");
  $("database-path").title = String(data.database_path || "");
  setText("database-size", formatBytes(data.database_bytes));
  const storageMode = $("storage-mode");
  const pruneDepth = asNumber(data.prune_depth);
  const prunedThrough = asNumber(data.pruned_through);
  if (pruneDepth === 0 && prunedThrough === 0) {
    storageMode.textContent = "Archive";
    storageMode.classList.remove("pruned");
  } else if (pruneDepth === 0) {
    storageMode.textContent = "Archive going forward / previously pruned";
    storageMode.classList.add("pruned");
  } else {
    storageMode.textContent = `Pruned | keep ${pruneDepth.toLocaleString(currentLocale())}`;
    storageMode.classList.add("pruned");
  }
  setText("prune-depth", pruneDepth > 0 ? `${pruneDepth.toLocaleString(currentLocale())} recent blocks` : "No future pruning");
  setText("pruned-through", prunedThrough > 0
    ? `Block #${prunedThrough.toLocaleString(currentLocale())}`
    : pruneDepth > 0 ? "Retention enabled; no eligible blocks yet" : "Not pruned");
  setText("diagnostic-protocol", data.protocol || "Unknown");
  setText("diagnostic-consensus", `v${asNumber(data.consensus_version, 1)} @ #${asNumber(data.consensus_activation).toLocaleString(currentLocale())}`);
  setText("diagnostic-listen", data.listen_address || "Not listening");
  setText("diagnostic-tip", data.tip_hash ? `#${localHeight.toLocaleString(currentLocale())} ${shortHash(data.tip_hash, 20)}` : "Unavailable");
  $("diagnostic-tip").title = String(data.tip_hash || "");
  setText("emission-blocks", `${asNumber(data.emission_blocks).toLocaleString(currentLocale())} blocks at ${asNumber(data.target_block_seconds).toLocaleString(currentLocale())} seconds`);
  setText("last-error", data.last_error ? translateError(data.last_error) : "None");
  $("last-error").classList.toggle("error-text", Boolean(data.last_error));

  const health = $("health-label");
  health.classList.toggle("error", Boolean(data.last_error));
  health.querySelector("span").textContent = data.last_error ? "Warning" : "Healthy";

  $("backup-alert").hidden = !data.wallet_needs_backup;
  const security = $("wallet-security-state");
  security.classList.toggle("warning", Boolean(data.wallet_needs_backup));
  security.querySelector("span").textContent = data.wallet_needs_backup ? "Backup needed" : "Recovery secured";

  $("open-restore-backup").disabled = Boolean(data.mining);
  $("open-restore-phrase").disabled = Boolean(data.mining);
  $("open-create-wallet").disabled = Boolean(data.mining);
  $("open-restore-backup").title = data.mining ? "Stop mining before restoring" : "Restore encrypted backup";
  $("open-restore-phrase").title = data.mining ? "Stop mining before restoring" : "Restore recovery phrase";
  $("open-create-wallet").title = data.mining ? "Stop mining before creating a wallet" : "Create wallet";
  $("switch-archive").disabled = pruneDepth === 0;
  $("switch-archive").title = pruneDepth === 0
    ? prunedThrough > 0 ? "Archive policy is active; previously deleted data remains unavailable" : "Archive policy is already active"
    : "Stop future pruning; previously deleted data will not be restored";

  createIcons({ icons: appIcons });
}

function transactionType(transaction) {
  switch (transactionKind(transaction)) {
    case "mining": return { label: "Mining reward", className: "mined", icon: "pickaxe" };
    case "sent": return { label: "Sent transaction", className: "sent", icon: "arrow-up-right" };
    default: return { label: "Received transaction", className: "received", icon: "arrow-down-left" };
  }
}

function amountCell(value, className) {
  const cell = document.createElement("td");
  const amount = String(value ?? "0");
  if (/^0(?:\.0+)?$/.test(amount)) {
    cell.className = "zero-amount";
    cell.textContent = "--";
  } else {
    cell.className = className;
    cell.textContent = `${formatAmount(amount)} ENT`;
  }
  return cell;
}

function renderHistory(transactions) {
  const body = $("history-body");
  body.replaceChildren();
  setText("history-count", transactions.length.toLocaleString(currentLocale()));
  setText("history-total", transactions.length.toLocaleString(currentLocale()));

  if (transactions.length === 0) {
    const row = body.insertRow();
    const cell = row.insertCell();
    cell.colSpan = 5;
    cell.className = "empty-cell";
    const emptyMessages = {
      all: "No wallet transactions yet",
      received: "No received transactions yet",
      sent: "No sent transactions yet",
      mining: "No mining rewards yet",
    };
    cell.textContent = emptyMessages[state.historyFilter] || emptyMessages.all;
    return;
  }

  for (const transaction of transactions) {
    const row = body.insertRow();
    row.className = "transaction-row";
    row.tabIndex = 0;
    row.title = "Open transaction details";
    row.addEventListener("click", () => openTransactionDetails(transaction));
    row.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        openTransactionDetails(transaction);
      }
    });
    row.insertCell().textContent = formatTimestamp(transaction.timestamp);

    const identityCell = row.insertCell();
    const identity = document.createElement("div");
    identity.className = "tx-identity";
    const type = transactionType(transaction);
    const direction = document.createElement("span");
    direction.className = `tx-direction ${type.className}`;
    direction.append(icon(type.icon));
    const details = document.createElement("div");
    const title = document.createElement("strong");
    title.textContent = type.label;
    const id = document.createElement("code");
    id.textContent = shortHash(transaction.id, 22);
    id.title = String(transaction.id || "");
    details.append(title, id);
    identity.append(direction, details);
    identityCell.append(identity);

    row.append(amountCell(transaction.received, "tx-received"));
    row.append(amountCell(transaction.sent, "tx-sent"));

    const statusCell = document.createElement("td");
    const badge = document.createElement("span");
    if (transaction.pending) {
      badge.className = "status-badge pending";
      badge.append(icon("clock-3"), document.createTextNode("Pending"));
    } else {
      const confirmations = asNumber(transaction.confirmations);
      badge.className = "status-badge confirmed";
      badge.append(icon("circle-check"), document.createTextNode(`${confirmations.toLocaleString(currentLocale())} confirmation${confirmations === 1 ? "" : "s"}`));
      if (transaction.block_height != null) badge.title = `Block #${asNumber(transaction.block_height).toLocaleString(currentLocale())}`;
    }
    statusCell.append(badge);
    row.append(statusCell);
  }

  createIcons({ icons: appIcons });
}

async function refreshDashboard() {
  if (!state.ready || state.dashboardRefreshing) return;
  state.dashboardRefreshing = true;
  try {
    const dashboard = await invoke("GetDashboard");
    renderDashboard(dashboard);
    state.lastDashboardError = "";
  } catch (error) {
    const message = errorMessage(error);
    $("status-dot").classList.add("error");
    setText("node-state-label", "Node offline");
    if (message !== state.lastDashboardError) showToast(message, "error");
    state.lastDashboardError = message;
  } finally {
    state.dashboardRefreshing = false;
  }
}

function renderUpdate(status) {
  state.updateStatus = status;
  state.updateChecked = true;
  setText("current-version", `v${status.current_version || "1.5.4"}`);
  const available = Boolean(status.available);
  setText("update-status", available ? `Entcoin v${status.latest_version} is available` : "Entcoin is up to date");
  $("install-update").hidden = !available;
  $("open-update").hidden = !available;
  setText("open-update-label", available ? `Update v${status.latest_version}` : "Update available");
}

async function checkForUpdates(manual = false) {
  if (state.updateChecking || state.updateInstalling) return;
  state.updateChecking = true;
  const button = $("check-update");
  setButtonBusy(button, true, "Checking");
  setText("update-status", "Checking for updates");
  try {
    const status = await invoke("CheckForUpdate");
    renderUpdate(status);
    if (manual) showToast(status.available ? `Entcoin v${status.latest_version} is available` : "Entcoin is up to date", "info");
  } catch (error) {
    state.updateChecked = true;
    setText("update-status", "Update check unavailable");
    if (manual) showToast(errorMessage(error), "error");
  } finally {
    state.updateChecking = false;
    setButtonBusy(button, false);
  }
}

async function installUpdate() {
  if (state.updateInstalling) return;
  state.updateInstalling = true;
  const button = $("install-update");
  setButtonBusy(button, true, "Preparing");
  renderUpdateProgress({ phase: "preparing" });
  try {
    const result = await invoke("InstallUpdate");
    setText("update-status", result.message || "Update ready; restarting");
    showToast(result.message || "Update ready; restarting");
  } catch (error) {
    setText("update-status", "Update installation did not start");
    showToast(errorMessage(error), "error");
  } finally {
    state.updateInstalling = false;
    resetUpdateProgress();
    setButtonBusy(button, false);
  }
}

function detailValue(label, value, asCode = false) {
  const wrapper = document.createElement("div");
  const term = document.createElement("dt");
  const detail = document.createElement("dd");
  term.textContent = label;
  if (asCode) {
    const code = document.createElement("code");
    code.textContent = String(value);
    detail.append(code);
  } else {
    detail.textContent = String(value);
  }
  wrapper.append(term, detail);
  return wrapper;
}

function transactionStatus(transaction) {
  if (transaction.pending) return "Pending in local pool";
  const confirmations = asNumber(transaction.confirmations);
  return `${confirmations.toLocaleString(currentLocale())} confirmation${confirmations === 1 ? "" : "s"}`;
}

function transactionEntries(title, entries, format, asCode, emptyMessage) {
  const section = document.createElement("section");
  const heading = document.createElement("h3");
  heading.textContent = `${title} (${entries.length.toLocaleString(currentLocale())})`;
  const list = document.createElement("ol");
  if (entries.length === 0) {
    const item = document.createElement("li");
    item.textContent = emptyMessage;
    list.append(item);
  } else {
    for (const entry of entries) {
      const item = document.createElement("li");
      const values = format(entry);
      if (asCode) {
        const code = document.createElement("code");
        code.textContent = values[0];
        item.append(code);
      } else {
        const amount = document.createElement("strong");
        const address = document.createElement("code");
        amount.textContent = values[0];
        address.textContent = values[1];
        item.append(amount, address);
      }
      list.append(item);
    }
  }
  section.append(heading, list);
  return section;
}

function openTransactionDetails(transaction) {
  const content = $("transaction-detail");
  content.replaceChildren();
  const overview = document.createElement("dl");
  overview.className = "transaction-overview";
  overview.append(
    detailValue("Transaction ID", transaction.id || "Unavailable", true),
    detailValue("Status", transactionStatus(transaction)),
    detailValue("Type", transactionType(transaction).label),
    detailValue("Time", formatTimestamp(transaction.timestamp)),
    detailValue("Received", `${formatAmount(transaction.received || "0")} ENT`),
    detailValue("Sent", `${formatAmount(transaction.sent || "0")} ENT`),
  );
  if (transaction.block_height != null) {
    overview.append(
      detailValue("Block height", `#${asNumber(transaction.block_height).toLocaleString(currentLocale())}`),
      detailValue("Block hash", transaction.block_hash || "Unavailable", true),
      detailValue("Position", asNumber(transaction.block_position).toLocaleString(currentLocale())),
    );
  }
  if (transaction.pruned) overview.append(detailValue("Body", "Pruned locally; indexed wallet totals remain available"));
  content.append(overview);

  const lists = document.createElement("div");
  lists.className = "transaction-io-grid";
  const bodyUnavailable = "Transaction body is unavailable";
  lists.append(transactionEntries(
    "Inputs",
    asArray(transaction.inputs),
    (input) => [`${input.transaction_id}:${input.output_index}`],
    true,
    transaction.pruned ? bodyUnavailable : transaction.coinbase ? "Coinbase has no inputs" : "No inputs",
  ));
  lists.append(transactionEntries(
    "Outputs",
    asArray(transaction.outputs),
    (output) => [`${formatAmount(output.amount)} ENT`, output.address],
    false,
    transaction.pruned ? bodyUnavailable : "No outputs",
  ));
  content.append(lists);
  openDialog("transaction-dialog");
}

async function refreshHistory(force = false) {
  if (!state.ready) return;
  if (state.historyRefreshing) {
    state.historyRefreshQueued = true;
    return;
  }
  state.historyRefreshing = true;
  const requestedFilter = state.historyFilter;
  const button = $("refresh-history");
  if (force) {
    button.disabled = true;
    button.classList.add("button-busy");
  }
  try {
    const history = asArray(await invoke("GetTransactionHistory", 100, requestedFilter));
    if (requestedFilter === state.historyFilter) {
      state.history = history;
      state.lastHistoryRefresh = Date.now();
      renderHistory(history);
      setText("history-updated", `Updated ${new Intl.DateTimeFormat(currentLocale(), { hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(new Date())}`);
    }
  } catch (error) {
    if (force || state.history.length === 0) showToast(errorMessage(error), "error");
    if (requestedFilter === state.historyFilter) {
      const body = $("history-body");
      body.replaceChildren();
      const row = body.insertRow();
      const cell = row.insertCell();
      cell.colSpan = 5;
      cell.className = "empty-cell";
      cell.textContent = "Transaction history is unavailable";
    }
  } finally {
    state.historyRefreshing = false;
    if (force) {
      button.disabled = false;
      button.classList.remove("button-busy");
    }
    if (state.historyRefreshQueued) {
      state.historyRefreshQueued = false;
      void refreshHistory(true);
    }
  }
}

function showStartupSection(section) {
  $("startup-loading").hidden = section !== "loading";
  $("migration-form").hidden = section !== "migration";
  $("startup-failed").hidden = section !== "failed";
}

async function checkStartup() {
  if (state.startupChecking || state.ready) return;
  state.startupChecking = true;
  try {
    const startup = await invoke("GetStartupState");
    state.startupCode = startup.code || "starting";
    setText("startup-message", startup.code === "startup_failed"
      ? translateError(startup.message)
      : translate(startup.message || "Opening wallet and ledger..."));

    if (startup.ready || startup.code === "ready") {
      state.ready = true;
      $("startup-overlay").classList.remove("visible");
      await Promise.all([refreshDashboard(), refreshHistory()]);
      void checkForUpdates(false);
      return;
    }
    $("startup-overlay").classList.add("visible");
    if (startup.code === "wallet_migration_required") {
      showStartupSection("migration");
    } else if (startup.code === "startup_failed") {
      setText("startup-error", translateError(startup.message || "Unknown startup error"));
      showStartupSection("failed");
    } else {
      showStartupSection("loading");
    }
  } catch (error) {
    state.startupCode = "startup_failed";
    setText("startup-error", errorMessage(error));
    showStartupSection("failed");
    $("startup-overlay").classList.add("visible");
  } finally {
    state.startupChecking = false;
  }
}

function activateView(name) {
  for (const tab of document.querySelectorAll("[data-view]")) {
    tab.classList.toggle("active", tab.dataset.view === name);
  }
  for (const panel of document.querySelectorAll("[data-view-panel]")) {
    const active = panel.dataset.viewPanel === name;
    panel.hidden = !active;
    panel.classList.toggle("active", active);
  }
  if (name === "transactions") refreshHistory(true);
  if (name === "entpay") void entpayViewActivated();
  if (name === "diagnostics" && !state.updateChecked) void checkForUpdates(false);
}

function clearSensitive(dialog) {
  for (const input of dialog.querySelectorAll('input[type="password"], textarea')) input.value = "";
  for (const input of dialog.querySelectorAll('input[type="checkbox"]')) input.checked = false;
  if (dialog.id === "recovery-dialog") {
    state.recoveryPhrase = "";
    $("recovery-grid").replaceChildren();
    $("recovery-placeholder").hidden = false;
    $("recovery-content").hidden = true;
    $("confirm-recovery").disabled = true;
  }
  if (dialog.id === "prune-dialog") {
    state.pendingPruneRetain = 0;
    $("confirm-prune").disabled = true;
  }
  if (dialog.id === "create-wallet-dialog") $("confirm-create-wallet").disabled = true;
  if (dialog.id === "remove-wallet-dialog") {
    state.pendingRemoveWallet = "";
    setText("remove-wallet-address", "--");
    $("confirm-remove-wallet").disabled = true;
  }
  updateAllSensitiveCounters();
}

function openDialog(id) {
  const dialog = $(id);
  clearSensitive(dialog);
  if (!dialog.open) dialog.showModal();
}

function closeDialog(id) {
  const dialog = $(id);
  if (dialog.open) dialog.close();
}

function updateAllSensitiveCounters() {
  updatePasswordCount("migration-password", "migration-password-bytes");
  updatePasswordCount("export-password", "export-password-bytes");
  updatePasswordCount("restore-backup-password", "restore-password-bytes");
  const words = $("restore-phrase").value.trim().split(/\s+/).filter(Boolean).length;
  setText("restore-word-count", `${words} / 24 words`);
}

async function switchWallet(address, button) {
  if (!address) return;
  setButtonBusy(button, true, "Switching");
  try {
    const result = await invoke("SwitchWallet", address);
    showToast(`${result.message || "Wallet activated"}: ${shortHash(address, 18)}`);
    state.history = [];
    await Promise.all([refreshDashboard(), refreshHistory()]);
  } catch (error) {
    showToast(errorMessage(error), "error");
    setButtonBusy(button, false);
  }
}

function openRemoveWallet(address) {
  if (!address) return;
  openDialog("remove-wallet-dialog");
  state.pendingRemoveWallet = address;
  setText("remove-wallet-address", address);
}

document.querySelectorAll("[data-view]").forEach((tab) => {
  tab.addEventListener("click", () => activateView(tab.dataset.view));
});

$("language-toggle").addEventListener("click", toggleLanguage);

document.querySelectorAll("[data-close-dialog]").forEach((button) => {
  button.addEventListener("click", () => closeDialog(button.dataset.closeDialog));
});

document.querySelectorAll("dialog").forEach((dialog) => {
  dialog.addEventListener("click", (event) => {
    if (event.target === dialog) dialog.close();
  });
  dialog.addEventListener("close", () => clearSensitive(dialog));
});

$("copy-address").addEventListener("click", () => copyText(state.dashboard?.address, "Address copied"));
$("copy-wallet-page-address").addEventListener("click", () => copyText(state.dashboard?.address, "Address copied"));
$("copy-database-path").addEventListener("click", () => copyText(state.dashboard?.database_path, "Database path copied"));
$("open-release-page").addEventListener("click", () => invoke("OpenReleasePage"));
$("check-update").addEventListener("click", () => checkForUpdates(true));
$("install-update").addEventListener("click", installUpdate);
$("open-update").addEventListener("click", () => activateView("diagnostics"));
document.querySelectorAll("[data-history-filter]").forEach((button) => {
  button.addEventListener("click", () => {
    state.historyFilter = button.dataset.historyFilter;
    document.querySelectorAll("[data-history-filter]").forEach((candidate) => {
      const active = candidate === button;
      candidate.classList.toggle("active", active);
      candidate.setAttribute("aria-pressed", String(active));
    });
    const body = $("history-body");
    body.replaceChildren();
    setText("history-count", "0");
    setText("history-total", "0");
    const row = body.insertRow();
    const cell = row.insertCell();
    cell.colSpan = 5;
    cell.className = "empty-cell";
    cell.textContent = "Loading transactions";
    void refreshHistory(true);
  });
});

$("send-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  const button = form.querySelector('button[type="submit"]');
  const to = $("send-to").value.trim();
  const amount = $("send-amount").value.trim();
  if (!to) {
    showToast("Recipient address is required", "error");
    return;
  }
  if (!isAmount(amount, false)) {
    showToast("Amount must be positive with no more than 8 decimal places", "error");
    return;
  }

  setButtonBusy(button, true, "Broadcasting");
  try {
    const result = await invoke("SendTransaction", to, amount);
    showToast(`${result.message || "Transaction submitted"}: ${shortHash(result.id)}`);
    $("send-amount").value = "";
    await Promise.all([refreshDashboard(), refreshHistory()]);
  } catch (error) {
    showToast(errorMessage(error), "error");
  } finally {
    setButtonBusy(button, false);
  }
});

$("toggle-mining").addEventListener("click", async () => {
  if (!state.dashboard) return;
  const button = $("toggle-mining");
  const enabling = !state.dashboard.mining;
  setButtonBusy(button, true, enabling ? "Starting" : "Stopping");
  try {
    const result = await invoke("SetMining", enabling);
    showToast(result.message || (enabling ? "Mining started" : "Mining stopping"));
    await refreshDashboard();
  } catch (error) {
    showToast(errorMessage(error), "error");
  } finally {
    setButtonBusy(button, false);
  }
});

$("mine-once").addEventListener("click", async () => {
  const button = $("mine-once");
  setButtonBusy(button, true, "Mining block");
  try {
    const result = await invoke("MineOneBlock");
    showToast(`${result.message || "Block mined"}: ${shortHash(result.id)}`);
    await Promise.all([refreshDashboard(), refreshHistory()]);
  } catch (error) {
    showToast(errorMessage(error), "error");
  } finally {
    setButtonBusy(button, false);
  }
});

$("refresh-history").addEventListener("click", () => refreshHistory(true));
$("backup-alert-action").addEventListener("click", () => openDialog("recovery-dialog"));
$("open-recovery").addEventListener("click", () => openDialog("recovery-dialog"));
$("open-export").addEventListener("click", () => openDialog("export-dialog"));
$("open-restore-backup").addEventListener("click", () => openDialog("restore-backup-dialog"));
$("open-restore-phrase").addEventListener("click", () => openDialog("restore-phrase-dialog"));
$("open-create-wallet").addEventListener("click", () => openDialog("create-wallet-dialog"));

$("create-wallet-check").addEventListener("change", (event) => {
  $("confirm-create-wallet").disabled = !event.currentTarget.checked;
});

$("confirm-create-wallet").addEventListener("click", async () => {
  if (!$("create-wallet-check").checked) return;
  const button = $("confirm-create-wallet");
  setButtonBusy(button, true, "Creating");
  try {
    const result = await invoke("CreateWallet");
    showToast(`${result.message || "Wallet created"}: ${shortHash(result.id, 18)}`);
    closeDialog("create-wallet-dialog");
    state.history = [];
    await Promise.all([refreshDashboard(), refreshHistory()]);
  } catch (error) {
    showToast(errorMessage(error), "error");
    setButtonBusy(button, false);
  }
});

$("remove-wallet-check").addEventListener("change", (event) => {
  $("confirm-remove-wallet").disabled = !event.currentTarget.checked;
});

$("confirm-remove-wallet").addEventListener("click", async () => {
  const address = state.pendingRemoveWallet;
  if (!address || !$("remove-wallet-check").checked) return;
  const button = $("confirm-remove-wallet");
  setButtonBusy(button, true, "Removing");
  try {
    const result = await invoke("RemoveWallet", address);
    showToast(result.message || "Wallet removed");
    closeDialog("remove-wallet-dialog");
    await refreshDashboard();
  } catch (error) {
    showToast(errorMessage(error), "error");
    setButtonBusy(button, false);
  }
});

$("prune-form").addEventListener("submit", (event) => {
  event.preventDefault();
  const text = $("prune-retain").value.trim();
  if (!/^[1-9]\d*$/.test(text)) {
    showToast("Retention must be a whole number", "error");
    return;
  }
  const retain = Number(text);
  if (!Number.isSafeInteger(retain) || retain < 120 || retain > 31536000) {
    showToast("Retention must be between 120 and 31,536,000 blocks", "error");
    return;
  }

  const height = asNumber(state.dashboard?.height);
  const currentHorizon = asNumber(state.dashboard?.pruned_through);
  const requestedHorizon = Math.max(0, height - retain);
  state.pendingPruneRetain = retain;
  if (currentHorizon > requestedHorizon) {
    setText("prune-impact", `The ledger is already pruned through block #${currentHorizon.toLocaleString(currentLocale())}. Deleted data will not be restored; future pruning will retain the newest ${retain.toLocaleString(currentLocale())} blocks.`);
  } else if (requestedHorizon === 0) {
    setText("prune-impact", `Current height is #${height.toLocaleString(currentLocale())}. No existing body is eligible yet; future pruning will retain the newest ${retain.toLocaleString(currentLocale())} blocks.`);
  } else {
    setText("prune-impact", `Block and transaction bodies plus undo records through block #${requestedHorizon.toLocaleString(currentLocale())} will be permanently removed. The newest ${retain.toLocaleString(currentLocale())} blocks remain complete.`);
  }
  openDialog("prune-dialog");
  state.pendingPruneRetain = retain;
});

$("prune-confirm-check").addEventListener("change", (event) => {
  $("confirm-prune").disabled = !event.currentTarget.checked;
});

$("switch-archive").addEventListener("click", async () => {
  const button = $("switch-archive");
  setButtonBusy(button, true, "Switching");
  try {
    const result = await invoke("PruneLedger", 0);
    showToast(`${result.message || "Archive policy enabled"}. Previously pruned data remains unavailable.`, "info");
    await refreshDashboard();
  } catch (error) {
    showToast(errorMessage(error), "error");
  } finally {
    setButtonBusy(button, false);
    if (asNumber(state.dashboard?.prune_depth) === 0) button.disabled = true;
  }
});

$("confirm-prune").addEventListener("click", async () => {
  const button = $("confirm-prune");
  const retain = state.pendingPruneRetain;
  if (!retain || !$("prune-confirm-check").checked) return;
  setButtonBusy(button, true, "Pruning");
  try {
    const result = await invoke("PruneLedger", retain);
    showToast(result.message || "Ledger pruning completed");
    closeDialog("prune-dialog");
    await refreshDashboard();
  } catch (error) {
    showToast(errorMessage(error), "error");
    setButtonBusy(button, false);
  }
});

$("reveal-recovery").addEventListener("click", async () => {
  const button = $("reveal-recovery");
  setButtonBusy(button, true, "Decrypting");
  try {
    const phrase = String(await invoke("GetRecoveryPhrase"));
    const words = phrase.trim().split(/\s+/).filter(Boolean);
    if (words.length !== 24) throw new Error("Backend returned an invalid recovery phrase");
    state.recoveryPhrase = words.join(" ");
    const grid = $("recovery-grid");
    grid.replaceChildren();
    words.forEach((word, index) => {
      const item = document.createElement("li");
      const number = document.createElement("span");
      number.textContent = String(index + 1);
      const value = document.createElement("code");
      value.textContent = word;
      item.append(number, value);
      grid.append(item);
    });
    $("recovery-placeholder").hidden = true;
    $("recovery-content").hidden = false;
  } catch (error) {
    const message = errorMessage(error);
    setText("recovery-action-detail", message);
    showToast(message, "error");
  } finally {
    setButtonBusy(button, false);
  }
});

$("copy-recovery").addEventListener("click", () => copyText(state.recoveryPhrase, "Recovery phrase copied"));
$("recovery-confirm-check").addEventListener("change", (event) => {
  $("confirm-recovery").disabled = !event.currentTarget.checked;
});
$("confirm-recovery").addEventListener("click", async () => {
  const button = $("confirm-recovery");
  if (!state.recoveryPhrase || !$("recovery-confirm-check").checked) return;
  setButtonBusy(button, true, "Saving");
  try {
    const result = await invoke("ConfirmWalletRecovery");
    showToast(result.message || "Wallet recovery confirmed");
    closeDialog("recovery-dialog");
    await refreshDashboard();
  } catch (error) {
    showToast(errorMessage(error), "error");
    setButtonBusy(button, false);
  }
});

$("export-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  const button = form.querySelector('button[type="submit"]');
  const password = $("export-password").value;
  const confirmation = $("export-confirm").value;
  try {
    validatePassword(password);
    if (password !== confirmation) throw new Error("Password confirmation does not match");
  } catch (error) {
    showToast(errorMessage(error), "error");
    return;
  }
  setButtonBusy(button, true, "Encrypting");
  try {
    const result = await invoke("ExportWalletBackup", password);
    showToast(result.message || "Wallet backup exported", result.message?.toLowerCase().includes("cancel") ? "info" : "success");
    closeDialog("export-dialog");
    await refreshDashboard();
  } catch (error) {
    showToast(errorMessage(error), "error");
  } finally {
    $("export-password").value = "";
    $("export-confirm").value = "";
    setButtonBusy(button, false);
  }
});

$("restore-backup-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  const button = form.querySelector('button[type="submit"]');
  const password = $("restore-backup-password").value;
  try {
    validatePassword(password);
    if (!$("restore-backup-check").checked) throw new Error("Confirm wallet import");
  } catch (error) {
    showToast(errorMessage(error), "error");
    return;
  }
  setButtonBusy(button, true, "Restoring");
  try {
    const result = await invoke("RestoreWalletBackup", password);
    showToast(result.id ? `${result.message}: ${shortHash(result.id, 18)}` : result.message, result.message?.toLowerCase().includes("cancel") ? "info" : "success");
    closeDialog("restore-backup-dialog");
    state.history = [];
    await Promise.all([refreshDashboard(), refreshHistory()]);
  } catch (error) {
    showToast(errorMessage(error), "error");
  } finally {
    $("restore-backup-password").value = "";
    setButtonBusy(button, false);
  }
});

$("restore-phrase-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  const button = form.querySelector('button[type="submit"]');
  const words = $("restore-phrase").value.trim().split(/\s+/).filter(Boolean);
  if (words.length !== 24) {
    showToast("Recovery phrase must contain exactly 24 words", "error");
    return;
  }
  if (!$("restore-phrase-check").checked) {
    showToast("Confirm wallet import", "error");
    return;
  }
  const phrase = words.join(" ");
  setButtonBusy(button, true, "Restoring");
  try {
    const result = await invoke("RestoreWalletMnemonic", phrase);
    showToast(`${result.message || "Wallet restored"}: ${shortHash(result.id, 18)}`);
    closeDialog("restore-phrase-dialog");
    state.history = [];
    await Promise.all([refreshDashboard(), refreshHistory()]);
  } catch (error) {
    showToast(errorMessage(error), "error");
  } finally {
    $("restore-phrase").value = "";
    setButtonBusy(button, false);
  }
});

$("migration-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  const button = form.querySelector('button[type="submit"]');
  const password = $("migration-password").value;
  const confirmation = $("migration-confirm").value;
  try {
    validatePassword(password);
    if (password !== confirmation) throw new Error("Password confirmation does not match");
  } catch (error) {
    showToast(errorMessage(error), "error");
    return;
  }
  setButtonBusy(button, true, "Encrypting");
  try {
    const result = await invoke("MigrateLegacyWallet", password);
    const cancelled = result.message?.toLowerCase().includes("cancel");
    showToast(result.message || "Legacy wallet migrated", cancelled ? "info" : "success");
    if (!cancelled) {
      state.startupCode = "starting";
      showStartupSection("loading");
      await checkStartup();
    }
  } catch (error) {
    showToast(errorMessage(error), "error");
  } finally {
    $("migration-password").value = "";
    $("migration-confirm").value = "";
    updateAllSensitiveCounters();
    setButtonBusy(button, false);
  }
});

$("retry-startup").addEventListener("click", async () => {
  state.startupCode = "starting";
  showStartupSection("loading");
  try {
    await invoke("RetryStartup");
  } catch (error) {
    showToast(errorMessage(error), "error");
  }
  await checkStartup();
});

for (const input of [$("migration-password"), $("export-password"), $("restore-backup-password")]) {
  input.addEventListener("input", updateAllSensitiveCounters);
}
$("restore-phrase").addEventListener("input", updateAllSensitiveCounters);

window.addEventListener("beforeunload", () => {
  document.querySelectorAll('input[type="password"], textarea').forEach((input) => { input.value = ""; });
  state.recoveryPhrase = "";
});

async function heartbeat() {
  if (!state.ready) {
    if (state.startupCode === "starting") await checkStartup();
    return;
  }
  await refreshDashboard();
  if (Date.now() - state.lastHistoryRefresh >= 5000) await refreshHistory();
}

initializeI18n();
initializeEntPay({ invoke, showToast, activateView, refreshIcons: () => createIcons({ icons: appIcons }) });
if (window.runtime?.EventsOnMultiple) {
  window.runtime.EventsOnMultiple("entcoin:update-progress", renderUpdateProgress, -1);
}
onLanguageChange(() => {
  if (state.dashboard) renderDashboard(state.dashboard);
  if (state.history.length > 0) renderHistory(state.history);
  if (state.updateStatus) renderUpdate(state.updateStatus);
  if (!$("view-entpay").hidden) void refreshEntPay(true);
  updateAllSensitiveCounters();
});
createIcons({ icons: appIcons });
updateAllSensitiveCounters();
checkStartup();
setInterval(heartbeat, 1200);
