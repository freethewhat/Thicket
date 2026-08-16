# thicket — type-ahead search within the active column

**Status:** Approved for planning
**Date:** 2026-08-16
**Amends:** `docs/superpowers/specs/2026-08-15-thicket-tui-file-browser-design.md` §3,
which lists "no search or fuzzy-filter within a pane" as a v1 non-goal. This
spec reverses that one line; every other non-goal in §3 (no file
create/rename/delete/copy/move, no bookmarks/tabs/split views, no config
file, no mouse support, no filesystem watching) remains in force unchanged.

## 1. Summary

Add a `/`-triggered, vim-style incremental type-ahead search to the active
(focused) Miller column. The user presses `/`, types characters, and the
cursor jumps live to the first entry (top-to-bottom) in the active
directory's listing whose name contains the typed substring
(case-insensitive). Nothing is hidden or reordered — this is cursor
navigation aided by search, not a filter/fuzzy-narrow. Matching entries
outside the active column (ancestors, preview) are untouched; search only
ever operates on the currently visible `activeEntries` list already backing
the active column's render.

## 2. Non-goals (this feature)

- No fuzzy matching (no fzf-style out-of-order character scoring) — plain
  case-insensitive substring containment only.
- No filtering/hiding of non-matching rows — every row stays visible and in
  its existing sorted position; only the cursor moves.
- No search history across `/` invocations — every `/` starts from an empty
  query.
- No `n`/`N` repeat-search keys — the live incremental re-jump on every
  keystroke covers the "next match" need within a single query session.
- No cross-directory / recursive search — scope is strictly the active
  column's current entry list.
- Does not touch, and is not affected by, the `showHidden` filter's own
  toggle — search operates on whatever `activeEntries` already contains at
  the time of the keystroke.

## 3. Model changes

Four new fields on `tui.Model` (`internal/tui/model.go`), alongside the
existing `activeCursor`/`activeScroll`/`showHidden`:

```go
searchMode       bool   // true while the / prompt is open (composing a query)
searchQuery      string // accumulated query text; "" when not searching
searchNoMatch    bool   // true when searchQuery currently matches nothing
searchPrevCursor int    // activeCursor value at the moment `/` was pressed
```

All four are zero-valued (`false`, `""`, `false`, `0`) in the initial state
built by `New()` — no change to `New()`'s behavior or signature.

## 4. Key handling

`Update()` gains a mode branch, in `internal/tui/update.go` (mutating
search logic lives alongside the existing `handleLeft`/`handleRight`/
`handleEnter` helpers there; `internal/tui/search.go`, per §5, holds only
the pure `firstMatch` function — no state mutation):

- `Ctrl-C` (`msg.String() == "ctrl+c"`) always hard-quits (`selected =
  false`, `quitting = true`, `tea.Quit`), in every mode. It's a control
  code, never query text, so this doesn't collide with typing.
- If `m.searchMode` is true, **every other key** is handled by the search
  branch below and never reaches the normal-mode handlers.
- If `m.searchMode` is false, `/` (`msg.String() == "/"`) is a new
  normal-mode key; all existing normal-mode keys are unchanged.

### Normal mode → search mode

`/`: if `activeCursor >= 0` there are entries to search (if the directory is
empty, `/` still opens the prompt — see §7 edge cases). Sets
`searchMode = true`, `searchQuery = ""`, `searchNoMatch = false`,
`searchPrevCursor = activeCursor`.

### Classifying keys while `searchMode` is true

The existing codebase switches on `msg.String()` (e.g.
`case "q", "esc", "ctrl+c":`), which works for normal mode because every
normal-mode key is a distinct, known string. Search mode **must not** use
that pattern for "append to query": `msg.String()` returns a multi-character
name for every key bubbletea recognizes, including keys that must never
become query text — e.g. `"tab"`, `"pgdown"`, `"f1"`. A `default:` branch on
`msg.String()` that appended the string would silently inject the literal
text `"tab"` into the query on a stray Tab press. The discriminator is
**`msg.Type`**, not `msg.String()`:

