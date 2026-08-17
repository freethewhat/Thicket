# Repository Guidelines

## Project Overview

`thicket` is a Go Miller-column TUI file browser for the terminal. It
replaces `cd`/`ls` navigation with a visual multi-column browser (arrow keys
or vi keys `hjkl`); pressing `Enter` `cd`s the *calling shell* into the
selected directory by having a shell wrapper function (`thicket`) capture the
program's stdout. Module path: `thicket` (unpublished, local module).
v1 scope is intentionally locked to navigation only — no file
create/rename/delete/copy/move, no config file, no mouse support, no
filesystem watching (see `docs/superpowers/specs/2026-08-15-thicket-tui-file-browser-design.md`
§3). Two amendments to that non-goal list have shipped since: a
`/`-triggered type-ahead cursor search within the active column
(`docs/superpowers/specs/2026-08-16-thicket-type-ahead-search-design.md`),
and vim/ranger-style directory marks/bookmarks
(`docs/superpowers/specs/2026-08-16-thicket-directory-marks-design.md`).
Every other v1 non-goal in §3 still holds.

## Architecture & Data Flow

Three-layer module; `cmd/thicket` depends on `internal/tui` and directly on
`internal/marks` (for `marks.DefaultPath()`), while `internal/tui` depends
on `internal/fsutil` and `internal/marks`:

1. **`internal/fsutil`** — pure filesystem I/O. `ListDir(dir, showHidden) ([]Entry, error)`
   reads a directory, classifies each entry (symlink resolution via
   `os.Lstat`/`os.Stat`, broken-symlink detection), filters dotfiles, and
   sorts directories-first then case-insensitive alphabetical.
   `ReadFilePreview(path)` reads up to 64 KiB and sniffs the first 8000
   bytes for a NUL byte to classify binary vs. text, returning a
   `FilePreview{Binary, Special, Lines, Size}`. Non-regular files (FIFOs,
   devices, sockets) are reported via `Special: true` *without* ever
   calling `os.Open` — opening a FIFO with no writer blocks indefinitely,
   which would freeze the whole TUI since preview reads run inline on the
   Bubble Tea event loop. No caching, no state.
2. **`internal/marks`** — pure disk I/O for directory marks (bookmarks): a
   `letter -> absolute-path` table persisted as sorted `letter\tpath\n`
   lines. `Load(path)`/`Save(path, m)` mirror `fsutil`'s per-entry error
   tolerance (a malformed line is skipped, not fatal) and no-caching
   design; `DefaultPath()` resolves `$XDG_STATE_HOME/thicket/marks`
   (falling back to `$HOME/.local/state/thicket/marks`).
3. **`internal/tui`** — Bubble Tea MVU (Model-Update-View / Elm-architecture).
   `Model` (`internal/tui/model.go`) holds *only* `activePath` plus two
   integers (`activeCursor`, `activeScroll`) — ancestor columns and the
   preview pane are **derived** on every render by walking
   `filepath.Dir(activePath)` and calling `fsutil.ListDir`/`ReadFilePreview`
   fresh, not cached. `Update()` (`internal/tui/update.go`) handles
   `tea.KeyMsg`/`tea.WindowSizeMsg`, mutating cursor/scroll/path/`showHidden`;
   errors set `statusErr` on the model rather than quitting. `render.go`
   implements `View()`, laying out ancestor/active/preview panes with
   lipgloss, adapting column count to terminal width (active-only below
   ~30 cols, min 15 cols/column). All I/O runs synchronously inside
   `Update`/`View` on the Bubble Tea event loop — **no goroutines, channels,
   or `tea.Cmd` async work**.
4. **`cmd/thicket`** — process entry point and terminal wiring
   (`cmd/thicket/main.go`). Opens `/dev/tty` directly (`os.OpenFile("/dev/tty", ...)`)
   for TUI input/output so the program still gets a real terminal even
   when its stdout is captured by shell command substitution; rebinds
   lipgloss's default renderer (`lipgloss.SetDefaultRenderer` +
   `tui.SetRenderer`) to that tty so color/border detection reflects the
   real display rather than the piped stdout. On quit, writes the chosen
   path to stdout (`writeSelection`) and exits 0; exits 1 on
   quit-without-selecting; exits 2 on any setup/runtime error.
5. **Shell wrappers** (`shell/thicket.bash`, `shell/thicket.zsh`) define a
   `th()` function: `dir=$(command thicket "$@") && [ -n "$dir" ] && cd -- "$dir"`.
   The binary itself never touches the calling shell's directory — the
   wrapper is the only thing that does.

