# Installation

clawsh is a single static binary for Windows, Linux and macOS (amd64 and arm64).

## Install script

Linux and macOS:

```sh
curl -fsSL https://varogonz95.github.io/clawsh/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://varogonz95.github.io/clawsh/install.ps1 | iex
```

The script finds the latest [release](https://github.com/varogonz95/clawsh/releases), downloads the archive for your OS and CPU, checks it against the release's `checksums.txt`, and installs the binary without sudo or admin rights:

- Linux and macOS: `~/.local/bin/clawsh`. If that directory isn't on your `PATH`, the script says so and prints the line to add.
- Windows: `%LOCALAPPDATA%\Programs\clawsh\clawsh.exe`, and that folder is added to your user `PATH` (open a new terminal afterwards).

Run it again to upgrade. Settings:

| Variable | Default | |
|---|---|---|
| `CLAWSH_VERSION` | latest | Install a specific release, e.g. `0.1.0` |
| `CLAWSH_INSTALL_DIR` | see above | Where the binary goes |

With `curl … | sh`, set them on the `sh` side: `curl -fsSL …/install.sh | CLAWSH_VERSION=0.1.0 sh`. In PowerShell, set `$env:CLAWSH_VERSION = '0.1.0'` first. To read the script before running it, open [install.sh](https://varogonz95.github.io/clawsh/install.sh) or [install.ps1](https://varogonz95.github.io/clawsh/install.ps1); the source is in `site/` in the repository.

## Updating

`clawsh update` replaces the binary with the latest release (`clawsh update --check` only looks); see [Commands](Commands.md#clawsh-update). With npm, run `npm install -g clawsh-cli@latest` instead.

## npm

With Node 18 or newer:

```sh
npm i -g clawsh-cli      # installs the clawsh command
npx clawsh-cli ls        # or run it without installing
```

The `clawsh-cli` package is a small launcher (npm doesn't allow the name `clawsh`; the command it installs is still `clawsh`). npm also installs the one platform package that matches your machine (`clawsh-linux-x64`, `clawsh-darwin-arm64`, `clawsh-win32-x64`, ...), which holds the real binary. If that package was skipped (pnpm or bun blocking it, `--omit=optional`, a lockfile made on another OS), the first run downloads it from your npm registry, checks it against the registry's sha512, and caches it.

Behind a proxy or a registry that blocks the download, point the launcher at a binary you already have: `CLAWSH_BINARY_PATH=/path/to/clawsh`. On Node 24+, `NODE_USE_ENV_PROXY=1` makes the download honor `HTTPS_PROXY`. Downloaded binaries are cached in `~/.cache/clawsh` (`$XDG_CACHE_HOME/clawsh`), `~/Library/Caches/clawsh` on macOS or `%LOCALAPPDATA%\clawsh\cache` on Windows; set `CLAWSH_CACHE_DIR` to use another directory.

## Prebuilt binaries

Download the archive for your platform from [Releases](https://github.com/varogonz95/clawsh/releases): `clawsh_<version>_<os>_<arch>.tar.gz` (`.zip` on Windows), plus `checksums.txt`, which has the SHA-256 of each archive. Each archive unpacks into a directory of the same name holding `clawsh` and the README.

Linux:

```sh
sha256sum -c --ignore-missing checksums.txt
tar xzf clawsh_0.1.0_linux_amd64.tar.gz
```

macOS:

```sh
grep clawsh_0.1.0_darwin_arm64.tar.gz checksums.txt | shasum -a 256 -c -
tar xzf clawsh_0.1.0_darwin_arm64.tar.gz
```

Windows (PowerShell): compare the output with the line for your archive in `checksums.txt`, then unzip it.

```powershell
(Get-FileHash clawsh_0.1.0_windows_amd64.zip -Algorithm SHA256).Hash
Expand-Archive clawsh_0.1.0_windows_amd64.zip .
```

## From source

Requires Go 1.22+.

```sh
git clone https://github.com/varogonz95/clawsh.git
cd clawsh
make build        # produces ./clawsh (or: go build -o clawsh ./cmd/clawsh)
```

## Every platform at once

```sh
make dist         # needs Go
make docker-dist  # needs Docker with buildx, no Go
```

Both write to `dist/`:

```
clawsh-darwin-amd64   clawsh-linux-amd64   clawsh-windows-amd64.exe
clawsh-darwin-arm64   clawsh-linux-arm64   clawsh-windows-arm64.exe
```

`make docker-dist` runs the same `make dist` inside a `golang` container and copies only the binaries out. Builds are `CGO_ENABLED=0`, trimmed and stripped.

## Put it on each machine

Copy the matching binary to every machine that should host sessions and to the machine you drive from (the same binary does both). Rename it to `clawsh` (or `clawsh.exe`) and put it on your `PATH`.

Machines that host sessions also need Claude Code installed, with `claude` on the `PATH` of the user running the agent (or pass its full path with `clawsh agent --claude`).

**Windows:** the first time `clawsh agent` runs, allow `clawsh.exe` through Windows Defender Firewall for private networks.

Next: [Quick start](Quick-Start.md).
