# Issue refinement

Issues are refined until an AI agent can pick one up and ship it on autopilot: one issue, one PR, no questions
back. This file is the contract for both the refinement agent and the implementation agent. It follows the same
model as `varogonz95/sproutlings-mobile` and `varogonz95/FloraBytes-PIO`, adapted to clawsh.
The stages after refinement (planning, review, testing, PR) are in [`sdlc-pipeline.md`](sdlc-pipeline.md).

## Labels

| Label              | Meaning                                                                 | Who sets it                  |
| ------------------ | ----------------------------------------------------------------------- | ---------------------------- |
| `needs_refinement` | Not ready. Default for every new issue.                                 | Maintainer / refinement agent |
| `agent-ready`      | Meets the Definition of Ready below. Safe to implement unattended.      | Refinement agent / maintainer |
| `needs_human`      | Can't be made agent-ready: an open design decision, a real Windows or macOS machine, an account or a secret. The blocking question is in a comment. | Refinement agent |

An issue carries exactly one of the three. Tracker issues that only group sub-issues (e.g. #21 session transfer)
carry `needs_refinement` until they are split, and are never implemented directly.

## Definition of Ready

An issue is `agent-ready` only when **all** of these hold:

1. **One PR.** A single deliverable that fits one reviewable PR. Bigger work is split into sub-issues of the
   tracker; the original becomes the tracker or is closed.
2. **No open decisions.** Every "decide here", "e.g.", "or" and "TBD" is resolved in the body: command and flag
   names, endpoint paths, wire types, config keys, defaults.
3. **Dependencies are merged.** Every blocking issue is closed. Unmerged blockers → stay `needs_refinement`.
4. **Doable in a Linux cloud container.** The change builds and is tested there: `make vet` covers windows and
   darwin compilation, `make test` runs the e2e suite with `/bin/sh` standing in for claude. Behaviour only a
   real Windows or macOS machine can confirm is listed under **Manual checks** with exact steps. An issue whose
   core can't be written without such a machine is `needs_human`.
5. **Grounded in the code as it is today.** Context names real files and symbols, and the issue isn't already
   done (check `git log` and the code before refining).
6. **Verifiable.** Each acceptance criterion maps to a Go test (`e2e_test.go` for endpoints and control
   messages), an npm test (`npm/test/`), a command an agent can run, or a manual check.
7. **Bounded.** Out-of-scope is listed explicitly, including any tempting adjacent work.
8. **Rules intact.** The change keeps the `CLAUDE.md` rules: `CGO_ENABLED=0` on all three OSes, the agent never
   takes the executable from the API, one writer per websocket, slow viewers are dropped. An issue that needs to
   break one is `needs_human`.

## Refined issue format

Refined issues use this body. Keep it short: link to code and docs instead of copying them. A section with
nothing to say reads "none" rather than being dropped.

```markdown
Part of #<tracker>   (omit if standalone)

## Goal
One or two sentences: what a user of `clawsh` (CLI, web UI or plugin) can do afterwards, and why.

## Context
- Current behaviour, with paths: `internal/agent/server.go` (`Server.attach`), `cmd/clawsh/main.go` (`runNew`)
- Protocol sections touched: top of `internal/api/types.go`, `docs/wiki/Protocol.md`
- Related issues/PRs and their state (merged / closed)

## Scope
**In**
- Concrete change 1

**Out**
- What not to touch, even if tempting

## Decisions
Names fixed by this issue: subcommands, flags, endpoints, `api` types, control messages, config keys, defaults.
Dependencies allowed: none / `<module>@<version>`.

## Acceptance criteria
- [ ] Observable, testable statement
- [ ] `make vet` and `make test` pass
- [ ] `docs/wiki` updated if commands, flags or the protocol change

## Verification
- `make vet`, `make test`, `node --test npm/test/*.test.mjs` (if `npm/` changes)
- New/updated test: `go test -race -run <TestName> ./internal/<pkg>`

## Manual checks
Steps on a real Windows/macOS machine or in a browser that an agent can't run (see `docs/PLAN.md`). "none" if
there is nothing.

## Dependencies
Blocked by: none
```

## Rules for the implementation agent

These apply to every `agent-ready` issue; the issue body doesn't need to repeat them.

- Read `CLAUDE.md` first and follow its layout, architecture and rules.
- Skip the issue while any issue under **Dependencies** is open.
- Branch `<type>/<number>/<short-title>` off the latest `main` (`feat`, `fix`, `chore` or `doc`), e.g.
  `fix/12/conpty-handle-after-close`. Conventional Commits.
- No Claude attribution in commits, PRs or comments (see `CLAUDE.md`).
- Open a PR whose body says `Closes #<number>`, maps each acceptance criterion to its test or check, and lists
  the issue's **Manual checks** for a human.
- Green before pushing: `make vet`, `make test`, and `node --test npm/test/*.test.mjs` when `npm/` changes.
- New endpoints or control messages: types in `internal/api` first, then a test in `e2e_test.go`.
- Update `docs/wiki` when commands, flags or the protocol change.
- Stay inside **Scope**. Found something else? Open a new issue labelled `needs_refinement`; don't widen the PR.
- Don't add dependencies the issue doesn't allow.
- Never skip, disable or loosen a test to get green.
- Blocked or the issue turns out wrong? Stop, comment on the issue with the exact question, remove `agent-ready`
  and any `stage:` label, add `needs_refinement` if the issue itself is wrong or incomplete or `needs_human`
  otherwise (the **Stop** section of [`sdlc-pipeline.md`](sdlc-pipeline.md)), and don't open a half-done PR.

## Rules for the refinement agent

- Input: an open issue labelled `needs_refinement`.
- Read the issue, its comments, its tracker, linked issues, `docs/PLAN.md`, the relevant `docs/wiki` pages and
  the code it touches.
- If it's stale or already done, comment with the evidence and close it (or ask, if unsure).
- If it's too big, split it into sub-issues (each labelled `needs_refinement` or `agent-ready`) under the same
  tracker.
- Rewrite the body in the format above; keep the original user story as the **Goal** if there is one.
- If every Definition of Ready item holds: swap `needs_refinement` for `agent-ready`.
- Otherwise: swap it for `needs_human` and comment with the one decision or resource a person must supply.
