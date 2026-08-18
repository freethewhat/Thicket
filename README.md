# thicket

A Miller-column TUI file browser for the terminal. Browse with arrow keys
(or vi keys), and press Enter to `cd` the calling shell into the selected
directory.

## Install

### From a release (Linux/macOS, amd64/arm64)

```sh
curl -fsSL https://raw.githubusercontent.com/freethewhat/Thicket/master/scripts/install.sh | sh
```

Installs `thicket-bin` and the man page under `/usr/local` (override with
`PREFIX=...`), and the shell wrappers under `/usr/local/share/thicket/shell`.
Pin a version with `... | sh -s -- v0.1.0` or `VERSION=v0.1.0 ... | sh`.
Prebuilt archives and checksums are also available directly from the
[Releases page](https://github.com/freethewhat/Thicket/releases).

### From source

```sh
go build -o thicket-bin ./cmd/thicket
sudo install -m 755 thicket-bin /usr/local/bin/thicket-bin
sudo install -m 644 man/thicket.1 /usr/local/share/man/man1/thicket.1
```

Then source the shell wrapper for your shell — from
`/usr/local/share/thicket/shell/` if installed via the script above, or
from your checkout's `shell/` directory if built from source:

```sh
# bash: add to ~/.bashrc
source /path/to/thicket.bash

# zsh: add to ~/.zshrc
source /path/to/thicket.zsh
```

This defines a `thicket` shell function. Rename it in your rc file if
you'd rather use a different name — the `thicket-bin` binary itself
doesn't care what the wrapper is called.

Run `thicket` (or `thicket /some/path` to start elsewhere; `thicket --help`
for a summary, `thicket --version` to print the version, `man thicket` for
the full manual). Keys:

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
| `f` | Recursive find: type to filter files/directories under the active directory; `Enter` jumps to the match's parent directory, `Esc` cancels |
| `?` | Toggle the in-app help screen |
| `m` | Bookmark the active directory under a letter |
| `` ` `` | Jump to a bookmarked directory by letter |
| `'` | Open the marks list |
| `d` (in marks list) | Delete the highlighted mark |

Navigation only in v1 — no file create/rename/delete/copy/move, no config
file.

## Updating

```sh
thicket-bin update
```

Downloads and installs the latest release for your OS/arch. `thicket update`
reads `PREFIX` from the environment at the time you run it (default `/usr/local`),
the same as `scripts/install.sh` — if you installed to a non-default `PREFIX`,
export it again before running `thicket update`, or the update will land under
`/usr/local` instead. Like the install script, `thicket update` includes the same
sudo-elevation behavior for a non-writable prefix.

thicket also checks for a newer release on every launch (a single
GitHub API request, 2-second timeout, fails silently if offline or
slow) and shows a 5-second "update available" notice in the status line
when one exists (release builds only — a `go build`/`go run` source build
reports version `dev` and never checks). Set `THICKET_NO_UPDATE_CHECK=1` to
disable this check entirely.

Set `THICKET_CHANNEL=beta` to track pre-release builds (tagged `vX.Y.Z-beta.N`)
instead of only stable releases — this affects `thicket-bin update`, the
on-launch check above, and `scripts/install.sh` alike. A beta offer is
labeled "beta update available" in the status line so it's clear you're
being offered a pre-release. Unset, or set to anything other than `beta`,
tracks stable only (the default).

## License

[MIT](LICENSE)
