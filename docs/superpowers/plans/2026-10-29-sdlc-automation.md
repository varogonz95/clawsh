# SDLC Automation Implementation Plan

> **For agentic workers:** implement one task per PR, in order within a phase. Each task becomes a GitHub issue in
> the `docs/issue-refinement.md` format before it is implemented. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Turn `docs/sdlc-pipeline.md` into a running pipeline that takes an `agent-ready` issue to a green,
reviewed PR with one human gate (plan approval), tuned for token efficiency, code quality, security, coverage,
correctness and plan integrity, with a memory system that gives every agent a fresh, small, current context.

**Architecture:** Event-driven GitHub Actions. One workflow (`pipeline.yml`) receives issue, comment, review and
`workflow_run` events and hands them to a deterministic orchestrator (`scripts/pipeline/orchestrator.py`) that
decides the next stage, enforces counters, limits and trust, and updates labels and the status comment. Each stage
runs `anthropics/claude-code-action` with a subagent from `.claude/agents/` and a prompt built by
`scripts/pipeline/context.py` from the memory stores. Deterministic gates (format, vet, staticcheck, govulncheck,
diff coverage, drift, acceptance-test lock) run before any agent reads a diff. Pipeline state and lessons live on an
orphan `pipeline-state` branch; lasting decisions live in `docs/decisions/`.

**Tech stack:** Python 3 stdlib for pipeline scripts (same as `.claude/hooks/attribution.py`), tested with
`unittest`. Go tools run pinned with `go run <module>@<version>` so `go.mod` gains nothing. GitHub Actions,
`anthropics/claude-code-action` pinned by commit SHA. GitHub REST only (GraphQL is blocked in cloud sessions).

**Spec:** `docs/sdlc-pipeline.md` plus the review of 2026-10-28 (severity rubric, plan schema, plan critic,
test-first, diff coverage, amendments, memory system).

## Global constraints

- No new Go module dependencies, no npm dependencies, no Python packages outside the stdlib.
- Agents never get `RELEASE_TOKEN` or the `releases` environment. Pipeline jobs use `PIPELINE_BOT_TOKEN` (bot
  account) to push and `ANTHROPIC_API_KEY` (or `CLAUDE_CODE_OAUTH_TOKEN`) for Claude.
- Runs are triggered only by the maintainer's events and CI results, never by the bot's own comments, reviews or
  labels; everything else is data. Counters live in the task record, written only by the orchestrator.
- Only same-repository branches, never forks: comment and review events check `head.repo.full_name` over REST.
- No Claude attribution anywhere (`CLAUDE.md`); the existing attribution hook and workflow stay in force.
- Every agent output is JSON validated against a schema in `pipeline/schemas/`; an invalid output is retried
  once, then the stage stops.
- Every script has unit tests; the orchestrator's state machine has a table test per edge of the flow diagram.
- `docs/sdlc-pipeline.md` stays the source of truth for behaviour; a task that changes behaviour updates it.

## Decisions (defaults; change before Phase 3 if needed)

| Decision                      | Default                                                                              |
| ----------------------------- | ------------------------------------------------------------------------------------ |
| Where agents run              | GitHub Actions only                                                                  |
| Claude credential             | `ANTHROPIC_API_KEY` secret                                                           |
| Plan approval                 | maintainer comments `/approve` or `/changes <text>` on the issue                     |
| Diff-coverage threshold       | 80% of changed Go lines; 100% of changed lines in `internal/agent`, `internal/web` auth |
| Model tiers                   | strong: planner, architect, plan-critic, security, writer; standard: other reviewers, tester; small: triage, status, retro |
| Default budgets per stage     | planning 400k tokens / 30 min; implementation 600k / 45 min; each reviewer 80k / 10 min; testing 300k / 30 min; fix round 150k / 15 min |
| Follow-up issues per task     | at most 3                                                                            |
| Lesson expiry                 | 90 days unless re-confirmed; promoted to a `CLAUDE.md` proposal after 3 confirmations |
| Claim label                   | `stage:*` labels; the unused GitHub label `agent:on_it` is deleted                   |

## Prerequisites (maintainer, manual)

- [ ] Add secrets `ANTHROPIC_API_KEY` and `PIPELINE_BOT_TOKEN` (fine-grained PAT of the bot account: contents,
      issues, pull requests: read/write on this repo only).
