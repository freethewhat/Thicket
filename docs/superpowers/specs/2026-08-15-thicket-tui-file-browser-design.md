# thicket — TUI Miller-column file browser with shell `cd` integration

**Status:** Approved for planning
**Date:** 2026-08-15

## 1. Summary

`thicket` is a Linux CLI tool that replaces manual `cd`-and-`ls` spelunking
with a Finder/ranger-style Miller-column TUI. The user launches it from their
shell, browses directories with arrow/vi keys, and on selection the tool
changes the calling shell's working directory and exits. It is navigation
only — no file operations (create/rename/delete/copy/move), no search, no
config file, no filesystem watching.

## 2. Naming

- Project / repository: `thicket`
- Go module path: `thicket` (bare local module path; not yet published — if
  it is later pushed to a public host, rename via `go mod edit -module
  github.com/<owner>/thicket` and update the two import paths in
  `cmd/thicket/main.go` and any internal cross-package imports at that time).
- Binary: `thicket`
- Suggested shell wrapper function name (user's own rc file, freely
  renameable, not baked into the binary): `th`

Naming collision check performed against `hop`, `spelunk`, `warren`,
`copse`, `grove`, `bough` — all have actively published CLI tools in
adjacent developer-tooling niches. `thicket`'s only found collision is an
unrelated Python library (LLNL/thicket, HPC performance-data analysis) that
does not install a global CLI binary — lowest practical `$PATH` collision
risk of the candidates considered.

## 3. Non-goals (v1)

- No create / rename / delete / copy / move file operations.
- No search or fuzzy-filter within a pane.
- No bookmarks, tabs, or split views beyond the single Miller-column strip.
- No config file or theme customization — keybindings and styling are
  compiled in.
- No mouse support.
- No filesystem watching / live external-change refresh (manual refresh key
  only, see §6).
- Linux is the only supported and tested target. The implementation is
  plain POSIX-path Go with no Linux-only syscalls, so it will likely run on
  macOS/BSD incidentally, but that is not a support commitment for v1.

## 4. Shell integration

A child process cannot change its parent shell's working directory
directly, so `thicket` uses the same handoff pattern as `zoxide`/`lf`/`broot`:

1. The `thicket` binary opens `/dev/tty` directly (`os.OpenFile("/dev/tty",
   os.O_RDWR, 0)`) and passes it to Bubble Tea via `tea.WithInput(tty)` and
   `tea.WithOutput(tty)`. This renders the TUI on the real terminal device
   while leaving the process's actual `os.Stdout` completely free of TUI
   escape sequences.
2. On **Enter**, after the Bubble Tea program returns, `main` writes the
   chosen absolute directory path to `os.Stdout` (a single line, no trailing
   decoration beyond `\n`) and exits with code `0`.
3. On **quit without selecting** (`q` / `Esc` / `Ctrl-C`), `main` writes
   nothing to stdout and exits with code `1`.
4. Ship two shell snippets, `shell/thicket.bash` and `shell/thicket.zsh`
   (identical function body; kept as two files so install instructions can
   point at the right one per shell rather than relying on bash/zsh syntax
   compatibility long-term):

   ```sh
   th() {
     local dir
     dir=$(command thicket "$@") && [ -n "$dir" ] && cd -- "$dir"
   }
   ```

   The `README.md` instructs users to source the appropriate file from
   `~/.bashrc` or `~/.zshrc`.
5. `thicket` accepts an optional positional argument, the starting
   directory (defaults to `.`, i.e. the process's current working
   directory, which is the shell's cwd at invocation time — exactly what
   makes `th` feel like an enhanced `cd`). A missing/inaccessible starting
   path is an error printed to stderr with exit code `2`, no stdout output.

## 5. Navigation state model

State is derived from a single active path rather than a tree of cached
panes, so there is no cache-invalidation logic to get wrong.

**Model fields:**

- `activePath string` — absolute, symlink-preserving path of the directory
  shown in the focused pane. Initialized from the resolved absolute form of
  the CLI's starting-directory argument.
- `activeCursor int` — index into the active directory's sorted entry list;
  `-1` when the directory is empty.
- `activeScroll int` — index of the first visible row, for vertical
  scrolling when the entry count exceeds the pane height.
- `showHidden bool` — dotfile visibility, default `false`.
- `statusErr string` — last error to show on the status line (cleared on
  the next successful navigation action).
- `width, height int` — terminal size from the latest `tea.WindowSizeMsg`.
- `quitting bool`, `chosenPath string`, `selected bool` — set once, on the
  key that ends the program (Enter sets `selected = true` and
  `chosenPath`; q/Esc/Ctrl-C leave `selected = false`).

**Derived, not stored:**

