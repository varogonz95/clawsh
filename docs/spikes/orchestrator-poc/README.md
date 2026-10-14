# Orchestrator PoC: message to a session as a channel event

Spike #74. **Unmerged branch** `doc/74/orchestrator-spike-poc`; the design is in `docs/spikes/orchestrator.md` on the spike PR.

## What it adds

- `api.Message`, `api.SendRequest` (`internal/api/types.go`).
- Agent: `POST /v1/messages` (queue a message for a session the agent knows: managed, or announced external; 404 otherwise, 429 when 100 are queued) and `GET /v1/inbox/{id}?wait=<dur>` (long-poll, drains the queue, max 30 s). In memory, `internal/agent/inbox.go`.
- Hub client: `Send`, `Inbox`.
- `clawsh mcp --channel`: declares `capabilities.experimental["claude/channel"]`, polls the inbox of its session (`$CLAWSH_SESSION_ID`, else the announce id) and writes each message to stdout as `notifications/claude/channel` with `content` and `meta` (`from`, `message_id`, sender meta whose keys are identifiers; the stub's own keys win). `instructions` tells claude the content is untrusted.

## What was proven

`go test -race -run TestChannelDeliversPostedMessage ./internal/mcp` drives a real agent (`httptest`) and the real `Run` + `ServeChannel` with a scripted MCP client over pipes:

1. `initialize` returns the `claude/channel` capability.
2. `POST /v1/messages` for an unknown session is refused.
3. A message posted for session `bbbb` comes out of the stub as a `notifications/claude/channel` line (no `id`) whose `content` and `meta` match; a `bad-key` meta entry is dropped and a spoofed `from` is replaced.

## What was not proven

- That a real Claude Code accepts and acts on the event. It needs a logged-in `claude` started with `--dangerously-load-development-channels plugin:clawsh@clawsh` (or `server:clawsh` for a bare `.mcp.json` entry) in an interactive session, and `channelsEnabled` on Team/Enterprise orgs. See the manual checks in the spike.
- Reply path (`reply`/`handoff` tool), sender gating beyond the bearer token, hub-to-agent routing across machines, durability, Windows.
- Idle-aware delivery: channel events queue inside claude, so none is needed for delivery, but an orchestrator still wants #33 to know when a stage is done.
