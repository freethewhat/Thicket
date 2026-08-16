# Type-Ahead Search Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `/`-triggered, vim-style incremental type-ahead search to
`thicket`'s active Miller column — typing jumps the cursor live to the
first entry containing the typed substring; nothing is hidden or
reordered.

**Architecture:** A mode branch in the existing synchronous
`Model.Update()` (Bubble Tea MVU, no goroutines/`tea.Cmd`): four new fields
on `Model` track search state; while `searchMode` is true, every key is
dispatched to a new `handleSearchKey` method instead of the normal-mode
switch. A new pure function `firstMatch` (new file
`internal/tui/search.go`) does the actual substring search over the
already-loaded `activeEntries` slice — no new filesystem I/O. `render.go`'s
`statusLine()` grows a search-aware branch for the prompt/no-match display.

**Tech Stack:** Go 1.24.6, `github.com/charmbracelet/bubbletea` v1.3.10
(pinned to v1.x), `github.com/charmbracelet/lipgloss` v1.1.0. Standard
library `testing` only — no testify, no mocks.

**Spec:** `docs/superpowers/specs/2026-08-16-thicket-type-ahead-search-design.md`
(amends `docs/superpowers/specs/2026-08-15-thicket-tui-file-browser-design.md`
§3's "no search" non-goal — every other v1 non-goal there still holds).

## Global Constraints

- No goroutines, channels, or `tea.Cmd` anywhere — `Update`/`View` stay
  synchronous (`AGENTS.md` "Concurrency" invariant).
- `internal/fsutil` is pure filesystem I/O only; UI-list search logic
  belongs in `internal/tui` (`internal/tui/search.go`), not `fsutil`.
- Case-insensitive string comparison follows the existing convention in
  `internal/fsutil/listing.go`: `strings.ToLower(a) `-based comparison, not
  `strings.EqualFold` or a new dependency.
- Per `AGENTS.md`'s testing rule, add test cases to the existing
  `internal/tui/update_test.go` / `internal/tui/render_test.go` files
  rather than new test files — `internal/tui` is not a new package. Test
  functions use `TestFunc_Behavior` naming (e.g.
  `TestUpdate_SearchTypingJumpsToFirstMatch`).
- No linter/CI config exists — verify with `go build ./... && go vet ./...`
  and scoped `go test ./internal/tui/... -run '<pattern>' -v`, not the
  full suite, per task.
- Commit messages follow this repo's existing `scope: short description`
  style (see `git log --oneline`, e.g. `tui: give status-line right side
  priority over hints`).
- Discriminate search-mode keys on `tea.KeyMsg.Type`, never
  `tea.KeyMsg.String()` — `String()` returns a name for every key
  bubbletea recognizes (`"tab"`, `"pgdown"`, ...), so a string-based
  default would leak those names into the query.

---

## Task 1: `firstMatch` — pure substring search

**Files:**
- Create: `internal/tui/search.go`
- Modify: `internal/tui/update_test.go` (add `"thicket/internal/fsutil"` to
  the import block)

**Interfaces:**
- Produces: `func firstMatch(entries []fsutil.Entry, query string) int` —
  index of the first entry (list order) whose `Name` case-insensitively
  contains `query`, or `-1` if `query == ""` or nothing matches. Used by
  Task 3's `applySearchMatch`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/update_test.go` (append at the end of the file):

```go
func TestFirstMatch_EmptyQueryReturnsNegativeOne(t *testing.T) {
	entries := []fsutil.Entry{{Name: "alpha"}, {Name: "beta"}}
	if got := firstMatch(entries, ""); got != -1 {
		t.Fatalf("firstMatch with empty query = %d, want -1", got)
	}
}

func TestFirstMatch_CaseInsensitiveSubstring(t *testing.T) {
	entries := []fsutil.Entry{{Name: "Reports"}, {Name: "budget.csv"}}
	if got := firstMatch(entries, "REPO"); got != 0 {
		t.Fatalf("firstMatch(%q) = %d, want 0", "REPO", got)
	}
}

func TestFirstMatch_ReturnsFirstInListOrderOnMultipleMatches(t *testing.T) {
	entries := []fsutil.Entry{{Name: "a-report"}, {Name: "b-report"}}
	if got := firstMatch(entries, "report"); got != 0 {
		t.Fatalf("firstMatch = %d, want 0 (first in list order)", got)
	}
}

func TestFirstMatch_NoMatchReturnsNegativeOne(t *testing.T) {
	entries := []fsutil.Entry{{Name: "alpha"}, {Name: "beta"}}
	if got := firstMatch(entries, "zzz"); got != -1 {
		t.Fatalf("firstMatch with no match = %d, want -1", got)
	}
}
```

Update the import block at the top of `internal/tui/update_test.go` to:

```go
import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"thicket/internal/fsutil"
)
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/... -run TestFirstMatch -v`
Expected: FAIL with `undefined: firstMatch`.

- [ ] **Step 3: Write the implementation**

Create `internal/tui/search.go`:

```go
package tui

import (
	"strings"

	"thicket/internal/fsutil"
)

// firstMatch returns the index of the first entry in entries (in list
// order) whose Name case-insensitively contains query, or -1 if query is
// empty or no entry matches. Search always scans from the top of the list
// for the whole current query (spec §5) — deterministic regardless of
// prior cursor position.
func firstMatch(entries []fsutil.Entry, query string) int {
	if query == "" {
		return -1
	}
	q := strings.ToLower(query)
	for i, e := range entries {
		if strings.Contains(strings.ToLower(e.Name), q) {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/... -run TestFirstMatch -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/search.go internal/tui/update_test.go
git commit -m "tui: add firstMatch pure substring search"
```

---

## Task 2: Enter search mode (`/`) and cancel with Esc

**Files:**
- Modify: `internal/tui/model.go` (add four fields to `Model`)
- Modify: `internal/tui/update.go` (mode branch in `Update`, `/` case, new
  methods)
- Modify: `internal/tui/update_test.go` (new tests)

**Interfaces:**
- Consumes: nothing new from other tasks.
- Produces: `Model.searchMode bool`, `Model.searchQuery string`,
  `Model.searchNoMatch bool`, `Model.searchPrevCursor int` — consumed by
  Tasks 3–6. `(m *Model) enterSearchMode()`, `(m *Model) exitSearchMode(restoreCursor bool)`,
  `(m *Model) handleSearchKey(msg tea.KeyMsg)` — `exitSearchMode` and the
  `Update` mode-branch guard are final as written here; `handleSearchKey`
  gains more `case`s in Tasks 3–4 but keeps this exact signature.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/update_test.go`:

```go
func TestUpdate_SlashEntersSearchMode(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	beforeCursor := m.activeCursor

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)

	if !m.searchMode {
		t.Fatal("expected searchMode == true after /")
	}
	if m.searchQuery != "" {
		t.Fatalf("searchQuery = %q, want empty", m.searchQuery)
	}
	if m.searchNoMatch {
		t.Fatal("expected searchNoMatch == false right after /")
	}
	if m.searchPrevCursor != beforeCursor {
		t.Fatalf("searchPrevCursor = %d, want %d", m.searchPrevCursor, beforeCursor)
	}
}

