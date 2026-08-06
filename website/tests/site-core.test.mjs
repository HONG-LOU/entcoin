import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  FALLBACK_RELEASE_URL,
  formatNodeStatus,
  selectReleaseAssets,
  translations,
  validateNodeStatus,
} from "../site-core.mjs";

const validStatus = {
  protocol: "entropy-mainnet-v1",
  name: "Entcoin",
  symbol: "ENT",
  height: 52_834,
  tip_hash: "1234567890abcdef".repeat(4),
  chain_work: "9497032523776",
  listen_port: 47_821,
};

test("English and Chinese expose identical non-empty translation keys", () => {
  assert.deepEqual(
    Object.keys(translations.en).sort(),
    Object.keys(translations.zh).sort(),
  );
  assert.ok(Object.keys(translations.en).length >= 70);

  for (const language of ["en", "zh"]) {
    for (const [key, value] of Object.entries(translations[language])) {
      assert.equal(typeof value, "string", `${language}.${key}`);
      assert.notEqual(value.trim(), "", `${language}.${key}`);
    }
  }
});

test("node status accepts the expected protocol and bounded fields", () => {
  const status = validateNodeStatus(validStatus);

  assert.equal(status.height, 52_834);
  assert.equal(status.listen_port, 47_821);
  assert.throws(
    () => validateNodeStatus({ ...validStatus, protocol: "other" }),
    /protocol/i,
  );
  assert.throws(
    () => validateNodeStatus({ ...validStatus, height: -1 }),
    /height/i,
  );
  assert.throws(
    () => validateNodeStatus({ ...validStatus, height: 1.2 }),
    /height/i,
  );
  assert.throws(
    () => validateNodeStatus({ ...validStatus, tip_hash: "bad" }),
    /hash/i,
  );
  assert.throws(
    () => validateNodeStatus({ ...validStatus, chain_work: "1e10" }),
    /work/i,
  );
  assert.throws(() => validateNodeStatus(null), /status/i);
});

test("formatted status groups height and shortens the hash", () => {
  const english = formatNodeStatus(validStatus, "en");
  const chinese = formatNodeStatus(validStatus, "zh");

  assert.equal(english.height, "52,834");
  assert.equal(chinese.height, "52,834");
  assert.equal(english.tip, "1234567890ab...cdef");
  assert.equal(english.protocol, "entropy-mainnet-v1");
});

test("release selection accepts only known stable release assets", () => {
  const selected = selectReleaseAssets({
    tag_name: "v1.5.1",
    html_url: "https://github.com/HONG-LOU/entcoin/releases/tag/v1.5.1",
    draft: false,
    prerelease: false,
    assets: [
      asset("Entcoin.exe"),
      asset("entcoin-amd64-installer.exe"),
      asset("entcoin_1.5.1_amd64.deb"),
      asset("entcoin-cli-linux-amd64"),
      asset("entcoin-cli.exe"),
      asset("SHA256SUMS-linux.txt"),
      asset("SHA256SUMS.txt"),
      {
        name: "foreign.exe",
        browser_download_url: "https://evil.example/foreign.exe",
      },
    ],
  });

  assert.equal(selected.version, "v1.5.1");
  assert.match(selected.windowsPortable, /Entcoin\.exe$/);
  assert.match(selected.windowsInstaller, /entcoin-amd64-installer\.exe$/);
  assert.match(selected.ubuntu, /entcoin_1\.5\.1_amd64\.deb$/);
  assert.match(selected.linuxCli, /entcoin-cli-linux-amd64$/);
  assert.match(selected.windowsCli, /entcoin-cli\.exe$/);
  for (const address of [
    selected.windowsPortable,
    selected.windowsInstaller,
    selected.ubuntu,
    selected.linuxCli,
    selected.windowsCli,
  ]) {
    assert.match(
      address,
      /^https:\/\/template-chat\.xyz\/downloads\/v1\.5\.1\//,
    );
  }
  assert.equal(
    selected.windowsChecksums,
    "https://github.com/HONG-LOU/entcoin/releases/download/v1.5.1/SHA256SUMS.txt",
  );
  assert.equal(
    selected.linuxChecksums,
    "https://github.com/HONG-LOU/entcoin/releases/download/v1.5.1/SHA256SUMS-linux.txt",
  );
  assert.equal(
    selected.release,
    "https://github.com/HONG-LOU/entcoin/releases/tag/v1.5.1",
  );
});

