# Web UI

`clawsh web` opens a browser page that shows every machine in `hosts.toml` and its Claude Code sessions. You can open a session in a full-page terminal, start new ones and end them, without typing other commands.

## Start it

```
clawsh web
```

It prints a link like `http://127.0.0.1:7421/?k=…` and opens it in your default browser. Keep the terminal open; Ctrl-C stops the web UI (your sessions keep running on their machines).

To keep the access key off the command line, the browser is opened through a short-lived private file that redirects to the link. If your browser can't read it (Snap browsers such as Ubuntu's default Firefox show "file not found"), open the printed link yourself.

| Flag | Default | |
|---|---|---|
| `--listen` | `127.0.0.1:7421` | Loopback address to serve on. Other addresses are refused. |
| `--no-open` | off | Print the link without opening a browser |
| `--config` | `<config dir>/clawsh/hosts.toml` | Hosts file |

## What you see

- **Your machines:** a card per host with its sessions. Offline machines show why. The page updates by itself.
- **A session:** the terminal fills the page. Leaving it only disconnects you; Claude keeps working. Shift+Enter inserts a newline in a Claude Code prompt instead of submitting it (it sends ESC+CR).
- **New session:** pick a machine, a folder and optionally a name and extra Claude arguments.
- **No machines yet:** the steps to add one. The page notices when you save `hosts.toml`.
- **Hosts file errors:** if `hosts.toml` stops parsing while `clawsh web` runs, a banner shows the error and the page keeps the machines from the last good version until you fix and save it.

## Limits

- Opens only on the computer running `clawsh web` (see [Security](Security.md)). Phones and other devices: issue #6.
- Editing machines from the page: issue #8.
- Dark theme only.
