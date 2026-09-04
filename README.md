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
| `e` | edit this project |
| `?` | help |
| `q` | quit |

Every action applies to the selected rows, defaulting to all of them.

**Projects** — always the first screen, with the cursor on whichever project
was open last. `enter` opens a project, `esc` comes back here, `n` builds a new
one from a directory scan, `e` edits it, `d` deletes it after a confirmation.
Deleting a project only removes the configuration entry; the repositories on
disk are never touched.

`repohop --project <name>` skips the list and opens that project directly.

**Editing a project** — `r` renames, `a` adds a repository, `d` removes one,
`ctrl+s` writes the change. Repository entries are written relative to the
project's `base` when they sit underneath it and absolute when they do not, so
the base itself never needs editing.

Anywhere repohop asks for a directory you get a tree: `→` opens a branch, `←`
closes it, `-` moves the root up a level, `enter` chooses the highlighted
directory. Only directories are shown, and the ones that are git repositories
are highlighted. There is no path to type.

```
▾ ~/src
  ▸ archived
  ▾ acme
    ▸ api
    ▸ web
  ▸ scratch
```

Projects that come from a committed `.repohop.yaml` are shown but not edited
here — rewriting a file the team shares is not the UI's business. Edit that
file instead.

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

The list appears immediately from the refs already on disk, and the repositories
are fetched in the background so a branch a colleague pushed a minute ago turns
up without you ever waiting on the network. `alt+f` fetches again on demand;
`alt+p` turns the after-checkout fast-forward off for this switch. Set
`defaults.fetch: false` if you would rather it never fetch on its own.

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
repohop switch <branch> [-n|--no-fetch] [-p|--no-pull] [--stash] [--only a,b]
repohop fetch  [--project NAME]
repohop pull   [--project NAME]
repohop projects
repohop projects add <name> [--base DIR] [--repo PATH]... [--scan DIR] [--depth N]
repohop projects rm  <name>
repohop projects use <name>
repohop config path
repohop version
```

`projects add` takes a whole directory tree at once, which is the
non-interactive equivalent of the setup flow:

```sh
repohop projects add acme --scan ~/src/acme
```

`-n` skips the fetch and switches against the refs already on disk; with `-p`
as well it is a fully offline switch.

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

When repohop writes to a config file it edits the YAML in place: comments, key
order and any keys it does not recognise survive, and only the project being
changed is rewritten. Blank lines between top-level keys are not preserved.

## Where repohop keeps things

```sh
repohop config path     # prints all three, and whether they exist yet
```

| | Linux / macOS | Windows |
|---|---|---|
| config | `~/.config/repohop/config.yaml` | `%AppData%\repohop\config.yaml` |
| active project | `~/.local/state/repohop/state.yaml` | `%LocalAppData%\repohop\state.yaml` |
| log | `~/.local/state/repohop/repohop.log` | `%LocalAppData%\repohop\repohop.log` |

`$XDG_CONFIG_HOME` and `$XDG_STATE_HOME` take precedence where they are set.
Nothing is written until there is something to write, so a run that goes well
leaves no state and no log.

## Uninstalling

repohop is one binary plus three files under your home directory. It installs no
service, edits no shell profile, and never touches your repositories — removing
it cannot lose your work.

**Linux and macOS**

```sh
rm "$(command -v repohop)"        # the binary, wherever you put it
rm -rf ~/.config/repohop          # your projects
rm -rf ~/.local/state/repohop     # remembered project and log
```

macOS uses the same paths as Linux — repohop deliberately keeps its config in
`~/.config` rather than `~/Library/Application Support`, so a config file is
portable between your machines.

**Windows** (PowerShell)

```powershell
Remove-Item (Get-Command repohop).Source
Remove-Item -Recurse -Force "$env:AppData\repohop"
Remove-Item -Recurse -Force "$env:LocalAppData\repohop"
```

**If you installed with `go install`**, the source also sits in the module
cache. `go clean -modcache` clears it for every module you have ever built;
to remove only this one, delete `$(go env GOMODCACHE)/github.com/rustbohr`.

**If you installed shell completions** yourself — repohop does not do this for
you — remove the file you created, typically
`/etc/bash_completion.d/repohop`, `~/.local/share/bash-completion/completions/repohop`,
or the zsh equivalent on your `$fpath`.

Keep `~/.config/repohop` if you might come back: reinstalling picks your
projects up again exactly as they were.

## When something goes wrong

A crash inside the interface is caught, explained on screen, and written to a
log file with its stack; the program keeps running so you do not lose your
place. Failures that repohop expects — a repository that has moved, a checkout
that git refused — are reported in the interface and logged too.

```sh
repohop config path     # includes the log's location
```

The log lives at `$XDG_STATE_HOME/repohop/repohop.log`, falling back to
`~/.local/state/repohop/repohop.log`. It is capped at 1 MiB and started again
when it grows past that — it is a record of what just went wrong, not an audit
trail. If you report a bug, the last entry is the useful part.

## Development

```sh
make build
make check    # build, vet, test, gofmt and the public-clean scan — what CI runs
```

`make check` includes `scripts/check-clean.sh`, which refuses absolute home
directories, real email addresses and key material. Point
`REPOHOP_EXTRA_PATTERNS` at a private file of your own patterns to have those
checked too — that file should live outside this repository.

The git layer is tested against real throwaway repositories, and the TUI with
`teatest`; both need `git` on PATH.

## License

MIT
