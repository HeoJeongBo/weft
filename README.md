# weft

> Orchestrate parallel Claude Code sessions across git worktrees + devcontainers.

**weft** (the *weft* threads woven across the warp — a metaphor for parallel work) ties
four things together into one motion:

```
git worktree  +  devcontainer  +  tmux window  +  claude
```

Each **session** is an isolated git worktree on its own branch, running inside its own
devcontainer, in its own tmux window, with a Claude Code agent attached. `weft` creates,
lists, attaches, and tears down these sessions — and gives you a TUI dashboard over all of
them.

## Requirements

macOS (primary), plus these on your `PATH`:

- **git**, **tmux**
- **Docker** (Docker Desktop, OrbStack, or colima) — a running daemon
- **Node.js** + the **Dev Container CLI**: `npm install -g @devcontainers/cli`
- The **Claude Code** CLI

Run `weft doctor` to verify everything at once.

## Install

```sh
brew install HeoJeongBo/tap/weft   # once the Homebrew tap is published
```

Until then (or for local development), build from source:

```sh
git clone https://github.com/HeoJeongBo/weft && cd weft && make install
```

Then verify and launch:

```sh
weft doctor      # check the environment
weft             # open the dashboard  (weft --help prints a styled command reference)
```

## Quick start

```sh
cd your-project            # a git repo with a .devcontainer/
weft init                  # scaffold weft.yaml
weft new feat-auth         # worktree + devcontainer + tmux window + claude, one motion
weft ls                    # see every session and its live status
weft attach feat-auth      # jump into a session
weft rm feat-auth          # tear it down (guards against losing uncommitted work)
```

Or just run `weft` and drive everything from the dashboard.

## Commands

| Command                     | What it does                                                    |
| --------------------------- | --------------------------------------------------------------- |
| `weft new <name>`           | worktree + devcontainer + tmux window + claude, in one motion   |
| `weft ls`                   | list sessions with live, reconciled status (`--json`)           |
| `weft status <name>`        | detailed status of a single session                             |
| `weft attach <name>`        | attach to a session's tmux window (`--start` to resume first)   |
| `weft start` / `stop <name>`| resume / pause a session (container up / down)                  |
| `weft rm <name>`            | tear down; refuses to lose uncommitted/unpushed work (`--force`)|
| `weft exec <name> -- <cmd>` | run a command inside the session's container                    |
| `weft cd <name>`            | print the worktree path — `cd "$(weft cd <name>)"`              |
| `weft init`                 | scaffold `weft.yaml` (detects the base branch + devcontainer)   |
| `weft doctor`               | check that dependencies are installed and healthy               |
| `weft repair`               | reconcile and clean up orphaned worktrees/containers/windows    |
| `weft version`              | build info (`weft --version` too)                               |

Every command takes `-h/--help`; global flags include `--dry-run`, `-v/-vv`, `--config`, `--no-color`.

## Dashboard

Running `weft` with no arguments opens a live TUI (a pipe or non-TTY falls back to `weft ls`):

| Key            | Action                    |
| -------------- | ------------------------- |
| `↑`/`k`, `↓`/`j` | move selection          |
| `enter`        | attach to the session     |
| `n`            | new session               |
| `s` / `S`      | stop / start              |
| `d`            | delete (confirm)          |
| `r`            | refresh now               |
| `?`            | toggle help               |
| `q`            | quit                      |
| `esc`          | cancel a prompt or form   |

## Concepts

A session's identity is a single **name**, stamped into every subsystem so weft never needs
a database to correlate them:

| Subsystem | Where the name lives                          |
| --------- | --------------------------------------------- |
| git       | branch `weft/<name>`, worktree dir `<name>`   |
| tmux      | window `<name>` in session `weft/<project>`   |
| docker    | label `weft.session=<project>/<name>`         |

Because of this, `weft ls` always reflects reality by reconciling live sources — there is no
authoritative state file to drift. Every session resolves to one of:
`ready` · `starting` · `detached` · `stopped` · `partial` · `orphaned`. Stopping or manually
removing a container shows the session as `stopped`; a running container whose tmux window is
gone shows as `detached` (both resume with `weft start`); manually removing the **worktree**
leaves an `orphaned` container/window that `weft repair` cleans up.

## Configuration

`weft.yaml` at the project root configures defaults; every field has a CLI flag override.
See [`weft.yaml.example`](./weft.yaml.example).

> **Note on devcontainers + worktrees:** a worktree's `.git` is a *file* pointing back into
> the main repository's `.git/worktrees/<name>`. So that git works inside the container,
> weft bind-mounts the repo's `.git` directory at its host path and registers it as a git
> `safe.directory`. Opt out with `devcontainer.mount_git: false`.

## Orchestrate any devcontainer from one terminal

`weft dc` scans the machine for devcontainers — including ones VS Code started — and
orchestrates them from a single terminal, wifi-scan style. Discovery uses the standard
`devcontainer.local_folder` docker label that the devcontainer CLI and VS Code stamp on
every container they create, so it works on any project, from any directory, with no
weft configuration at all.

```sh
weft dc                  # scan → pick with ↑/↓ → enter attaches (r rescans, q quits)
weft dc oasys-ui         # unique match attaches directly
weft dc --shell          # open a shell window instead of claude
weft dc oasys-ui -- pnpm test   # one-shot command, no tmux
weft dc gantry --start   # bring a stopped devcontainer up first
```

Picking a devcontainer opens (or reuses) a window in the dedicated tmux session
`weft/dc` whose foreground runs **claude inside that container, resuming its last
conversation** (`claude --continue`, falling back to a fresh claude, then to a shell
when claude is not installed in the image). Each picked devcontainer gets its own
window, so one terminal drives them all: switch with `prefix+n`/`p` or just run
`weft dc` again — entries marked `*` already have a window. Detach with `prefix+d`;
the claudes keep running in tmux.

Note: a claude already running in another terminal (e.g. a VS Code panel) cannot be
adopted — its process belongs to that terminal. `--continue` resumes the same
conversation instead; claudes started by weft live in tmux and can be attached and
detached freely. Picking a stopped devcontainer brings it up automatically. Piped
output (`weft dc | cat`) prints a plain table instead of the picker.

## Without a devcontainer

The devcontainer is optional. Set `devcontainer.enabled: false` in `weft.yaml` (this is what
`weft init` writes when the repo has no `.devcontainer/`) and weft becomes a plain
**worktree + tmux + claude** orchestrator — no Docker required:

```sh
weft new feat-auth          # git worktree + tmux window running claude on the host
weft ls                     # the session shows Ready (no container involved)
weft attach feat-auth       # CLI attach to its tmux window (switch-client inside
                            # tmux, or a blocking `tmux attach` outside)
weft rm feat-auth
```

You can also skip the container for a single session on a devcontainer-enabled project with
`weft new <name> --no-devcontainer`. In tmux-only mode a session's readiness is driven by its
live tmux window rather than a container.

## Development

```sh
make build      # -> ./weft
make run        # go run ./cmd/weft
make test       # go test -race ./...
make lint       # golangci-lint
make cover-100  # fail unless total coverage is 100% (enforced in CI)
make doctor     # run the env checks from source
make help       # list targets
```

The test suite is kept at **100% statement coverage** — CI runs `make cover-100`, so new
code needs tests.

## License

[MIT](./LICENSE)