- [ ] Enable secret scanning and push protection in repository settings.
- [ ] Keep `main` protected (PRs only); leave `pipeline-state` writable by the bot.
- [ ] Set the repository variable `PIPELINE_MAINTAINER` to the maintainer's login.

## File structure

```
.claude/agents/              subagent definitions (one .md per role: model, tools, output schema)
.github/workflows/pipeline.yml
docs/decisions/              README.md, 0000-template.md, NNNN-*.md (ADRs)
docs/sdlc-pipeline.md        behaviour (updated in Task 0.1)
pipeline/schemas/            plan, finding, handoff, task, lesson, review-routing (JSON Schema)
pipeline/prompts/            per-stage prompt templates
pipeline/review-routing.json path globs → reviewer dimensions
scripts/pipeline/            checks.sh, diffcov.py, drift.py, testlock.py, schema.py, state.py, context.py,
                             index.py, orchestrator.py, run_stage.py, ghapi.py, tests/
pipeline-state branch:       tasks/<issue>/{task.json,plan-vN.md,findings.jsonl,handoffs/,acceptance.json},
                             lessons.jsonl, index/<commit>.json
```

## Phase 0: Spec and schemas

### Task 0.1: Update the pipeline spec

- [ ] Fold the review into `docs/sdlc-pipeline.md`: severity rubric, plan schema, plan critic before the human
      gate, test-first acceptance tests and lock, diff coverage, mechanical drift check, amendments (small: logged;
      Decisions change: re-plan), path-routed and delta reviews, dual security review with a verifier, design
      comments on the PR route to Planning, testing-cycle count spans re-plans, concurrency group per issue,
      budgets and follow-up cap, memory system.
- [ ] Redraw the flow diagram (plan critic, test-first, amendment edge, PR comment routing).
- **Done when:** the doc covers every item above and the Mermaid block renders.

### Task 0.2: Decision log

- [ ] `docs/decisions/README.md` (when to write an ADR, statuses `proposed|accepted|superseded`, numbering) and
      `0000-template.md` (Context, Decision, Consequences, Paths, Supersedes).
- [ ] `0001-sdlc-pipeline-on-actions.md` recording the Architecture above.
- **Done when:** the template has a `Paths:` field the context builder can match.

### Task 0.3: Schemas and validator

- [ ] JSON Schemas in `pipeline/schemas/` for plan, finding, handoff, task record, lesson, review routing.
- [ ] `scripts/pipeline/schema.py`: stdlib validator for the subset used (type, required, enum, maxLength,
      maxItems, pattern), with tests per schema (valid sample, each required field missing, size limits).
- **Done when:** `python3 -m unittest discover scripts/pipeline/tests` passes in CI (new `pipeline` job in
  `ci.yml`, triggered on `scripts/pipeline/**` and `pipeline/**`).

## Phase 1: Deterministic gates

Useful on every PR on their own, before any agent exists.

### Task 1.1: `make check`

- [ ] `scripts/pipeline/checks.sh`: `gofmt -l` (fail on output), `make vet`, `staticcheck` and `govulncheck`
      at pinned versions via `go run`, `make test` with `-coverprofile`.
- [ ] `make check` target; CI runs it instead of the separate vet and test steps.
- **Done when:** CI fails on an unformatted file, a staticcheck finding and a known-vulnerable call (each shown
  with a throwaway commit in the PR description).

### Task 1.2: Diff coverage

- [ ] `scripts/pipeline/diffcov.py --base <ref> --profile cover.out [--rules pipeline/coverage.json]`: maps
      `git diff -U0` added lines in `.go` files (excluding `_test.go`) to coverprofile blocks; prints JSON
      (per-file and total %, uncovered line ranges); exits 1 under threshold.
- [ ] Per-path thresholds in `pipeline/coverage.json`.
- [ ] Tests with fixture diffs and profiles: new file, edited file, deleted lines only, no Go changes.
- **Done when:** CI posts the uncovered ranges in the job summary and fails under threshold.

### Task 1.3: Drift check

- [ ] `scripts/pipeline/drift.py --plan plan.json --base <ref>`: changed files outside `files_touched`, new
      modules in `go.mod` or packages in `package.json` not in `dependencies_allowed`, new exported identifiers in
      `internal/api` not listed in `interfaces`; prints findings JSON (severity from the rubric).
- **Done when:** tests cover each drift kind and an amendment that legitimately extends `files_touched`.

### Task 1.4: Acceptance-test lock