test("release selection falls back for drafts, prereleases, and foreign URLs", () => {
  const draft = selectReleaseAssets({
    tag_name: "v1.5.1",
    draft: true,
    prerelease: false,
    assets: [],
  });
  const prerelease = selectReleaseAssets({
    tag_name: "v1.0.3-rc1",
    draft: false,
    prerelease: true,
    assets: [],
  });
  const foreign = selectReleaseAssets({
    tag_name: "v1.5.1",
    html_url: "https://evil.example/release",
    draft: false,
    prerelease: false,
    assets: [
      {
        name: "entcoin-cli-linux-amd64",
        browser_download_url: "https://evil.example/cli",
      },
    ],
  });

  assert.equal(draft.release, FALLBACK_RELEASE_URL);
  assert.equal(prerelease.release, FALLBACK_RELEASE_URL);
  assert.equal(foreign.release, FALLBACK_RELEASE_URL);
  assert.equal(foreign.linuxCli, FALLBACK_RELEASE_URL);
});

test("homepage translation keys are all defined", async () => {
  const html = await readFile(
    new URL("../index.html", import.meta.url),
    "utf8",
  );
  const keys = [...html.matchAll(/data-i18n(?:-html)?="([^"]+)"/g)].map(
    (match) => match[1],
  );

  assert.ok(keys.length >= 70);
  assert.equal((html.match(/<h1\b/g) ?? []).length, 1);
  for (const id of [
    "main",
    "network",
    "about",
    "entpay",
    "technology",
    "economics",
    "download",
    "open-source",
    "security",
  ]) {
    assert.match(html, new RegExp(`id="${id}"`), id);
  }
  assert.match(html, /class="skip-link"/);
  assert.match(html, /id="language-toggle"/);
  assert.match(html, /id="download-menu"/);
  assert.match(html, /<canvas[^>]+id="network-canvas"/);
  assert.equal(
    (
      html.match(
        /href="https:\/\/community\.entcoin\.xyz" data-i18n="nav\.community"/g,
      ) ?? []
    ).length,
    3,
  );
  for (const assetName of [
    "entcoin-amd64-installer.exe",
    "Entcoin.exe",
    "entcoin_1.5.1_amd64.deb",
    "entcoin-cli-linux-amd64",
    "entcoin-cli.exe",
  ]) {
    assert.ok(html.includes(`/downloads/v1.5.1/${assetName}`), assetName);
  }
  assert.ok(
    html.includes(
      "https://github.com/HONG-LOU/entcoin/releases/download/v1.5.1/SHA256SUMS.txt",
    ),
    "GitHub checksum source",
  );
  for (const key of keys) {
    assert.ok(translations.en[key], `missing English translation: ${key}`);
    assert.ok(translations.zh[key], `missing Chinese translation: ${key}`);
  }

  const publicCopy = `${html}\n${Object.values(translations.en).join("\n")}\n${Object.values(translations.zh).join("\n")}`;
  assert.doesNotMatch(
    publicCopy,
    /real-world value|no actual value|现实世界价值|没有实际价值|无实际价值/i,
  );
});

test("desktop update fallback names the current official release", async () => {
  const manifest = JSON.parse(
    await readFile(new URL("../update.json", import.meta.url), "utf8"),
  );
  assert.deepEqual(manifest, {
    version: "1.5.1",
    published_at: "2026-08-06T00:00:00Z",
    release_url: "https://github.com/HONG-LOU/entcoin/releases/tag/v1.5.1",
  });
});

test("website and desktop packages use the Entcoin E icon", async () => {
  const [sourceIcon, websiteIcon, windowsIcon, favicon] = await Promise.all([
    readFile(new URL("../../build/appicon.png", import.meta.url)),
    readFile(new URL("../assets/appicon.png", import.meta.url)),
    readFile(new URL("../../build/windows/icon.ico", import.meta.url)),
    readFile(new URL("../favicon.ico", import.meta.url)),
  ]);

  assert.equal(
    createHash("sha256").update(sourceIcon).digest("hex"),
    "20ed760a9bc8d6f65ebcc55570647aee499d372b34cb50fda344dabd05141df3",
  );
  assert.deepEqual(websiteIcon, sourceIcon);
  assert.equal(
    createHash("sha256").update(windowsIcon).digest("hex"),
    "10495951579e22b12714c0974658029d53ffa6fbdd8a0a3b90a69cf322a94103",
  );
  assert.deepEqual(favicon, windowsIcon);
});

