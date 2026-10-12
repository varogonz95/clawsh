# Plan

Goal: one terminal on any PC shows every Claude Code session on every LAN machine with live status, and can attach, type, detach, start, and kill.

## Decisions

- One Go binary, two modes (`agent`, hub commands). CGO-free, cross-compiles for Windows, Linux, macOS.
- The agent owns the PTYs: creack/pty on Unix, ConPTY on Windows. No tmux dependency.
- Status via Claude Code hooks, not screen-scraping (M3).
- Attach is full-screen passthrough; Ctrl-] detaches.
- Shared bearer token per agent.

## Milestones

- [x] **M0, PTY spike.** Spawn in a PTY, I/O, resize, exit codes. Unix verified by tests; Windows compiles, needs a manual run.
- [x] **M1, agent API.** REST create/list/kill, WebSocket attach, scrollback replay, token auth, repaint nudge on attach.
- [~] **M2, hub.** Done: `hosts.toml`, `hosts`, `ls` across machines, `new`, `attach`, `kill`, cancellable stdin reader in attach. Todo: bubbletea TUI (session list + attach + return to list).
- [x] **Web UI (#5).** `clawsh web`: loopback server, per-launch access key (localStorage, not a cookie), SSE overview, attach bridge, embedded UI (dashboard, terminal, new session, first run). Spec `docs/superpowers/specs/2026-10-01-web-ui-design.md`. Follow-ups: #6 LAN access, #7 per-agent UI, #8 edit hosts, #9 landing showcase.
- [~] **Auto-agent PoC** (`docs/poc-auto-agent.md`). Claude Code plugin + `clawsh mcp` stub: spawns a detached agent if none runs, and announces sessions started outside clawsh (`/v1/external` leases, `external` in `ls`). Linux verified; Windows/macOS need the manual check in that doc.
- [ ] **M3, status and alerts.** Agent launches claude with `--settings` injecting `Stop`, `Notification`, `UserPromptSubmit` hooks that run `clawsh hook <event>` (reads `CLAWSH_SESSION_ID`, posts to the agent). New states: working, idle, needs_input. Hub shows badges and rings the bell on needs_input.
- [ ] **M4, hardening.** Agent as a per-user login service (systemd user unit, launchd agent, Windows logon task), done by #36 (`clawsh agent install-service`); hub auto-reconnect; Windows Job Object; multiple-viewer resize policy.

## Later

Split-pane multi-view; mDNS discovery; TLS or Tailscale for off-LAN; session persistence across agent restarts.

## Manual Windows check (M0 exit criteria)

1. `clawsh agent` on Windows, `clawsh new <win-host>` from another machine.
2. Claude's TUI renders, arrow keys and Esc work, window resize reflows.
3. Detach, re-attach: screen repaints cleanly.
4. `clawsh kill`: claude process gone in Task Manager.
5. While attached, paste text and type non-ASCII (ñ, an emoji): it arrives intact. Click in the console (focus events) and keep typing: nothing is lost or stuck.

## Manual web UI check

Run after changes to `internal/web/static`: the M1–M11 list in `docs/superpowers/plans/2026-10-01-web-ui.md`, Task 7, Step 8. On Windows also check that `clawsh web` opens the default browser.
