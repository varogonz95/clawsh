# Configuration

clawsh keeps its files in the OS user config directory (Go's `os.UserConfigDir`):

| OS | Directory |
|---|---|
| Linux | `$XDG_CONFIG_HOME/clawsh` or `~/.config/clawsh` |
| macOS | `~/Library/Application Support/clawsh` |
| Windows | `%AppData%\clawsh` |

clawsh was called ccm before. If the `clawsh` directory doesn't exist but an old `ccm` one does, clawsh keeps using the old one, so existing tokens and `hosts.toml` still work. Rename the directory to `clawsh` to migrate.

## `hosts.toml` (hub)

Lists the agents the hub talks to. Override the path with `--config`.

```toml
[[host]]
name = "desk"
url = "http://192.168.1.20:7420"
token = "paste-token-from-desk"

[[host]]
name = "laptop"
url = "http://192.168.1.31:7420"
token = "paste-token-from-laptop"
```

- `name` and `url` are required; names must be unique. The name is what you type in `clawsh new desk` and `desk/<id>`.
- A trailing `/` on `url` is ignored.
- `token` is the output of `clawsh token` on that machine.
- `url` must start with `http://` or `https://`.
- `clawsh web` can edit this file for you (Machines page). When it saves, it re-encodes the file: comments and formatting are lost, and the previous contents are kept in `hosts.toml.bak` (mode `0600`).

A template lives at `hosts.example.toml` in the repo.

## `agent.token` (agent)

A 64-character hex token, generated on the agent's first run and written with mode `0600`. Delete it and restart the agent to rotate; then update `hosts.toml` everywhere. Use `--token-file` to keep it elsewhere.

## `recent-dirs.json` (agent)

A JSON array of the last 20 folders the agent started sessions in, most recent first, written with mode `0600` next to `agent.token`. The web UI shows it as recent folders in the new session dialog. Delete the file to clear it. Folders that no longer exist are hidden but stay in the file. Sessions announced through `/v1/external` are not recorded.

## Session environment

Every session is started with the agent's environment plus:

- `CLAWSH_SESSION_ID`: the session's ID
- `TERM=xterm-256color`, `COLORTERM=truecolor` (not on Windows)
