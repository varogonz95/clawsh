# Protocol

The agent speaks JSON over HTTP plus one WebSocket endpoint. The Go types live in `internal/api/types.go`. The protocol isn't versioned separately; `GET /v1/health` reports the agent's build version.

## Auth

Every endpoint except `/v1/health` requires:

```
Authorization: Bearer <token>
```

Failures return `401` with `{"error":"unauthorized"}`. All errors use `{"error":"<message>"}`.

## Endpoints

| Method | Path | |
|---|---|---|
| `GET` | `/v1/health` | `{"host","os","version"}`; no auth |
| `GET` | `/v1/sessions` | Array of sessions, oldest first |
| `POST` | `/v1/sessions` | Create a session |
| `DELETE` | `/v1/sessions/{id}` | Stop and remove a session |
| `GET` | `/v1/sessions/{id}/attach` | WebSocket upgrade |
| `GET` | `/v1/external` | Array of external sessions, oldest first |
| `PUT` | `/v1/external/{id}` | Announce or renew an external session |
| `DELETE` | `/v1/external/{id}` | Withdraw an external session |
| `POST` | `/v1/hooks` | Report a Claude Code hook event (`204` applied, `202` ignored, `400` bad JSON) |

`{id}` may be a unique prefix. An ambiguous prefix or unknown ID is an error.

### Session

```json
{
  "id": "815856a6",
  "name": "api",
  "dir": "/home/alvaro/src/api",
  "args": ["--resume"],
  "pid": 41233,
  "created": "2026-09-30T10:12:00Z",
  "status": "running",
  "viewers": 1
}
```

`status` is `running` (live, no hook seen yet), `working`, `idle`, `needs_input` (driven by [hooks](#hooks)), or `exited`; `gone` is reserved for external sessions. An exited session stays `exited`. `origin` is `managed` for sessions the agent spawned. `exit_code` is present only once exited; `args` only when non-empty. `claude_session_id`, `transcript` and `last_event` appear after the first hook.

### Create request

```json
{ "name": "api", "dir": "~/src/api", "args": ["--resume"], "cols": 120, "rows": 40 }
```

All fields are optional. `dir` defaults to the agent user's home and `~` is expanded on the agent. Size defaults to 120×40. There is no field for the executable; that is fixed by the agent's `--claude` flag.

### Hooks

`POST /v1/hooks` takes `{"event","session_id","transcript_path","cwd","message","clawsh_session_id","pid"}` (`message` and `clawsh_session_id` optional). With a `clawsh_session_id` matching a managed session, the event sets its status: `UserPromptSubmit` → `working`, `Stop` and `SessionStart` → `idle`, `Notification` → `needs_input`, `SessionEnd` → unchanged. Events without a known `clawsh_session_id` answer `202` and are ignored.

### External sessions

Claude sessions the agent doesn't own, announced by `clawsh mcp` (see [Claude Code plugin](Plugin.md)). They're listed only; you can't attach to them or kill them.

`PUT /v1/external/{id}` takes `{"name","dir","pid"}`, all optional; `name` defaults to the basename of `dir`. The caller picks the `{id}`: 1–64 characters of `[0-9a-zA-Z_-]`. The call is an upsert, and each one renews a lease. The agent forgets entries not renewed within 90 s.

```json
{ "id": "aecd9193be8d78aa", "name": "api", "dir": "/home/alvaro/src/api", "pid": 5120,
  "created": "2026-10-02T02:10:39Z", "seen": "2026-10-02T02:11:09Z" }
```

## Attach WebSocket

- **Binary frames** carry raw terminal bytes in both directions.
- **Text frames** carry JSON control messages:

| `type` | Direction | Fields |
|---|---|---|
| `resize` | hub → agent | `cols`, `rows` |
| `exit` | agent → hub | `code` |

On connect the agent sends the scrollback as binary, then live output. When the process ends it sends `exit` and closes normally. The agent pings every 20 s and drops a client silent for 60 s.
