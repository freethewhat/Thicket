# thicket — directory marks (bookmarks)

**Status:** Approved for planning
**Date:** 2026-08-16
**Amends:** `docs/superpowers/specs/2026-08-15-thicket-tui-file-browser-design.md` §3,
which lists "no bookmarks, tabs, or split views beyond the single Miller-column
strip" as a v1 non-goal. This spec reverses the bookmarks half of that line;
tabs and split views remain out of scope. Every other non-goal in §3 (no file
create/rename/delete/copy/move, no config file/theme customization, no mouse
support, no filesystem watching) remains in force unchanged.

## 1. Summary

Add vim/ranger-style directory marks: `m<letter>` bookmarks the active
directory under a letter, `` `<letter> `` jumps straight to it, and `'` opens
a full-screen list of all marks for browsing/jumping/deleting without
remembering letters. Marks bookmark the *active directory path only* — not
individual files — consistent with thicket having no file-open action to
bookmark toward. Marks persist to disk across separate `thicket`
invocations, unlike every other piece of Model state (which is
session-only and derived from `activePath`).

## 2. Non-goals (this feature)

- No marking of individual files, only directories (`activePath` at the
  moment `m` is pressed).
- No confirmation prompt on overwriting an existing letter's mark — matches
  vim/ranger's silent-overwrite convention.
- No mark import/export, syncing, or sharing between machines.
- No numeric or auto-generated marks (e.g. "last 5 visited directories") —
  every mark is an explicit, user-chosen single letter (`a`-`z`, `A`-`Z`;
  52 slots).
- No editing a mark's target path in place — deleting (`d` in the list
  screen) and re-setting (`m<letter>` elsewhere) is the only path to change
  one.
- No tabs or split views — that half of the original non-goal line stays in
  force; marks are a single global letter→path table, not per-pane state.

## 3. New package: `internal/marks`

A new leaf package, parallel to `internal/fsutil` in the dependency graph
(`cmd/thicket → internal/tui → {internal/fsutil, internal/marks}`). Pure
disk I/O for the mark table; no Bubble Tea/TUI concerns, matching the
existing layering rule ("pure filesystem I/O" is `fsutil`'s charter — mark
persistence is the same kind of concern, just a different data shape).

```go
package marks

// Marks is a letter → absolute-directory-path table. Only 'a'-'z' and
// 'A'-'Z' are valid keys; callers are responsible for validating the rune
// before inserting (Update() in internal/tui does this at the keystroke
// boundary, per §5).
type Marks map[rune]string

// Load reads path and returns its Marks table. A missing file is not an
// error — it returns an empty, non-nil Marks. Malformed individual lines
// (wrong field count, non-letter key) are skipped, not fatal, mirroring
// fsutil.classify's per-entry error tolerance. Any other read error
// (e.g. permission denied on an existing file) is returned to the caller.
func Load(path string) (Marks, error)

// Save writes m to path as sorted `letter\tpath\n` lines — lowercase
// letters before uppercase, alphabetical within each case (vim/ranger's
// `:marks` convention; NOT plain ascending rune order, since 'A' (65) <
// 'a' (97) in ASCII — ascending-rune sort would put A-Z first). Sorted
// for deterministic file content/diffs and simpler testing. Creates the
// parent directory (mode 0o700) if absent.
func Save(path string, m Marks) error

// DefaultPath returns $XDG_STATE_HOME/thicket/marks, falling back to
// $HOME/.local/state/thicket/marks when XDG_STATE_HOME is unset.
func DefaultPath() (string, error)
```

- **File format:** one `letter\tpath` line per mark, `\n`-terminated,
  written lowercase-before-uppercase (see `Save`'s doc comment above —
  this is a custom comparator, not Go's default ascending-rune sort).
  Chosen over JSON/TOML for hand-editability and because the schema (one
  rune, one path, no nesting) doesn't benefit from a structured format.
  Directory paths containing an embedded tab or newline byte (legal but
  vanishingly rare on Linux) will not round-trip correctly — `Load` skips
  the resulting malformed line silently, same as any other corrupt line.
  Accepted as an unsupported edge case; not worth a structured/escaped
  format for a 52-slot letter table.
- **Load semantics:** `os.ReadFile` returning `os.IsNotExist` → empty
  `Marks{}`, `nil` error. Any other `ReadFile` error (permission denied,
  I/O error) → returned as-is. Per-line parsing: split on `\t`; a line that
  doesn't split into exactly 2 fields, or whose first field isn't exactly
  one ASCII letter (`a`-`z` or `A`-`Z`, matching §2's 52-slot bound — not
  `unicode.IsLetter`, which would accept non-ASCII letters and break that
  bound), is skipped silently —
  no partial-line error surfaces, matching the "one bad entry doesn't abort
  the whole read" convention already used in `fsutil.ListDir`/`classify`.
- **Save semantics:** `os.MkdirAll(filepath.Dir(path), 0o700)` then
  `os.WriteFile(path, ..., 0o600)`. Called synchronously right after every
  mutation (set or delete) — no batching, no debounce, consistent with the
  rest of the codebase's "no cache to invalidate" design principle (spec
  2026-08-15 §5 preamble).

