# clawsh

See and drive every Claude Code session on every machine in your LAN from one terminal.

```
clawsh ls
TARGET         NAME      STATUS   VIEWERS  AGE    DIR
desk/815856a6  api       running  0        2h14m  /home/alvaro/src/api
laptop/3fa0c1  frontend  running  1        12m    C:\src\frontend
```

One binary, two roles:

- **agent**: runs on each machine, owns `claude` processes inside PTYs (ConPTY on Windows), keeps them alive while nobody is watching, and serves a small REST + WebSocket API.
- **hub**: the `hosts / ls / new / attach / kill / web` commands, run from whichever terminal you're in.

Full docs live in the [wiki](docs/wiki/Home.md); the landing page is https://varogonz95.github.io/clawsh/ (source in `site/index.html`)

## Setup

Install the latest release (checks the download against the release's checksums, no sudo or admin needed):

```sh
curl -fsSL https://varogonz95.github.io/clawsh/install.sh | sh   # Linux, macOS
```

```powershell
irm https://varogonz95.github.io/clawsh/install.ps1 | iex   # Windows PowerShell
```

With Node 18+: `npm i -g clawsh`, or run it without installing: `npx clawsh ls`. Prebuilt binaries are also on the [Releases](https://github.com/varogonz95/clawsh/releases) page. To build from source you need Go 1.22+.

```sh
go mod tidy          # first time only
make build           # or: go build -o clawsh ./cmd/clawsh
make dist            # cross-compile all platforms into dist/
make docker-dist     # same, built inside Docker (needs buildx, no local Go)
```

On **each machine that should host sessions**:

```sh
clawsh agent            # listens on :7420, creates a token on first run
clawsh token            # prints the token
```

Windows: allow `clawsh.exe` through Windows Defender Firewall (private networks) when prompted.

On **the machine you drive from**, copy `hosts.example.toml` to the path shown by `clawsh help`, fill in each agent's URL and token, then:

```sh
clawsh hosts                                   # who's reachable
clawsh new desk --dir ~/src/api                # start claude there and attach
clawsh new desk --dir ~/src/api -- --resume    # pass args through to claude
clawsh ls                                      # everything, everywhere
clawsh attach desk/8158                        # id prefix is enough
                                            # Ctrl-] detaches, session keeps running
clawsh kill desk/8158
```

## Security

Anyone holding an agent's token can run Claude Code on that machine, with that machine's permissions. Traffic is plain HTTP, so keep agents on a trusted LAN until TLS/Tailscale support lands (see `docs/PLAN.md`). The command an agent runs is fixed by `--claude` on the agent; the API only accepts extra args.

## Known limits (MVP)

- Only manages sessions started through clawsh. With the experimental [Claude Code plugin](docs/wiki/Plugin.md), `claude` launched in a plain terminal is listed as `external`, but can't be attached to.
- Sessions stop when the agent stops. `claude --resume` recovers the conversation.
- Status is `running` / `exited`. Working / idle / needs-input arrives with M3.
- Windows: killing a session doesn't yet kill claude's child processes (Job Object TODO).

## License

clawsh is source-available under the [Apache License 2.0 with the Commons Clause](LICENSE). You can use it for free, including at work, and copy, modify and share it, but you can't sell it or charge for a product or service (hosting, support, consulting) whose value comes mostly from clawsh.