```
th (shell function) → thicket binary (/dev/tty for UI) → stdout: chosen path
                                │
                    cmd/thicket/main.go
                          │            │
                   internal/tui        │
            (Model/Update/View,        │
             Bubble Tea)               │
                  │       │            │
                  │       └─────┬──────┘
                  ▼             ▼
        internal/fsutil   internal/marks (Load,
        (ListDir,          Save, DefaultPath)
         ReadFilePreview)
```

## Key Directories

| Path | Purpose |
|---|---|
| `cmd/thicket/` | CLI entry point (`main.go`); process/tty wiring, exit codes |
| `internal/tui/` | Bubble Tea `Model`/`Update`/`View` — navigation state machine and rendering |
| `internal/marks/` | Pure directory-marks (bookmark) persistence — letter→path table, load/save, no TUI concerns |
| `internal/fsutil/` | Pure filesystem helpers — directory listing, entry classification, file preview |
| `shell/` | `thicket` shell-function wrappers for bash and zsh (source into rc files) |
| `man/` | `thicket.1` troff man page (`man thicket` after install) |
| `docs/superpowers/plans/` | Implementation plan doc(s) — task breakdown with code templates |
| `docs/superpowers/specs/` | Design spec doc(s) — authoritative source for UX/behavior decisions (e.g. exit codes, layout thresholds, non-goals) |

## Development Commands

No Makefile/justfile/CI config exists — use the Go toolchain directly.

```sh
# Build the binary
go build -o thicket-bin ./cmd/thicket

# Install system-wide (README-documented flow)
sudo install -m 755 thicket-bin /usr/local/bin/thicket-bin

# Run all tests
go test ./...

# Static analysis
go vet ./...

# Run a single package's tests
go test ./internal/fsutil/...
go test -run TestListDir_SortsDirsFirstAndFiltersHidden ./internal/fsutil/...
```

Manual run without installing: `go run ./cmd/thicket [path]`.

## Code Conventions & Common Patterns

- **Naming**: exported identifiers PascalCase (`Entry`, `ListDir`,
  `ReadFilePreview`, `IndexOfName`, `New`, `Result`); unexported
  PascalCase-free camelCase (`activePath`, `classify`, `writeSelection`).
  Test functions use `TestFunc_Behavior` (e.g.
  `TestUpdate_RightIntoPermissionDeniedSetsStatusErrAndKeepsPath`,
  `TestView_StatusLineAt80ColumnsStillShowsError`) — descriptive enough to
  double as a regression changelog.
- **Error handling**: plain Go `error` return values, no wrapping helpers
  or sentinel error types. UI-facing errors are stored on `Model.statusErr`
  and rendered in the status line rather than crashing the TUI (e.g. a
  permission-denied directory sets `statusErr` and keeps the old path/entries).
  Fatal setup/runtime errors in `main.go` are printed to stderr as
  `"thicket: %v\n"` and mapped to exit codes 1 (quit without selecting) or
  2 (error).
- **State management**: single source of truth per `Model` —
  `activePath` + `activeCursor`/`activeScroll` integers. Everything else
  (ancestor columns, preview pane contents) is *recomputed from disk on
  every `View()` call* rather than cached — see `Model` doc comment in
  `internal/tui/model.go`. Do not introduce a pane cache without updating
  that documented invariant.
- **Concurrency**: none. Everything runs synchronously inside Bubble Tea's
  `Update`/`View` calls; no `tea.Cmd`, goroutines, or channels are used
  anywhere in the codebase.
- **Dependency injection**: none needed/used — `fsutil` functions are
  called directly by `internal/tui`; no interfaces/mocks for the
  filesystem layer. Package-level lipgloss styles in `internal/tui` are
  rebound post-`init()` via `tui.SetRenderer()` (called once from `main.go`)
  because they're constructed during Go's `init` phase before the tty is open.
- **Symlink handling convention**: `os.Lstat` first to detect a symlink,
  then `os.Stat` to resolve it for directory classification; broken
  symlinks are marked `Broken: true` rather than erroring the whole listing.

## Important Files

