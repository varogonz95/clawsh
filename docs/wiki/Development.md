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

Releasing takes three steps, and only the first one is manual:

1. **Cut**: run the `release-cut` workflow (Actions tab, or `gh workflow run release-cut.yml`). It tags a commit on `main` as `v<version>`, by default the tip (`-f ref=<sha>` to stop at an earlier commit). Nothing is committed: the version lives only in the tag.
2. **Build**: the tag starts `release.yml`, which runs CI, builds every platform with `make dist` and publishes the archives and `checksums.txt` as a GitHub release.
3. **Publish**: the same run then publishes the npm packages.

`release-cut` computes the version from the last stable release on `main` and the pull requests merged since then that changed the product: the largest of their `release:major` / `release:minor` / `release:patch` labels picks the bump (a PR without one counts as a patch). `-f bump=patch|minor|major` forces the bump, and `-f version=0.4.0` (or `0.4.0-rc.1` for a pre-release) sets the version outright. The version's `major.minor.patch` must be higher than every stable release (pre-release tags aren't compared). It refuses when nothing since the last release touches a product path, so docs-only or workflow-only changes don't produce a release. The paths are listed in `.github/product-paths` (`cmd/`, `internal/`, `npm/`, `plugin/`, the Go module files, `Makefile`, `Dockerfile`, `LICENSE` and the installers); tick `allow_non_product` to override. Docs and the site deploy from `main` through `pages.yml` and don't need a release.

`release-cut` and `release-hotfix` use a secret `RELEASE_TOKEN` (a fine-grained token with Contents: read and write), because tags pushed with the default `GITHUB_TOKEN` don't start other workflows. The secret lives in the `releases` environment, which both run in, so `main` must be allowed to deploy to it.

`.github/workflows/pr-labels.yml` labels every pull request `product` or `non-product` from the files it changes (the same `.github/product-paths`), and resets that label on each push, so don't edit it by hand. Give every product PR one of `release:patch`, `release:minor` or `release:major`: the check warns when a product PR has none and fails when it has two.

The Claude Code plugin's `plugin.json` has no `version`: Claude Code then versions the plugin by the commit of its `plugin/` folder, so users get a plugin change as soon as it's on `main`, without a release.

To ship only chosen commits as a patch on an earlier release, run `release-hotfix` (`gh workflow run release-hotfix.yml -f version=0.1.1 -f commits="<sha> <sha>"`). It checks out `release/<major>.<minor>` (created from the newest `v<major>.<minor>.*` tag the first time), cherry-picks the commits from `main` with `-x`, then pushes the branch and the tag together. Its `major.minor.patch` must be higher than the newest stable release on that line, and merge commits are refused. If `release/<major>.<minor>` exists but lacks that release (it was cut from `main` with `release-cut`), the run stops; merge the tag into the branch by hand first. A line whose release branch starts from a tag older than the Latest/dist-tag handling below has a `release.yml` that would publish the hotfix as GitHub's Latest and npm's `latest`, so the run stops until you add the commit on `main` that changed `release.yml` to `commits`; picking a commit that changes `.github/workflows/` needs the token's Workflows permission. A commit already on the branch is skipped; a conflicting pick stops the run with the commit and files named, so resolve it by hand on the branch. For new features use `release-cut` instead; cherry-picking features pulls in untested combinations. Both workflows share one concurrency group: one release runs at a time and at most one waits, and a newer dispatch cancels a waiting one, so dispatch the next release after the previous run finishes.

To do the same by hand, push a tag:

```sh
git tag v0.1.0 && git push origin v0.1.0
```

`.github/workflows/release.yml` runs the CI checks (`make vet`, `make test`, the npm launcher tests), cross-compiles with `make dist`, packages one archive per platform plus `checksums.txt`, and creates the GitHub release with generated notes (if a release for the tag already exists, e.g. made by hand, it uploads the assets to that one instead). Then it publishes the same binaries to npm: `npm/build.mjs` stages one `clawsh-<os>-<cpu>` package per platform plus the `clawsh-cli` launcher (`npm/clawsh/`; npm refuses the name `clawsh`, and the command it installs is still `clawsh`), all at the tag's version. Tags with a suffix (`v0.1.0-rc.1`) become pre-releases on GitHub and the `next` dist-tag on npm. A release on an older line (a hotfix `0.1.1` after `0.2.0`) is not marked "Latest" on GitHub and goes to the npm dist-tag `release-0.1` (`next-0.1` for its pre-releases), so `latest` and `next` stay on the newest line. `.github/workflows/ci.yml` runs the same checks on every push to `main` and on pull requests, except those that only touch Markdown, `docs/`, `site/`, `.claude/` or `LICENSE`.

`.github/workflows/claude.yml` has Claude review each pull request (when it's opened, on every push, when it leaves draft, and when it's assigned to Claude) with inline comments, and answer `@claude` mentions in PR and issue comments. It needs the Claude GitHub App on the repo and a `CLAUDE_CODE_OAUTH_TOKEN` secret (from `claude setup-token`). Pull requests from forks aren't reviewed.

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

`.github/workflows/pages.yml` builds the site with `scripts/docs/build.mjs`: it copies `site/` and renders each page into `docs/<page>.html` with the site's look (`site/docs.css`), using a vendored copy of [marked](https://github.com/markedjs/marked) (`scripts/docs/vendor/`), so there's nothing to install. A link to a missing page or heading fails the build; pull requests that touch the docs or the site run the build too. The last item of every page's nav is the newest release (the highest `vX.Y.Z` tag): `release.yml` runs `pages.yml` after publishing a release that becomes Latest, so the label follows each release. In CI the build fails if it finds no tag; a local build without tags just leaves the label out. To preview:

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
