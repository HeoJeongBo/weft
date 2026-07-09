# Contributing to weft

Thanks for your interest! This is a Go CLI; the bar is standard Go tooling and DX.

## Prerequisites

- Go 1.24+
- `golangci-lint` and `gofumpt` for linting/formatting
- The runtime dependencies from the README (git, tmux, docker, devcontainer, claude) if you
  want to run end-to-end smoke tests

## Layout

```
cmd/weft/         entrypoint (thin; main -> cli.Execute)
internal/
  cli/            cobra command tree (one file per subcommand); execute.go wraps fang
  engine/         orchestration facade shared by CLI and TUI
  domain/         pure types (sessions, status)
  config/         weft.yaml load (koanf) + flag overrides
  git/ tmux/ devcontainer/ dockerx/   external-tool wrappers (interface + exec + fake)
  sysexec/        os/exec abstraction (dry-run + fake point)
  tui/            bubbletea dashboard
  paths/ logx/ wefterr/ version/   XDG paths, slog setup, typed errors+exit codes, build version
```

Every external-tool wrapper is an interface with a real (`exec`) implementation and a
`fake` used in tests — the engine depends on interfaces, never on `os/exec` directly. A few
unexported func-var seams (e.g. `newRunner`, `execCommand`, `newProgram`) let tests inject
fakes for otherwise-interactive paths.

## Workflow

```sh
make build        # compile
make test         # go test -race ./...
make lint         # golangci-lint
make fmt          # gofumpt -w .
make cover-100    # enforce 100% total coverage (what CI runs)
```

- Keep the code idiomatic; match the surrounding style.
- Errors should say *what failed, why, and the next step*.
- `stdout` is for data (and `--json`); logs go to `stderr` (`log/slog`).
- **Coverage is kept at 100%.** CI runs `make cover-100` (it merges unit-test covdata with a
  coverage-instrumented binary run so `main()` counts). New code needs tests; see the
  existing `_test.go` files and `internal/sysexec/fake.go` for the patterns.

A `.devcontainer/` is included: "Reopen in Container" (or `devcontainer up`) gives a ready Go
toolchain, and mounts your host `~/.claude` so Claude Code is authenticated inside.

## Commit messages

Conventional-commits style (`feat:`, `fix:`, `docs:`, `chore:`, `refactor:`, `test:`), used
by the changelog generator.

## Releasing

Releases are cut by pushing a tag:

```sh
git tag v0.1.0 && git push origin v0.1.0
```

GitHub Actions runs GoReleaser, which builds the binaries and publishes the Homebrew cask to
`HeoJeongBo/homebrew-tap`. This requires a repo secret `HOMEBREW_TAP_TOKEN` (a fine-grained
PAT with `contents: write` on the tap repo).
