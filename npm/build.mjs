// Stages the npm package for a release from the binaries in dist/:
//   node npm/build.mjs <version> [distDir] [outDir]
// writes outDir/clawsh/ (the launcher) stamped with <version>, plus
// lib/checksums.json: the SHA-256 of every dist/ binary. The release workflow
// attaches those same binaries to the GitHub release, and the launcher
// downloads its platform's one on first run and checks it against this file.
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const [version, distDir = "dist", outDir = "out/npm"] = process.argv.slice(2);
if (!/^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/.test(version || "")) {
  console.error("usage: node npm/build.mjs <version> [distDir] [outDir]   (version like 0.1.0, no leading v)");
  process.exit(2);
}

// Every platform the launcher supports (Makefile PLATFORMS), named as in dist/.
const assets = [
  "clawsh-darwin-arm64",
  "clawsh-darwin-amd64",
  "clawsh-linux-arm64",
  "clawsh-linux-amd64",
  "clawsh-windows-arm64.exe",
  "clawsh-windows-amd64.exe",
];

const checksums = {};
for (const asset of assets) {
  const src = path.join(distDir, asset);
  if (!fs.existsSync(src)) throw new Error(`missing ${src}; run make dist first`);
  checksums[asset] = crypto.createHash("sha256").update(fs.readFileSync(src)).digest("hex");
}

fs.rmSync(outDir, { recursive: true, force: true });
const dir = path.join(outDir, "clawsh");
fs.cpSync(path.join(here, "clawsh"), dir, { recursive: true });
const pkg = JSON.parse(fs.readFileSync(path.join(dir, "package.json"), "utf8"));
pkg.version = version;
fs.writeFileSync(path.join(dir, "package.json"), JSON.stringify(pkg, null, 2) + "\n");
fs.writeFileSync(path.join(dir, "lib", "checksums.json"), JSON.stringify(checksums, null, 2) + "\n");
fs.copyFileSync(path.join(here, "..", "README.md"), path.join(dir, "README.md"));
fs.copyFileSync(path.join(here, "..", "LICENSE"), path.join(dir, "LICENSE"));

console.log(`staged clawsh ${version} in ${dir} (checksums for ${assets.length} binaries)`);