func TestUpdate_SearchImmediateEscIsNoop(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	prevCursor := m.activeCursor

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("Esc during search must not quit the program")
	}
	if m.searchMode {
		t.Fatal("expected searchMode == false after Esc")
	}
	if m.activeCursor != prevCursor {
		t.Fatalf("activeCursor = %d, want unchanged %d", m.activeCursor, prevCursor)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/... -run 'TestUpdate_Slash|TestUpdate_SearchImmediateEsc' -v`
Expected: FAIL — `m.searchMode` etc. don't exist yet (compile error).

- [ ] **Step 3: Write the implementation**

In `internal/tui/model.go`, add four fields to the `Model` struct (after
the existing `statusErr string` field):

```go
type Model struct {
	activePath    string
	activeEntries []fsutil.Entry
	activeCursor  int // -1 when the active directory is empty
	activeScroll  int
	showHidden    bool
	statusErr     string
	// searchMode/searchQuery/searchNoMatch/searchPrevCursor: type-ahead
	// search state (spec docs/superpowers/specs/2026-08-16-thicket-type-ahead-search-design.md).
	// Zero-valued at construction — no change to New()'s behavior.
	searchMode       bool
	searchQuery      string
	searchNoMatch    bool
	searchPrevCursor int
	width            int
	height           int
	quitting         bool
	selected         bool
	chosenPath       string
}
```

In `internal/tui/update.go`, replace the `tea.KeyMsg` case of `Update` to
add the mode branch and the `/` key:

```go
	case tea.KeyMsg:
		if m.searchMode {
			m.handleSearchKey(msg)
			return m, nil
		}
		switch msg.String() {
		case "up", "k":
			m.moveCursor(-1)
		case "down", "j":
			m.moveCursor(1)
		case "right", "l":
			m.handleRight()
		case "left", "h":
			m.handleLeft()
		case "enter":
			m.handleEnter()
			return m, tea.Quit
		case "q", "esc", "ctrl+c":
			m.quitting = true
			m.selected = false
			return m, tea.Quit
		case ".":
			m.showHidden = !m.showHidden
			m.reload()
		case "r":
			m.reload()
		case "/":
			m.enterSearchMode()
		}
```

Append these new methods to the end of `internal/tui/update.go`:

```go
// enterSearchMode opens the type-ahead search prompt (spec §4). It never
// touches activeEntries/activePath — only the query state and a saved
// cursor to restore on cancel.
func (m *Model) enterSearchMode() {
	m.searchMode = true
	m.searchQuery = ""
	m.searchNoMatch = false
	m.searchPrevCursor = m.activeCursor
}

// exitSearchMode closes the prompt. If restoreCursor is true, activeCursor
// is reset to the value it had before / was pressed (Esc / empty-backspace
// behavior, spec §4); otherwise activeCursor is left wherever the search
// moved it (Enter behavior).
func (m *Model) exitSearchMode(restoreCursor bool) {
	m.searchMode = false
	m.searchQuery = ""
	m.searchNoMatch = false
	if restoreCursor {
		m.activeCursor = m.searchPrevCursor
		m.clampScroll()
	}
}

// handleSearchKey processes one key while searchMode is true (spec §4).
// Discriminates on msg.Type, not msg.String() — see Global Constraints.
// Any msg.Type not matched by a case below (arrows, Tab, Ctrl-U, Home,
// PageUp, F-keys, ...) is an explicit no-op: Go's switch already does
// nothing when no case matches, which is exactly the spec §4 catch-all
// rule.
func (m *Model) handleSearchKey(msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyEsc:
		m.exitSearchMode(true)
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/... -run 'TestUpdate_Slash|TestUpdate_SearchImmediateEsc' -v`
Expected: PASS.

Also run the full package to confirm no regressions:
`go test ./internal/tui/... -v`

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/update.go internal/tui/update_test.go
git commit -m "tui: enter type-ahead search mode with / and cancel with Esc"
```

---

## Task 3: Typing jumps the cursor to the first match

**Files:**
- Modify: `internal/tui/update.go` (`handleSearchKey`, two new methods)
- Modify: `internal/tui/update_test.go` (new tests)

**Interfaces:**
- Consumes: `firstMatch` (Task 1), `handleSearchKey`/`exitSearchMode`
  (Task 2).
- Produces: `(m *Model) appendQuery(runes ...rune)`,
  `(m *Model) applySearchMatch()` — consumed by Task 4's Backspace case.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/update_test.go`:

```go
func TestUpdate_SearchTypingJumpsToFirstMatch(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)

	want := fsutil.IndexOfName(m.activeEntries, "file.txt")
	if m.activeCursor != want {
		t.Fatalf("activeCursor = %d, want %d (file.txt)", m.activeCursor, want)
	}
	if m.searchNoMatch {
		t.Fatal("expected searchNoMatch == false")
	}
}

func TestUpdate_SearchNoMatchKeepsCursorAndSetsFlag(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	prevCursor := m.activeCursor

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	m = updated.(Model)

	if m.activeCursor != prevCursor {
		t.Fatalf("activeCursor = %d, want unchanged %d", m.activeCursor, prevCursor)
	}
	if !m.searchNoMatch {
		t.Fatal("expected searchNoMatch == true")
	}
}

func TestUpdate_SearchSpaceKeyAppendsLiteralSpace(t *testing.T) {
	root := setupFixture(t)
	mustMkdir(t, filepath.Join(root, "My Documents"))
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("My")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Doc")})
	m = updated.(Model)

	if m.searchQuery != "My Doc" {
		t.Fatalf("searchQuery = %q, want %q", m.searchQuery, "My Doc")
	}
	want := fsutil.IndexOfName(m.activeEntries, "My Documents")
	if m.activeCursor != want {
		t.Fatalf("activeCursor = %d, want %d (My Documents)", m.activeCursor, want)
	}
}

func TestUpdate_SearchMultiRuneKeyMsgAppendsAllRunes(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fil")})
	m = updated.(Model)

	if m.searchQuery != "fil" {
		t.Fatalf("searchQuery = %q, want %q", m.searchQuery, "fil")
	}
	want := fsutil.IndexOfName(m.activeEntries, "file.txt")
	if m.activeCursor != want {
		t.Fatalf("activeCursor = %d, want %d (file.txt)", m.activeCursor, want)
	}
}

func TestUpdate_SearchLettersLikeQAndHDoNotTriggerNavCommands(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)
	for _, r := range "qhjklr." {
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
		if cmd != nil {
			t.Fatalf("key %q produced a command (expected none) while searching", r)
		}
	}

	if !m.searchMode {
		t.Fatal("expected still in searchMode after typing q/h/j/k/l/r/.")
	}
	if m.searchQuery != "qhjklr." {
		t.Fatalf("searchQuery = %q, want %q", m.searchQuery, "qhjklr.")
	}
}

func TestUpdate_SlashOnEmptyDirectorySetsNoMatchImmediately(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, filepath.Join(root, "sub", "grand"))
	if m.activeCursor != -1 {
		t.Fatalf("precondition: activeCursor = %d, want -1 (empty dir)", m.activeCursor)
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = updated.(Model)

	if !m.searchNoMatch {
		t.Fatal("expected searchNoMatch == true on empty directory")
	}
	if m.activeCursor != -1 {
		t.Fatalf("activeCursor = %d, want -1 (unchanged)", m.activeCursor)
	}
}

func TestUpdate_SearchEscRestoresPreSearchCursor(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	prevCursor := m.activeCursor

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)
	if m.activeCursor == prevCursor {
		t.Fatal("precondition: expected cursor to move off its pre-search position")
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("Esc during search must not quit the program")
	}
	if m.searchMode {
		t.Fatal("expected searchMode == false after Esc")
	}
	if m.activeCursor != prevCursor {
		t.Fatalf("activeCursor = %d, want restored %d", m.activeCursor, prevCursor)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/... -run TestUpdate_Search -v`
Expected: FAIL — typing a rune has no effect yet (`activeCursor`/`searchQuery`
assertions fail; `TestUpdate_SearchEscRestoresPreSearchCursor`'s
precondition check fails since the cursor never moves).

- [ ] **Step 3: Write the implementation**

In `internal/tui/update.go`, replace `handleSearchKey` with:

```go
func (m *Model) handleSearchKey(msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyRunes:
		m.appendQuery(msg.Runes...)
	case tea.KeySpace:
		m.appendQuery(' ')
	case tea.KeyEsc:
		m.exitSearchMode(true)
	}
}

// appendQuery adds runes to searchQuery and re-runs the match. A single
// KeyMsg can carry more than one rune (bracketed paste, composed input),
// so callers pass every rune from one message, not just the first.
func (m *Model) appendQuery(runes ...rune) {
	m.searchQuery += string(runes)
	m.applySearchMatch()
}

// applySearchMatch re-jumps activeCursor to firstMatch(activeEntries,
// searchQuery), always searching from the top of the list (spec §5). On no
// match, activeCursor is left unchanged and searchNoMatch is set.
func (m *Model) applySearchMatch() {
	i := firstMatch(m.activeEntries, m.searchQuery)
	if i < 0 {
		m.searchNoMatch = true
		return
	}
	m.activeCursor = i
	m.searchNoMatch = false
	m.clampScroll()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/... -run TestUpdate_Search -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/update.go internal/tui/update_test.go
git commit -m "tui: jump cursor to first match while typing a search query"
```

---

## Task 4: Backspace and Enter

**Files:**
- Modify: `internal/tui/update.go` (`handleSearchKey`)
- Modify: `internal/tui/update_test.go` (new tests)

**Interfaces:**
- Consumes: `applySearchMatch`, `exitSearchMode` (Tasks 2–3).
- Produces: nothing new consumed by later tasks — `handleSearchKey` is
  functionally complete for text-editing keys after this task (Task 5 only
  adds a top-level Ctrl-C guard and tests, no further `handleSearchKey`
  changes).

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/update_test.go`:

```go
func TestUpdate_SearchBackspaceShrinksQueryAndRejumps(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fix")}) // no match
	m = updated.(Model)
	if !m.searchNoMatch {
		t.Fatal("precondition: expected no match for \"fix\"")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(Model)

	if m.searchQuery != "fi" {
		t.Fatalf("searchQuery = %q, want %q", m.searchQuery, "fi")
	}
	want := fsutil.IndexOfName(m.activeEntries, "file.txt")
	if m.activeCursor != want {
		t.Fatalf("activeCursor = %d, want %d (file.txt) after backspacing to \"fi\"", m.activeCursor, want)
	}
	if m.searchNoMatch {
		t.Fatal("expected searchNoMatch == false after backspacing to a matching query")
	}
}

func TestUpdate_SearchBackspaceOnEmptyQueryExitsAndRestoresCursor(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	prevCursor := m.activeCursor

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)
	if m.activeCursor == prevCursor {
		t.Fatal("precondition: expected cursor to move off its pre-search position")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace}) // "f" -> ""
	m = updated.(Model)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyBackspace}) // "" -> exit, restore
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("backspacing past an empty query must not quit the program")
	}
	if m.searchMode {
		t.Fatal("expected searchMode == false after backspacing past an empty query")
	}
	if m.activeCursor != prevCursor {
		t.Fatalf("activeCursor = %d, want restored %d", m.activeCursor, prevCursor)
	}
}

