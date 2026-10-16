# Development

## Commands

```sh
make build        # ./clawsh
make test         # go test -race -timeout 3m ./...
make vet          # go vet for linux, windows and darwin
make dist         # cross-compile into dist/
make docker-dist  # same, inside Docker
```

Run one test:

```sh
go test -race -run TestReplayOnLateAttach ./internal/agent
```

The end-to-end tests (`internal/agent/e2e_test.go`) start a real agent over HTTP/WebSocket with `/bin/sh` standing in for claude. They're skipped on Windows, which needs a manual check (see `docs/PLAN.md`).

For a manual loop, run `./clawsh agent --claude /bin/sh` and point a `hosts.toml` at `http://localhost:7420`.

## Releases

`clawsh version` prints the version baked in at build time. The Makefile takes it from the latest `v*` tag (`v0.1.0` → `0.1.0`, later commits `0.1.0-3-gabc1234`); before the first tag it's `dev+<commit>`. Override with `make dist VERSION=1.2.3`. A plain `go build` from a checkout reports `dev+<commit>` too (Go records the commit in the binary), and `dev` when there's no git information. `clawsh hosts` shows release versions with a `v` prefix and dev builds as they are.

To publish, run the `release-prepare` workflow (Actions tab, or `gh workflow run release-prepare.yml -f version=0.2.0`). It takes the version (`0.2.0`, or `0.2.0-rc.1` for a pre-release) and optionally a commit on `main` to release (default: the tip). Its `major.minor.patch` must be higher than that of every stable release (pre-release tags aren't compared), and the commit newer than the last release on `main`. It bumps `plugin/.claude-plugin/plugin.json` on top of that commit, merges the result into `main` (a fast-forward when releasing the tip) and pushes `main` and the `v<version>` tag together, so either both land or nothing is published. It needs a secret `RELEASE_TOKEN` (a fine-grained token with Contents: read and write that may push to `main`, plus Workflows: read and write, which GitHub requires to push commits that change `.github/workflows/`), because tags pushed with the default `GITHUB_TOKEN` don't start other workflows. The secret lives in the `release` environment, which both `release-prepare` and `release-hotfix` run in, so `main` must be allowed to deploy to it.

`release-prepare` refuses to tag when nothing since the last release on `main` touches a product path, so docs-only or workflow-only changes don't produce a release. The paths are listed in `.github/product-paths` (`cmd/`, `internal/`, `npm/`, `plugin/`, the Go module files, `Makefile`, `Dockerfile`, `LICENSE` and the installers; `plugin.json` version bumps don't count); tick `allow_non_product` to override. Docs and the site deploy from `main` through `pages.yml` and don't need a release.

To ship only chosen commits as a patch on an earlier release, run `release-hotfix` (`gh workflow run release-hotfix.yml -f version=0.1.1 -f commits="<sha> <sha>"`). It checks out `release/<major>.<minor>` (created from the newest `v<major>.<minor>.*` tag the first time), cherry-picks the commits from `main` with `-x`, bumps `plugin.json`, then pushes the branch and the tag together. Its `major.minor.patch` must be higher than the newest stable release on that line, and merge commits are refused. If `release/<major>.<minor>` exists but lacks that release (it was cut from `main` with `release-prepare`), the run stops; merge the tag into the branch by hand first. A line whose release branch starts from a tag older than the Latest/dist-tag handling below has a `release.yml` that would publish the hotfix as GitHub's Latest and npm's `latest`, so the run stops until you add the commit on `main` that changed `release.yml` to `commits`; picking a commit that changes `.github/workflows/` needs the token's Workflows permission. A commit already on the branch is skipped; a conflicting pick stops the run with the commit and files named, so resolve it by hand on the branch. The bump stays on the release branch: `main`'s `plugin.json` is set at its next `release-prepare`. For new features use `release-prepare` instead; cherry-picking features pulls in untested combinations. Both workflows share one concurrency group: one release runs at a time and at most one waits, and a newer dispatch cancels a waiting one, so dispatch the next release after the previous run finishes.

