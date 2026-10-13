// Exercises the launcher's download from a local fake GitHub release, with a
// shell script standing in for the clawsh binary, and the release staging in
// build.mjs. Unix only.
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
const { assetName, verifySHA256 } = require("../clawsh/lib/resolve.js");
const VERSION = require("../clawsh/package.json").version;

const unix = process.platform !== "win32";
const sha256 = (b) => crypto.createHash("sha256").update(b).digest("hex");
// A fake binary that echoes its args and exits 3.
const fakeBin = Buffer.from('#!/bin/sh\necho "fake clawsh $*"\nexit 3\n');
let tmp, launcher, server, base;

before(async () => {
  if (!unix) return;
  tmp = fs.mkdtempSync(path.join(os.tmpdir(), "clawsh-npm-"));
  // A copy of the launcher package, as published: with lib/checksums.json.
  const pkg = path.join(tmp, "pkg");
  fs.cpSync(path.join(here, "..", "clawsh"), pkg, { recursive: true });
  fs.writeFileSync(path.join(pkg, "lib", "checksums.json"), JSON.stringify({ [assetName()]: sha256(fakeBin) }));
  launcher = path.join(pkg, "bin", "clawsh.js");

  // Serves <base>/v<version>/<asset> by redirecting elsewhere, like GitHub does.
  server = http.createServer((req, res) => {
    if (req.url === `/v${VERSION}/${assetName()}`) {
      res.statusCode = 302;
      res.setHeader("location", "/objects/blob");
      res.end();
    } else if (req.url === "/objects/blob") {
      res.end(server.body ?? fakeBin);
    } else {
      res.statusCode = 404;
      res.end();
    }
  });
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  base = `http://127.0.0.1:${server.address().port}`;
});

after(() => {
  server?.close();
  if (tmp) fs.rmSync(tmp, { recursive: true, force: true });
});

// Async on purpose: the fake release server runs in this process, so a sync
// spawn would block the server the launcher is downloading from.
function run(args, env, bin = launcher) {
  return new Promise((resolve) => {
    const child = spawn(process.execPath, [bin, ...args], {
      env: { ...process.env, CLAWSH_BINARY_PATH: "", ...env },
    });
    let stdout = "", stderr = "";
    child.stdout.on("data", (d) => (stdout += d));
    child.stderr.on("data", (d) => (stderr += d));
    child.on("close", (status) => resolve({ status, stdout, stderr }));
  });
}

const cachedBinaries = (dir) => fs.existsSync(dir) && fs.readdirSync(dir, { recursive: true }).some((f) => /clawsh(\.exe)?$/.test(f));

test("maps the platform to its release asset", () => {
  assert.equal(assetName("linux", "x64"), "clawsh-linux-amd64");
  assert.equal(assetName("darwin", "arm64"), "clawsh-darwin-arm64");
  assert.equal(assetName("win32", "arm64"), "clawsh-windows-arm64.exe");
  assert.throws(() => assetName("freebsd", "x64"), /no prebuilt binary/);
  assert.throws(() => assetName("linux", "ia32"), /no prebuilt binary/);
});

test("verifies SHA-256", () => {
  verifySHA256(fakeBin, sha256(fakeBin));
  assert.throws(() => verifySHA256(Buffer.from("tampered"), sha256(fakeBin)), /SHA-256 check/);
});

test("downloads on first run, following the redirect, then uses the cache", { skip: !unix }, async () => {
  const cache = path.join(tmp, "cache-ok");
  const first = await run(["ls", "-a"], { CLAWSH_DOWNLOAD_BASE: base, CLAWSH_CACHE_DIR: cache });
  assert.equal(first.status, 3, first.stderr); // exit code passes through
  assert.equal(first.stdout, "fake clawsh ls -a\n");
  assert.match(first.stderr, /downloading the .* binary once/);

  // Release unreachable now: the cached copy must be enough.
  const second = await run(["hosts"], { CLAWSH_DOWNLOAD_BASE: "http://127.0.0.1:1", CLAWSH_CACHE_DIR: cache });
  assert.equal(second.stdout, "fake clawsh hosts\n");
  assert.doesNotMatch(second.stderr, /downloading/);
});

test("refuses a binary that doesn't match the packaged checksum", { skip: !unix }, async () => {
  server.body = Buffer.from("#!/bin/sh\necho tampered\n");
  try {
    const cache = path.join(tmp, "cache-bad");
    const r = await run([], { CLAWSH_DOWNLOAD_BASE: base, CLAWSH_CACHE_DIR: cache });
    assert.equal(r.status, 1);
    assert.match(r.stderr, /SHA-256 check/);
    assert.match(r.stderr, /CLAWSH_BINARY_PATH/);
    assert.equal(cachedBinaries(cache), false);
  } finally {
    server.body = undefined;
  }
});

test("reports a missing release asset", { skip: !unix }, async () => {
  const cache = path.join(tmp, "cache-404");
  const r = await run([], { CLAWSH_DOWNLOAD_BASE: `${base}/nothing-here`, CLAWSH_CACHE_DIR: cache });
  assert.equal(r.status, 1);
  assert.match(r.stderr, /HTTP 404/);
  assert.match(r.stderr, /releases\/tag\/v/);
});

test("a source checkout without checksums refuses to download", { skip: !unix }, async () => {
  const cache = path.join(tmp, "cache-src");
  const source = path.join(here, "..", "clawsh", "bin", "clawsh.js");
  const r = await run([], { CLAWSH_DOWNLOAD_BASE: base, CLAWSH_CACHE_DIR: cache }, source);
  assert.equal(r.status, 1);
  assert.match(r.stderr, /no checksums/);
  assert.equal(cachedBinaries(cache), false);
});

test("CLAWSH_BINARY_PATH wins over everything", { skip: !unix }, async () => {
  const bin = path.join(tmp, "custom");
  fs.writeFileSync(bin, "#!/bin/sh\necho custom\n", { mode: 0o755 });
  const r = await run([], { CLAWSH_BINARY_PATH: bin, CLAWSH_DOWNLOAD_BASE: "http://127.0.0.1:1" });
  assert.equal(r.status, 0, r.stderr);
  assert.equal(r.stdout, "custom\n");
});

test("build.mjs stages the launcher with every binary's checksum", { skip: !unix }, () => {
  const dist = path.join(tmp, "dist");
  fs.mkdirSync(dist);
  const names = ["darwin-arm64", "darwin-amd64", "linux-arm64", "linux-amd64", "windows-arm64.exe", "windows-amd64.exe"].map((t) => `clawsh-${t}`);
  for (const n of names) fs.writeFileSync(path.join(dist, n), `binary ${n}`);
  const out = path.join(tmp, "npmout");
  execFileSync(process.execPath, [path.join(here, "..", "build.mjs"), "1.2.3", dist, out]);

  const staged = path.join(out, "clawsh");
  assert.deepEqual(fs.readdirSync(out), ["clawsh"]); // just the launcher
  const pkg = JSON.parse(fs.readFileSync(path.join(staged, "package.json"), "utf8"));
  assert.equal(pkg.version, "1.2.3");
  assert.equal(pkg.optionalDependencies, undefined);
  const sums = JSON.parse(fs.readFileSync(path.join(staged, "lib", "checksums.json"), "utf8"));
  assert.deepEqual(Object.keys(sums).sort(), [...names].sort());
  for (const n of names) assert.equal(sums[n], sha256(`binary ${n}`));
  for (const f of ["README.md", "LICENSE"]) assert.ok(fs.existsSync(path.join(staged, f)), f);
  // The asset this machine would download is among them.
  assert.ok(sums[assetName()]);
});
