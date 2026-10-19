# Spike: pet view

Issue #72. Every session becomes a generated pet on a canvas; pets react to what their session does, talk in
bubbles, and spawn tiny sub-pets for sub-agents. This is a design doc plus a throwaway prototype
(`docs/spikes/pet-view/prototype.html`, open it straight from disk). No product code changed.

**Recommendation:** build it, as a new `#/pets` route plus a dashboard toggle, driven by a separate `activity`
SSE event (not the overview), rendered with DOM + SVG. It needs #33 (state hooks) and #35 (global hook install);
a richer experience (bubbles, tools, sub-pets) needs three more hook events beyond #33's list.

## 1. States

Hook events come from Claude Code ([hooks reference](https://code.claude.com/docs/en/hooks)). Pet state is a pure
function of the latest relevant event for that `session_id` (or `agent_id` for sub-pets).

| Pet state | Trigger | Look |
| --- | --- | --- |
| sleeping | `SessionStart`, or no event for 10 min | eyes closed, slow breathing, `z` |
| idle | `Stop` plus 4s, or `Notification` of type idle | awake, slow bob |
| thinking | `UserPromptSubmit`, until first text/tool event | eyes up, `…` over the head, antenna lit |
| talking | `MessageDisplay` (`final:false`) | mouth animates, bubble shows text |
| tool: read | `PreToolUse` `Read`/`Grep`/`Glob` | book icon, wiggle |
| tool: edit | `PreToolUse` `Edit`/`Write`/`NotebookEdit` | pencil icon |
| tool: run | `PreToolUse` `Bash` | keyboard icon |
| tool: web | `PreToolUse` `WebFetch`/`WebSearch` | globe icon |
| waiting | `Notification` (permission / idle prompt), also #33's `needs_input` | hopping, yellow `!` |
| happy | `Stop` | closed happy eyes, hops for ~4s, then idle |
| hurt | `StopFailure`, `PostToolUseFailure` | `x` eyes, shake for ~3s, then back to the state before |
| leaving | `SessionEnd`, or `Status` `exited`/`gone` | fades up and out |

Any `Post*` event or the next `PreToolUse` ends a tool state. A tool state older than 60s decays to thinking.

**No hooks installed.** `api.Session.Status` is only `running`/`exited` (and `gone` for external sessions). The
pet is then alive (awake, idle bob, no bubbles, no tool icons) or leaving. The web view shows a small
"install hooks for more life" hint once per session (#35). Without #33, `exited` is the only change a pet can show.

Mapping to #33's statuses: `working` = thinking/talking/tool, `idle` = idle/happy, `needs_input` = waiting,
`running` (no hook seen yet) = awake idle.

## 2. Bubbles

| State | Source | Max length | Stays up |
| --- | --- | --- | --- |
| talking | `MessageDisplay.delta`, accumulated per `message_id`, last full sentence | 80 chars | 4s after the last delta |
| happy | `Stop.last_assistant_message`, first sentence | 80 chars | 4.5s |
| tool | `tool_name` plus a short arg (file basename, command first word) | 40 chars | 2.5s |
| waiting | `Notification.message` | 80 chars | until the state changes |
| hurt | fixed text plus `tool_name` | 40 chars | 3s |
| thinking | fixed ("Hmm…"): **no hook exposes reasoning text** | n/a | until first output |

Rate limit: at most one new bubble per pet per 900ms (the prototype uses this); a newer bubble replaces the old
one, never stacks. Truncate with an ellipsis; render as text (`textContent`), never HTML.

**Privacy.** Bubble text is assistant output and tool arguments, and it leaves the session machine: agent →
`clawsh web` (over the hub connection, already token-authenticated) → browser tabs on the web UI's loopback
listener with its access key. Anyone who can open the web UI can already attach to the terminal and read
everything, so the pet view adds no new audience. It does add a new on-the-wire copy, so: no transcript text is
persisted by the agent, the activity buffer is memory-only, and tool arguments are reduced to a basename or the
first word before they leave the agent (commands can hold secrets). A per-host `--no-bubbles` flag is cheap to add
and listed in the follow-ups.

### `MessageDisplay` hook latency (measured)

Claude Code waits for the hook before it displays each batch (10s default timeout), so the cost lands on every
batch of streamed text.

Method: `clawsh hook` built from PR #40's branch (`feat/33/hook-ingest`, `CGO_ENABLED=0`), a real `clawsh agent`
(`--claude /bin/sh`) on loopback in the same container (4 vCPU Linux), a `MessageDisplay`-shaped JSON on stdin
(~120 byte delta), 300 sequential invocations timed with `perf_counter` around `subprocess.run`. Baseline is the
same loop around `/bin/true`.

| Command | p50 | p95 | max |
| --- | --- | --- | --- |
| `/bin/true` (process spawn floor) | 1.1 ms | 1.5 ms | 2.2 ms |
| `clawsh hook MessageDisplay` (agent up) | 3.0 ms | 3.6 ms | 5.1 ms |
| `node -e ""` (npm launcher floor) | 22 ms | 26 ms | n/a (30 runs) |

Reading: the Go hook adds about 2 ms over a bare spawn; when installed through the npm launcher
(`bin/clawsh.js` spawning the binary) add roughly one Node startup, about 25 ms per batch. Both are invisible
next to the model's token rate, but the launcher figure is large enough that hooks should call the native binary
directly where #35 can resolve it (`CLAWSH_BINARY_PATH` or the cached binary). Caveat: the agent was local and
the posted session id was unknown (agent answers 202 without touching a session), so a slow or remote agent is not
covered; #33's hook has a 1s timeout and always exits 0, which bounds the worst case at 1s per batch on a dead
agent. **Fallback if that is too slow:** register `MessageDisplay` as an async hook (if supported) or drop it and
bubble from `PreToolUse` and `Stop.last_assistant_message` only (the pet then talks once per turn instead of
streaming). Not measured: Windows process spawn, which is slower; list it under manual checks of the follow-up.

## 3. Sub-pets

- **Spawn** on `SubagentStart` (`agent_id`, `agent_type`); **leave** on `SubagentStop` with the same `agent_id`,
  and when the parent leaves.
- **Activity** from any hook carrying that `agent_id` (`PreToolUse` etc.). Reduced state set: idle, working (tool
  or text), hurt, leaving. No waiting, no happy.
- **Caps:** 6 sub-pets per pet (extras are counted in a `+N` badge, not drawn), 150 total in the view; oldest
  idle sub-pet is dropped first.
- **Look:** a blob with two eyes, colour derived from the parent (parent colour ×1.1 brightness), no ears,
  pattern or accessory, ~37% of parent size, orbits its parent. Bubbles are rare (the prototype shows half of the
  would-be bubbles; production: only `SubagentStop.last_assistant_message`, 40 chars).
- Sub-pets are in-memory only: an `agent_id` unknown to the view (missed `SubagentStart`) spawns on first sight.

## 4. Generation

Deterministic from the session id: `hash(id)` (FNV-1a) seeds a small PRNG (mulberry32); the draw order is fixed:
body (`blob`/`tall`/`wide`), eyes (`dots`/`big`/`sleepy`/`wink`), ears (`none`/`cat`/`horns`/`antenna`), pattern
(`none`/`spots`/`stripes`/`belly`), accessory (`none`/`bow`/`hat`/`scarf`), then name (two syllables from a list of
12). Colour comes from the host: `hash(host) % HOST_COLORS.length`, using the same `--host-colors` list as
`hostColor()` in `app.js`, so a machine's pets are recognisable as a family. 3×4×4×4×4 trait combinations times 144
names times host colours is plenty for a handful of sessions; collisions are cosmetic. The prototype's `gen()` is
the reference implementation; a Playwright check confirms the same id renders identical SVG after reload. **Never
reorder the draws** once shipped, or every pet changes.

## 5. Wire format

Option A, extend `api.Session` and the overview SSE: simple, but the overview is polled and pushed on any change
(`internal/web/events.go`), so every `MessageDisplay` delta would re-send the whole overview to every tab.
Rejected.

Option B (**recommended**): keep `Session.Status` for the coarse state (#33) and add a separate activity stream.

Agent: `GET /v1/activity` (SSE, `Authorization: Bearer`), plus `GET /v1/activity/snapshot` for the current per-pet
state when a tab connects. Hub/web: `clawsh web` merges the per-host streams into one `GET /api/activity` SSE
(`events.go` pattern, one writer per connection, a slow tab is dropped).

Draft types (to go in `internal/api` first, then an e2e test):

```go
// Activity is one pet-relevant moment in a managed session. It is derived from
// a HookEvent by the agent and never persisted.
type Activity struct {
	Session string `json:"session"`            // clawsh session id
	Agent   string `json:"agent,omitempty"`    // Claude subagent_id; empty = the session itself
	AgentType string `json:"agent_type,omitempty"`
	Kind    string `json:"kind"`               // prompt|text|tool|tool_failed|waiting|done|failed|sub_start|sub_stop|end
	Tool    string `json:"tool,omitempty"`     // tool_name for kind=tool|tool_failed
	Text    string `json:"text,omitempty"`     // already truncated to 80 chars, tool args reduced
	At      time.Time `json:"at"`
}

// ActivitySnapshot is the first message of /v1/activity: current pet state, so
// a late tab does not start from "sleeping".
type ActivitySnapshot struct {
	Pets []PetState `json:"pets"`
}
type PetState struct {
	Session string   `json:"session"`
	State   string   `json:"state"`   // table in section 1
	Tool    string   `json:"tool,omitempty"`
	Subs    []string `json:"subs,omitempty"` // live subagent ids
	Bubble  string   `json:"bubble,omitempty"`
}
```

The agent truncates and reduces text before it leaves the machine (section 2). `HookEvent` (#33) needs
`AgentID`, `AgentType`, `ToolName`, `LastAssistantMessage`, `Delta`, `NotificationType` added (all optional,
`omitempty`), so the ingest change is additive. Each stream is lossy by design: a full subscriber channel drops the
viewer, as for attach; the browser reconnects and takes a new snapshot. Rate: the agent coalesces `text` activities
to one per 250ms per session.

## 6. Rendering

| | DOM + SVG | `<canvas>` 2D |
| --- | --- | --- |
| 50 pets + 200 sub-pets at 60fps | fine if animation is CSS transform/opacity on ~250 elements (compositor) | easy |
| Crisp at any zoom / DPR, theme via CSS variables | yes | manual |
| Click, focus, `title`, screen readers | native | hit-testing and a parallel list |
| Per-frame cost with many SVG redraws | rebuild only on state change (as the prototype does), never per frame | n/a |

**Recommend DOM + SVG.** Layout is a random walk: each pet picks a new target every 4s inside the stage, moves with
a CSS transition (3s ease), sub-pets orbit their parent every 2.5s. Above ~60 pets switch to a grid and stop
wandering. `prefers-reduced-motion`: no wander, no bob, state shown by the drawing and the label only (the
prototype drops all animation). Accessible fallback: the same data as a `<ul>` (name, host/id, state, sub-agent
count), always in the DOM, which is also what the stage `aria-label` points to. Colours come from theme tokens
(#52): `--ground`, `--ink`, `--ink-soft`, bubble = `--ink` on `--ground`; host colours from `--host-colors`.
Click a pet: `location.hash = '#/s/<host>/<id>'`.

## 7. Placement

Both: a `#/pets` route (full stage, the fun view) and a dashboard toggle ("cards | pets") remembered in
`localStorage`. The dashboard stays the default, since cards carry information pets do not (folder, age, viewers).
With the session sidebar (#69) the pets can later appear as small avatars in the list, which is free once
`gen()` exists.

## 8. Risks and open questions for a human

1. **Is it worth the privacy surface?** Bubbles copy assistant text through two extra hops (section 2). Default on
   or opt-in (`clawsh web --pets`)?
2. **Hook install is the bottleneck.** Pets without hooks are dull; #35 is `needs_human`. The feature is only as
   good as its install story, including for sessions started outside clawsh.
3. **Hook budget.** `MessageDisplay` blocks display (section 2); we should ship it behind a flag until Windows
   latency is checked.
4. **Event availability.** `MessageDisplay`, `SubagentStart/Stop` and `agent_id` are documented but depend on the
   Claude Code version; the pet view must degrade (no bubbles, no sub-pets) not fail.
5. **Art budget.** The prototype is procedural SVG. Whether it is "fun, unique and catchy" enough is a human call
   (issue manual check); a hand-drawn set would change the generation section.
6. External sessions (`clawsh mcp`, `gone` status) have no `CLAWSH_SESSION_ID`, so hooks can only be matched by
   `Pid`; #33 ignores them. Pets for them would be alive/gone only.

## Follow-up issue outlines

Not filed; a human decides. Each follows `docs/issue-refinement.md`.

### A. Hooks carry pet data (agent + api)

- **Goal:** the agent turns hook events into pet state and an activity stream.
- **In:** extend `api.HookEvent` with `AgentID`, `AgentType`, `ToolName`, `LastAssistantMessage`, `Delta`,
  `NotificationType`; `clawsh hook` passes them; agent derives `PetState`/`Activity`; `GET /v1/activity` (SSE) and
  `/v1/activity/snapshot`; text truncation and argument reduction in the agent; e2e tests.
- **Out:** web UI, hook installation.
- **Decisions:** types from section 5; coalesce 250ms; caps from section 3; no new dependency.
- **Acceptance:** snapshot after `UserPromptSubmit` is `thinking`; `SubagentStart/Stop` adds/removes a sub id;
  bubble ≤ 80 chars; secrets in `Bash` args never leave the agent; slow subscriber dropped; `make vet`, `make test`.
- **Manual checks:** Windows hook latency with `MessageDisplay`.
- **Blocked by:** #33 (PR #40).

### B. Web proxy for activity

- **Goal:** `clawsh web` merges per-host activity streams.
- **In:** `GET /api/activity` SSE in `internal/web` (one writer per connection, reconnect with snapshot), tests with
  fake agents.
- **Out:** UI.
- **Blocked by:** A.

### C. Pet view UI

- **Goal:** `#/pets` route and dashboard toggle with generated pets, bubbles, sub-pets.
- **In:** `internal/web/static/pets.js` (generation from the prototype, fixed draw order), `app.css` tokens,
  reduced-motion and list fallback, click to attach; `TestStaticAssetsServed` entry; docs in `Web-UI.md`.
- **Out:** sound, persistence, custom pets.
- **Acceptance:** same id gives same SVG (npm test in `npm/test/`); every state in the table renders; ≤ 60 pets wander,
  more switch to grid; wiki updated.
- **Manual checks:** browser review of all six themes (#52), phone width, 50-pet load.
- **Blocked by:** B, #52 (tokens), #69 (optional).

### D. Hook install and privacy switch

- **Goal:** one command installs the pet-relevant hooks, with an opt-out for bubble text.
- **In:** extend #35's install to the new events; `clawsh agent --no-bubbles`; call the native binary directly (not
  the npm launcher).
- **Blocked by:** #35 (`needs_human`) and A.