func TestUpdate_SearchEnterCommitsAndKeepsCursor(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)
	matchedCursor := m.activeCursor

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("Enter committing a search must not quit the program")
	}
	if m.searchMode {
		t.Fatal("expected searchMode == false after Enter")
	}
	if m.activeCursor != matchedCursor {
		t.Fatalf("activeCursor = %d, want unchanged %d", m.activeCursor, matchedCursor)
	}

	// A second, separate Enter now performs the ordinary select/cd action
	// (file.txt is a file, so chosenPath falls back to the active directory).
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected the second Enter (normal mode) to quit the program")
	}
	path, ok := m.Result()
	if !ok {
		t.Fatal("expected selected == true after the normal-mode Enter")
	}
	if path != root {
		t.Fatalf("chosenPath = %q, want %q", path, root)
	}
}

func TestUpdate_SearchImmediateEnterIsNoop(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	prevCursor := m.activeCursor

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("immediate Enter after / must not quit the program")
	}
	if m.searchMode {
		t.Fatal("expected searchMode == false after Enter")
	}
	if m.activeCursor != prevCursor {
		t.Fatalf("activeCursor = %d, want unchanged %d", m.activeCursor, prevCursor)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/... -run 'TestUpdate_SearchBackspace|TestUpdate_SearchEnter|TestUpdate_SearchImmediateEnter' -v`
Expected: FAIL — Backspace and Enter are currently no-ops while searching
(unmatched `msg.Type` cases), so query/cursor/mode assertions fail.

- [ ] **Step 3: Write the implementation**

In `internal/tui/update.go`, replace `handleSearchKey` with:

```go
func (m *Model) handleSearchKey(msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyRunes:
		m.appendQuery(msg.Runes...)
	case tea.KeySpace:
		m.appendQuery(' ')
	case tea.KeyBackspace:
		if m.searchQuery == "" {
			m.exitSearchMode(true)
			return
		}
		runes := []rune(m.searchQuery)
		m.searchQuery = string(runes[:len(runes)-1])
		m.applySearchMatch()
	case tea.KeyEnter:
		m.exitSearchMode(false)
	case tea.KeyEsc:
		m.exitSearchMode(true)
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/... -run 'TestUpdate_SearchBackspace|TestUpdate_SearchEnter|TestUpdate_SearchImmediateEnter' -v`
Expected: PASS. Then run the full package: `go test ./internal/tui/... -v`.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/update.go internal/tui/update_test.go
git commit -m "tui: search Backspace edits query, Enter commits the match"
```

---

## Task 5: Ctrl-C always quits; arrows/Tab/etc. stay no-ops

**Files:**
- Modify: `internal/tui/update.go` (top-level `Ctrl-C` guard)
- Modify: `internal/tui/update_test.go` (new tests)

**Interfaces:**
- Consumes: the `Update` mode branch (Task 2).
- Produces: nothing new — this is the last task touching `Update`'s
  control flow.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/update_test.go`:

```go
func TestUpdate_CtrlCQuitsEvenDuringSearch(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("expected Ctrl-C to quit even while searching")
	}
	if _, ok := m.Result(); ok {
		t.Fatal("expected selected == false on Ctrl-C during search")
	}
}

func TestUpdate_SearchArrowKeysAreNoOps(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)
	beforeCursor, beforeQuery := m.activeCursor, m.searchQuery

	for _, kt := range []tea.KeyType{tea.KeyUp, tea.KeyDown, tea.KeyLeft, tea.KeyRight} {
		updated, cmd := m.Update(tea.KeyMsg{Type: kt})
		m = updated.(Model)
		if cmd != nil {
			t.Fatalf("arrow key %v produced a command while searching", kt)
		}
	}

	if m.activeCursor != beforeCursor {
		t.Fatalf("activeCursor = %d, want unchanged %d", m.activeCursor, beforeCursor)
	}
	if m.searchQuery != beforeQuery {
		t.Fatalf("searchQuery = %q, want unchanged %q", m.searchQuery, beforeQuery)
	}
	if !m.searchMode {
		t.Fatal("expected still in searchMode")
	}
}

