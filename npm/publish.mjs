#!/usr/bin/env node
// Builds and publishes the ciblame npm packages for one release.
//
// It takes the archives goreleaser attached to the GitHub release, checks
// them against checksums.txt, and wraps each binary in a per-platform
// package (ciblame-<os>-<cpu>). The main `ciblame` package depends on all of
// them as optional dependencies; npm installs only the one that matches.
//
// Usage:
//   node npm/publish.mjs --tag v0.1.0                 # download, build, publish
//   node npm/publish.mjs --tag v0.1.0 --dry-run       # build and `npm publish --dry-run`
//   node npm/publish.mjs --tag v0.1.0 --assets dist --pack-only   # local test from a folder of archives
//
// Publishing needs NODE_AUTH_TOKEN (an npm automation or granular token).
// Re-running is safe: versions that are already on npm are skipped.

import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const REPO = "amlaban65/ciblame";
const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, "..");

// Go GOOS/GOARCH -> Node process.platform/process.arch.
const TARGETS = [
  { goos: "darwin", goarch: "arm64", os: "darwin", cpu: "arm64", label: "macOS (Apple silicon)" },
  { goos: "darwin", goarch: "amd64", os: "darwin", cpu: "x64", label: "macOS (Intel)" },
  { goos: "linux", goarch: "arm64", os: "linux", cpu: "arm64", label: "Linux (arm64)" },
  { goos: "linux", goarch: "amd64", os: "linux", cpu: "x64", label: "Linux (x64)" },
  { goos: "windows", goarch: "arm64", os: "win32", cpu: "arm64", label: "Windows (arm64)" },
  { goos: "windows", goarch: "amd64", os: "win32", cpu: "x64", label: "Windows (x64)" },
];

function parseArgs(argv) {
  const opts = { dryRun: false, packOnly: false, out: path.join(root, "dist", "npm") };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    const val = () => {
      if (i + 1 >= argv.length) throw new Error(`${a} needs a value`);
      return argv[++i];
    };
    if (a === "--tag") opts.tag = val();
    else if (a === "--assets") opts.assets = path.resolve(val());
    else if (a === "--out") opts.out = path.resolve(val());
    else if (a === "--dry-run") opts.dryRun = true;
    else if (a === "--pack-only") opts.packOnly = true;
    else throw new Error(`unknown argument ${a}`);
  }
  if (!opts.tag || !/^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/.test(opts.tag)) {
    throw new Error("--tag must be a release tag like v0.1.0");
  }
  opts.version = opts.tag.slice(1);
  return opts;
}

async function fetchAsset(opts, name) {
  if (opts.assets) return fs.readFileSync(path.join(opts.assets, name));
  const url = `https://github.com/${REPO}/releases/download/${opts.tag}/${name}`;
  const res = await fetch(url);
  if (!res.ok) throw new Error(`downloading ${url}: HTTP ${res.status}`);
  return Buffer.from(await res.arrayBuffer());
}

function parseChecksums(text) {
  const sums = new Map();
  for (const line of text.split("\n")) {
    const m = line.trim().match(/^([0-9a-f]{64})\s+\*?(.+)$/);
    if (m) sums.set(m[2], m[1]);
  }
  return sums;
}

function extractBinary(archive, archiveName, exe, destDir) {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "ciblame-npm-"));
  try {
    const file = path.join(tmp, archiveName);
    fs.writeFileSync(file, archive);
    if (archiveName.endsWith(".zip")) {
      execFileSync("unzip", ["-q", "-o", file, exe, "-d", tmp]);
    } else {
      execFileSync("tar", ["-xzf", file, "-C", tmp, exe]);
    }
    fs.mkdirSync(destDir, { recursive: true });
    const dest = path.join(destDir, exe);
    fs.copyFileSync(path.join(tmp, exe), dest);
    fs.chmodSync(dest, 0o755);
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
}

function writeJSON(file, value) {
  fs.writeFileSync(file, JSON.stringify(value, null, 2) + "\n");
}

// npmjs.com can't resolve the README's repo-relative links, so point them at GitHub.
function absolutizeReadme(md, tag) {
  const blob = `https://github.com/${REPO}/blob/${tag}/`;
  const raw = `https://raw.githubusercontent.com/${REPO}/${tag}/`;
  return md
    .replace(/(src=")(?!https?:|#)([^"]+)"/g, (_, p, url) => `${p}${raw}${url}"`)
    .replace(/(\]\()(?!https?:|#|mailto:)([^)\s]+)\)/g, (_, p, url) => `${p}${blob}${url})`);
}

function platformPackage(t, version, dir) {
  const name = `ciblame-${t.os}-${t.cpu}`;
  writeJSON(path.join(dir, "package.json"), {
    name,
    version,
    description: `${t.label} binary for ciblame. Install the ciblame package instead of this one.`,
    homepage: `https://github.com/${REPO}#readme`,
    repository: { type: "git", url: `git+https://github.com/${REPO}.git` },
    license: "MIT",
    author: "Ahmed Laban",
    os: [t.os],
    cpu: [t.cpu],
    files: ["bin/", "LICENSE"],
    preferUnplugged: true,
  });
  fs.writeFileSync(
    path.join(dir, "README.md"),
    `# ${name}\n\nThe ${t.label} binary for [ciblame](https://github.com/${REPO}). ` +
      "You don't need to install this directly: `npm install -g ciblame` picks the right one for your machine.\n"
  );
  fs.copyFileSync(path.join(root, "LICENSE"), path.join(dir, "LICENSE"));
  return name;
}

