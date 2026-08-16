# thicket

A Miller-column TUI file browser for the terminal. Browse with arrow keys
(or vi keys), and press Enter to `cd` the calling shell into the selected
directory.

## Install

```sh
go build -o thicket-bin ./cmd/thicket
sudo install -m 755 thicket-bin /usr/local/bin/thicket-bin
sudo install -m 644 man/thicket.1 /usr/local/share/man/man1/thicket.1
```

Then source the shell wrapper for your shell:

```sh
# bash: add to ~/.bashrc
source /path/to/thicket/shell/thicket.bash

# zsh: add to ~/.zshrc
source /path/to/thicket/shell/thicket.zsh
```

This defines a `thicket` shell function. Rename it in your rc file if
you'd rather use a different name — the `thicket-bin` binary itself
doesn't care what the wrapper is called.

## Usage

Run `thicket` (or `thicket /some/path` to start elsewhere; `thicket --help`
for a summary, `man thicket` for the full manual). Keys:

| Key(s) | Action |
|---|---|
| `↑`, `k` | Move selection up |
| `↓`, `j` | Move selection down |
| `PgUp`, `PgDn` | Move selection by a full page |
| `Home`, `End` | Jump to the first/last entry |
| `→`, `l` | Open selected directory, move focus right (no-op on files) |
| `←`, `h` | Go up one directory, move focus left (no-op at `/`) |
| `Enter` | Choose: cd to the selected directory, or to the active directory if a file/empty directory is selected; exit |
| `q`, `Esc`, `Ctrl-C` | Quit without changing directory |
| `.` | Toggle hidden (dotfile) visibility (default off) |
| `r` | Refresh the active directory's listing |
| `/` | Type-ahead search the active column: type to jump to the first matching entry; `Enter` keeps the match, `Esc` cancels back to where the cursor was |
| `?` | Toggle the in-app help screen |

Navigation only in v1 — no file create/rename/delete/copy/move, no config
file.