- *Ancestor panes* (left of active): computed by walking
  `filepath.Dir(activePath)` upward. Each ancestor pane's highlighted row is
  the path component leading toward `activePath` (or toward the next
  ancestor down the chain) — no separate cursor state needed per ancestor.
  Only as many ancestors are rendered as fit the terminal width (§6); the
  window follows `activePath`, so ancestors "fall off" the left edge as the
  user descends deeper than the terminal is wide, and reappear as the user
  ascends — this is the "unbounded Miller columns" behavior.
- *Preview pane* (right of active): computed from
  `listing(activePath)[activeCursor]`. If that entry is a directory, the
  preview is that directory's listing (read fresh, not cached across
  frames). If it is a file, the preview is the file-preview described in
  §7. If `activeCursor == -1` (empty directory) or the entry is unreadable,
  the preview pane is blank with a one-line status note.

**Transitions:**

- `Right`: let `entry := listing(activePath)[activeCursor]`. If `entry` is
  a directory (following symlinks, see §7): `activePath =
  filepath.Join(activePath, entry.Name)`, `activeCursor = 0`, `activeScroll
  = 0`. If listing the new path fails (e.g. permission denied), do not
  change `activePath`; set `statusErr` instead. If `entry` is a file, or
  `activeCursor == -1`, this is a no-op.
- `Left`: if `activePath == "/"`, no-op. Otherwise let `child :=
  filepath.Base(activePath)` and `parent := filepath.Dir(activePath)`.
  Read `parent`'s listing; set `activePath = parent`, `activeCursor =`
  index of the entry named `child` in that listing (so the pane just left
  reappears as the preview pane — this is what "the active pane moves
  left" means visually), `activeScroll` recomputed so `activeCursor` is
  visible. If `child` is somehow absent from the parent listing (deleted
  externally between reads), fall back to `activeCursor = 0`.
- `Up` / `Down`: `activeCursor` moves by ∓1/±1, clamped to
  `[0, len(entries)-1]` (or stays `-1` if the directory is empty);
  `activeScroll` adjusted to keep `activeCursor` within the visible window.
- `Enter`: let `entries := listing(activePath)`.
  - If `activeCursor >= 0` and `entries[activeCursor]` is a directory:
    `chosenPath = filepath.Join(activePath, entries[activeCursor].Name)`.
  - Otherwise (entry is a file, or the directory is empty):
    `chosenPath = activePath`.
  - Set `selected = true`, `quitting = true`, return `tea.Quit`.
- `q` / `Esc` / `Ctrl-C`: set `selected = false`, `quitting = true`, return
  `tea.Quit`.
- `.`: toggle `showHidden`; if this makes `activeCursor` point past the end
  of the now-shorter/longer list, clamp it; re-derive preview.
- `r`: force a fresh read of `activePath`'s listing (directory contents are
  otherwise only re-read when `activePath` changes — see §9 non-goals on
  filesystem watching).

There is deliberately no `..` pseudo-row in any listing — ascending is
exclusively the Left key. Deliberate simplification vs. tools like ranger:
going Left then Right without an intervening Up/Down reloads the child
directory fresh (cursor resets to top) rather than restoring the exact
deep cursor history. This keeps the entire model derivable from
`activePath` plus two integers, with no cache to invalidate. If this proves
annoying in practice it is a contained, addressable follow-up (a
`map[string]int` of last-cursor-per-path), not a re-architecture.

## 6. Rendering and layout

- Column count: as many of {ancestors…, active, preview} as fit the
  terminal width, always including active + preview when width allows.
  Column width = `terminal width / visible column count`, minimum 15
  columns; below that minimum the leftmost (oldest ancestor) column is
  dropped and the rest recompute, repeating until either everything fits
  or only active+preview remain. Below 30 total terminal columns, drop to
  active-only (no preview). Row count per pane = `terminal height - 2`
  (one header/breadcrumb line, one status line).
- Entry name truncation: names wider than the column truncate with a
  trailing `…`, preserving the extension is not attempted (v1 keeps this
  simple — truncate raw runes).
- Sort order within a listing: directories before files; within each
  group, case-insensitive lexicographic by name. `showHidden == false`
  filters out entries whose name starts with `.` before sorting.
- Symlinks: displayed with a trailing `@` after the name. Navigation
  treats a symlink as a directory if `os.Stat` (which follows symlinks) on
  its target reports a directory; a broken symlink (`os.Stat` errors) is
  displayed as a symlink but is inert for `Right` (no-op, same as a file).
- Unreadable directory (`os.ReadDir` returns `EACCES`/`EPERM`): the pane
  shows a single centered line `[permission denied]` instead of entries;
  see the `Right` transition above for how this interacts with navigation.
- Selected row style: reverse video. Directory names: bold. Symlink
  suffix: dim. These are the only style rules — no per-filetype color
  coding in v1 (YAGNI; matches "navigation only" scope).
