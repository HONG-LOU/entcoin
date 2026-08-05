import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

async function text(relativePath) {
  return readFile(path.join(root, relativePath), "utf8");
}

async function json(relativePath) {
  return JSON.parse(await text(relativePath));
}

const packageDocument = await json("frontend/package.json");
const lockDocument = await json("frontend/package-lock.json");
const wailsDocument = await json("wails.json");
const updateDocument = await json("website/update.json");
const version = packageDocument.version;
const expectedTag = `v${version}`;

assert.match(
  version,
  /^\d+\.\d+\.\d+$/,
  "frontend version must be canonical semver",
);
assert.equal(lockDocument.version, version, "package-lock top-level version");
assert.equal(
  lockDocument.packages[""].version,
  version,
  "package-lock package version",
);
assert.equal(
  wailsDocument.info.productVersion,
  version,
  "Wails product version",
);
assert.equal(updateDocument.version, version, "website update version");
assert.equal(
  updateDocument.release_url,
  `https://github.com/HONG-LOU/entcoin/releases/tag/${expectedTag}`,
  "website update release URL",
);

const sourceChecks = [
  ["internal/updater/client.go", `CurrentVersion          = "${version}"`],
  ["frontend/src/main.js", `status.current_version || "${version}"`],
  ["frontend/index.html", `id="current-version">${expectedTag}<`],
  [
    "website/site-core.mjs",
    `https://github.com/HONG-LOU/entcoin/releases/tag/${expectedTag}`,
  ],
  ["website/index.html", `"softwareVersion": "${version}"`],
  ["README.md", `Entcoin ${expectedTag}`],
  ["README.zh-CN.md", `Entcoin ${expectedTag}`],
  ["CHANGELOG.md", `## [${version}]`],
  ["RELEASE_NOTES.md", `# Entcoin ${expectedTag}`],
];

for (const [relativePath, expected] of sourceChecks) {
  assert.ok(
    (await text(relativePath)).includes(expected),
    `${relativePath} is missing ${expected}`,
  );
}

const requestedVersion = process.argv[2];
if (requestedVersion) {
  assert.equal(
    requestedVersion.replace(/^v/, ""),
    version,
    "release tag version",
  );
}

console.log(`Release metadata is consistent for ${expectedTag}.`);
