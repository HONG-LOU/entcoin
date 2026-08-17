import { currentLocale } from "./i18n.js";

const ACTIVE_STAGES = new Set(["preparing_payment", "broadcast", "submitting", "confirming", "fulfilling", "verifying"]);
const SENT_STAGES = new Set(["broadcast", "submitting", "confirming", "fulfilling", "verifying", "complete"]);
const DELETABLE_STAGES = new Set(["awaiting_approval", "complete", "invalid", "expired", "rejected", "failed_terminal"]);
export const ENTPAY_PAGE_SIZE = 5;

const stageCopy = Object.freeze({
  received: "Received",
  inspecting: "Verifying request",
  awaiting_approval: "Awaiting approval",
  preparing_payment: "Preparing payment",
  broadcast: "Payment broadcast",
  submitting: "Submitting proof",
  confirming: "Confirming",
  fulfilling: "Merchant fulfilling",
  verifying: "Verifying delivery",
  complete: "Complete",
  invalid: "Invalid request",
  expired: "Expired",
  rejected: "Rejected",
  failed_retryable: "Action required",
  failed_terminal: "Verification failed",
});

let sessions = [];
let selectedID = "";
let loading = false;
let invokeBackend;
let notify;
let activate;
let settingsRevision = 0;
let currentPage = 0;
let refreshIcons = () => {};

const byID = (id) => document.getElementById(id);

export function formatEnt(units) {
  let value;
  try { value = BigInt(units ?? 0); } catch { return "--"; }
  const whole = value / 100000000n;
  const fraction = String(value % 100000000n).padStart(8, "0").replace(/0+$/, "");
  return `${whole.toLocaleString(currentLocale())}${fraction ? `.${fraction}` : ""} ENT`;
}

export function parseEnt(value) {
	const match = String(value ?? "").trim().match(/^(\d+)(?:\.(\d{1,8}))?$/);
	if (!match) throw new Error("Maximum amount must be a positive ENT amount with at most 8 decimal places");
	const units = BigInt(match[1]) * 100000000n + BigInt((match[2] || "").padEnd(8, "0") || "0");
	if (units <= 0n || units > 9007199254740991n) throw new Error("Maximum amount is outside the supported range");
	return Number(units);
}

export function entpayStageLabel(stage) {
  return stageCopy[stage] || "Unknown state";
}

export function paymentSent(session) {
  return Boolean(session?.transaction_id) || SENT_STAGES.has(session?.stage);
}

export function revisionMatches(rendered, current) {
  return Number.isSafeInteger(Number(rendered)) && Number(rendered) === Number(current);
}