test("visual system includes responsive and accessibility contracts", async () => {
  const css = await readFile(new URL("../styles.css", import.meta.url), "utf8");

  for (const contract of [
    ":focus-visible",
    "prefers-reduced-motion: reduce",
    "@media (max-width: 960px)",
    "@media (max-width: 700px)",
    ".hero-live",
    ".download-menu",
    ".node-scene",
    ".entpay-scene",
    ".payment-rail",
    ".protocol-scene",
    ".supply-scene",
    ".join-scene",
    ".continuum",
  ]) {
    assert.ok(css.includes(contract), `missing CSS contract: ${contract}`);
  }

  assert.doesNotMatch(css, /font-size\s*:[^;]*vw/i);
  assert.doesNotMatch(css, /border-radius\s*:\s*(?:[1-9]\d|\d{3,})px/i);
  assert.doesNotMatch(css, /(?:linear|radial|conic)-gradient\s*\(/i);
  assert.match(css, /\.scene\s*{[^}]*position:\s*relative/s);
  assert.match(css, /\.protocol-scene\s*{[^}]*background:\s*var\(--paper\)/s);
  assert.match(css, /\.motion-ready \[data-reveal\]/);
});

test("homepage uses the real EntPay approval and delivery assets", async () => {
  const [html, approval, delivery] = await Promise.all([
    readFile(new URL("../index.html", import.meta.url), "utf8"),
    readFile(new URL("../assets/entpay-agent-approval.png", import.meta.url)),
    readFile(new URL("../assets/entpay-generated-photo.jpg", import.meta.url)),
  ]);

  assert.match(html, /\/assets\/entpay-agent-approval\.png/);
  assert.match(html, /\/assets\/entpay-generated-photo\.jpg/);
  assert.equal(
    createHash("sha256").update(approval).digest("hex"),
    "7e65021a82ca2ef753c7072ae49121dacf952a944308b1d434d127be589e937e",
  );
  assert.equal(
    createHash("sha256").update(delivery).digest("hex"),
    "66150d33df60aaf75230ee590ee1de281c49439cef6412646185717f1877c8d9",
  );
});

test("browser module wires language, live data, menus, and motion preferences", async () => {
  const script = await readFile(new URL("../app.js", import.meta.url), "utf8");

  for (const contract of [
    'from "./site-core.mjs"',
    '"entcoin-language"',
    'searchParams.get("lang")',
    'navigator.language.toLowerCase().startsWith("zh")',
    'fetchWithTimeout("/api/network-status"',
    'matchMedia("(prefers-reduced-motion: reduce)")',
    '"IntersectionObserver" in window',
    'addEventListener("visibilitychange"',
    'addEventListener("keydown"',
    'event.key === "Escape"',
  ]) {
    assert.ok(
      script.includes(contract),
      `missing browser contract: ${contract}`,
    );
  }
  assert.doesNotMatch(script, /api\.github\.com/);
});

test("production nginx host isolates the website and read-only status proxy", async () => {
  const [nginx, html] = await Promise.all([
    readFile(new URL("../deploy/entcoin-website.nginx", import.meta.url), "utf8"),
    readFile(new URL("../index.html", import.meta.url), "utf8"),
  ]);

  assert.match(nginx, /root \/var\/www\/entcoin\/current;/);
  assert.match(nginx, /server_name www\.entcoin\.xyz;/);
  assert.match(nginx, /return 301 https:\/\/entcoin\.xyz\$request_uri;/);
  assert.match(nginx, /location = \/api\/network-status/);
  assert.match(nginx, /proxy_pass http:\/\/127\.0\.0\.1:47821\/v2\/status;/);
  assert.match(nginx, /proxy_set_header Host node\.entcoin\.xyz;/);
  assert.match(nginx, /limit_except GET/);
  assert.match(nginx, /location \^~ \/downloads\//);
  assert.match(nginx, /alias \/var\/www\/entcoin\/downloads\//);
  assert.match(nginx, /Accept-Ranges "bytes"/);
  assert.match(nginx, /max-age=31536000, immutable/);
  assert.match(nginx, /Content-Security-Policy/);
  const structuredData = html.match(
    /<script type="application\/ld\+json">([\s\S]*?)<\/script>/,
  )?.[1];
  assert.ok(structuredData, "homepage structured data must exist");
  const structuredDataHash = createHash("sha256")
    .update(structuredData)
    .digest("base64");
  assert.ok(
    nginx.includes(`'sha256-${structuredDataHash}'`),
    "website CSP must allow the current structured data",
  );
  assert.match(nginx, /location \^~ \/wallet\//);
  assert.match(nginx, /root \/var\/www\/entcoin-wallet\/current;/);
  assert.match(nginx, /script-src 'self' 'wasm-unsafe-eval'/);
  assert.match(nginx, /Permissions-Policy "camera=\(self\)/);
  assert.ok(nginx.includes(String.raw`location ~* \.mjs$ {`));
  assert.match(
    nginx,
    /location ~\* \\.mjs\$ \{[^}]*default_type application\/javascript;/s,
  );
  assert.doesNotMatch(nginx, /entcoin-node/);
});

function asset(name) {
  return {
    name,
    browser_download_url: `https://github.com/HONG-LOU/entcoin/releases/download/v1.5.1/${name}`,
  };
}
