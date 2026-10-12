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

`.github/workflows/release.yml` runs the CI checks (`make vet`, `make test`, the npm launcher tests), cross-compiles with `make dist`, packages one archive per platform plus `checksums.txt`, and creates the GitHub release with generated notes (if a release for the tag already exists, e.g. made by hand, it uploads the assets to that one instead). Then it publishes the same binaries to npm: `npm/build.mjs` stages one `clawsh-<os>-<cpu>` package per platform plus the `clawsh` launcher (`npm/clawsh/`), all at the tag's version. Tags with a suffix (`v0.1.0-rc.1`) become pre-releases on GitHub and the `next` dist-tag on npm. `.github/workflows/ci.yml` runs the same checks on every push to `main` and on pull requests, except those that only touch Markdown, `docs/`, `site/`, `.claude/` or `LICENSE`.

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

The user docs live in the repository, in `docs/wiki/` (`Home.md` is the index), and are read on GitHub like any other Markdown; there's no separate GitHub wiki. Link between pages with relative `.md` links (`[Commands](Commands.md)`) so they work both on GitHub and in a local checkout, and change them in the same PR as the code they describe.

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