function element(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function short(value, length = 16) {
  const text = String(value || "");
  return text.length > length ? `${text.slice(0, length)}...` : text || "--";
}

function expiryText(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "--";
  return new Intl.DateTimeFormat(currentLocale(), { dateStyle: "medium", timeStyle: "medium" }).format(date);
}

function setStatus(message, kind = "") {
  const status = byID("entpay-status");
  status.textContent = message;
  status.dataset.kind = kind;
}

function mergeSession(session) {
  const index = sessions.findIndex((item) => item.id === session.id);
  if (index >= 0) sessions[index] = session;
  else sessions.unshift(session);
  sessions.sort((a, b) => new Date(b.updated_at) - new Date(a.updated_at));
}

export function entpayPageCount(itemCount, pageSize = ENTPAY_PAGE_SIZE) {
  return Math.max(1, Math.ceil(Math.max(0, Number(itemCount) || 0) / pageSize));
}

export function canDeleteEntPaySession(session) {
  return DELETABLE_STAGES.has(session?.stage) || (session?.stage === "failed_retryable" && !paymentSent(session));
}

async function deleteSession(session, button) {
  if (!canDeleteEntPaySession(session) || button.disabled) return;
  button.disabled = true;
  try {
    const result = await invokeBackend("DeleteEntPaySession", session.id);
    sessions = sessions.filter((item) => item.id !== session.id);
    if (selectedID === session.id) selectedID = "";
    currentPage = Math.min(currentPage, entpayPageCount(sessions.length) - 1);
    if (!selectedID && sessions.length) selectedID = sessions[currentPage * ENTPAY_PAGE_SIZE]?.id || sessions[0].id;
    notify(result?.message || "Request deleted");
    await refreshEntPay(true);
  } catch (error) {
    button.disabled = false;
    notify(error?.message || "History could not be deleted", "error");
  }
}

function renderList() {
  const list = byID("entpay-list");
  list.replaceChildren();
  byID("entpay-count").textContent = String(sessions.length);
  const pageCount = entpayPageCount(sessions.length);
  currentPage = Math.min(currentPage, pageCount - 1);
  const pageStart = currentPage * ENTPAY_PAGE_SIZE;
  for (const session of sessions.slice(pageStart, pageStart + ENTPAY_PAGE_SIZE)) {
    const item = element("li", "entpay-row");
    const button = element("button", `entpay-row-select${selectedID === session.id ? " selected" : ""}`);
    button.type = "button";
    button.dataset.sessionId = session.id;
    button.append(
      element("strong", "", session.product?.name || session.merchant || "Payment request"),
      element("span", "entpay-row-merchant", session.endpoint || "--"),
      element("span", `entpay-stage stage-${session.stage}`, entpayStageLabel(session.stage)),
      element("b", "entpay-row-amount", session.invoice ? formatEnt(session.invoice.amount) : "--"),
    );
    button.addEventListener("click", () => selectSession(session.id));
    item.append(button);
    if (canDeleteEntPaySession(session)) {
      const remove = element("button", "entpay-row-delete");
      remove.type = "button";
      remove.title = "Delete request";
      remove.setAttribute("aria-label", "Delete request");
      const icon = element("i");
      icon.dataset.lucide = "trash-2";
      remove.append(icon);
      remove.addEventListener("click", () => deleteSession(session, remove));
      item.append(remove);
    }
    list.append(item);
  }
  if (!sessions.length) list.append(element("li", "entpay-list-empty", "No Agent Pay requests yet"));
  const pagination = byID("entpay-pagination");
  pagination.hidden = pageCount <= 1;
  byID("entpay-page-status").textContent = `${currentPage + 1} / ${pageCount}`;
  byID("entpay-page-previous").disabled = currentPage === 0;
  byID("entpay-page-next").disabled = currentPage === pageCount - 1;
  refreshIcons();
}

async function showPage(page) {
  currentPage = Math.max(0, Math.min(page, entpayPageCount(sessions.length) - 1));
  const first = sessions[currentPage * ENTPAY_PAGE_SIZE];
  if (first) selectedID = first.id;
  renderList();
  if (first) await selectSession(first.id);
}

function detailRow(term, value, code = false) {
  const row = element("div");
  row.append(element("dt", "", term), element("dd", code ? "entpay-code" : "", value));
  return row;
}

export function normalizeEntPayResult(result) {
  if (!result || typeof result !== "object") return null;
  const schema = String(result.schema || "");
  const summary = String(result.summary || "").trim();
  if (!schema || !summary || !("data" in result)) return null;
  return { schema, summary, data: result.data };
}

function verificationRail(session) {
  const current = session.stage === "awaiting_approval" ? 0 :
    ["preparing_payment", "broadcast", "submitting"].includes(session.stage) ? 1 :
      session.stage === "confirming" ? 2 : ["fulfilling", "verifying"].includes(session.stage) ? 3 : session.stage === "complete" ? 4 : -1;
  const labels = ["Approve", "Broadcast", "Confirm", "Verify", "Done"];
  const rail = element("ol", "entpay-rail");
  labels.forEach((label, index) => {
    const item = element("li", index <= current ? "reached" : "", label);
    if (index === current) item.setAttribute("aria-current", "step");
    rail.append(item);
  });
  return rail;
}

function actionButton(label, className, action) {
  const button = element("button", className, label);
  button.type = "button";
  button.addEventListener("click", action);
  return button;
}

async function mutate(method, session, button) {
  if (button.disabled || !revisionMatches(button.dataset.revision, session.revision)) return;
  button.disabled = true;
  try {
    const result = await invokeBackend(method, session.id, session.revision);
    if (result?.stage) mergeSession(result);
    await refreshEntPay(true);
  } catch (error) {
    const prefix = paymentSent(session) ? "Payment was sent. " : "Payment has not been sent. ";
    notify(`${prefix}${error?.message || "The operation failed"}`, "error");
    await refreshEntPay(true);
  }
}

function renderDetail(detail) {
  const session = detail.session;
  const root = byID("entpay-detail");
  root.replaceChildren();
  const header = element("header", "entpay-detail-header");
  const title = element("div");
  title.append(element("p", "eyebrow", entpayStageLabel(session.stage)), element("h2", "", session.product?.name || "Payment request"));
  header.append(title, element("strong", "entpay-amount", session.invoice ? formatEnt(session.invoice.amount) : "--"));
  root.append(header, verificationRail(session));

  if (session.error_message) {
    const error = element("div", "entpay-error");
    error.append(element("strong", "", paymentSent(session) ? "Payment was sent" : "Payment has not been sent"), element("span", "", session.error_message));
    root.append(error);
  }

  const facts = element("dl", "entpay-facts");
  facts.append(
    detailRow("Merchant", session.merchant || "--", true),
    detailRow("Merchant endpoint", session.endpoint || "--", true),
    detailRow("Signing key fingerprint", short(session.signing_key_fingerprint, 24), true),
    detailRow("Trust", session.merchant_trust_state || "--"),
    detailRow("Wallet", short(session.wallet_address, 24), true),
    detailRow("Fee ceiling", formatEnt(session.fee_ceiling)),
    detailRow("Expires", expiryText(session.invoice?.expires_at)),
    detailRow("Confirmations", `${session.current_confirmations || 0} / ${session.invoice?.confirmations || 0}`),
  );
  root.append(facts);

  if (detail.input) {
    const input = element("section", "entpay-input");
    input.append(element("h3", "", "Verified request"));
    const pre = element("pre");
    try { pre.textContent = JSON.stringify(detail.input, null, 2); } catch { pre.textContent = "Request data unavailable"; }
    input.append(pre);
    root.append(input);
  }

	const delivery = normalizeEntPayResult(session.result);
	if (delivery) {
		const result = element("section", "entpay-result");
		const resultHeader = element("header", "entpay-result-header");
		const resultTitle = element("div");
		resultTitle.append(element("span", "entpay-result-label", "Verified result"), element("h3", "", delivery.summary));
		resultHeader.append(resultTitle, element("code", "entpay-result-schema", delivery.schema));
		const dataLabel = element("h4", "", "Merchant data");
		const pre = element("pre");
		try { pre.textContent = JSON.stringify(delivery.data, null, 2); } catch { pre.textContent = "Result data unavailable"; }
		result.append(resultHeader, dataLabel, pre);
		root.append(result);
	}
	if (session.delivery_artifact) {
		const artifact = session.delivery_artifact;
		const evidence = element("section", "entpay-artifact-evidence");
		evidence.append(element("h3", "", "Verified attachment"));
		const artifactFacts = element("dl", "entpay-artifact-facts");
		artifactFacts.append(
			detailRow("File", artifact.file_name || "--", true),
			detailRow("Media type", artifact.media_type || "--", true),
			detailRow("Size", `${Number(artifact.bytes || 0).toLocaleString(currentLocale())} bytes`),
			detailRow("SHA-256", artifact.sha256 || "--", true),
		);
		evidence.append(artifactFacts);
		root.append(evidence);
	}

  const actions = element("div", "entpay-actions");
  if (session.stage === "awaiting_approval") {
    const reject = actionButton("Reject without paying", "secondary-button", (event) => mutate("RejectEntPaySession", session, event.currentTarget));
    const approve = actionButton(`Pay ${formatEnt(session.invoice?.amount)}`, "entpay-pay-button", (event) => mutate("ApproveEntPaySession", session, event.currentTarget));
    reject.dataset.revision = String(session.revision);
    approve.dataset.revision = String(session.revision);
    const expired = new Date(session.invoice?.expires_at).getTime() <= Date.now();
    approve.disabled = expired || session.merchant_trust_state === "changed";
    actions.append(reject);
    if (session.merchant_trust_state === "changed") {
      const trust = actionButton("Trust this changed merchant", "secondary-button", (event) => mutate("ReestablishEntPayMerchantTrust", session, event.currentTarget));
      trust.dataset.revision = String(session.revision);
      actions.append(trust);
    }
    actions.append(approve);
  } else if (session.stage === "failed_retryable") {
    const retry = actionButton("Retry", "entpay-pay-button", (event) => mutate("RetryEntPaySession", session, event.currentTarget));
    retry.dataset.revision = String(session.revision);
    actions.append(retry);
  } else if (ACTIVE_STAGES.has(session.stage)) {
    actions.append(element("span", "entpay-progress-copy", "This request will continue automatically."));
  }
	if (session.stage === "complete" && session.artifact_path) {
		actions.prepend(actionButton("Show in folder", "secondary-button", () => invokeBackend("RevealEntPayArtifact", session.id).catch((error) => notify(error?.message || "Artifact unavailable", "error"))));
		actions.prepend(actionButton("Open file", "secondary-button", () => invokeBackend("OpenEntPayArtifact", session.id).catch((error) => notify(error?.message || "Artifact unavailable", "error"))));
	}
	if (canDeleteEntPaySession(session)) {
		const label = ["awaiting_approval", "failed_retryable"].includes(session.stage) ? "Delete request" : "Delete history";
		actions.append(actionButton(label, "secondary-button", (event) => deleteSession(session, event.currentTarget)));
	}
  root.append(actions);
}

async function loadSettings() {
	try {
		const settings = await invokeBackend("GetEntPaySettings");
		settingsRevision = Number(settings.revision);
		byID("entpay-maximum").value = formatEnt(settings.maximum_amount).replace(" ENT", "").replaceAll(",", "");
		byID("entpay-artifact-directory").value = settings.artifact_directory || "";
		byID("entpay-retention").value = String(settings.request_retention_days);
	} catch (error) { notify(error?.message || "Agent Pay settings are unavailable", "error"); }
}

async function selectSession(id) {
  selectedID = id;
  renderList();
  try {
    const detail = await invokeBackend("GetEntPaySession", id);
    if (selectedID === id) renderDetail(detail);
  } catch (error) {
    setStatus(error?.message || "Payment details are unavailable", "error");
  }
}

export async function refreshEntPay(force = false) {
  if (loading && !force) return;
  loading = true;
  try {
    const overview = await invokeBackend("GetEntPayOverview");
    sessions = Array.isArray(overview.sessions) ? overview.sessions : [];
    setStatus(overview.available ? "Requests are verified locally before payment" : overview.error || "Agent Pay unavailable", overview.available ? "ready" : "error");
    if (overview.launch_error) notify(overview.launch_error, "error");
    if (selectedID && !sessions.some((session) => session.id === selectedID)) selectedID = "";
    if (!selectedID && sessions.length) selectedID = sessions[0].id;
    renderList();
    if (selectedID) await selectSession(selectedID);
  } catch (error) {
    setStatus(error?.message || "Agent Pay unavailable", "error");
  } finally {
    loading = false;
  }
}

export function initializeEntPay({ invoke, showToast, activateView, refreshIcons: renderIcons = () => {} }) {
  invokeBackend = invoke;
  notify = showToast;
  activate = activateView;
  refreshIcons = renderIcons;
  byID("entpay-page-previous").addEventListener("click", () => void showPage(currentPage - 1));
  byID("entpay-page-next").addEventListener("click", () => void showPage(currentPage + 1));
  byID("entpay-link-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    const input = byID("entpay-link");
    try {
      await invokeBackend("OpenEntPayLink", input.value.trim());
      input.value = "";
      setStatus("Payment request queued", "ready");
    } catch (error) {
      notify(error?.message || "Invalid payment link", "error");
    }
  });
	byID("entpay-settings").addEventListener("toggle", (event) => { if (event.currentTarget.open) void loadSettings(); });
	byID("entpay-choose-folder").addEventListener("click", async () => {
		try {
			const directory = await invokeBackend("ChooseEntPayArtifactDirectory");
			if (directory) byID("entpay-artifact-directory").value = directory;
		} catch (error) { notify(error?.message || "Folder selection failed", "error"); }
	});
	byID("entpay-settings-form").addEventListener("submit", async (event) => {
		event.preventDefault();
		try {
			const settings = {
				revision: settingsRevision, require_every_approval: true,
				maximum_amount: parseEnt(byID("entpay-maximum").value),
				artifact_directory: byID("entpay-artifact-directory").value,
				request_retention_days: Number(byID("entpay-retention").value),
			};
			const updated = await invokeBackend("SaveEntPaySettings", settings, settingsRevision);
			settingsRevision = Number(updated.revision);
			notify("Agent Pay settings saved");
		} catch (error) { notify(error?.message || "Settings could not be saved", "error"); }
	});
	byID("entpay-register-links").addEventListener("click", async () => {
		try {
			const status = await invokeBackend("RegisterEntPayLinks");
			notify(status.message || "Payment links registered");
		} catch (error) { notify(error?.message || "Payment link registration failed", "error"); }
	});
  if (window.runtime?.EventsOnMultiple) {
    window.runtime.EventsOnMultiple("entcoin:entpay-session", (session) => {
      mergeSession(session);
      renderList();
      if (selectedID === session.id) void selectSession(session.id);
    }, -1);
    window.runtime.EventsOnMultiple("entcoin:entpay-incoming", (session) => {
      mergeSession(session);
      selectedID = session.id;
      currentPage = 0;
      byID("entpay-unread").hidden = false;
      activate("entpay");
      renderList();
      void selectSession(session.id);
    }, -1);
    window.runtime.EventsOnMultiple("entcoin:entpay-launch-error", (message) => {
      activate("entpay");
      setStatus(message || "The EntPay payment link was invalid and was not opened.", "error");
    }, -1);
  }
}

export function entpayViewActivated() {
  byID("entpay-unread").hidden = true;
  return refreshEntPay();
}