func TestUpdate_SearchTabAndOtherControlKeysAreNoOps(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)
	beforeCursor, beforeQuery := m.activeCursor, m.searchQuery

	for _, kt := range []tea.KeyType{tea.KeyTab, tea.KeyCtrlU, tea.KeyHome, tea.KeyPgUp, tea.KeyF1} {
		updated, cmd := m.Update(tea.KeyMsg{Type: kt})
		m = updated.(Model)
		if cmd != nil {
			t.Fatalf("key %v produced a command while searching", kt)
		}
	}

	if m.activeCursor != beforeCursor {
		t.Fatalf("activeCursor = %d, want unchanged %d", m.activeCursor, beforeCursor)
	}
	if m.searchQuery != beforeQuery {
		t.Fatalf("searchQuery = %q, want unchanged %q", m.searchQuery, beforeQuery)
	}
	if !m.searchMode {
		t.Fatal("expected still in searchMode")
	}
}

func TestUpdate_WindowResizeWorksDuringSearch(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)

	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 60, Height: 15})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("resize must not quit the program")
	}
	if m.width != 60 || m.height != 15 {
		t.Fatalf("width/height = %d/%d, want 60/15", m.width, m.height)
	}
	if !m.searchMode || m.searchQuery != "f" {
		t.Fatalf("resize must not disturb search state: searchMode=%v searchQuery=%q", m.searchMode, m.searchQuery)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/... -run 'TestUpdate_CtrlCQuitsEvenDuringSearch|TestUpdate_SearchArrowKeysAreNoOps|TestUpdate_SearchTabAndOtherControlKeysAreNoOps|TestUpdate_WindowResizeWorksDuringSearch' -v`
Expected: `TestUpdate_CtrlCQuitsEvenDuringSearch` FAILs (Ctrl-C is currently
swallowed by `handleSearchKey`'s empty default, so `cmd == nil`). The other
three tests already PASS by construction (Go's `switch` no-ops on an
unmatched `msg.Type`, and `tea.WindowSizeMsg` is handled in a separate
outer `case` entirely unaffected by `searchMode`) — that's expected; they
still need to exist to lock these invariants in against regressions.

- [ ] **Step 3: Write the implementation**

In `internal/tui/update.go`, add a top-level Ctrl-C guard before the
search-mode branch, and drop the now-redundant `"ctrl+c"` from the
normal-mode string switch (unreachable since the guard returns first):

```go
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			m.quitting = true
			m.selected = false
			return m, tea.Quit
		}
		if m.searchMode {
			m.handleSearchKey(msg)
			return m, nil
		}
		switch msg.String() {
		case "up", "k":
			m.moveCursor(-1)
		case "down", "j":
			m.moveCursor(1)
		case "right", "l":
			m.handleRight()
		case "left", "h":
			m.handleLeft()
		case "enter":
			m.handleEnter()
			return m, tea.Quit
		case "q", "esc":
			m.quitting = true
			m.selected = false
			return m, tea.Quit
		case ".":
			m.showHidden = !m.showHidden
			m.reload()
		case "r":
			m.reload()
		case "/":
			m.enterSearchMode()
		}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/... -run 'TestUpdate_CtrlCQuitsEvenDuringSearch|TestUpdate_SearchArrowKeysAreNoOps|TestUpdate_SearchTabAndOtherControlKeysAreNoOps|TestUpdate_WindowResizeWorksDuringSearch' -v`
Expected: PASS. Then confirm no regressions on the existing quit-key test:
`go test ./internal/tui/... -run TestUpdate_QuitKeysDoNotSelect -v`
Expected: PASS (Esc/Ctrl-C/`q` still all quit in normal mode — refactored
dispatch, same observable behavior).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/update.go internal/tui/update_test.go
git commit -m "tui: Ctrl-C always quits, even mid-search"
```

