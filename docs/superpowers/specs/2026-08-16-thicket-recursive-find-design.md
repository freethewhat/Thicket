# thicket — recursive find under the active directory

**Status:** Approved for planning
**Date:** 2026-08-16
**Amends:** `docs/superpowers/specs/2026-08-15-thicket-tui-file-browser-design.md` §3,
which lists "no search or fuzzy-filter within a pane" as a v1 non-goal — already
reversed once by `docs/superpowers/specs/2026-08-16-thicket-type-ahead-search-design.md`
for single-column type-ahead search. This spec reverses the remaining part of
that line: search is no longer confined to a single already-visible column.
Every other non-goal in the base spec's §3 (no file create/rename/delete/copy/
move, no tabs/split views, no config file, no mouse support, no filesystem
watching) remains in force unchanged. This feature is independent of, and
does not modify, the directory-marks feature
(`docs/superpowers/specs/2026-08-16-thicket-directory-marks-design.md`).

## 1. Summary

Add an `f`-triggered recursive find over the subtree rooted at the active
directory. Pressing `f` walks `activePath` and everything under it once,
capturing both files and directories (bounded by a depth cap and an
entry-count cap), and replaces the Miller-column view with a full-screen,
live-filtered result list — the same full-screen-replacement pattern
`helpMode`/`renderHelp` already establishes. Typing filters the
already-captured list in memory (no repeated filesystem walks per
keystroke); `Up`/`Down` move a selection cursor over the filtered results;
`Enter` relocates the Miller-column view to the selected match's parent
directory and returns to normal mode, exactly mirroring how `/` type-ahead
search commits, so a second `Enter` performs the ordinary cd+exit.

## 2. Non-goals (this feature)

- No fuzzy matching — plain case-insensitive substring containment, same
  rule `firstMatch` (type-ahead search) already uses.
