import {
  formatNodeStatus,
  translations,
  validateNodeStatus,
} from "./site-core.mjs";

const LANGUAGE_KEY = "entcoin-language";
const languageToggle = document.querySelector("#language-toggle");
const menuToggle = document.querySelector("#menu-toggle");
const mobileNav = document.querySelector("#mobile-nav");
const downloadMenu = document.querySelector("#download-menu");
const headerDownload = document.querySelector("#header-download");
const heroDownload = document.querySelector("#hero-download");
const downloadTriggers = [
  headerDownload,
  heroDownload,
  ...document.querySelectorAll("[data-menu-trigger]"),
].filter(Boolean);
const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");

let language = initialLanguage();
let latestStatus = null;

setLanguage(language, { updateUrl: false });
bindNavigation();
bindDownloadMenu();
bindPageMotion();
bindReveals();
void loadNodeStatus();
startNetworkCanvas();

function bindReveals() {
  const elements = [...document.querySelectorAll("[data-reveal]")];
  if (elements.length === 0) return;
  if (reducedMotion.matches || !("IntersectionObserver" in window)) {
    elements.forEach((element) => element.classList.add("revealed"));
    return;
  }

  document.documentElement.classList.add("motion-ready");
  const observer = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        entry.target.classList.add("revealed");
        observer.unobserve(entry.target);
      }
    },
    { rootMargin: "0px 0px -12%", threshold: 0.08 },
  );
  elements.forEach((element) => observer.observe(element));
}

function bindPageMotion() {
  const progress = document.querySelector("[data-page-progress]");
  const header = document.querySelector("[data-header]");
  if (!progress && !header) return;

  let scheduled = false;
  const render = () => {
    const distance = Math.max(
      1,
      document.documentElement.scrollHeight - window.innerHeight,
    );
    const ratio = Math.min(1, Math.max(0, window.scrollY / distance));
    if (progress) progress.style.transform = `scaleX(${ratio})`;
    header?.classList.toggle("scrolled", window.scrollY > 24);
    scheduled = false;
  };
  window.addEventListener(
    "scroll",
    () => {
      if (scheduled) return;
      scheduled = true;
      window.requestAnimationFrame(render);
    },
    { passive: true },
  );
  render();
}

function initialLanguage() {
  const requested = new URL(window.location.href).searchParams.get("lang");
  if (requested === "zh" || requested === "en") return requested;
  try {
    const saved = window.localStorage.getItem(LANGUAGE_KEY);
    if (saved === "zh" || saved === "en") return saved;
  } catch {
    // Fall through to the browser language when storage is blocked.
  }
  return navigator.language.toLowerCase().startsWith("zh") ? "zh" : "en";
}

function setLanguage(nextLanguage, { updateUrl = true } = {}) {
  language = nextLanguage === "zh" ? "zh" : "en";
  const copy = translations[language];
  document.documentElement.lang = language === "zh" ? "zh-CN" : "en";
  document.documentElement.dataset.language = language;

  for (const element of document.querySelectorAll("[data-i18n]")) {
    const value = copy[element.dataset.i18n];
    if (value) element.textContent = value;
  }
  for (const element of document.querySelectorAll("[data-i18n-aria-label]")) {
    const value = copy[element.dataset.i18nAriaLabel];
    if (value) element.setAttribute("aria-label", value);
  }

  document.title = copy["meta.title"];
  updateMeta("meta[name='description']", copy["meta.description"]);
  updateMeta("meta[property='og:title']", copy["meta.title"]);
  updateMeta("meta[property='og:description']", copy["meta.description"]);
  updateMeta("meta[name='twitter:title']", copy["meta.title"]);
  updateMeta("meta[name='twitter:description']", copy["meta.description"]);

  try {
    window.localStorage.setItem(LANGUAGE_KEY, language);
  } catch {
    // The page remains fully usable when storage is blocked.
  }

  if (updateUrl) {
    const url = new URL(window.location.href);
    if (language === "zh") url.searchParams.set("lang", "zh");
    else url.searchParams.delete("lang");
    window.history.replaceState(
      null,
      "",
      `${url.pathname}${url.search}${url.hash}`,
    );
  }

  if (latestStatus) renderNodeStatus(latestStatus);
}

function updateMeta(selector, value) {
  const element = document.querySelector(selector);
  if (element) element.setAttribute("content", value);
}

function bindNavigation() {
  languageToggle?.addEventListener("click", () =>
    setLanguage(language === "en" ? "zh" : "en"),
  );

  menuToggle?.addEventListener("click", () => {
    const open = menuToggle.getAttribute("aria-expanded") !== "true";
    setMobileNavigation(open);
  });

  mobileNav?.addEventListener("click", (event) => {
    if (event.target.closest("a")) setMobileNavigation(false);
  });

  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
      closeDownloadMenu();
      setMobileNavigation(false);
    }
  });
}