- [ ] `scripts/pipeline/testlock.py lock|verify`: records SHA-256 of the acceptance test files in
      `acceptance.json`; `verify` fails if any changed after the lock.
- **Done when:** tests cover lock, unchanged verify, edited file, deleted file.

## Phase 2: Memory system

### Task 2.1: State store

- [ ] `scripts/pipeline/state.py`: read and write the `pipeline-state` branch through a temporary `git worktree`;
      task record (stage, plan version, review round, testing cycle, fix round, budgets used, links), plan
      versions, `findings.jsonl`, handoffs, `acceptance.json`. Writes validate against schemas and retry on
      non-fast-forward (fetch, rebase, push, max 3). A claim is compare-and-swap: after a rejected push it
      re-reads the task record and gives up if another run claimed it meanwhile.
- [ ] Creates the orphan branch on first use.
- **Done when:** tests run against a local bare repo, including two concurrent writers to different tasks.

### Task 2.2: Code index

- [ ] `scripts/pipeline/index.py`: from `go list -json ./...` and `go doc -short`, packages, exported symbols,
      files, and which `_test.go` files exercise which package; cached as `index/<commit>.json`.
- **Done when:** the index for `main` lists `internal/agent` symbols and its tests; a second run is a cache hit.

### Task 2.3: Context builder

- [ ] `scripts/pipeline/context.py --stage <s> --issue <n>`: assembles the prompt in a fixed, cache-friendly
      order: stage rules (from `pipeline/prompts/`), `CLAUDE.md` digest, context pack, current plan, previous
      handoff, matching ADRs and lessons, then the changing parts (diff, findings).
- [ ] Selection by path overlap with the plan's `files_touched`; stale entries (recorded commit older than the
      last change to their paths) dropped unless re-confirmed; expired lessons dropped.
- [ ] Hard token budget per stage (estimate: chars / 4); lowest-priority sections trimmed first, never rules or
      plan.
- **Done when:** tests cover ordering, path selection, staleness, expiry and trimming; output is byte-identical
  for identical inputs (cache-friendly).

### Task 2.4: Lessons lifecycle

- [ ] Retro step writes candidate lessons (scope paths, rule, source issue and finding, commit, expiry) from the
      findings that caused a loop.
- [ ] Confirmation increments a counter; at 3 the orchestrator opens a `needs_refinement` issue proposing the
      `CLAUDE.md` rule.
- **Done when:** tests cover create, confirm, expire, promote.

## Phase 3: Agents

### Task 3.1: Subagent definitions

- [ ] `.claude/agents/`: `researcher`, `planner`, `architect`, `plan-critic`, `tester`, `writer`,
      `reviewer-drift`, `reviewer-integration`, `reviewer-security`, `security-verifier`, `reviewer-coverage`,
      `reviewer-architecture`, `reviewer-cloud`, `triage`, `ci-fixer`, `retro`.
- [ ] Each sets its model tier and least-privilege tools (reviewers, critic and verifier read-only; only
      `writer`, `tester` and `ci-fixer` edit files), and names its output schema.
- [ ] Prompt templates in `pipeline/prompts/`, including the severity rubric, the plan schema and the
      "reviewers don't see the Writer's reasoning" rule.

### Task 3.2: Stage runner

- [ ] `scripts/pipeline/run_stage.py`: builds context, invokes the stage's agent, validates JSON output (one
      retry), records tokens and duration in the task record, enforces the stage budget, writes the handoff.
- **Done when:** tests run it with a fake agent command: valid output, invalid then valid, invalid twice
  (stop), budget exceeded (stop).

## Phase 4: Orchestration

### Task 4.1: Orchestrator state machine

- [ ] `scripts/pipeline/orchestrator.py`: pure `next(state, event) -> (state, actions)`; actions are label
      changes, status comment upsert, stage runs, stop. Covers every edge of the flow diagram, counters (review
      round per cycle, testing cycle per issue across re-plans, fix round per PR), trust filter, stop summary
      plus `needs_human`. Stages chain inside one run; the orchestrator never waits for its own label events.
- [ ] Claim inside the per-issue concurrency group from the task record (no stage running), not from labels.
- [ ] `reconcile()`: for each open task record, handle comments, labels and PR checks newer than the last
      processed ids; runs at the end of every run and on schedule.