function mainPackage(version, platformNames, dir) {
  const src = path.join(here, "ciblame");
  fs.mkdirSync(path.join(dir, "bin"), { recursive: true });
  fs.copyFileSync(path.join(src, "bin", "ciblame.js"), path.join(dir, "bin", "ciblame.js"));
  fs.chmodSync(path.join(dir, "bin", "ciblame.js"), 0o755);
  const pkg = JSON.parse(fs.readFileSync(path.join(src, "package.json"), "utf8"));
  pkg.version = version;
  pkg.optionalDependencies = Object.fromEntries(platformNames.map((n) => [n, version]));
  writeJSON(path.join(dir, "package.json"), pkg);
  const readme = fs.readFileSync(path.join(root, "README.md"), "utf8");
  fs.writeFileSync(path.join(dir, "README.md"), absolutizeReadme(readme, `v${version}`));
  fs.copyFileSync(path.join(root, "LICENSE"), path.join(dir, "LICENSE"));
  return pkg.name;
}

function alreadyPublished(name, version) {
  try {
    const out = execFileSync("npm", ["view", `${name}@${version}`, "version"], {
      stdio: ["ignore", "pipe", "ignore"],
    });
    return out.toString().trim() === version;
  } catch {
    return false; // npm view exits non-zero for unknown packages
  }
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// Returns true when it actually published (false when skipped), so the caller
// knows whether to pause before the next package.
function publish(dir, name, opts) {
  if (opts.packOnly) {
    const out = execFileSync("npm", ["pack", dir, "--pack-destination", opts.out], { encoding: "utf8" });
    console.log(`packed ${out.trim().split("\n").pop()}`);
    return false;
  }
  if (!opts.dryRun && alreadyPublished(name, opts.version)) {
    console.log(`skip ${name}@${opts.version} (already on npm)`);
    return false;
  }
  const args = ["publish", dir, "--access", "public"];
  if (opts.dryRun) args.push("--dry-run");
  if (process.env.GITHUB_ACTIONS === "true" && !opts.dryRun) args.push("--provenance");
  try {
    const out = execFileSync("npm", args, { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
    process.stdout.write(out);
  } catch (err) {
    const output = `${err.stdout || ""}${err.stderr || ""}`;
    process.stdout.write(output);
    // A version still being processed by npm doesn't show in `npm view` yet,
    // so a re-run can hit it again; that's a skip, not a failure.
    if (/previously published|cannot publish over|EPUBLISHCONFLICT/i.test(output)) {
      console.log(`skip ${name}@${opts.version} (already on npm)`);
      return false;
    }
    if (/spam detection/i.test(output)) {
      throw new Error(
        `npm's spam filter blocked ${name}. This happens to new accounts publishing several ` +
          "packages at once. Re-run this workflow in a few hours (published packages are skipped), " +
          "or ask npm support to review the account."
      );
    }
    throw new Error(`npm publish failed for ${name}`);
  }
  console.log(`${opts.dryRun ? "checked" : "published"} ${name}@${opts.version}`);
  return !opts.dryRun;
}

async function main() {
  const opts = parseArgs(process.argv.slice(2));
  if (!opts.dryRun && !opts.packOnly && !process.env.NODE_AUTH_TOKEN) {
    throw new Error("NODE_AUTH_TOKEN is not set. Add an npm token as the NPM_TOKEN repository secret.");
  }
  fs.rmSync(opts.out, { recursive: true, force: true });
  fs.mkdirSync(opts.out, { recursive: true });

  const sums = parseChecksums((await fetchAsset(opts, "checksums.txt")).toString("utf8"));
  const names = [];
  for (const t of TARGETS) {
    const ext = t.goos === "windows" ? "zip" : "tar.gz";
    const archiveName = `ciblame_${opts.version}_${t.goos}_${t.goarch}.${ext}`;
    const want = sums.get(archiveName);
    if (!want) throw new Error(`${archiveName} is not listed in checksums.txt`);
    const archive = await fetchAsset(opts, archiveName);
    const got = createHash("sha256").update(archive).digest("hex");
    if (got !== want) throw new Error(`checksum mismatch for ${archiveName}`);

    const dir = path.join(opts.out, `ciblame-${t.os}-${t.cpu}`);
    const exe = t.goos === "windows" ? "ciblame.exe" : "ciblame";
    extractBinary(archive, archiveName, exe, path.join(dir, "bin"));
    names.push(platformPackage(t, opts.version, dir));
  }
  const mainDir = path.join(opts.out, "ciblame");
  mainPackage(opts.version, names, mainDir);

  // Platform packages first, so the main package never points at missing
  // versions. Pause between publishes: npm's spam filter flags new accounts
  // that create several similarly named packages within seconds.
  const pause = Number(process.env.PUBLISH_DELAY_SECONDS ?? 45) * 1000;
  for (const n of names) {
    if (publish(path.join(opts.out, n), n, opts) && pause > 0) await sleep(pause);
  }
  publish(mainDir, "ciblame", opts);
}

main().catch((err) => {
  console.error(`npm publish: ${err.message}`);
  process.exit(1);
});
