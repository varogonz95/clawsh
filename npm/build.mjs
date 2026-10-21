// Stages the npm packages for a release from the binaries in dist/:
//   node npm/build.mjs <version> [distDir] [outDir]
// writes outDir/clawsh-<os>-<cpu>/ (one binary each, limited to its os/cpu)
// and outDir/clawsh/ (the launcher), all stamped with <version>.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const [version, distDir = "dist", outDir = "out/npm"] = process.argv.slice(2);
if (!/^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/.test(version || "")) {
  console.error("usage: node npm/build.mjs <version> [distDir] [outDir]   (version like 0.1.0, no leading v)");
  process.exit(2);
}

// Go GOOS/GOARCH (Makefile PLATFORMS) -> npm os/cpu.
const targets = [
  ["darwin", "arm64", "darwin", "arm64"],
  ["darwin", "amd64", "darwin", "x64"],
  ["linux", "arm64", "linux", "arm64"],
  ["linux", "amd64", "linux", "x64"],
  ["windows", "arm64", "win32", "arm64"],
  ["windows", "amd64", "win32", "x64"],
];

const main = JSON.parse(fs.readFileSync(path.join(here, "clawsh", "package.json"), "utf8"));
const license = path.join(here, "..", "LICENSE");
const write = (file, data) => {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, data);
};
fs.rmSync(outDir, { recursive: true, force: true });

for (const [goos, goarch, os, cpu] of targets) {
  const name = `clawsh-${os}-${cpu}`;
  const exe = goos === "windows" ? ".exe" : "";
  const src = path.join(distDir, `clawsh-${goos}-${goarch}${exe}`);
  if (!fs.existsSync(src)) throw new Error(`missing ${src}; run make dist first`);
  const dir = path.join(outDir, name);
  fs.mkdirSync(path.join(dir, "bin"), { recursive: true });
  fs.copyFileSync(src, path.join(dir, "bin", `clawsh${exe}`));
  fs.chmodSync(path.join(dir, "bin", `clawsh${exe}`), 0o755);
  write(path.join(dir, "package.json"), JSON.stringify({
    name,
    version,
    description: `The clawsh binary for ${os} ${cpu}. Install "clawsh-cli" instead; it picks this up.`,
    license: main.license,
    homepage: main.homepage,
    repository: main.repository,
    os: [os],
    cpu: [cpu],
    files: ["bin"],
    preferUnplugged: true,
  }, null, 2) + "\n");
  write(path.join(dir, "README.md"), [
    `# ${name}`,
    "",
    `The prebuilt \`clawsh\` binary for ${os} ${cpu}. Don't install this directly: \`npm i -g clawsh-cli\` installs it for you.`,
    "",
    "clawsh starts Claude Code sessions on any machine in your network and lets you attach to them from any terminal or browser.",
    "",
    `- Source and docs: ${main.repository.url.replace(/^git\+/, "").replace(/\.git$/, "")}`,
    `- The same binary, with checksums: ${main.repository.url.replace(/^git\+/, "").replace(/\.git$/, "")}/releases/tag/v${version}`,
    "- License: Apache-2.0 with the Commons Clause (free to use, modify and share; not to sell). See LICENSE.",
    "",
  ].join("\n"));
  fs.copyFileSync(license, path.join(dir, "LICENSE"));
}

const mainDir = path.join(outDir, "clawsh");
fs.cpSync(path.join(here, "clawsh"), mainDir, { recursive: true });
main.version = version;
for (const dep of Object.keys(main.optionalDependencies)) main.optionalDependencies[dep] = version;
write(path.join(mainDir, "package.json"), JSON.stringify(main, null, 2) + "\n");
fs.copyFileSync(path.join(here, "..", "README.md"), path.join(mainDir, "README.md"));
fs.copyFileSync(license, path.join(mainDir, "LICENSE"));

console.log(`staged ${main.name} ${version}: ${targets.length} platform packages + launcher in ${outDir}`);