| `msg.Type` | Effect |
|---|---|
| `tea.KeyRunes` | Append **every** rune in `msg.Runes` (not just the first) to `searchQuery` — a single `KeyMsg` can carry more than one rune (bracketed paste, composed input). Then recompute `firstMatch(activeEntries, searchQuery)` (§5): match at index `i` → `activeCursor = i`, reclamp `activeScroll` (reuse `clampScroll()`), `searchNoMatch = false`; no match → `activeCursor` unchanged (last successful match, or `searchPrevCursor` if none yet), `searchNoMatch = true`. |
| `tea.KeySpace` | bubbletea reports the space bar as this distinct type, not `KeyRunes` (`Key.String() == " "`, confirmed in `key.go`) — treat identically to `tea.KeyRunes` with `Runes = []rune{' '}`. Required for filenames containing spaces (e.g. `"My Documents"`) to be searchable at all. |
| `tea.KeyBackspace`, `searchQuery != ""` | Pop the last rune off `searchQuery`; recompute the match against the shorter query the same way. |
| `tea.KeyBackspace`, `searchQuery == ""` | Same as `tea.KeyEsc` (below). |
| `tea.KeyEnter` | Exit search mode: `searchMode = false`, `searchQuery = ""`, `searchNoMatch = false`. `activeCursor` is left exactly where the search left it (does **not** restore `searchPrevCursor`). A subsequent, separate Enter (now in normal mode) performs the ordinary select/cd action per the base spec. |
| `tea.KeyEsc` | Exit search mode the same way as Enter, except `activeCursor = searchPrevCursor` (reclamp `activeScroll`) — restores the pre-search position. |
| `tea.KeyUp`, `tea.KeyDown`, `tea.KeyLeft`, `tea.KeyRight` (dedicated arrow keys; NOT `hjkl`, which arrive as `tea.KeyRunes` and are query text) | No-op / swallowed. |
| Anything else (`tea.KeyTab`, `tea.KeyCtrlU`, `tea.KeyHome`, `tea.KeyPgUp`, `tea.KeyF1`, …) | No-op / swallowed — explicit catch-all, not left implicit. Readline-style query editing (Ctrl-U to clear, moving a cursor within the query, etc.) is out of scope; Backspace is the only edit operation. |
| `tea.WindowSizeMsg` | Processed exactly as in normal mode — resize always works, in every mode. |

Note `q`, `h`, `j`, `k`, `l`, `.`, `r` all arrive as `tea.KeyRunes` and are
therefore query text while `searchMode` is true — none of them can invoke a
command mid-search. Exiting search mode (Enter or Esc, one keystroke)
always restores access to them.

## 5. Matching logic

New file `internal/tui/search.go`, a pure function (mirrors the layering
rule: this is UI-list search over already-loaded entries, not filesystem
I/O, so it belongs in `internal/tui`, not `internal/fsutil`):

```go
// firstMatch returns the index of the first entry in entries (in list
// order) whose Name case-insensitively contains query, or -1 if query is
// empty or no entry matches.
func firstMatch(entries []fsutil.Entry, query string) int
```

Search always starts from index 0 of `activeEntries` for the *whole*
current query on every keystroke (not incremental-from-cursor) — this is
deliberately deterministic: a given query always lands on the same entry
regardless of prior cursor position.

## 6. Rendering (status line)

`internal/tui/render.go`'s `statusLine()`/`composeStatusLine()` already
split into a left side (key hints, or `statusErr`/`activePath`-priority
right side) — extend, no new style constants:

- **Left side**, while `searchMode`: the live query prompt, e.g. `/report`,
  replacing the normal key-hint string. Add `· / search` to the normal-mode
  hint string so the feature is discoverable. The prompt is subject to the
  same `composeStatusLine`/`truncate` width-fitting as the normal-mode
  hints — display-only truncation on very narrow terminals; the underlying
  `searchQuery` field itself is never truncated, only its rendering.
- **Right side, precedence while `searchMode` is true**: the slot is
  dedicated to search state — `no match` when `searchNoMatch`, otherwise
  the unchanged `activePath` — for the entire search session, reusing the
  existing error-style appearance for the `no match` case. A stale
  `statusErr` from before `/` was pressed (nothing clears it mid-search,
  since `r`/`.`/`Left`/`Right` are all unreachable while typing) is **not**
  displayed during this time; the `statusErr` field itself is untouched and
  reappears once search exits, if still set and not since cleared by a
  `reload()`.
- The preview pane already re-derives from `activeCursor` on every `View()`
  call (base spec §5); it therefore follows the cursor live as the user
  types, with no additional code required — called out explicitly here so
  it reads as an intended consequence of the existing derived-render
  architecture, not accidental scope creep.
- No other rendering changes: `buildColumns`, `buildAncestors`,
  `buildPreview`, `renderColumn` are untouched. Search only ever moves
  `activeCursor`/`activeScroll`, which those functions already consume;
  rows are never hidden or reordered.

