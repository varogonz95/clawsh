# Commands

```
clawsh <command> [args]
```

Flags may appear before or after positional arguments. For `new`, everything after a literal `--` goes to claude.

## Agent side

Run these on each machine that hosts sessions.

### `clawsh agent`

Runs the agent in the foreground. Ctrl-C (or SIGTERM) stops every session and exits.

| Flag | Default | |
|---|---|---|
| `--listen` | `:7420` | Address to listen on |
| `--claude` | `claude` | Executable to run for every session (name on `PATH` or full path) |
| `--token-file` | `<config dir>/clawsh/agent.token` | Bearer token file, created on first run |
| `--scrollback` | `2097152` (2 MiB) | Bytes of output kept per session for replay |

The executable is fixed here. The API only lets clients add arguments, never choose what runs.

### `clawsh token`

Prints the agent's token, creating it if needed. Accepts `--token-file`.

### `clawsh mcp`

An MCP stdio server with no tools, run by Claude Code through the [plugin](Plugin.md). It starts a detached agent if none answers, and announces the session to the agent. Flags are listed on the plugin page.

## Hub side

Run these anywhere. All accept `--config` (default `<config dir>/clawsh/hosts.toml`; `clawsh help` prints the exact path).

Sessions are addressed as `<host>/<id>`, where `host` is a name from `hosts.toml` and `id` is the session ID or any unique prefix of it.

### `clawsh hosts`

Checks every configured agent: health, then an authenticated call. Timeout is 3 s per host.

### `clawsh ls [host]`

Alias: `list`. Lists sessions on every host, or just one. Columns: `TARGET`, `NAME`, `STATUS` (`running`, `exited(code)`, or `external` for sessions announced by the [plugin](Plugin.md)), `VIEWERS`, `AGE`, `DIR`. Unreachable hosts are reported on stderr after the table.

### `clawsh new <host> [flags] [-- claude args...]`

| Flag | Default | |
|---|---|---|
| `--dir` | remote user's home | Working directory on the remote machine; `~` is expanded there |
| `--name` | basename of `--dir` | Session name shown in `ls` |
| `--detached` | off | Create without attaching |

The session starts at your current terminal size.

### `clawsh run [flags] [-- claude args...]`

Like plain `claude` in the current terminal, but the session belongs to the local agent: it needs no `hosts.toml` entry, can be attached from any hub (`clawsh attach local/<id>`) and survives closing the terminal. Starts in the current directory and attaches at the current terminal size. **Ctrl-]** detaches and exits 0; when claude exits, `clawsh run` exits with its exit code. It does not start an agent: if none answers it fails with `no local agent at <url>; start one with 'clawsh agent' or 'clawsh agent install-service'`.

| Flag | Default | |
|---|---|---|
| `--name` | basename of the directory | Session name shown in `ls` |
| `--listen` | `:7420` | Address of the local agent |
| `--token-file` | per-user token file | Agent token |

To use it in place of `claude`:

```sh
alias claude='clawsh run --'          # bash / zsh
```

```powershell
function claude { clawsh run -- @args }   # PowerShell profile
```

### `clawsh attach <host>/<id>`

Alias: `a`. Takes over your terminal until you press **Ctrl-]**, the session exits, or the connection drops. Needs an interactive terminal. Terminal resizes are forwarded.

### `clawsh kill <host>/<id>`

Alias: `rm`. Asks the process to stop, forces it after 3 s, and removes the session from the agent. On Unix the whole process group gets the signal, so claude's children (MCP servers, shells) stop too.

### `clawsh web`

Serves the browser UI on `127.0.0.1:7421` and opens it. Flags: `--listen` (loopback addresses only), `--no-open`, `--config`. See [Web UI](Web-UI.md).

### `clawsh version`, `clawsh help`
