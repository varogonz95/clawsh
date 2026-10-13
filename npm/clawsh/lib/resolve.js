// Finds the clawsh binary for this platform, in order:
//   1. CLAWSH_BINARY_PATH, if set;
//   2. a copy downloaded earlier into the user cache;
//   3. a fresh download of the raw binary attached to the matching GitHub
//      release (v<version>), checked against the SHA-256 that build.mjs wrote
//      into lib/checksums.json when this package was published.
// The checksums ship inside the npm package, so a release asset that changed
// after publishing is refused rather than run.
"use strict";

const crypto = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

const VERSION = require("../package.json").version;
const RELEASES = "https://github.com/varogonz95/clawsh/releases/download";

// Node platform/arch -> Go GOOS/GOARCH, as named by `make dist`.
const GOOS = { darwin: "darwin", linux: "linux", win32: "windows" };
const GOARCH = { arm64: "arm64", x64: "amd64" };

// The release asset for this machine, e.g. clawsh-linux-amd64 or clawsh-windows-arm64.exe.
function assetName(platform = process.platform, arch = process.arch) {
  const goos = GOOS[platform], goarch = GOARCH[arch];
  if (!goos || !goarch) {
    throw new Error(`no prebuilt binary for ${platform}-${arch}; build from source: https://github.com/varogonz95/clawsh`);
  }
  return `clawsh-${goos}-${goarch}${goos === "windows" ? ".exe" : ""}`;
}

function binName(platform = process.platform) {
  return platform === "win32" ? "clawsh.exe" : "clawsh";
}

function cacheDir() {
  if (process.env.CLAWSH_CACHE_DIR) return process.env.CLAWSH_CACHE_DIR;
  const home = os.homedir();
  if (process.platform === "win32") return path.join(process.env.LOCALAPPDATA || path.join(home, "AppData", "Local"), "clawsh", "cache");
  if (process.platform === "darwin") return path.join(home, "Library", "Caches", "clawsh");
  return path.join(process.env.XDG_CACHE_HOME || path.join(home, ".cache"), "clawsh");
}

// Where release assets are downloaded from; CLAWSH_DOWNLOAD_BASE points at a
// mirror laid out the same way (<base>/v<version>/<asset>).
function downloadURL(asset) {
  const base = (process.env.CLAWSH_DOWNLOAD_BASE || RELEASES).replace(/\/+$/, "");
  return `${base}/v${VERSION}/${asset}`;
}

function expectedSHA256(asset) {
  let sums;
  try {
    sums = require("./checksums.json");
  } catch {
    throw new Error("this clawsh package has no checksums (a source checkout, not a published release)");
  }
  if (!sums[asset]) throw new Error(`no checksum for ${asset} in this package`);
  return sums[asset];
}

function verifySHA256(buf, expected) {
  const actual = crypto.createHash("sha256").update(buf).digest("hex");
  if (actual !== expected) throw new Error("downloaded binary failed its SHA-256 check");
}

async function download(asset, dest) {
  const url = downloadURL(asset);
  const expected = expectedSHA256(asset);
  let res;
  try {
    res = await fetch(url); // follows GitHub's redirect to its download host
  } catch (err) {
    throw new Error(`could not download ${url}: ${err.cause?.message || err.message}`);
  }
  if (!res.ok) throw new Error(`could not download ${url}: HTTP ${res.status}`);
  const bin = Buffer.from(await res.arrayBuffer());
  verifySHA256(bin, expected);
  fs.mkdirSync(path.dirname(dest), { recursive: true });
  const tmp = `${dest}.${process.pid}.tmp`;
  fs.writeFileSync(tmp, bin, { mode: 0o755 });
  fs.renameSync(tmp, dest); // atomic, so a concurrent first run never sees half a file
}

async function resolveBinary() {
  if (process.env.CLAWSH_BINARY_PATH) return process.env.CLAWSH_BINARY_PATH;

  const asset = assetName();
  const cached = path.join(cacheDir(), VERSION, binName());
  if (fs.existsSync(cached)) return cached;

  process.stderr.write(`clawsh: downloading the ${asset} binary once from the v${VERSION} GitHub release...\n`);
  try {
    await download(asset, cached);
  } catch (err) {
    throw new Error(`${err.message}\nDownload ${asset} from https://github.com/varogonz95/clawsh/releases/tag/v${VERSION} yourself and set CLAWSH_BINARY_PATH to it.`);
  }
  return cached;
}

module.exports = { resolveBinary, assetName, binName, cacheDir, downloadURL, verifySHA256 };
