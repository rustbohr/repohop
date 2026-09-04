# repohop

A cross-platform terminal UI for driving a set of git repositories as one unit:
see every repo's branch at a glance, and switch them all onto the same branch
from an interactive fuzzy picker.

One static binary. No `fzf`, no bash, no runtime beyond `git` itself.

```
  REPO      BRANCH                 STATE   SYNC    LAST COMMIT
› api       master                 clean   ↑0 ↓2   3 hours ago
✓ web       feat/new-checkout      dirty   ↑1 ↓0   12 minutes ago
  worker    master                 clean   =       2 days ago
  docs      (detached @ a1b2c3d)   clean   —       5 days ago
```

## Why

Teams that split one product across many repos end up scripting the same three
things: *what branch is everything on?*, *put everything on branch X*, and *does
branch X even exist everywhere?* Those scripts are bash, they hardcode paths,
and the good ones need `fzf` — so they don't travel to a teammate on macOS or
Windows. repohop is that workflow as one distributable binary.

It is deliberately **not** a git client: no staging, diffing, merging or
conflict resolution. Per-repo work belongs in `lazygit`, `gitui` or `git`
itself. There is also no push orchestration, on purpose.

## Install

```sh
go install github.com/rustbohr/repohop/cmd/repohop@latest
```

Or download a prebuilt binary from the releases page (linux, macOS and Windows;
amd64 and arm64) and put it on your PATH.

## Getting started

Run `repohop` with no arguments. On first run it offers to scan a directory for
git repositories and build a project from what it finds.

## The interface

`repohop` in a terminal opens the TUI.

**Dashboard** — one row per repository, filled in as the concurrent status reads
land.

| key | what it does |
|---|---|
| `space` | select the row |
| `a` | select all, or clear the selection |
| `s` | switch (opens the branch picker) |
| `f` / `p` | fetch / pull |
| `r` | refresh |
| `?` | help |
| `q` | quit |

Every action applies to the selected rows, defaulting to all of them.

**Branch picker** — type to fuzzy-filter the union of local and `origin`
branches across the selected repositories. The preview shows which repos carry
the highlighted branch and where:

```
  › feat/new-checkout                  │ feat/new-checkout
  3 branches of 24                     │ 3/4 repos
                                       │
› feat/new-checkout            3/4     │ ✓ api      local origin
  feat/new-checkout-v2         1/4     │ ✓ web      origin
  master                       4/4     │ · worker   —
                                       │ ✓ docs     local
```

The preview moves below the list on a narrow terminal and collapses to a
one-line summary on a very narrow one.

**Run screen** — repositories with local changes are not a hard failure: you
choose to skip them, or to stash and switch (the stashes are restorable from
the summary with `u`). Failures expand with `enter` to the exact `git` command
and its stderr, so you can run it yourself.

## Switch semantics

Deliberately conservative:

- A branch is checked out only if it already exists locally **or** on `origin`.
  It is **never created**. A repository without it is reported as
  `no such branch` and left untouched.
- After checkout, the upstream is set to `origin/<branch>` when that ref exists
  and the branch is fast-forwarded. A non-fast-forward is reported, never
  forced, never merged or rebased.
- A local-only branch with no `origin/<branch>` is reported as such and not
  pulled.
- Nothing is ever pushed.

## Without a terminal

When stdout is not a terminal, repohop never starts the TUI: it runs the
equivalent command and prints a plain table, so it works in scripts and CI.

```
repohop status [--project NAME] [--json] [--only a,b]
repohop switch <branch> [--no-fetch] [--no-pull] [--stash] [--only a,b]
repohop fetch  [--project NAME]
repohop pull   [--project NAME]
repohop projects
repohop config path
repohop version
```

Exit codes: `0` all good · `1` partial failure (some repository did not switch
or could not be read) · `2` usage or configuration error.

## Configuration

repohop reads, in precedence order:

1. `--config <path>`
2. `.repohop.yaml` in the current directory or any ancestor — commit this into
   a workspace repo and a teammate has the project the moment they clone
3. `$XDG_CONFIG_HOME/repohop/config.yaml`, falling back to
   `~/.config/repohop/config.yaml` (`%AppData%\repohop\config.yaml` on Windows)

Projects from a directory config are merged into the user's and win on a name
collision. The remembered active project lives in
`$XDG_STATE_HOME/repohop/state.yaml`, never in the shared config.

```yaml
version: 1

defaults:
  fetch: true          # fetch before a switch
  pull: true           # fast-forward after a successful checkout
  concurrency: 8

projects:
  - name: acme
    base: ~/src/acme         # optional; bare repo entries resolve against it
    repos:
      - api
      - web
      - worker
      - path: ~/other/place/docs   # absolute paths escape base
        name: docs                  # display-name override
  - name: side-project
    repos:
      - path: ~/dev/thing
```

`~` and `$VAR` are expanded in every path. A repository that has vanished or is
not a git repo is shown as such, never silently dropped.

## Development

```sh
make build
make test     # go test ./...
make vet
```

The git layer is tested against real throwaway repositories, and the TUI with
`teatest`; both need `git` on PATH.

## License

MIT
