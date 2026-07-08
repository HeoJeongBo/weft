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
brew install HeoJeongBo/tap/weft
weft doctor      # verify your environment
weft             # open the dashboard
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

## Concepts

A session's identity is a single **name**, stamped into every subsystem so weft never needs
a database to correlate them:

| Subsystem | Where the name lives                          |
| --------- | --------------------------------------------- |
| git       | branch `weft/<name>`, worktree dir `<name>`   |
| tmux      | window `<name>` in session `weft/<project>`   |
| docker    | label `weft.session=<project>/<name>`         |

Because of this, `weft ls` always reflects reality by reconciling live sources — there is no
authoritative state file to drift. A crash or a manual `docker rm` just shows up as a
`Partial` session that `weft repair` can clean up.

## Configuration

`weft.yaml` at the project root configures defaults; every field has a CLI flag override.
See [`weft.yaml.example`](./weft.yaml.example).

> **Note on devcontainers + worktrees:** a worktree's `.git` is a *file* pointing back into
> the main repository's `.git/worktrees/<name>`. The devcontainer must be able to see that
> path for git to work inside the container. See the config comments and CONTRIBUTING.

## Development

```sh
make build      # -> ./weft
make run        # go run ./cmd/weft
make test       # go test -race ./...
make lint       # golangci-lint
make doctor     # run the env checks from source
make help       # list targets
```

## License

[MIT](./LICENSE)
