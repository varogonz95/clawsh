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
- **A session:** the terminal fills the page. Leaving it only disconnects you; Claude keeps working.
- **New session:** pick a machine, a folder and optionally a name and extra Claude arguments. Under the folder field, the folders recently used on that machine show up (3 at first, "Show all" for the rest); click one to fill the field. The list follows the machine you pick and stays hidden when there is none.
- **Machines page** (`Machines` in the top bar): add, edit, test and remove machines without editing `hosts.toml`. The access key field is write-only: saved keys are never sent back to the browser, and leaving it empty on edit keeps the saved key. *Test connection* checks the address and key (reports reachable, unreachable or key rejected) before or after saving. Saving rewrites `hosts.toml` (written atomically, mode `0600`); comments in a hand-edited file are dropped, and the previous version is kept as `hosts.toml.bak`. If `hosts.toml` doesn't parse, the page refuses to save until you fix it by hand.
- **No machines yet:** the steps to add one, or a link to the Machines page. The page notices when you save `hosts.toml`.
- **Hosts file errors:** if `hosts.toml` stops parsing while `clawsh web` runs, a banner shows the error and the page keeps the machines from the last good version until you fix and save it.

## Limits

- Opens only on the computer running `clawsh web` (see [Security](Security.md)). Phones and other devices: issue #6.
- Machines are added by hand-typed access key; fetching keys without running `clawsh token` on each machine is issue #46.
- Dark theme only.