function setMobileNavigation(open) {
  if (!menuToggle || !mobileNav) return;
  menuToggle.setAttribute("aria-expanded", String(open));
  menuToggle.setAttribute(
    "aria-label",
    translations[language][open ? "nav.close" : "nav.open"],
  );
  mobileNav.hidden = !open;
  document.body.classList.toggle("menu-open", open && window.innerWidth <= 820);
}

function bindDownloadMenu() {
  for (const trigger of downloadTriggers) {
    trigger.addEventListener("click", (event) => {
      event.preventDefault();
      const alreadyOpen =
        !downloadMenu.hidden &&
        trigger.getAttribute("aria-expanded") === "true";
      if (alreadyOpen) closeDownloadMenu();
      else openDownloadMenu(trigger);
    });
  }

  document.addEventListener("click", (event) => {
    if (downloadMenu.hidden) return;
    if (
      downloadMenu.contains(event.target) ||
      downloadTriggers.some((trigger) => trigger.contains(event.target))
    )
      return;
    closeDownloadMenu();
  });

  downloadMenu?.addEventListener("click", (event) => {
    if (event.target.closest("a")) closeDownloadMenu();
  });
}

function openDownloadMenu(trigger) {
  closeDownloadMenu();
  const modalContext = trigger !== headerDownload;
  downloadMenu.hidden = false;
  downloadMenu.classList.toggle("download-menu-modal", modalContext);
  downloadMenu.dataset.context = modalContext ? "page" : "header";
  trigger.setAttribute("aria-expanded", "true");

  const requestedGroup = trigger.dataset.menuTrigger;
  const selector =
    requestedGroup === "cli"
      ? "[data-download='linuxCli']"
      : requestedGroup === "windows"
        ? "[data-download='windowsInstaller']"
        : "[role='menuitem']";
  window.requestAnimationFrame(() =>
    downloadMenu.querySelector(selector)?.focus(),
  );
}

function closeDownloadMenu() {
  if (!downloadMenu) return;
  downloadMenu.hidden = true;
  downloadMenu.classList.remove("download-menu-modal");
  downloadMenu.removeAttribute("data-context");
  for (const trigger of downloadTriggers)
    trigger.setAttribute("aria-expanded", "false");
}

async function loadNodeStatus() {
  setLiveState("loading");
  try {
    const response = await fetchWithTimeout("/api/network-status", {
      headers: { Accept: "application/json" },
    });
    if (!response.ok)
      throw new Error(`Node status returned ${response.status}`);
    latestStatus = validateNodeStatus(await response.json());
    renderNodeStatus(latestStatus);
    setLiveState("connected");
  } catch {
    setLiveState("unavailable");
  }
}

function renderNodeStatus(status) {
  const formatted = formatNodeStatus(status, language);
  setText("[data-live-height]", formatted.height);
  setText("[data-product-height]", formatted.height);
  setText("[data-terminal-height]", formatted.height);
  setText("[data-terminal-tip]", formatted.tip);
}

function setLiveState(state) {
  const key =
    state === "connected"
      ? "status.connected"
      : state === "loading"
        ? "status.loading"
        : "status.unavailable";
  for (const element of document.querySelectorAll("[data-live-state]")) {
    element.dataset.i18n = key;
    element.textContent = translations[language][key];
  }
  for (const dot of document.querySelectorAll("[data-status-dot]")) {
    dot.classList.toggle("online", state === "connected");
    dot.classList.toggle("offline", state === "unavailable");
  }
}

async function fetchWithTimeout(url, options = {}, timeoutMs = 6_000) {
  const controller = new AbortController();
  const timer = window.setTimeout(() => controller.abort(), timeoutMs);
  try {
    return await window.fetch(url, { ...options, signal: controller.signal });
  } finally {
    window.clearTimeout(timer);
  }
}

function setText(selector, value) {
  for (const element of document.querySelectorAll(selector))
    element.textContent = value;
}

