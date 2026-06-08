#!/usr/bin/env node
"use strict";

const { execFileSync } = require("child_process");
const path = require("path");

const ext = process.platform === "win32" ? ".exe" : "";
const bin = path.join(__dirname, "..", "bin", "archery-cli" + ext);

try {
  execFileSync(bin, process.argv.slice(2), { stdio: "inherit" });
} catch (e) {
  if (e.code === "ENOENT") {
    console.error(`Binary not found. This package is unreleased; build from source with 'go build -o bin/archery-cli${ext} ./cmd/archery-cli'.`);
  }
  process.exit(e.status || 1);
}
