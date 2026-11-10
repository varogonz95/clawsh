# OpenSpec in the SDLC pipeline: research and refactor proposal

**Status:** proposal, for the maintainer to decide (Open decisions at the end).
**Applies to:** `docs/sdlc-pipeline.md`, `docs/issue-refinement.md`,
`docs/superpowers/plans/2026-10-29-sdlc-automation.md`, tracker #118 and its sub-issues #119–#129.

## 1. What OpenSpec is (as of 1.17.0, 2026-11-09)

Facts from the official docs at `github.com/Fission-AI/OpenSpec/docs` (MIT, npm `@fission-ai/openspec`, Node
≥ 22.12):

- **Not a Claude Code plugin package.** `openspec init --tools claude` writes skills to
  `.claude/skills/openspec-*/SKILL.md` and slash commands to `.claude/commands/opsx/<id>.md`, committed with the
  repo. `openspec update` regenerates them after an upgrade.
- **Two trees.** `openspec/specs/<capability>/spec.md` is the source of truth for current behaviour.
  `openspec/changes/<change-id>/` holds one proposed change: `.openspec.yaml` (schema, `skip_specs`,
  `retire_capabilities`, and any `x-*` keys of ours), `proposal.md`, delta specs in `specs/<capability>/spec.md`,
  `design.md`, `tasks.md`, plus whatever a custom schema adds.
- **Spec format.** `### Requirement:` with SHALL/MUST/SHOULD, each with `#### Scenario:` GIVEN/WHEN/THEN. Deltas
  use `## ADDED|MODIFIED|REMOVED|RENAMED Requirements`.
- **Commands.** Default profile: `/opsx:explore`, `/opsx:propose`, `/opsx:apply`, `/opsx:update`, `/opsx:sync`,
  `/opsx:archive`. Expanded profile (`openspec config profile`, then `openspec update`): `/opsx:new`,
  `/opsx:continue`, `/opsx:ff`, `/opsx:verify` (completeness, correctness, coherence; CRITICAL / WARNING /
  SUGGESTION), `/opsx:bulk-archive`, `/opsx:onboard`.
- **CLI for scripts.** `list`, `show`, `validate [--all|--changes|--specs] --strict --json`, `status --change <id>
  --json`, `instructions <artifact|apply|archive> --change <id> --json`, `new change <id>`,
  `archive <id> --yes` (required with no TTY: CI and agents). `validate --archived` checks archived changes have
  every task ticked. A change with no spec deltas fails validation unless `.openspec.yaml` sets `skip_specs: true`.
- **Customisation.** `openspec/config.yaml`: default `schema`, `context` (injected into every artifact prompt),
  per-artifact `rules`, `operations.apply|archive.guidance`. Custom schemas in `openspec/schemas/<name>/`
  (`schema.yaml` + `templates/`): artifacts with `generates`, `instruction`, `template`, `requires`, and an
  `apply` block (`requires`, `tracks: tasks.md`). `openspec schema fork spec-driven <name>`,
  `openspec schema validate <name>`.
- **Gates are advisory.** `requires` only checks that artifact files exist; `/opsx:verify` doesn't block archive.
  Enforcement has to come from CI or hooks. (The `anvil` community schema says the same about its review gate.)
- **Close community schemas.** `anvil`: proposal → specs → design → **review** (fresh-context, read-only reviewer
  emitting `VERDICT: APPROVE | APPROVE_WITH_CHANGES | REVISE`) → **test-plan** (every scenario mapped to a named
  test, red/green ledger) → tasks → apply → **verify**. `intent-driven`: adds an `adr` artifact for durable,
  supersedable decisions.

## 2. Why it fits

Most of what #119–#129 would build by hand already exists in OpenSpec:

| Our pipeline concept (`sdlc-pipeline.md`, plan)        | OpenSpec equivalent                                                    |
| ------------------------------------------------------ | ---------------------------------------------------------------------- |
| Plan schema (Goal, Decisions, Interfaces, Files…)      | change artifacts: `proposal.md`, `design.md`, `tasks.md`               |
| Acceptance criteria → tests map                        | delta-spec `#### Scenario:`s + a `test-plan` artifact (anvil pattern)  |
| Plan critic before the human gate                      | `review` artifact with a `VERDICT` line (anvil pattern)                |
| Semantic plan-drift reviewer                           | `/opsx:verify` (completeness, correctness, coherence)                  |
| Plan amendments                                        | `/opsx:update` (small); a new change (intent changes)                  |
| Plan validator (#121)                                  | `openspec validate --strict --json` + `openspec schema validate`       |
| Rules digest injected per stage (#128)                 | `config.yaml` `context` and per-artifact `rules`                       |
| Long-term behaviour memory                             | `openspec/specs/` (living specs), `changes/archive/` (history)         |
| Decision log (#120)                                    | `adr` artifact (intent-driven pattern), copied to `docs/decisions/`    |

What OpenSpec does **not** provide, and stays ours: the event-driven orchestrator, triggers, actor filter, claims,
counters and stop rules; deterministic gates (format, vet, staticcheck, govulncheck, diff coverage, test lock);
enforcement of every gate; findings and lessons storage; the PR fix loop.

## 3. Target design

### 3.1 Repository layout

```
openspec/
  config.yaml                  schema: clawsh; context: points at CLAUDE.md rules; per-artifact rules
  schemas/clawsh/              custom schema (forked from spec-driven, anvil-style review + test-plan)
    schema.yaml
    templates/{proposal,spec,design,threat-model,review,test-plan,tasks,verify}.md
  specs/<capability>/spec.md   living specs (baseline seeded, see 3.5)
  changes/<change-id>/         one per issue; id = branch title (e.g. fix-conpty-handle-after-close)
  changes/archive/             history
.claude/skills/openspec-*/     generated by `openspec init --tools claude`
.claude/commands/opsx/         generated
```

### 3.2 The `clawsh` schema

Artifacts, in order (`requires` in brackets):

1. `proposal` []: Goal, Context, Scope In/Out (today's refined-issue sections), `Closes #<n>`.
2. `specs` [proposal]: delta specs; acceptance criteria become `#### Scenario:`s. Docs/tooling-only changes set
   `skip_specs: true`.
3. `design` [proposal, specs]: Decisions, Interfaces (`internal/api` types first), File changes, Risks.
4. `threat-model` [design]: trust boundaries touched (agent token, WebSocket, web access key, fixed executable,
   npm checksum), with "none" allowed.
5. `review` [design, threat-model]: the plan critic. Fresh context, read-only, checks against the issue, the
   `CLAUDE.md` rules, `openspec/specs/`, ADRs and other open changes. Ends with `VERDICT:`.
6. `test-plan` [review]: every scenario → named test (`go test -run …`, `e2e_test.go`, `npm/test/…`) or a manual
   check.
7. `tasks` [test-plan]: tasks, with acceptance tests first (test-first).
8. `verify` [tasks]: written after apply by `/opsx:verify` plus our review findings summary.

`apply: { requires: [tasks], tracks: tasks.md }`.

Machine-readable plan fields our scripts need go in `.openspec.yaml` `x-*` keys, which OpenSpec allows:
`x-issue`, `x-files` (globs the change may touch), `x-dependencies-allowed`, `x-risk` (`high` when a trust
boundary or dependency changes).

### 3.3 Stage mapping

| Stage (`sdlc-pipeline.md`) | OpenSpec step                                                     | Our enforcement on top                                                       |
| -------------------------- | ----------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| 1–2 Input, Refinement      | `/opsx:explore` while refining; issue stays the intake record      | Definition of Ready unchanged; `agent-ready` trigger unchanged               |
| 3 Planning                 | `openspec new change <id>`, then `/opsx:ff` (or `/opsx:continue`) up to `test-plan`; `review` by the plan-critic agent | `openspec validate <id> --strict`; orchestrator reads `VERDICT`; REVISE loops to the Planner (max 2) |
| Human gate                 | draft PR containing only `openspec/changes/<id>/`                  | `/approve` or `/changes` comment from the maintainer (actor filter)           |
| 4 Implementation           | tester writes scenario tests from `test-plan`; `/opsx:apply` ticks `tasks.md` | `make check`, test lock on the test-plan's tests, drift check vs `x-files`  |
| Amendments                 | `/opsx:update` for small revisions; intent change → back to Planning | re-approval when `design` Decisions or delta specs change                   |
| 5 Review                   | `/opsx:verify` = the plan-drift reviewer                           | other reviewers (security, coverage, architecture…) as today; CRITICAL blocks |
| 6 Testing                  | E2E per scenario from `test-plan`                                  | failing scenario = bug; triage classifies implementation vs plan             |
| 7 PR                       | after green: `openspec archive <id> --yes` in the same PR          | CI: `openspec validate --all --strict`, `--archived`; fix loop unchanged     |
| Retro                      | lessons → `config.yaml` `rules` proposals                          | promotion still needs the maintainer                                         |

Archiving inside the PR keeps `openspec/specs/` and the code in step: they merge together.

### 3.4 Memory system, revised

| Layer           | Before (plan)                         | With OpenSpec                                                      |
| --------------- | ------------------------------------- | ------------------------------------------------------------------ |
| Rules           | `CLAUDE.md`                           | `CLAUDE.md` + `config.yaml` `context`/`rules` (no duplication: context points to `CLAUDE.md`) |
| Behaviour       | code index                            | `openspec/specs/` (living specs); code index becomes optional       |
| Decisions       | `docs/decisions/`                     | same, written from the `design`/`adr` decisions on archive          |
| Plan            | `pipeline-state` `plan-vN.md`         | `openspec/changes/<id>/` in the PR branch; versions = git history   |
| Runtime state   | `pipeline-state` task record          | unchanged: counters, claims, findings, handoffs, lessons            |

The context builder (#128) assembles prompts from `openspec instructions <artifact> --change <id> --json` (which
already injects `context` and `rules`), the relevant `openspec/specs/` (by `x-files` overlap), the previous
handoff, findings and lessons.

### 3.5 Baseline specs

`openspec/specs/` starts empty. Seed the capabilities most changes touch, one spec each, written from the code and
`docs/wiki`: `protocol` (`internal/api`), `agent-sessions` (lifecycle, attach, scrollback), `web-auth`,
`hub-attach`, `npm-launcher`. Other capabilities get their spec the first time a change touches them (OpenSpec's
"progressive rigor").

## 4. Refactor of the filed work

| Issue | Change                                                                                                     |
| ----- | ---------------------------------------------------------------------------------------------------------- |
| new   | **OpenSpec bootstrap**: `openspec init --tools claude` (expanded profile), `config.yaml`, `clawsh` schema + templates, CI job `openspec validate --all --strict` and `validate --archived` (Node 22 pinned) |
| new   | **Baseline specs** for the five capabilities in 3.5                                                        |
| #119  | Rewrite `sdlc-pipeline.md` stages around section 3.3; `issue-refinement.md` hands the refined issue to `proposal` |
| #120  | Keep `docs/decisions/`; the template matches the schema's decision format; archive copies accepted decisions |
| #121  | Shrinks: no plan schema (OpenSpec validates); JSON schemas only for findings, handoff, task record, lesson  |
| #122  | Unchanged (`make check`)                                                                                   |
| #123  | Unchanged (diff coverage)                                                                                  |
| #124  | Reads `x-files` and `x-dependencies-allowed` from `.openspec.yaml` instead of `plan.json`                  |
| #125  | Locks the tests named in `test-plan.md`                                                                    |
| #126  | Shrinks: no plan versions; keeps claims, counters, findings, handoffs, lessons                             |
| #127  | Deprioritised: living specs cover most of what agents needed from it                                       |
| #128  | Sources: `openspec instructions/show --json`, relevant specs, handoff, findings, lessons                   |
| #129  | Promotion target: a `config.yaml` rule (per artifact) or a `CLAUDE.md` rule, maintainer's choice            |
| Phase 3 | Agents wrap `/opsx:*` skills: planner (`ff`), plan critic (`review`), writer (`apply`), drift reviewer (`verify`) |
| Phase 4 | Orchestrator gates on `openspec validate`, `status --json`, the `VERDICT` line and verify's CRITICAL count |

Order: bootstrap → #119 → baseline specs → the rest as planned. #122 and #123 don't depend on OpenSpec and can go
first.

## 5. Risks

- **Advisory gates.** Every OpenSpec gate is enforced by our orchestrator or CI, never assumed.
- **Generated files drift.** `.claude/skills/openspec-*` and commands change with `openspec update`; pin the CLI
  version in CI and regenerate in a dedicated PR on upgrade.
- **Node 22 in CI.** Only for tooling; product code and `go.mod` are unaffected.
- **Context cost.** Six skills add prompt weight; load only the ones a stage needs (each agent's tool list).
- **Spec rot.** Living specs are only as good as archive discipline; `validate --archived` and the drift reviewer
  keep them honest.

## 6. Open decisions

1. **Schema base**: fork `spec-driven` and add our artifacts (recommended), or vendor `anvil` and adapt (check its
   licence first).
2. **Where the plan waits for approval**: a draft PR with only the change folder (recommended), or an issue
   comment.
3. **Baseline specs**: seed the five now (recommended) or only as changes touch them.
4. **Archive timing**: inside the PR before merge (recommended) or in a follow-up PR.