function startNetworkCanvas() {
  const canvas = document.querySelector("#network-canvas");
  if (!(canvas instanceof HTMLCanvasElement)) return;
  const context = canvas.getContext("2d", { alpha: true });
  if (!context) return;

  const nodes = createNodes(24);
  const pointer = { x: 0, y: 0 };
  let width = 0;
  let height = 0;
  let frame = 0;
  let running = false;
  let startTime = performance.now();

  const resize = () => {
    const rect = canvas.getBoundingClientRect();
    const ratio = Math.min(window.devicePixelRatio || 1, 2);
    width = Math.max(1, rect.width);
    height = Math.max(1, rect.height);
    canvas.width = Math.round(width * ratio);
    canvas.height = Math.round(height * ratio);
    context.setTransform(ratio, 0, 0, ratio, 0, 0);
    draw(performance.now());
  };

  const draw = (now) => {
    context.clearRect(0, 0, width, height);
    drawGrid(context, width, height);
    const elapsed = reducedMotion.matches ? 0 : (now - startTime) / 1_000;
    const positions = nodes.map((node) => ({
      ...node,
      px:
        node.x * width +
        Math.sin(elapsed * node.speed + node.phase) * node.drift +
        pointer.x * node.depth,
      py:
        node.y * height +
        Math.cos(elapsed * node.speed + node.phase) * node.drift +
        pointer.y * node.depth,
    }));
    drawLinks(context, positions);
    drawNodes(context, positions, elapsed);
  };

  const loop = (now) => {
    if (!running) return;
    draw(now);
    frame = window.requestAnimationFrame(loop);
  };

  const updateAnimation = () => {
    const shouldRun = !reducedMotion.matches && !document.hidden;
    if (shouldRun && !running) {
      running = true;
      startTime = performance.now();
      frame = window.requestAnimationFrame(loop);
    } else if (!shouldRun && running) {
      running = false;
      window.cancelAnimationFrame(frame);
      draw(performance.now());
    } else if (!shouldRun) {
      draw(performance.now());
    }
  };

  canvas.addEventListener("pointermove", (event) => {
    if (reducedMotion.matches) return;
    const rect = canvas.getBoundingClientRect();
    pointer.x = (event.clientX - rect.left - rect.width / 2) / rect.width;
    pointer.y = (event.clientY - rect.top - rect.height / 2) / rect.height;
  });
  canvas.addEventListener("pointerleave", () => {
    pointer.x = 0;
    pointer.y = 0;
  });
  document.addEventListener("visibilitychange", updateAnimation);
  reducedMotion.addEventListener("change", updateAnimation);
  new ResizeObserver(resize).observe(canvas);
  resize();
  updateAnimation();
}

function createNodes(count) {
  return Array.from({ length: count }, (_, index) => ({
    x: 0.12 + pseudoRandom(index * 3 + 1) * 0.76,
    y: 0.11 + pseudoRandom(index * 3 + 2) * 0.78,
    radius: index === 0 ? 5 : 2 + pseudoRandom(index * 3 + 3) * 2,
    phase: pseudoRandom(index + 31) * Math.PI * 2,
    speed: 0.14 + pseudoRandom(index + 61) * 0.2,
    drift: 2 + pseudoRandom(index + 91) * 5,
    depth: 3 + pseudoRandom(index + 121) * 8,
    primary: index === 0,
  }));
}

function pseudoRandom(seed) {
  const value = Math.sin(seed * 999.91) * 43_758.5453;
  return value - Math.floor(value);
}

function drawGrid(context, width, height) {
  context.strokeStyle = "rgba(216, 225, 221, 0.055)";
  context.lineWidth = 1;
  const spacing = 64;
  context.beginPath();
  for (let x = 0.5; x <= width; x += spacing) {
    context.moveTo(x, height * 0.08);
    context.lineTo(x, height * 0.92);
  }
  for (let y = 0.5; y <= height; y += spacing) {
    context.moveTo(width * 0.04, y);
    context.lineTo(width * 0.96, y);
  }
  context.stroke();

  context.strokeStyle = "rgba(88, 214, 189, 0.1)";
  context.beginPath();
  context.arc(width * 0.73, height * 0.48, Math.min(width, height) * 0.3, 0, Math.PI * 2);
  context.stroke();
}

function drawLinks(context, nodes) {
  context.lineWidth = 1;
  for (let left = 0; left < nodes.length; left += 1) {
    for (let right = left + 1; right < nodes.length; right += 1) {
      const dx = nodes[left].px - nodes[right].px;
      const dy = nodes[left].py - nodes[right].py;
      const distance = Math.hypot(dx, dy);
      if (distance > 160) continue;
      context.strokeStyle = `rgba(88, 214, 189, ${0.2 * (1 - distance / 160)})`;
      context.beginPath();
      context.moveTo(nodes[left].px, nodes[left].py);
      context.lineTo(nodes[right].px, nodes[right].py);
      context.stroke();
    }
  }
}

function drawNodes(context, nodes, elapsed) {
  nodes.forEach((node, index) => {
    context.beginPath();
    context.arc(node.px, node.py, node.radius, 0, Math.PI * 2);
    context.fillStyle = node.primary
      ? "#ff6b5f"
      : index % 3 === 0
        ? "#58d6bd"
        : "#aeb9b5";
    context.fill();
    if (node.primary) {
      context.beginPath();
      context.arc(
        node.px,
        node.py,
        16 + Math.sin(elapsed * 2) * 4,
        0,
        Math.PI * 2,
      );
      context.strokeStyle = "rgba(255, 107, 95, 0.4)";
      context.stroke();
    }
  });
}
