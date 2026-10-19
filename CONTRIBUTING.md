# Contributing

Thanks for helping. Bugs and ideas go in [issues](https://github.com/varogonz95/clawsh/issues); questions go in [Discussions](https://github.com/varogonz95/clawsh/discussions).

## Before you start

- Open an issue first for anything bigger than a small fix, so the approach is agreed before you write code.
- Read the Rules section of [`CLAUDE.md`](CLAUDE.md): everything must build for Windows, Linux and macOS with `CGO_ENABLED=0`, keep dependencies minimal, and keep the websocket single-writer pattern.
- Issue refinement and labels are described in [`docs/issue-refinement.md`](docs/issue-refinement.md).

## Workflow

- Branch from the latest `main`, named `<type>/<ticket>/<title>` (type: `feat`, `fix`, `chore` or `doc`; ticket: the issue number), e.g. `fix/12/conpty-handle-after-close`.
- Run `make test` and `make vet` (the latter cross-checks windows, linux and darwin) before opening a PR.
- New endpoints or control messages: add types to `internal/api` first, then a test in `internal/agent/e2e_test.go`.
- Update the matching page in `docs/wiki/` when flags, commands or the protocol change.

## License

By contributing you agree that your work is released under the project's [license](LICENSE).
