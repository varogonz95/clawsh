#!/usr/bin/env node
// Runs the clawsh binary for this platform with the same arguments.
// lib/resolve.js finds it, downloading it from the GitHub release on first run.
"use strict";

const { spawnSync } = require("node:child_process");
const { resolveBinary } = require("../lib/resolve");

resolveBinary()
  .then((bin) => {
    const r = spawnSync(bin, process.argv.slice(2), { stdio: "inherit" });
    if (r.error) {
      console.error(`clawsh: could not run ${bin}: ${r.error.message}`);
      process.exit(1);
    }
    if (r.signal) process.kill(process.pid, r.signal);
    process.exit(r.status ?? 1);
  })
  .catch((err) => {
    console.error(`clawsh: ${err.message}`);
    process.exit(1);
  });