- No asynchronous/background walking. The whole codebase is deliberately
  synchronous (`AGENTS.md`: "no goroutines, channels, or `tea.Cmd` async
  work anywhere"); this feature does not introduce an exception. The walk
  is bounded by hard caps instead (§3).
- No re-walking the filesystem while typing — the walk happens once, when
  `f` is pressed; every keystroke after that only re-filters the
  already-captured in-memory result list.
- No following symlinked directories during the walk (no new cycle-risk
  class — see §3). A symlinked directory still appears as a leaf result,
  it is just never descended into.
- No guarantee of a complete result set. Permission-denied subdirectories
  are skipped silently, and the two caps mean very large or very deep
  subtrees are truncated. This is a best-effort convenience scan, not an
  index, matching the posture the base spec already takes on symlink loops
  (§7: "same way the shell's own `cd` does").
- No search root other than the active directory. `f` always walks the
  subtree under whatever `activePath` currently is — no whole-filesystem
  mode, no configurable root.
- No cross-session result caching. Every `f` press starts a fresh walk;
  nothing persists once find mode exits.

## 3. `internal/fsutil`: `WalkSubtree`

New file `internal/fsutil/walk.go`. Pure I/O, no caching internally — same
layering rule `ListDir` already follows (`AGENTS.md`: "`internal/fsutil` —
pure filesystem I/O... No caching, no state").

```go
const (
	walkMaxDepth   = 12    // descent levels below the root
	walkMaxEntries = 20000 // total entries visited, whichever cap is hit first stops the walk
)

// WalkEntry is one node found under a WalkSubtree root.
type WalkEntry struct {
	Entry           // Name, IsDir, IsSymlink, Broken, Size, ModTime
	RelPath string  // path relative to the walk root, e.g. "sub/dir/file.go"
}

// WalkSubtree recursively lists root and everything under it (files and
// directories), reusing the same per-entry classification ListDir already
// uses (Lstat for symlink detection, Stat-follow for directory
// classification). showHidden is honored at every level exactly like
// ListDir: a dotfile/dotdir is skipped, and a skipped directory is never
// descended into — a hidden subtree contributes nothing to the walk when
// showHidden is false. Symlinked directories are included as leaf
// WalkEntry results but are never traversed, regardless of depth (no
// symlink-cycle risk — see non-goals). Permission-denied subdirectories
// are skipped silently; the walk continues with everything else. The walk
// stops as soon as walkMaxDepth or walkMaxEntries is reached, whichever
// comes first; truncated reports whether that happened before the whole
// subtree was covered.
func WalkSubtree(root string, showHidden bool) (entries []WalkEntry, truncated bool, err error)
```

`err` is non-nil only if `root` itself cannot be listed (mirrors
`ListDir`'s error contract exactly). Errors on subdirectories deeper in the
tree are never surfaced as `err` — they are silently skipped per the
non-goals above.

## 4. `internal/tui`: Model changes

New fields on `tui.Model` (`internal/tui/model.go`), grouped and commented
the same way the existing `searchMode`/`markSetPending` groups are:

```go
// findMode/findQuery/findResults/findCursor/findTruncated: recursive find
// state (spec docs/superpowers/specs/2026-08-16-thicket-recursive-find-design.md).
// Zero-valued at construction, same as searchMode/helpMode/markSetPending.
// findResults is captured once when f is pressed and held for the
// session; it is never re-walked while findMode is true, only re-filtered.
// Mutually exclusive with searchMode/helpMode/markSetPending/
// markJumpPending/marksListMode — Update's early-return dispatch order
// guarantees only one mode is ever active.
findMode      bool
findQuery     string
findResults   []fsutil.WalkEntry
findCursor    int  // index into the *filtered* view; -1 when the filtered view is empty
findTruncated bool // mirrors WalkSubtree's truncated return for the current findResults
```

No change to `New()`'s signature or behavior.

## 5. Key handling

`Update()` (`internal/tui/update.go`) gains one more mode branch, added to
the existing early-return chain (order among the mutually-exclusive modes
does not matter; placed alongside `searchMode`'s branch since the two share
the most logic in spirit):

```go
if m.findMode {
	m.handleFindKey(msg)
	return m, nil
}
```

`Ctrl-C` continues to hard-quit unconditionally, in every mode, checked
before any mode branch — unchanged.

### Normal mode → find mode

`f` (new normal-mode key, alongside the existing `up`/`down`/`pgup`/
`pgdown`/`home`/`end`/`right`/`left`/`enter`/`q`/`esc`/`.`/`r`/`/`/`?`/`m`/
`` ` ``/`'` switch in `Update`): calls `fsutil.WalkSubtree(m.activePath,
m.showHidden)`.

- If it errors (root itself unreadable): set `statusErr`, do not enter find
  mode — mirrors `handleRight`'s existing treatment of a failed `ListDir`.
- Otherwise: `findResults` = the walk output, `findTruncated` = the walk's
  `truncated` return, `findMode = true`, `findQuery = ""`, `findCursor = 0`
  if `findResults` is non-empty else `-1`.

### Classifying keys while `findMode` is true

Same `msg.Type` discrimination rule the type-ahead search spec established
(`msg.String()` is unsafe as a query-text discriminator — it returns
readable names like `"tab"`/`"pgdown"` for keys that must never become
query text):

| `msg.Type` | Effect |
|---|---|
| `tea.KeyRunes` | Append every rune in `msg.Runes` to `findQuery`. Recompute `filterWalk(findResults, findQuery)` (§6); `findCursor` resets to `0` if the filtered list is non-empty, else `-1`. |
| `tea.KeySpace` | Treated identically to `tea.KeyRunes` with `Runes = []rune{' '}` — same rationale as type-ahead search: filenames containing spaces must be searchable. |
| `tea.KeyBackspace`, `findQuery != ""` | Pop the last rune off `findQuery`; recompute the filter and `findCursor` the same way. |
| `tea.KeyBackspace`, `findQuery == ""` | Same as `tea.KeyEsc` (below). |
| `tea.KeyUp` | `findCursor` moves up by 1 within the current filtered view, clamped at `0`. No-op if the filtered view is empty. This is the one behavioral departure from `/`: find mode keeps a real list cursor, not a single first-match jump. |
| `tea.KeyDown` | `findCursor` moves down by 1 within the current filtered view, clamped at the last index. No-op if the filtered view is empty. |
| `tea.KeyLeft`, `tea.KeyRight` | No-op / swallowed (dedicated arrow keys only — `hjkl` arrive as `tea.KeyRunes` and are query text, same convention `/` already uses). |
| `tea.KeyEnter` | If `findCursor >= 0`: let `we := filtered[findCursor]` (the selected `WalkEntry`). Set `m.activePath = filepath.Join(m.activePath, filepath.Dir(we.RelPath))` (or `m.activePath` unchanged if `filepath.Dir(we.RelPath) == "."`), `m.reload()` to repopulate `activeEntries` for that directory, `m.activeCursor = fsutil.IndexOfName(m.activeEntries, we.Name)`, reclamp `activeScroll`. Exit find mode (`findMode = false`, `findQuery = ""`). If `findCursor == -1` (empty query or no matches): no-op, same as `/`'s immediate-Enter case — the prompt simply closes with no relocation. |
| `tea.KeyEsc` | Exit find mode (`findMode = false`, `findQuery = ""`), no change to `activePath`/`activeCursor`/`activeScroll`. |
| Anything else (`tea.KeyTab`, `tea.KeyCtrlU`, `tea.KeyHome`, `tea.KeyPgUp`, `tea.KeyF1`, …) | No-op / swallowed — explicit catch-all, same posture as `/`. |
| `tea.WindowSizeMsg` | Processed exactly as in every other mode — resize always works. |

Note `q`, `h`, `j`, `k`, `l`, `.`, `r`, `m`, `` ` ``, `'` all arrive as
`tea.KeyRunes` and are therefore query text while `findMode` is true — none
of them can invoke a command mid-find, identical to how `/` already
neutralizes them.

## 6. Matching logic

New addition to `internal/tui/search.go` (same file `firstMatch` already
lives in — this is UI-list filtering over already-captured entries, not
filesystem I/O, so it belongs in `internal/tui`, not `internal/fsutil`,
same layering rule `firstMatch`'s doc comment already states):

```go
// filterWalk returns the entries in results (in walk order) whose RelPath
// case-insensitively contains query. An empty query matches every entry
// (returns results unchanged, in walk order) — deliberately diverging
// from firstMatch's "empty query matches nothing" convention: find mode
// is a browsable list, useful immediately after f is pressed, not only
// once the user starts typing (§5: findCursor starts at 0, not -1, when
// findResults is non-empty).
func filterWalk(results []fsutil.WalkEntry, query string) []fsutil.WalkEntry
```

Filtering always re-scans the full `findResults` slice for the whole
current query on every keystroke (not incremental-from-cursor) — same
determinism rule §5 of the type-ahead search spec already established: a
given query always produces the same filtered list regardless of prior
state.

## 7. Rendering

`internal/tui/render.go`'s `View()` gains a third full-screen branch,
checked before the existing help/column branches:

```go
if m.findMode {
	return m.renderFind(rows)
}
```

`renderFind` mirrors `renderHelp`'s structure (full-screen replacement of
the column layout, not an overlay — lipgloss has no z-index compositing):

- A query prompt line, e.g. `find/ report` (same `/`-prefixed convention
  the status-line search prompt already uses, adapted to `find` instead of
  the bare `/`).
- The filtered result list, one `RelPath` per row, the row at `findCursor`
  in `selectedStyle`. Directory entries keep `dirStyle` bold; symlinks keep
  the dim trailing `@`; the width-fitting `truncate` helper (already
  shared by `renderColumn`/`composeStatusLine`) truncates any `RelPath`
  wider than the pane.
- If `findResults` is empty, or the filtered list is empty for the current
  query: a single centered `no matches` line, reusing the existing
  error-style treatment `searchNoMatch` already established for the same
  situation in type-ahead search.
- If `findTruncated` is true: a trailing `… truncated, refine your query`
  line, the same phrasing pattern the existing 1000-entry directory-preview
  cap already uses for its `… N more` marker (base spec §7).
- Match/result count shown alongside the prompt, e.g. `find/report (3)`,
  so the user can tell at a glance whether narrowing further is needed.

**Status-line hint:** add `f find` to the normal-mode key-hint string,
alongside the existing `/ search`.

## 8. Edge cases

- `f` when `WalkSubtree` fails on `activePath` itself (e.g. it became
  unreadable since the last reload): `statusErr` is set, find mode never
  opens — same treatment `handleRight` already gives a failed `ListDir`.
- `f` on an empty active directory: the walk still runs (finds nothing
  below an empty root), `findResults` is empty, `findCursor = -1`, find
  mode still opens — matches `/`'s existing "opens on an empty directory"
  edge case (type-ahead search spec §7).
- Per-subdirectory permission errors during the walk are swallowed, never
  surfacing a new `statusErr` path — same "no new error surface" posture
  the type-ahead search spec already committed to for its own feature.
- `showHidden` toggling is unreachable while `findMode` is true (`.` is
  query text mid-find, same as every other normal-mode key). `findResults`
  reflects whatever `showHidden` was at the moment `f` was pressed; a fresh
  `f` press after toggling re-walks with the new setting.
- Symlinked directories at any depth (including immediate children of
  `activePath`) are included as leaf `WalkEntry` results but never
  descended into — no special-casing for depth-0 vs. deeper; broken
  symlinks are included with `Broken: true`, same as `ListDir` already
  reports them.
- Pressing `f` then immediately `Enter` or `Esc`, before typing anything
  (`findQuery == ""`, `findCursor` at whatever the initial `0`/`-1` was):
  both are well-defined — `Esc` is always a no-op-relocation exit; `Enter`
  relocates to the first walk-order result if `findCursor == 0` (i.e. the
  walk found something and the user accepts the top entry without typing),
  or no-ops if `findCursor == -1` (no results at all).
- `Up`/`Down` at the ends of the filtered list clamp rather than wrap —
  same clamping convention `moveCursor` already uses for the normal-mode
  column cursor.
- A subsequent `f` press while normal mode is active (find mode already
  exited) always starts a fresh walk and a fresh empty query — no state
  survives a find session, same "no cross-session caching" rule as §2.

## 9. Testing strategy

Follows the existing `TestXxx_Behavior` convention. Per `AGENTS.md`'s
testing rule, new cases go into existing test files for existing packages;
`internal/fsutil/walk_test.go` is a new file only because `WalkSubtree` is
new exported surface in an existing package (same precedent
`preview_test.go` set for `ReadFilePreview`).

`internal/fsutil/walk_test.go` (new file):
- `TestWalkSubtree_FindsNestedFilesAndDirs`
- `TestWalkSubtree_SkipsHiddenWhenShowHiddenFalse`
- `TestWalkSubtree_DoesNotDescendIntoSymlinkedDir`
- `TestWalkSubtree_SkipsPermissionDeniedSubdir`
- `TestWalkSubtree_StopsAtMaxDepthAndSetsTruncated`
- `TestWalkSubtree_StopsAtMaxEntriesAndSetsTruncated`
- `TestWalkSubtree_RelPathIsRelativeToRoot`
- `TestWalkSubtree_ErrorsOnUnreadableRoot`

`internal/tui/update_test.go` additions:
- `TestUpdate_FEntersFindModeAndWalksSubtree`
- `TestUpdate_FOnUnreadableRootSetsStatusErrAndSkipsFindMode`
- `TestUpdate_FOnEmptyDirectoryOpensFindModeWithNoResults`
- `TestUpdate_FindTypingFiltersResultsLive`
- `TestUpdate_FindBackspaceShrinksQueryAndRefilters`
- `TestUpdate_FindBackspaceOnEmptyQueryExitsWithNoRelocation`
- `TestUpdate_FindUpDownMovesResultCursorClampedAtEnds`
- `TestUpdate_FindEnterRelocatesActivePathToMatchParentAndExits`
- `TestUpdate_FindEnterOnEmptyResultsIsNoop`
- `TestUpdate_FindEscExitsWithNoRelocation`
- `TestUpdate_FindLettersLikeQAndHDoNotTriggerNavCommands`
- `TestUpdate_FindArrowLeftRightAreNoOps`
- `TestUpdate_FindTabAndOtherControlKeysAreNoOps`
- `TestUpdate_CtrlCQuitsEvenDuringFind`
- `TestFilterWalk_CaseInsensitiveSubstringOverRelPath`
- `TestFilterWalk_EmptyQueryMatchesNothing`

`internal/tui/render_test.go` additions:
- `TestView_FindModeShowsFullScreenResultList`
- `TestView_FindModeShowsNoMatches`
- `TestView_FindModeShowsTruncatedIndicator`
- `TestView_FindModeShowsMatchCount`

Manual smoke test (added to the existing manual-smoke-test list, part of
the same `/dev/tty` interactive path already called out as untestable
headlessly): launch `thicket` in a directory with several nested
subdirectories and similarly-named files, press `f`, confirm the walk
completes and shows results, type a substring, confirm live filtering,
confirm `Up`/`Down` move the selection among multiple matches, confirm
`Enter` relocates the Miller-column view to the match's parent with the
cursor on the match, confirm a following `Enter` cds, confirm `Esc` from a
fresh `f` press leaves `activePath` untouched.

## 10. Documentation updates (implementation-plan scope, not spec scope)

`README.md`'s keybinding table, `internal/tui/help.go`'s `Keybindings`
table (which `cmd/thicket --help` also renders from), `man/thicket.1`, and
`AGENTS.md`'s non-goals line all need a one-line update to reflect this
feature once implemented — left to the implementation plan's cleanup
phase, not detailed further here, same as the type-ahead search spec
handled its own documentation updates.
