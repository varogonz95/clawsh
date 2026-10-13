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

To publish, bump `plugin/.claude-plugin/plugin.json` to the new version (the release fails if it doesn't match the tag), then push a tag:

```sh
git tag v0.1.0 && git push origin v0.1.0
```

`.github/workflows/release.yml` runs the CI checks (`make vet`, `make test`, the npm launcher tests), cross-compiles with `make dist`, packages one archive per platform, the raw binaries and `checksums.txt`, and creates the GitHub release with generated notes (if a release for the tag already exists, e.g. made by hand, it uploads the assets to that one instead). Then it publishes the `clawsh` npm launcher (`npm/clawsh/`): `npm/build.mjs` stamps it with the tag's version and writes the SHA-256 of every binary into `lib/checksums.json`. The launcher holds no binaries; on first run it downloads its platform's raw binary from that GitHub release and checks it against those checksums, so the npm job runs only after the release assets are up. Tags with a suffix (`v0.1.0-rc.1`) become pre-releases on GitHub and the `next` dist-tag on npm. `.github/workflows/ci.yml` runs the same checks on every push to `main` and on pull requests, except those that only touch Markdown, `docs/`, `site/`, `.claude/` or `LICENSE`.

### npm trusted publishing

npm publishing uses [trusted publishing](https://docs.npmjs.com/trusted-publishers): the workflow proves who it is with GitHub's OIDC token, so there's no npm token secret, and every version gets a provenance statement. The `clawsh` package needs `varogonz95/clawsh` with workflow `release.yml` added as a trusted publisher on npmjs.com (package Settings → Trusted publishing). npm only lets you do that for a package that already exists, so the first time, with the GitHub release for that version already published (its raw binaries are what the checksums must match):

```sh
mkdir -p dist && gh release download v0.1.1 -p 'clawsh-*' -D dist   # the release's own binaries
node npm/build.mjs 0.1.1
npm publish out/npm/clawsh/ --access public   # logged in with npm login
```

then add the trusted publisher. Later releases publish from CI; a version already on npm is skipped, so re-running the job is safe.

To try the launcher locally without publishing: `make dist`, `node npm/build.mjs <version>`, serve `dist/` under `<dir>/v<version>/` with any static server, and run `out/npm/clawsh/bin/clawsh.js` with `CLAWSH_DOWNLOAD_BASE` pointing at it.

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
