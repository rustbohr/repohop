# repohop

A cross-platform terminal UI for driving a set of git repositories as one unit:
see every repo's branch at a glance, and switch them all onto the same branch
from an interactive fuzzy picker.

Single static binary. No `fzf`, no bash, no runtime beyond `git` itself.

> Status: early development. See `PLAN.md` in the workspace repo for the design.

## Install

```sh
go install github.com/rustbohr/repohop/cmd/repohop@latest
```

## Usage

```sh
repohop                  # interactive TUI
repohop status           # plain table of every repo in the active project
repohop switch <branch>  # put every repo onto <branch>
repohop fetch
repohop pull
repohop projects
repohop config path
repohop version
```

## Configuration

`repohop` reads, in precedence order:

1. `--config <path>`
2. `.repohop.yaml` in the current directory or any ancestor
3. `$XDG_CONFIG_HOME/repohop/config.yaml` (`%AppData%\repohop\config.yaml` on
   Windows)

```yaml
version: 1

defaults:
  fetch: true
  pull: true
  concurrency: 8

projects:
  - name: acme
    base: ~/src/acme
    repos:
      - api
      - web
      - worker
      - path: ~/other/place/docs
        name: docs
```

## License

MIT
