# Spike: Claude Code cloud sessions in clawsh

Issue: #63. Checked on 2026-10-13. Re-check the sources below before acting on this.

## Question

Can clawsh list Claude Code cloud sessions (the ones at claude.ai/code, started from the browser, the Claude
app, `claude --cloud` or a routine) and attach to them, the way it does for sessions on LAN machines?

## Answer

Not yet. Anthropic documents no API to list, read, stream or attach to cloud sessions. clawsh doesn't support
them; #63 tracks the feature until such an API exists.

## What is documented

| Surface | What it does | Auth | Fit for clawsh |
| --- | --- | --- | --- |
| `claude --cloud "<task>"` | Creates a cloud session for the current repo's GitHub remote and branch (`--remote` is a deprecated alias) | claude.ai sign-in (`claude auth login`) | Create only. Runs as a normal session program on an agent. |
| `claude -p "<msg>" --cloud <id>` | Queues one message into a running session and exits; `--output-format json` prints `{ok, session_id, url}` | claude.ai sign-in | Send only, no reply or output. `--cloud <id>` without `-p` is rejected. |
| `claude --teleport [<id>]` | Pulls a session's branch and history into a local checkout. Makes a local copy, not a live view. Without an id it opens an interactive picker. | claude.ai sign-in, clean tree, same repo, same account | Works as a clawsh session today (see below). |
| Routines fire API, `POST /v1/claude_code/routines/{id}/fire` | Starts a run of one routine, returns the new session id and URL | Per-routine token, no read access | Create only, one routine per token. |
| Managed Agents API, `/v1/sessions` (beta) | Create, list, stream and message agent sessions | Claude API key | A different product: these are not claude.ai/code sessions. |
| Compliance API, `/v1/compliance/apps/sessions/*` | Read-only transcripts for compliance export | Compliance key (admin) | Admin-scoped. Cloud Claude Code sessions are not in the documented surfaces. |
| Remote Control | Steers a local session from claude.ai or the app | claude.ai sign-in only, short-lived tokens | The transport is internal. No third-party protocol is documented. |

Sources:

- [Use Claude Code in the cloud](https://code.claude.com/docs/en/claude-code-on-the-web): `--cloud`, follow-ups, `--teleport`, errors
- [Remote Control](https://code.claude.com/docs/en/remote-control)
- [Routines](https://code.claude.com/docs/en/routines), [fire API](https://platform.claude.com/docs/en/api/claude-code/routines-fire)
- [Managed Agents](https://platform.claude.com/docs/en/managed-agents/overview), [sessions API](https://platform.claude.com/docs/en/api/beta/sessions)
- [Compliance sessions API](https://platform.claude.com/docs/en/api/compliance/apps/sessions)
- [Legal and compliance: authentication and credential use](https://code.claude.com/docs/en/legal-and-compliance#authentication-and-credential-use)

## Constraints

- **Credentials.** Third-party developers may not offer claude.ai login, route requests through a user's
  subscription, or "collect, store, or intermediate Claude.ai credentials or session tokens". An end user signing
  in to the unmodified `claude` binary is allowed. So clawsh:
  - may run the official `claude` binary that the user signed in to;
  - must not read `~/.claude` credentials, replay tokens, or call claude.ai or Remote Control endpoints directly.
- **No listing.** Nothing returns the user's sessions as data. Ids come from the claude.ai/code URL or the
  `claude -p --cloud` / routine output.
- **No live stream.** No documented way to receive a cloud session's output, so nothing to attach a terminal
  or the web UI to.
- **Policy.** Cloud sessions need a Pro, Max, Team or Enterprise plan, an Anthropic account (not Bedrock, Vertex
  or Foundry), and the org policy `allow_remote_sessions`.

## What works today without code

The agent already runs `claude` with any args from `clawsh new <host> -- <args>`. The executable stays fixed by
`clawsh agent --claude`, so this keeps the CLAUDE.md rule. That covers:

- Continue a cloud session on a LAN machine, managed by clawsh:
  `clawsh new <host> --dir <repo-checkout> -- --teleport <session-id>`.
  This needs the agent's user signed in with `claude auth login` and a clean checkout of the same repo.
- Start a cloud task from a machine whose `claude` is signed in:
  `clawsh new <host> --dir <repo> -- --cloud "<task>"`.
  The PTY shows the setup checklist and the session link.

Neither was run during this spike. Verify both before documenting them in `docs/wiki`.

## Options considered

1. **Wait for a public API (chosen).** Track it in #63 and ask Anthropic for it through `/feedback`. No code.
2. **Wrap the documented CLI.** `clawsh cloud send <id> <msg>` around `claude -p --cloud --output-format json`,
   plus `clawsh cloud teleport <host> <id>`. Legal and small, but without a list or live output it adds
   little over the commands above. Revisit if a list command with JSON output ships.
3. **Use undocumented endpoints or the Remote Control transport.** Rejected: against the terms above, and it
   breaks without notice.
4. **Managed Agents sessions as a clawsh "host".** Possible with an API key, but it's a different product and
   doesn't show the user's claude.ai/code sessions. A separate idea, not this one.

## Re-check when

Any of these appear in the docs:

- an endpoint or CLI command (with JSON output) that lists cloud sessions;
- a documented way to stream a session's events;
- auth that a third-party tool may hold for these.

At that point, refine #63 into sub-issues per `docs/issue-refinement.md`:

- a `cloud` pseudo-host in `hub.Overview` / `clawsh ls`;
- a read-only web UI view;
- attach, if the stream allows input.