| File | Role |
|---|---|
| `cmd/thicket/main.go` | Process entry point: arg parsing, `/dev/tty` setup, lipgloss renderer rebinding, exit codes |
| `internal/tui/model.go` | `Model` struct + `New()`/`Init()`/`Result()` — state shape and invariants |
| `internal/tui/update.go` | Keyboard input → state transitions (navigation, hidden toggle, refresh) |
| `internal/tui/render.go` | `View()` — Miller-column layout, styling, terminal-width adaptation |
| `internal/fsutil/entry.go` | `Entry` struct (Name, IsDir, IsSymlink, Broken, Size, ModTime) |
| `internal/fsutil/listing.go` | `ListDir`, `IndexOfName`, `classify` — directory reading/sorting/symlink classification |
| `internal/fsutil/preview.go` | `ReadFilePreview`/`FilePreview` — binary detection, text line splitting |
| `internal/tui/search.go` | `firstMatch` — pure case-insensitive substring search over an already-loaded entry list, used by type-ahead search |
| `internal/marks/marks.go` | `Marks`, `Load`, `Save`, `DefaultPath` — the letter→path bookmark table and its on-disk format |
| `internal/tui/marks.go` | `sortedMarkLetters`, `marksListCursorFor` — pure helpers behind the marks list screen and `` ` ``/`'` navigation |
| `internal/tui/help.go` | `Keybindings` (single source of truth for the `?` help screen and `cmd/thicket --help`) and `renderHelp` |
| `shell/thicket.bash`, `shell/thicket.zsh` | `th()` wrapper functions that `cd` the calling shell |
| `man/thicket.1` | Troff man page — NAME/SYNOPSIS/OPTIONS/KEYS/EXIT STATUS/SHELL INTEGRATION, hand-maintained in sync with `internal/tui/help.go` and the README table |
| `go.mod` | Module `thicket`, Go 1.24.6, pins `bubbletea` to v1.x (not v2) |
| `README.md` | Install/usage instructions and the full keybinding table |
| `docs/superpowers/specs/2026-08-15-thicket-tui-file-browser-design.md` | Authoritative design spec (exit codes, layout thresholds, v1 non-goals) |
| `docs/superpowers/plans/2026-08-15-thicket-tui-file-browser.md` | Implementation plan with per-task code templates |

## Runtime/Tooling Preferences

- **Language/runtime**: Go 1.24.6 (per `go.mod`); no other language runtime
  involved.
- **Package manager**: standard Go modules (`go.mod`/`go.sum`) — no vendoring,
  no `go.work`.
- **Key dependencies**: `github.com/charmbracelet/bubbletea` v1.3.10
  (pinned to v1.x, deliberately not v2 per the design spec), `github.com/charmbracelet/lipgloss` v1.1.0, `github.com/muesli/termenv` v0.16.0 — all TUI/terminal-rendering libraries; remaining `go.mod` entries are transitive.
- **OS target**: Linux-only in practice — relies on `/dev/tty` and POSIX
  shell wrappers (bash/zsh). No Windows support implied anywhere.
- **No linter config** (no `.golangci.yml`), **no CI config** (no
  `.github/workflows`), **no Makefile/Dockerfile/.editorconfig/LICENSE** —
  rely on `gofmt`/`go vet`/`go build`/`go test` directly.
- `.gitignore` only excludes `.worktrees/` and `.superpowers/` — the built
  `thicket-bin` binary is not gitignored; avoid committing it if you build
  in-repo (`go build -o thicket-bin ./cmd/thicket` from the README literally
  writes it to the repo root).

## Testing & QA

- **Framework**: Go standard library `testing` only — no testify, no
  mocking library, no external test deps.
- **Run everything**: `go test ./...` from the repo root; `go vet ./...`
  for static checks. No project-specific test flags or build tags.
- **Fixtures**: `t.TempDir()` for isolated directory trees; shared helpers
  `mustMkdir(t, path)` / `mustWriteFile(t, path, content)` in
  `internal/fsutil/helpers_test.go` (both call `t.Helper()`); `setupFixture(t)`
  in `internal/tui` builds a reusable nested-directory tree for
  render/update tests.
- **Patterns**: table-free but heavily parametrized `TestXxx_Behavior`
  functions; `reflect.DeepEqual` for slice/struct comparison;
  `strings.Contains` for rendered-output assertions; `t.Cleanup` for
  restoring global state (e.g. `restoreColorProfile(t)` after lipgloss
  renderer tests, chmod reverts after permission-denied tests).
  Permission-denied tests guard with `if os.Geteuid() == 0 { t.Skip(...) }`
  since root bypasses filesystem permission checks.
- **Coverage shape**: `internal/fsutil` and `internal/tui/update.go` get
  pure-function unit tests against real temp-dir fixtures (including
  broken symlinks and unreadable directories); `internal/tui/render.go` is
  covered by string-content assertions on `View()` output at specific
  terminal widths (regression tests explicitly reference past bugs, e.g.
  `TestView_StatusLineAt80ColumnsStillShowsError`). `internal/marks` gets
  the same real-file-fixture treatment as `internal/fsutil` (round-trip,
  malformed-line, and permission-denied cases against `t.TempDir()`
  paths). Shell integration (`thicket` wrapper + `/dev/tty` behavior) and
  cross-process mark persistence are **not automated** — both are manual
  smoke tests only, since they require a real terminal or two separate
  process invocations sharing one marks file.
- When adding behavior to `Update`/`ListDir`/`ReadFilePreview`, add a
  matching `TestXxx_Behavior`-style case in the corresponding `*_test.go`
  file rather than a new test file, unless testing a new package.