To do the same by hand, bump `plugin/.claude-plugin/plugin.json` to the new version (the release fails if it doesn't match the tag), then push a tag:

```sh
git tag v0.1.0 && git push origin v0.1.0
```

`.github/workflows/release.yml` runs the CI checks (`make vet`, `make test`, the npm launcher tests), cross-compiles with `make dist`, packages one archive per platform plus `checksums.txt`, and creates the GitHub release with generated notes (if a release for the tag already exists, e.g. made by hand, it uploads the assets to that one instead). Then it publishes the same binaries to npm: `npm/build.mjs` stages one `clawsh-<os>-<cpu>` package per platform plus the `clawsh` launcher (`npm/clawsh/`), all at the tag's version. Tags with a suffix (`v0.1.0-rc.1`) become pre-releases on GitHub and the `next` dist-tag on npm. A release on an older line (a hotfix `0.1.1` after `0.2.0`) is not marked "Latest" on GitHub and goes to the npm dist-tag `release-0.1` (`next-0.1` for its pre-releases), so `latest` and `next` stay on the newest line. `.github/workflows/ci.yml` runs the same checks on every push to `main` and on pull requests, except those that only touch Markdown, `docs/`, `site/`, `.claude/` or `LICENSE`.

### npm trusted publishing

npm publishing uses [trusted publishing](https://docs.npmjs.com/trusted-publishers): the workflow proves who it is with GitHub's OIDC token, so there's no npm token secret, and every version gets a provenance statement. Each of the seven packages needs `varogonz95/clawsh` with workflow `release.yml` added as a trusted publisher on npmjs.com (package Settings → Trusted publishing). npm only lets you do that for a package that already exists, so the first time:

```sh
make dist VERSION=0.1.0
node npm/build.mjs 0.1.0
for d in out/npm/clawsh-*/ out/npm/clawsh/; do npm publish "$d" --access public; done   # logged in with npm login
```

then add the trusted publisher to each package. Later releases publish from CI; versions already on npm are skipped, so re-running the workflow finishes a partial publish.

To try the packages locally without publishing: `npm pack` the staged directories and `npm install` the tarballs into a scratch project.

## Branches

`<type>/<ticket>/<title>`, cut from the latest `main`:

- `type`: `feat`, `fix`, `chore` or `doc`
- `ticket`: GitHub issue number; leave the segment out when there's no issue
- `title`: short kebab-case summary

```
feat/6/web-lan-access
fix/12/conpty-handle-after-close
chore/release-workflow
```

## Docs

The user docs live in the repository, in `docs/wiki/` (`Home.md` is the index, `_Sidebar.md` the page list and order); there's no separate GitHub wiki. They're published on the site at https://varogonz95.github.io/clawsh/docs/ and also read fine on GitHub as plain Markdown. Link between pages with relative `.md` links (`[Commands](Commands.md#flags)`) and change them in the same PR as the code they describe.

`.github/workflows/pages.yml` builds the site with `scripts/docs/build.mjs`: it copies `site/` and renders each page into `docs/<page>.html` with the site's look (`site/docs.css`), using a vendored copy of [marked](https://github.com/markedjs/marked) (`scripts/docs/vendor/`), so there's nothing to install. A link to a missing page or heading fails the build; pull requests that touch the docs or the site run the build too. To preview:

```sh
node scripts/docs/build.mjs            # writes out/site
python3 -m http.server -d out/site     # then open http://localhost:8000/docs/
```

## Layout

| Path | |
|---|---|
| `cmd/clawsh` | CLI entrypoint, every subcommand |
| `internal/api` | Wire types shared by agent and hub |
| `internal/ptyx` | PTY abstraction: creack/pty on Unix, ConPTY on Windows |
| `internal/agent` | Session manager, HTTP/WebSocket server, token, scrollback |
| `internal/hub` | `hosts.toml`, agent client, terminal attach |
| `internal/web` | `clawsh web`: loopback server, SSE overview, attach bridge, embedded UI in `static/` |
| `internal/mcp` | `clawsh mcp`: MCP stdio stub run by the plugin; spawns an agent, announces the session |
| `internal/paths` | Per-user config dir (`<config>/clawsh`, falling back to `<config>/ccm`) |
| `npm/` | npm launcher package, `build.mjs` to stage release packages, launcher tests |
| `plugin/` | The Claude Code plugin (`.mcp.json` runs `clawsh mcp`) |

## Rules

- Everything builds for Windows, Linux and macOS with `CGO_ENABLED=0`. OS-specific code goes in `_windows.go` / `_unix.go` / `_other.go` files with build tags. Run `make vet` after touching them.
- The agent never takes the executable from the API, only args.
- gorilla/websocket allows one writer per connection. Keep the single-writer pattern.
- A slow viewer is dropped, never allowed to block a session.
- Keep dependencies minimal; prefer the standard library.
- New endpoints or control messages: add types to `internal/api` first, then a test in `e2e_test.go`.