## 4. Model changes (`internal/tui/model.go`)

```go
markTable       marksPkg.Marks // loaded once in New(); mutated in place
marksPath       string         // where Save() writes; set once in New()
markSetPending  bool           // true after `m`, awaiting a letter
markJumpPending bool           // true after `, awaiting a letter
marksListMode   bool           // true while the full-screen marks list is open
marksCursor     int            // cursor row within the marks list; -1 when empty
```

(Field is named `markTable`, not `marks`, to avoid colliding with the
`marksPkg "thicket/internal/marks"` import alias used throughout this
package. Decided here, not deferred to the implementation plan.)

All five fields are zero-valued/absent at construction except `markTable`
and `marksPath`, which `New()` populates:

```go
func New(startPath, marksPath string) (Model, error) {
    // ... existing startPath handling unchanged ...
    m.marksPath = marksPath
    loaded, err := marksPkg.Load(marksPath)
    if err != nil {
        return Model{}, err
    }
    m.markTable = loaded
    m.marksCursor = marksListCursorFor(loaded) // 0 if non-empty, -1 if empty
    return m, nil
}
```

**Signature change:** `New(startPath string)` becomes
`New(startPath, marksPath string)`. The only caller is `cmd/thicket/main.go`,
which computes `marksPath` via `marks.DefaultPath()` before calling `tui.New`
(and exits 2 on a `DefaultPath` error, same pattern as the existing tty-open
and `tui.New` error paths). `internal/tui`'s test suite has one shared
constructor helper, `newTestModel(t, path)` (`update_test.go`), used by
most tests, plus ~8 call sites that bypass it and call `New` directly
(`update_test.go` and `render_test.go`) — both `newTestModel` and every
direct call site need the new `marksPath` argument, each pointed at
`filepath.Join(t.TempDir(), "marks")` for a real, isolated file per test
with no `$XDG_STATE_HOME` environment coupling.

A `Load` error (e.g. an existing-but-permission-denied marks file) fails
`New()` exactly like a missing/inaccessible start-path does today — printed
to stderr by `main.go`, exit code 2. This is a deliberate choice over
silently starting with an empty in-memory mark table: silently ignoring a
real load error and then having the first `m<letter>` press overwrite the
file would be silent data loss for whatever marks already existed on disk.

## 5. Key handling (`internal/tui/update.go`)

Dispatch order in `Update()`'s `tea.KeyMsg` branch, extending the existing
early-return chain (`Ctrl-C` → `helpMode` → `searchMode` → normal mode):

```
Ctrl-C (hard quit, unchanged)
  → helpMode          (unchanged)
  → marksListMode      (new, same precedence tier as helpMode)
  → searchMode         (unchanged)
  → markSetPending      (new)
  → markJumpPending     (new)
  → normal mode         (adds m, `, ' as new cases)
```

`markSetPending`/`markJumpPending`/`marksListMode` are mutually exclusive
with each other and with `searchMode`/`helpMode` by construction — nothing
sets one while another is already true, and `Update()`'s early-return
dispatch guarantees only one branch runs per keystroke, the same guarantee
already documented for `helpMode`/`searchMode` in `model.go`.

**Normal mode → pending-letter modes:**

- `m`: `markSetPending = true`. No-op if already in any other mode (dead
  code path given dispatch order, but stated for clarity).
- `` ` `` (backtick): `markJumpPending = true`.
- `'`: `marksListMode = true`; `marksCursor` recomputed via
  `marksListCursorFor(m.markTable)` (0 if non-empty, `-1` if empty — mirrors
  `activeCursor`'s `-1`-when-empty convention).

**While `markSetPending` is true**, every key is consumed by this branch
(never reaches normal-mode handlers, matching the `searchMode` precedent):

| Key | Effect |
|---|---|
| `tea.KeyRunes`, exactly one rune in `a`-`z`/`A`-`Z` | `m.markTable[rune] = m.activePath`; `marks.Save(m.marksPath, m.markTable)` — on error, set `statusErr`, keep the in-memory entry; clear `markSetPending`. |
| Anything else (including Esc, multi-rune paste, digits, punctuation) | Cancel: clear `markSetPending`, no mutation. |

**While `markJumpPending` is true**, same total-consumption rule:

| Key | Effect |
|---|---|
| `tea.KeyRunes`, exactly one rune in `a`-`z`/`A`-`Z`, present in `m.markTable` | `target := m.markTable[rune]`; attempt `fsutil.ListDir(target, m.showHidden)`. Success: `activePath = target`, `activeEntries` = result, `activeCursor = 0` (or `-1` if empty), `activeScroll = 0`, `statusErr` cleared. Failure (directory deleted/permission-changed since marking): `activePath` unchanged, `statusErr = "mark " + string(rune) + ": " + err.Error()` — same shape as `handleRight`'s permission-denied handling. Clear `markJumpPending` either way. |
| `tea.KeyRunes`, exactly one rune in `a`-`z`/`A`-`Z`, absent from `m.markTable` | `statusErr = "no mark: " + string(rune)`; clear `markJumpPending`. |
| Anything else | Cancel: clear `markJumpPending`, no mutation, `statusErr` untouched. |

**While `marksListMode` is true:**

| Key | Effect |
|---|---|
| `up`, `k` | `marksCursor` moves -1, clamped to `[0, len(sortedMarks)-1]` (no-op if `-1`/empty). |
| `down`, `j` | `marksCursor` moves +1, same clamping. |
| `enter` | If `marksCursor >= 0`: jump to that mark's directory using the exact same success/failure logic as the `markJumpPending` table above (`ListDir` success → move + clear mode; failure → `statusErr`, stay in `marksListMode` so the user sees the error against the list rather than getting silently bounced to a blank normal-mode screen). If `marksCursor == -1` (empty list): no-op. |
| `d` | If `marksCursor >= 0`: delete that letter from `m.markTable`, `marks.Save(m.marksPath, m.markTable)`. On `Save` error: set `statusErr`, keep the deletion in memory (same disk/memory-diverges tradeoff as the `markSetPending` save-error case above), and skip the recompute/clamp below so the cursor stays put and the user can see the error. On success: clear `statusErr`, recompute the sorted list, clamp `marksCursor` to the new (possibly shorter) length, or `-1` if now empty. |
| `q`, `esc` | `marksListMode = false`. No mutation. |
| Everything else | No-op (explicit catch-all, mirrors `handleSearchKey`'s documented "Go's switch already does nothing" pattern). |

`Ctrl-C` is checked before all of the above (unconditional hard-quit,
unchanged from existing behavior) and works from every one of these new
states, same as it already does during `searchMode`.

## 6. Marks list logic (`internal/tui/marks.go`, new file)

Pure functions only — no state mutation — mirroring how `search.go` holds
only `firstMatch` while mutation lives in `update.go`:

```go
// sortedMarkLetters returns the letters of m sorted lowercase-before-
// uppercase, alphabetical within each case (vim/ranger's `:marks`
// convention — NOT Go's default ascending-rune sort, which would put
// 'A'-'Z' before 'a'-'z'), for stable, deterministic list-screen row
// order.
func sortedMarkLetters(m marksPkg.Marks) []rune

// marksListCursorFor returns 0 if m is non-empty, -1 if empty — the
// initial cursor position when marksListMode opens or after a delete
// changes the list length.
func marksListCursorFor(m marksPkg.Marks) int
```

## 7. Rendering (`internal/tui/render.go`)

**Marks list screen** — new `renderMarksList(rows int) string` method on
`Model`, structurally parallel to the existing `renderHelp(rows int)`
(full-screen replacement of the column layout, not an overlay — no
z-index compositing available, matching the established pattern for modal
states):

- Non-empty: one row per letter in `sortedMarkLetters` order,
  `"%c  %s"` (letter, two spaces, absolute path); the row at index
  `marksCursor` rendered with the existing `selectedStyle` (reverse video),
  same style used for the active column's highlighted row.
- Empty (`len(m.markTable) == 0`): single centered line `"no marks set"` —
  same centered-message convention as `[permission denied]` in
  `renderColumn`.
- Long paths wider than the pane: truncate with the existing `truncate()`
  helper (same trailing-`…` behavior as entry-name truncation).

**Status line** — `statusLine()`/`composeStatusLine()` gain awareness of
the three new modes for the left-side hint string (mirroring how
`searchMode` currently replaces the hint string with the live query):

- `markSetPending`: left side shows `mark: _` (awaiting-letter prompt).
- `markJumpPending`: left side shows `` jump to mark: _ ``.
- `marksListMode`: left side shows `↑/k ↓/j move · enter jump · d delete · q/esc close`.
- Normal-mode hint string gains `· m mark · ` \` jump · ' marks` (appended
  to the existing `... · / search · ? help · q quit` string).
- Right side (`statusErr`-or-`activePath` precedence) keeps its existing
  default behavior in `marksListMode` — no override, unlike the
  `helpMode`/`searchMode` branches that force `right = m.activePath`.
  This is required, not incidental: the `enter`-on-deleted-target case in
  §5's `marksListMode` table sets `statusErr` and deliberately stays in
  `marksListMode`, and that error must still surface on the status line
  while the marks list is showing. Outside of the new `statusErr` values
  already described in §5, no new right-side states are introduced.
- `View()` gains a `marksListMode` branch parallel to the existing
  `helpMode` one (`if m.helpMode { return header + "\n" + m.renderHelp(rows)
  + "\n" + m.statusLine() }`): `if m.marksListMode { return header + "\n" +
  m.renderMarksList(rows) + "\n" + m.statusLine() }`, checked in the same
  position (before `buildColumns`).

## 8. Documentation updates

- `internal/tui/help.go`'s `Keybindings` table gains four rows: `m`
  ("Bookmark the active directory under a letter"), `` ` `` ("Jump to a
  bookmarked directory by letter"), `'` ("Open the marks list"), and a
  contextual note that `d` deletes within the marks list (documented as
  its own row scoped to that screen, e.g. `d (in marks list)` —
  `--help`/`?`/README/man page all share this one source of truth per the
  existing convention, so this is a single edit point).
- `README.md`'s keybinding table and `man/thicket.1` get the same four
  rows (kept in sync manually, per the existing documented convention in
  `cmd/thicket/main.go`'s `helpText()` comment).
- `AGENTS.md`'s "v1 scope is intentionally locked to navigation only... no
  bookmarks" line needs a one-line update noting this amendment, matching
  how the type-ahead search amendment is already referenced in `AGENTS.md`'s
  opening paragraph.

## 9. Testing strategy

Follows the existing `TestXxx_Behavior` convention; new test file only for
the new package, per `AGENTS.md`'s rule ("add a matching test case to the
corresponding `*_test.go` file rather than a new test file, unless testing
a new package" — `internal/marks` is a new package here).

**`internal/marks/marks_test.go` (new file, new package):**
- `TestLoad_MissingFileReturnsEmptyMarks`
- `TestLoad_MalformedLineSkipped` (wrong field count, non-letter key,
  multi-rune key)
- `TestLoad_PermissionDeniedReturnsError` (skip under root, matching the
  existing `os.Geteuid() == 0` skip convention used elsewhere in the repo)
- `TestSaveThenLoad_RoundTrips`
- `TestSave_CreatesParentDirectory`
- `TestSave_SortsByLetterAscending` (a-z before A-Z, alphabetical within
  each case)
- `TestDefaultPath_UsesXDGStateHomeWhenSet`
- `TestDefaultPath_FallsBackToHomeLocalState`

**`internal/tui/update_test.go` additions:**
- `TestUpdate_MSetsMarkSetPending`
- `TestUpdate_MarkSetPendingLetterSavesMarkAndClearsPending`
- `TestUpdate_MarkSetPendingNonLetterCancelsWithoutMutation`
- `TestUpdate_MarkSetOverwritesExistingLetterSilently`
- `TestUpdate_BacktickSetsMarkJumpPending`
- `TestUpdate_MarkJumpPendingKnownLetterNavigatesAndClearsPending`
- `TestUpdate_MarkJumpPendingUnknownLetterSetsStatusErr`
- `TestUpdate_MarkJumpPendingDeletedTargetSetsStatusErrKeepsPath`
- `TestUpdate_MarkJumpPendingNonLetterCancelsWithoutMutation`
- `TestUpdate_QuoteOpensMarksListMode`
- `TestUpdate_MarksListUpDownClampsAtBothEnds`
- `TestUpdate_MarksListEnterNavigatesAndClosesList`
- `TestUpdate_MarksListEnterOnDeletedTargetSetsStatusErrStaysOpen`
- `TestUpdate_MarksListDDeletesHighlightedMarkAndClampsCursor`
- `TestUpdate_MarksListDOnEmptyListIsNoop`
- `TestUpdate_MarksListQAndEscCloseWithoutMutation`
- `TestUpdate_CtrlCQuitsFromEveryMarksMode` (parametrized/table-style over
  `markSetPending`/`markJumpPending`/`marksListMode`)

**`internal/tui/render_test.go` additions:**
- `TestView_MarksListEmptyShowsNoMarksSetMessage`
- `TestView_MarksListShowsSortedLetterPathRows`
- `TestView_MarksListHighlightsCursorRow`
- `TestView_StatusLineShowsMarkSetPrompt`
- `TestView_StatusLineShowsMarkJumpPrompt`
- `TestView_StatusLineShowsMarksListHints`

**Manual smoke test** (added to the existing manual-smoke-test list,
alongside the `/dev/tty` and type-ahead entries): mark a directory with
`m` + a letter, quit thicket entirely, relaunch it from a different
starting directory, press `` ` `` + the same letter, confirm it jumps to
the originally-marked directory — the one behavior (persistence across
process boundaries) no in-process automated test can cover.