---

## Task 6: Status line — search prompt and no-match indicator

**Files:**
- Modify: `internal/tui/render.go` (`statusLine`)
- Modify: `internal/tui/render_test.go` (new tests)

**Interfaces:**
- Consumes: `Model.searchMode`, `Model.searchQuery`, `Model.searchNoMatch`
  (Task 2), `composeStatusLine` (existing, unchanged signature).
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/render_test.go`:

```go
func TestView_StatusLineShowsSearchPromptWhileTyping(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fi")})
	m = updated.(Model)

	out := m.View()
	if !strings.Contains(out, "/fi") {
		t.Fatalf("expected search prompt \"/fi\" in status line:\n%s", out)
	}
}

func TestView_StatusLineShowsNoMatchIndicator(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("zzz")})
	m = updated.(Model)

	out := m.View()
	if !strings.Contains(out, "no match") {
		t.Fatalf("expected \"no match\" indicator in status line:\n%s", out)
	}
}

func TestView_StatusLineSearchSuppressesStaleStatusErr(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.statusErr = "open /x: permission denied"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)

	out := m.View()
	if strings.Contains(out, "permission denied") {
		t.Fatalf("expected stale statusErr suppressed while searching:\n%s", out)
	}
	if m.statusErr == "" {
		t.Fatal("expected statusErr field to remain set (only display suppressed)")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/... -run TestView_StatusLineSearch -v`
Expected: FAIL — `statusLine()` doesn't know about search state yet, so
neither the prompt nor "no match" appear, and the stale error isn't
suppressed.

- [ ] **Step 3: Write the implementation**

In `internal/tui/render.go`, replace `statusLine()`:

```go
func (m Model) statusLine() string {
	hints := "↑/k ↓/j move · →/l open · ←/h up · Enter cd+exit · . hidden · r refresh · / search · q quit"
	left := hints
	right := m.activePath
	isErr := m.statusErr != ""
	if isErr {
		right = m.statusErr
	}
	if m.searchMode {
		// The right slot is dedicated to search state for the whole
		// session — a stale statusErr from before / was pressed is not
		// displayed (spec §6); the statusErr field itself is untouched.
		left = "/" + m.searchQuery
		right = m.activePath
		isErr = false
		if m.searchNoMatch {
			right = "no match"
			isErr = true
		}
	}
	return composeStatusLine(left, right, isErr, m.width)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/... -run TestView_StatusLineSearch -v`
Expected: PASS. Then run the full package: `go test ./internal/tui/... -v`
and `go vet ./...`.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/render.go internal/tui/render_test.go
git commit -m "tui: show search prompt and no-match indicator in status line"
```

---

## Task 7: Documentation

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`

**Interfaces:** none — documentation only, no code.

- [ ] **Step 1: Update `README.md`'s keybinding table and non-goals line**

In `README.md`, add a row after the `r` row (currently the last row of the
keybinding table) and update the closing paragraph:

```markdown
| `.` | Toggle hidden (dotfile) visibility (default off) |
| `r` | Refresh the active directory's listing |
| `/` | Type-ahead search the active column: type to jump to the first matching entry; `Enter` keeps the match, `Esc` cancels back to where the cursor was |

Navigation only in v1 — no file create/rename/delete/copy/move, no config
file.
```

(This removes the earlier `no search,` clause from the closing paragraph,
since the type-ahead search feature now exists.)

- [ ] **Step 2: Update `AGENTS.md`'s non-goals line and Important Files table**

In `AGENTS.md`, in the "Project Overview" section, change:

```markdown
v1 scope is intentionally locked to navigation only — no file
create/rename/delete/copy/move, no search, no config file, no mouse
support, no filesystem watching (see `docs/superpowers/specs/2026-08-15-thicket-tui-file-browser-design.md` §3).
```

to:

```markdown
v1 scope is intentionally locked to navigation only — no file
create/rename/delete/copy/move, no config file, no mouse support, no
filesystem watching (see `docs/superpowers/specs/2026-08-15-thicket-tui-file-browser-design.md`
§3). A `/`-triggered type-ahead cursor search within the active column was
added afterward, per `docs/superpowers/specs/2026-08-16-thicket-type-ahead-search-design.md`,
which amends that one non-goal — every other v1 non-goal in §3 still holds.
```

In the "Important Files" table, add a row after the `internal/fsutil/preview.go`
row:

```markdown
| `internal/tui/search.go` | `firstMatch` — pure case-insensitive substring search over an already-loaded entry list, used by type-ahead search |
```

- [ ] **Step 3: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: document type-ahead search keybinding and non-goal amendment"
```

---

## Manual Verification (after Task 7)

Automated tests cover every keystroke transition; the `/dev/tty` +
Bubble Tea interactive path itself is not automatable (same limitation as
the base spec's shell-handoff smoke test — see spec §8). Run once by hand:

1. `go build -o thicket ./cmd/thicket`
2. In a directory with several similarly-named entries (e.g. this repo's
   `internal/` — `fsutil`, `tui`), run `./thicket internal`.
3. Press `/`, type `t`. Confirm the cursor jumps live to `tui` (or the
   first matching entry) as you type, and the status line's left side
   shows `/t`.
4. Press Backspace until the query is empty, then press Backspace once
   more. Confirm the prompt closes and the cursor returns to its
   pre-search row.
5. Press `/`, type a query with no match (e.g. `zzz`). Confirm the status
   line's right side shows `no match`.
6. Press Esc. Confirm the cursor returns to its pre-search row.
7. Press `/`, type a query that matches, press Enter. Confirm the prompt
   closes and the cursor stays on the match. Press Enter again. Confirm
   the program exits 0 (check with `echo $?`) and prints the expected
   path to stdout.
