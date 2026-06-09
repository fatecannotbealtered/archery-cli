#!/usr/bin/env node
"use strict";

// Thin forwarder: exec the prebuilt binary with this process's argv and exit code.
const { execFileSync } = require("child_process");
const path = require("path");

const ext = process.platform === "win32" ? ".exe" : "";
const bin = path.join(__dirname, "..", "bin", "archery-cli" + ext);

try {
  execFileSync(bin, process.argv.slice(2), { stdio: "inherit" });
} catch (e) {
  if (e.code === "ENOENT") {
    console.error(
      "archery-cli binary not found.\n" +
      "Reinstall it with:  npm rebuild @fatecannotbealtered-/archery-cli\n" +
      "or reinstall the package:  npm install -g @fatecannotbealtered-/archery-cli"
    );
  }
  process.exit(e.status || 1);
}