- Status line (last terminal row): left side shows key hints — `↑/k ↓/j
  move · →/l open · ←/h up · Enter cd+exit · . hidden · r refresh · q
  quit`; right side shows `statusErr` when non-empty, otherwise the
  active path.
- `tea.WindowSizeMsg` updates `width`/`height` and triggers a re-layout on
  the next `View()` call — no separate resize state machine needed since
  layout is computed fresh from `width`/`height` every render.

## 7. Filesystem reading and previews

- Directory listing: `os.ReadDir(path)`, then `os.Lstat` per entry to
  determine symlink-ness, then `os.Stat` (follow) per entry to classify
  directory-vs-file for sorting/navigation and to read size/mtime for the
  preview metadata line. Errors reading an individual entry's stat (e.g. a
  symlink target that vanished mid-read) mark that entry as a broken
  symlink rather than aborting the whole listing.
- File preview: read at most 64 KiB from the start of the file. Scan the
  first 8000 bytes read for a `0x00` byte; if found, render the preview
  pane as `<binary file, N bytes>` (using the real file size from `Stat`,
  not the truncated read length) instead of the byte content. Otherwise,
  split the read bytes on `\n` and render as many leading lines as fit the
  pane height, each truncated (not wrapped) to the pane's column width —
  no line-wrapping, to keep row counts predictable.
- Directory preview (when the highlighted entry is itself a directory):
  same read/sort/filter pipeline as the main listing, capped at the first
  1000 entries with a trailing `… N more` marker if truncated, to bound
  preview cost for very large directories. This cap applies to the
  preview pane only — actually navigating `Right` into such a directory
  reads its full listing.
- No symlink-loop detection: `Right`/`Left` operate purely on
  `filepath.Join`/`filepath.Dir` string manipulation plus `os.Stat`, the
  same way the shell's own `cd` does. A user can walk into a symlink cycle
  the same way they could with `cd`; this is accepted, not a bug to guard
  against in v1.

## 8. Keybindings

| Key(s) | Action |
|---|---|
| `↑`, `k` | Move selection up |
| `↓`, `j` | Move selection down |
| `→`, `l` | Open selected directory, move focus right (no-op on files) |
| `←`, `h` | Go up one directory, move focus left (no-op at `/`) |
| `Enter` | Choose (cd to selected directory, or to the active directory if a file/empty dir is selected), print path, exit 0 |
| `q`, `Esc`, `Ctrl-C` | Quit without changing directory, print nothing, exit 1 |
| `.` | Toggle hidden (dotfile) visibility, default off |
| `r` | Force re-read of the active directory's listing |

Vi-style `hjkl` are aliases alongside arrow keys, matching the convention
used by comparable tools (ranger, lf, nnn) at negligible implementation
cost.

## 9. Distribution and installation

- `go install thicket/cmd/thicket@latest` once published, or `go build
  ./cmd/thicket` locally against this repo.
- `README.md` documents: build/install step, sourcing
  `shell/thicket.bash` or `shell/thicket.zsh` from the user's rc file, and
  the full keybinding table from §8.
- No package-manager (apt/brew) distribution in v1.

## 10. Testing strategy

- `Update(model, msg) (model, cmd)` is a pure function — unit tests drive
  it with synthetic `tea.KeyMsg` values against a `t.TempDir()` fixture
  tree (a handful of nested directories, a regular text file, a binary
  file, an unreadable directory via `os.Chmod(0o000)`, a symlink to a
  directory, and a broken symlink) to cover: `Right` into a directory,
  `Right` no-op on a file, `Right` into a permission-denied directory
  (verify `activePath` unchanged, `statusErr` set), `Left` at `/`
  (no-op), `Left` mid-tree (verify cursor lands back on the child just
  left), `Up`/`Down` clamping at both ends and on an empty directory,
  `Enter` on a directory / on a file / on an empty directory (verify the
  three different `chosenPath` outcomes), `.` toggling hidden files, `q`
  producing `selected == false`.
- `View(model) string` is also pure given model state — golden-file
  snapshot tests for a handful of canonical states (root directory, a
  path several levels deep with ancestors visible, a narrow terminal
  triggering the active-only layout, a permission-denied pane, a binary
  file preview) catch layout regressions without needing a real terminal.
- `internal/fsutil` (listing, sorting, hidden-filtering, symlink
  classification, file preview reading/binary-detection) gets direct unit
  tests against the same fixture tree, independent of Bubble Tea.
- The `/dev/tty` + Bubble Tea altscreen wiring and the actual shell
  wrapper handoff are impractical to cover with an automated headless
  test; the plan will call these out as a documented manual smoke test
  (launch `thicket`, navigate, press Enter, confirm the invoking shell's
  cwd actually changed) rather than skip verification silently.
