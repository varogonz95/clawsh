// Exercises the launcher's download fallback against a local fake registry,
// with a shell script standing in for the clawsh binary. Unix only.
import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { execFileSync, spawn } from "node:child_process";
import crypto from "node:crypto";
import fs from "node:fs";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const here = path.dirname(fileURLToPath(import.meta.url));
const launcher = path.join(here, "..", "clawsh", "bin", "clawsh.js");
const { platformPackage, extractFromTarball, verifyIntegrity } = require("../clawsh/lib/resolve.js");
const VERSION = require("../clawsh/package.json").version;

const unix = process.platform !== "win32";
let tmp, tgz, server, registry, integrity;

before(async () => {
  if (!unix) return;
  tmp = fs.mkdtempSync(path.join(os.tmpdir(), "clawsh-npm-"));
  // A fake platform package whose "binary" echoes its args and exits 3.
  const pkg = path.join(tmp, "pkg");
  fs.mkdirSync(path.join(pkg, "bin"), { recursive: true });
  fs.writeFileSync(path.join(pkg, "bin", "clawsh"), '#!/bin/sh\necho "fake clawsh $*"\nexit 3\n', { mode: 0o755 });
  fs.writeFileSync(path.join(pkg, "package.json"), JSON.stringify({ name: platformPackage(), version: VERSION, files: ["bin"] }));
  const file = execFileSync("npm", ["pack", "--silent", "--pack-destination", tmp], { cwd: pkg, encoding: "utf8" }).trim();
  tgz = fs.readFileSync(path.join(tmp, file));
  integrity = "sha512-" + crypto.createHash("sha512").update(tgz).digest("base64");

  server = http.createServer((req, res) => {
    const base = `http://127.0.0.1:${server.address().port}`;
    if (req.url === `/${platformPackage()}/${VERSION}`) {
      res.setHeader("content-type", "application/json");
      res.end(JSON.stringify({ dist: { tarball: `${base}/pkg.tgz`, integrity: server.integrity ?? integrity } }));
    } else if (req.url === "/pkg.tgz") {
      res.end(tgz);
    } else {
      res.statusCode = 404;
      res.end();
    }
  });
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  registry = `http://127.0.0.1:${server.address().port}`;
});

after(() => {
  server?.close();
  if (tmp) fs.rmSync(tmp, { recursive: true, force: true });
});

// Async on purpose: the fake registry runs in this process, so a sync spawn
// would block the server the launcher is downloading from.
function run(args, env) {
  return new Promise((resolve) => {
    const child = spawn(process.execPath, [launcher, ...args], {
      env: { ...process.env, CLAWSH_BINARY_PATH: "", ...env },
    });
    let stdout = "", stderr = "";
    child.stdout.on("data", (d) => (stdout += d));
    child.stderr.on("data", (d) => (stderr += d));
    child.on("close", (status) => resolve({ status, stdout, stderr }));
  });
}

test("extracts the binary from a packed tarball", { skip: !unix }, () => {
  assert.match(extractFromTarball(tgz, "clawsh").toString(), /fake clawsh/);
  assert.throws(() => extractFromTarball(tgz, "clawsh.exe"), /not found/);
});

test("verifies sha512 integrity", { skip: !unix }, () => {
  verifyIntegrity(tgz, integrity);
  assert.throws(() => verifyIntegrity(Buffer.from("tampered"), integrity), /integrity check/);
  assert.throws(() => verifyIntegrity(tgz, "sha1-abc"), /unsupported/);
});

test("downloads on first run, then uses the cache", { skip: !unix }, async () => {
  const cache = path.join(tmp, "cache-ok");
  const first = await run(["ls", "-a"], { npm_config_registry: registry, CLAWSH_CACHE_DIR: cache });
  assert.equal(first.status, 3, first.stderr); // exit code passes through
  assert.equal(first.stdout, "fake clawsh ls -a\n");
  assert.match(first.stderr, /downloading it once/);

  // Registry unreachable now: the cached copy must be enough.
  const second = await run(["hosts"], { npm_config_registry: "http://127.0.0.1:1/", CLAWSH_CACHE_DIR: cache });
  assert.equal(second.stdout, "fake clawsh hosts\n");
  assert.doesNotMatch(second.stderr, /downloading/);
});

test("refuses a download that fails its integrity check", { skip: !unix }, async () => {
  server.integrity = "sha512-" + crypto.createHash("sha512").update("something else").digest("base64");
  try {
    const cache = path.join(tmp, "cache-bad");
    const r = await run([], { npm_config_registry: registry, CLAWSH_CACHE_DIR: cache });
    assert.equal(r.status, 1);
    assert.match(r.stderr, /integrity check/);
    assert.match(r.stderr, /CLAWSH_BINARY_PATH/);
    assert.equal(fs.existsSync(cache) && fs.readdirSync(cache, { recursive: true }).some((f) => f.endsWith("clawsh")), false);
  } finally {
    server.integrity = undefined;
  }
});

test("CLAWSH_BINARY_PATH wins over everything", { skip: !unix }, async () => {
  const bin = path.join(tmp, "custom");
  fs.writeFileSync(bin, "#!/bin/sh\necho custom\n", { mode: 0o755 });
  const r = await run([], { CLAWSH_BINARY_PATH: bin, npm_config_registry: "http://127.0.0.1:1/" });
  assert.equal(r.status, 0, r.stderr);
  assert.equal(r.stdout, "custom\n");
});

test("sets CLAWSH_LAUNCHER=npm for the child", { skip: !unix }, async () => {
  const bin = path.join(tmp, "env-bin");
  fs.writeFileSync(bin, '#!/bin/sh\necho "launcher=$CLAWSH_LAUNCHER"\n', { mode: 0o755 });
  const r = await run([], { CLAWSH_BINARY_PATH: bin });
  assert.equal(r.stdout, "launcher=npm\n", r.stderr);
});
