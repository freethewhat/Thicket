# thicket

A Miller-column TUI file browser for the terminal. Browse with arrow keys
(or vi keys), and press Enter to `cd` the calling shell into the selected
directory.

## Install

```sh
go build -o thicket ./cmd/thicket
sudo install -m 755 thicket /usr/local/bin/thicket
```

Then source the shell wrapper for your shell:

```sh
# bash: add to ~/.bashrc
source /path/to/thicket/shell/thicket.bash

# zsh: add to ~/.zshrc
source /path/to/thicket/shell/thicket.zsh
```

This defines a `th` shell function. Rename it in your rc file if you'd
rather use a different name — the `thicket` binary itself doesn't care
what the wrapper is called.

## Usage

Run `th` (or `th /some/path` to start elsewhere). Keys:

| Key(s) | Action |
|---|---|
| `↑`, `k` | Move selection up |
| `↓`, `j` | Move selection down |
| `→`, `l` | Open selected directory, move focus right |
| `←`, `h` | Go up one directory, move focus left |
| `Enter` | cd to the selection and exit |
| `q`, `Esc`, `Ctrl-C` | Quit without changing directory |
| `.` | Toggle hidden (dotfile) visibility |
| `r` | Refresh the active directory's listing |

Navigation only in v1 — no file create/rename/delete/copy/move, no search,
no config file.
