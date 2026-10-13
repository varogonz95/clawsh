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

## npm

With Node 18 or newer:

```sh
npm i -g clawsh      # installs the clawsh command
npx clawsh ls        # or run it without installing
```

The `clawsh` package is a small launcher with no binaries in it. On first run it downloads the binary for your machine from the matching [GitHub release](https://github.com/varogonz95/clawsh/releases) (the same file as in the archives below), checks it against the SHA-256 that shipped inside the npm package, and caches it. Later runs use the cached copy and need no network. Nothing runs at install time, so `--ignore-scripts`, pnpm and bun all work.

The first run needs to reach `github.com`. Behind a proxy or a firewall that blocks it, point the launcher at a binary you already have: `CLAWSH_BINARY_PATH=/path/to/clawsh`, or at a mirror laid out like the releases page (`<base>/v<version>/clawsh-<os>-<arch>[.exe]`): `CLAWSH_DOWNLOAD_BASE=https://mirror.example/clawsh`. The checksum is still enforced for a mirror. On Node 24+, `NODE_USE_ENV_PROXY=1` makes the download honor `HTTPS_PROXY`. Downloaded binaries are cached in `~/.cache/clawsh` (`$XDG_CACHE_HOME/clawsh`), `~/Library/Caches/clawsh` on macOS or `%LOCALAPPDATA%\clawsh\cache` on Windows; set `CLAWSH_CACHE_DIR` to use another directory.

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
