#!/usr/bin/env node
// Runs the prebuilt ciblame binary for this platform. The binary ships in a
// per-platform package (e.g. ciblame-darwin-arm64) that npm installs as an
// optional dependency, so only the one matching this machine is downloaded.
"use strict";

const { spawnSync } = require("child_process");

const PACKAGES = {
  "darwin-arm64": "ciblame-darwin-arm64",
  "darwin-x64": "ciblame-darwin-x64",
  "linux-arm64": "ciblame-linux-arm64",
  "linux-x64": "ciblame-linux-x64",
  // Scoped: npm's spam filter rejects the unscoped ciblame-win32-* names.
  "win32-arm64": "@amlaban/ciblame-win32-arm64",
  "win32-x64": "@amlaban/ciblame-win32-x64",
};

function fail(msg) {
  process.stderr.write(`ciblame: ${msg}\n`);
  process.exit(1);
}

function binaryPath() {
  if (process.env.CIBLAME_BINARY) return process.env.CIBLAME_BINARY;
  const key = `${process.platform}-${process.arch}`;
  const pkg = PACKAGES[key];
  if (!pkg) {
    fail(
      `there is no prebuilt binary for ${key}.\n` +
        "Install it with Go instead: go install github.com/amlaban65/ciblame/cmd/ciblame@latest"
    );
  }
  const exe = process.platform === "win32" ? "ciblame.exe" : "ciblame";
  try {
    return require.resolve(`${pkg}/bin/${exe}`);
  } catch {
    fail(
      `the ${pkg} package is missing. It's installed as an optional dependency, so it is ` +
        "skipped by --no-optional / --omit=optional. Reinstall ciblame without that flag."
    );
  }
}

const result = spawnSync(binaryPath(), process.argv.slice(2), { stdio: "inherit" });
if (result.error) fail(result.error.message);
if (result.signal) {
  process.kill(process.pid, result.signal);
} else {
  process.exit(result.status === null ? 1 : result.status);
}