## 7. Edge cases

- `/` on an empty active directory (`activeCursor == -1`): search mode
  opens; `firstMatch` always returns `-1` against an empty `activeEntries`,
  so every keystroke immediately shows `searchNoMatch = true`; `activeCursor`
  stays `-1` throughout; Esc/Enter/Backspace-to-empty exit cleanly with no
  special-casing needed beyond what's already specified.
- Search never triggers new filesystem I/O — it operates purely on the
  `activeEntries` slice already held by `Model` for the current render, so
  there is no new error surface (no new `statusErr` paths).
- `Left`/`Right` (change active directory) are unreachable while
  `searchMode` is true (they're normal-mode-only keys, and `l`/`h` are query
  text mid-search); search mode itself never needs to survive a directory
  change because a fresh `/` press always starts a new session in the newly
  active directory's listing.
- `.` (toggle hidden) and `r` (refresh) are unreachable mid-search for the
  same reason — by design, not an oversight; exiting search (Enter/Esc) is
  one keystroke away.
- Pressing `/` and immediately Enter or Esc, before typing anything
  (`searchQuery == ""`): both are well-defined no-ops — `activeCursor` was
  never moved from `searchPrevCursor`, so the prompt simply closes with the
  cursor unchanged.
- Keys outside §4's table (Tab, Ctrl-U, Home, PageUp, F-keys, …) are
  explicit no-ops per that table's catch-all row — not undefined behavior,
  and specifically not appended to the query as their `msg.String()` name.

## 8. Testing strategy

Follows the existing `TestXxx_Behavior` convention. Per `AGENTS.md`'s
testing rule ("add a matching test case to the corresponding `*_test.go`
file rather than a new test file, unless testing a new package" —
`internal/tui` is not a new package here), all new tests — including for
the standalone `firstMatch` function — go into the existing
`internal/tui/update_test.go`, not a new `search_test.go` file. Only
`internal/tui/search.go` (implementation) is new; no new test file.

`internal/tui/update_test.go` additions:
- `TestUpdate_SlashEntersSearchMode`
- `TestUpdate_SearchTypingJumpsToFirstMatch`
- `TestUpdate_SearchNoMatchKeepsCursorAndSetsFlag`
- `TestUpdate_SearchBackspaceShrinksQueryAndRejumps`
- `TestUpdate_SearchBackspaceOnEmptyQueryExitsAndRestoresCursor`
- `TestUpdate_SearchEscRestoresPreSearchCursor`
- `TestUpdate_SearchEnterCommitsAndKeepsCursor`
- `TestUpdate_SearchImmediateEnterIsNoop`
- `TestUpdate_SearchImmediateEscIsNoop`
- `TestUpdate_SearchLettersLikeQAndHDoNotTriggerNavCommands`
- `TestUpdate_SearchSpaceKeyAppendsLiteralSpace`
- `TestUpdate_SearchMultiRuneKeyMsgAppendsAllRunes`
- `TestUpdate_SearchArrowKeysAreNoOps`
- `TestUpdate_SearchTabAndOtherControlKeysAreNoOps`
- `TestUpdate_CtrlCQuitsEvenDuringSearch`
- `TestUpdate_SlashOnEmptyDirectorySetsNoMatchImmediately`
- `TestFirstMatch_EmptyQueryReturnsNegativeOne`
- `TestFirstMatch_CaseInsensitiveSubstring`
- `TestFirstMatch_ReturnsFirstInListOrderOnMultipleMatches`
- `TestFirstMatch_NoMatchReturnsNegativeOne`

`internal/tui/render_test.go` additions:
- `TestView_StatusLineShowsSearchPromptWhileTyping`
- `TestView_StatusLineShowsNoMatchIndicator`
- `TestView_StatusLineSearchSuppressesStaleStatusErr`

Manual smoke test (added to the existing manual-smoke-test list, since it's
part of the same `/dev/tty` interactive path already called out as
untestable headlessly): launch `thicket` in a directory with several
similarly-named entries, press `/`, type a substring, confirm the cursor
jumps live per keystroke, confirm Esc restores the original cursor, confirm
Enter commits and a following Enter selects the entry.

## 9. Documentation updates (implementation-plan scope, not spec scope)

`README.md`'s keybinding table and `AGENTS.md`'s non-goals line both need a
one-line update to reflect this feature once implemented. Left to the
implementation plan's cleanup phase, not detailed further here.