- [ ] `scripts/pipeline/ghapi.py`: REST helpers (labels, comments, PRs) with the bot token.
- **Done when:** table test per diagram edge and per stop condition; tests for a dropped `/approve` picked up
  by reconcile and for two runs racing for the same claim.

### Task 4.2: Workflow

- [ ] `.github/workflows/pipeline.yml`: the triggers in the **Triggers** table of `docs/sdlc-pipeline.md`
      (`issues: opened, labeled`, `issue_comment: created`, `pull_request_review: submitted`, `workflow_run` of
      `ci` completed, `schedule` every 30 minutes); a first step resolves the issue number (from the event, or
      for `workflow_run` from the head branch `<type>/<issue>/<title>`, falling back to the PR's `Closes #<n>`);
      `concurrency: pipeline-issue-<n>` without cancel; trust filter on the actor (bot events ignored);
      same-repo check for PR events; `PIPELINE_DRY_RUN` variable.
- [ ] Actor filter: comment, review and label events act only when the sender is `PIPELINE_MAINTAINER` with
      `admin`/`maintain` permission (REST); tested with a `/approve` from another account, which starts nothing.
- **Done when:** dry run on a test issue posts the status comment and the stage it would run.

### Task 4.3: Planning stage

- [ ] Researcher writes the context pack; Planner and Architect produce the plan (schema) and draft ADRs;
      plan critic checks issue, `CLAUDE.md` rules, ADRs, code and other open plans; failed critique loops to the
      Planner (max 2), then stop.
- [ ] Posts the plan (or, on re-plan, the diff against the last approved version) with the critic's report;
      `stage:plan-review`; `/approve` and `/changes` handling.

### Task 4.4: Test-first and implementation

- [ ] Tester writes failing acceptance tests from the criteria, commits them, `testlock lock`.
- [ ] Writer implements; `make check`, `testlock verify`, `drift.py` must pass; simplification pass; amendments
      recorded in the task record (small: logged; Decisions change: back to Planning).

### Task 4.5: Review stage

- [ ] `pipeline/review-routing.json` picks reviewers by changed paths; drift and security always run.
- [ ] Round 2 and 3 review only the delta plus the dimensions that blocked.
- [ ] High-risk changes (trust-boundary paths, dependency changes) get two independent security reviews; a
      security finding blocks only after `security-verifier` traces a realistic path.
- [ ] Low findings go to the PR body; at most 3 follow-up issues.

### Task 4.6: Testing stage

- [ ] E2E against acceptance criteria; manual checks listed for the PR; bugs posted with repro, failed criterion,
      logs and a failing regression test; `triage` classifies cause (implementation / plan) with a reason.

### Task 4.7: PR stage

- [ ] Opens the PR via REST (body per `issue-refinement.md` plus Follow-ups and Manual checks).
- [ ] `ci-fixer` on red CI; review comments routed by `triage`: small → `ci-fixer`, design → Planning.
- [ ] Fix rounds capped at 3.

### Task 4.8: Retro

- [ ] On PR merge or stop: `retro` writes lessons (Task 2.4) and closes out the task record.

## Phase 5: Rollout

### Task 5.1: Dry runs

- [ ] Run the pipeline in dry-run mode on 3 small `agent-ready` issues; compare its decisions with what the
      maintainer would do; fix the gaps.

### Task 5.2: Metrics

- [ ] Task record carries tokens, wall time, loops and findings by severity per stage; a monthly summary comment
      on a pinned tracking issue.

### Task 5.3: Go live

- [ ] Turn off dry run; tune budgets from the dry-run numbers.

## Order and parallelism

Phase 0 first. Phase 1 and Phase 2 can run in parallel. Phase 3 needs 0.3 and 2.3. Phase 4 needs everything
before it, in task order. Phase 5 last. The pipeline doesn't exist yet, so Phases 0–3 are implemented through
ordinary sessions; Phase 4 tasks can start dogfooding from Task 4.2's dry run.

## Risks

- **Actions and API cost:** budgets plus dry-run measurements before go-live.
- **`claude-code-action` input changes:** pinned by SHA; upgrades are their own PR.
- **`pipeline-state` write races:** per-issue concurrency group plus rebase-retry in `state.py`.
- **Dropped events:** GitHub keeps one pending run per concurrency group; reconcile at the end of every run and
  on schedule.
- **Prompt injection through issue text:** trust filter on actors; agents treat other text as data.
- **Flaky e2e tests causing loops:** a failure that doesn't reproduce on one re-run is reported, not looped on.
