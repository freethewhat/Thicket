# Directory Marks (Bookmarks) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add vim/ranger-style directory marks to `thicket`: `m<letter>`
bookmarks the active directory, `` `<letter> `` jumps to it, and `'` opens a
full-screen list of all marks to browse/jump/delete. Marks persist to disk
(`$XDG_STATE_HOME/thicket/marks`) across separate `thicket` invocations.

**Architecture:** A new leaf package `internal/marks` (parallel to
`internal/fsutil`) owns pure disk I/O for a `letter -> absolute-path` table.
`internal/tui` gains five `Model` fields, a new pure-function file
`internal/tui/marks.go` (sort/cursor helpers, mirroring `search.go`'s
`firstMatch`), three new early-return mode branches in `Update()` (mirroring
the existing `helpMode`/`searchMode` pattern), and a new full-screen
`renderMarksList` view (mirroring `renderHelp`). `cmd/thicket/main.go`
computes the marks file path and threads it into `tui.New`. Everything stays
synchronous — no goroutines, channels, or `tea.Cmd`.

**Tech Stack:** Go 1.24.6, `github.com/charmbracelet/bubbletea` v1.3.10
(pinned to v1.x), `github.com/charmbracelet/lipgloss` v1.1.0. Standard
library `testing` only — no testify, no mocks.

**Spec:** `docs/superpowers/specs/2026-08-16-thicket-directory-marks-design.md`
(amends `docs/superpowers/specs/2026-08-15-thicket-tui-file-browser-design.md`
§3's "no bookmarks" non-goal — every other v1 non-goal there still holds).

## Global Constraints

- No goroutines, channels, or `tea.Cmd` anywhere — `Update`/`View` stay
  synchronous (`AGENTS.md` "Concurrency" invariant).
- `internal/marks` is pure disk I/O only, parallel to `internal/fsutil` in
  the dependency graph (`cmd/thicket -> internal/tui -> {internal/fsutil,
  internal/marks}`). It never imports Bubble Tea or `internal/tui`.
- Mark-letter validation (exactly one ASCII `a`-`z`/`A`-`Z` rune) happens at
  two independent boundaries by design, per spec §3/§5: `internal/marks.Load`
  skips malformed keys when reading the file from disk, and
  `internal/tui`'s keystroke handling (`singleLetterRune` in `update.go`)
  validates the rune the user just typed before it ever reaches
  `internal/marks`. Do not remove either check to "avoid duplication" —
  they guard different inputs (an on-disk file that could have been hand-
  edited or corrupted vs. a live keypress).
- Sort convention — lowercase letters before uppercase, alphabetical within
  each case (vim/ranger's `:marks` convention; **not** Go's default
  ascending-rune order, which would put `'A'` (65) before `'a'` (97)) — is
  implemented independently in both `internal/marks` (`Save`, sorting
  letter-path pairs for the file) and `internal/tui/marks.go`
  (`sortedMarkLetters`, sorting bare letters for the UI list). These are
  two different packages operating on different shapes of data with no
  shared exported symbol between them; do not add a cross-package export
  solely to deduplicate a five-line comparator (YAGNI) — each package
  defines its own small unexported `less` helper.
- Ruling (resolved spec ambiguity, decided during planning): spec §6's doc
  comment for `marksListCursorFor` says it's reused "after a delete
  changes the list length," but §5's own prose for the `d` key says
  "clamp `marksCursor` to the new (possibly shorter) length" — and a
  clamp, by definition, preserves an in-range value and only adjusts one
  that's now out of range. Unconditionally resetting to `0` on every
  delete (what reusing `marksListCursorFor` verbatim would do) is not a
  clamp — it would jump the cursor to the top of the list after every
  single deletion, a surprising UX regression. This plan follows §5's
  prose and the *existing* `reload()` convention already in
  `internal/tui/update.go` (`reload` preserves `activeCursor` if it's
  still in range after a mutation, resetting to `0` only when it's now
  out of range — see `reload`'s body for the exact idiom) instead of
  reusing `marksListCursorFor` after a delete. `marksListCursorFor` is
  used only for the "list just opened" case its signature actually
  supports — it takes no previous-cursor parameter, so it cannot
  preserve one.
- `Ctrl-C` (`msg.Type == tea.KeyCtrlC`) is checked first, before every mode
  branch, and always hard-quits — unchanged existing invariant that also
  covers all three new modes (`markSetPending`, `markJumpPending`,
  `marksListMode`) automatically, since it sits above all of them in
  `Update`'s dispatch order.
- Discriminate a mark-letter keystroke (while `markSetPending` or
  `markJumpPending` is true) on `msg.Type == tea.KeyRunes` plus exactly one
  rune — never `msg.String()` — mirroring `handleSearchKey`'s existing
  convention (`String()` returns a name for every recognized key, e.g.
  `"tab"`, `"pgdown"`, which a string switch could mistreat). A single
  `singleLetterRune(msg tea.KeyMsg) (rune, bool)` helper in `update.go`
  implements this once for both pending modes. Keys that *enter* a mode
  (`m`, `` ` ``, `'`) and keys inside `marksListMode` (`up`/`k`/`down`/`j`/
  `enter`/`d`/`q`/`esc`) are matched via `msg.String()`, exactly like the
  existing `.`/`/`/`?`/`r`/`q`/`esc` cases already are.
- Per `AGENTS.md`'s testing rule: `internal/marks` is a new package, so it
  gets its own new `internal/marks/marks_test.go`. `internal/tui` is not a
  new package, so all new TUI tests are added to the existing
  `update_test.go`/`render_test.go` files, never a new test file. Test
  functions use `TestFunc_Behavior` naming.
- No linter/CI config exists — verify each task with
  `go build ./... && go vet ./...` plus a scoped
  `go test ./<package>/... -run '<pattern>' -v`, not the full suite. Run
  the full `go test ./...` once at the very end (after Task 5).
- Commit messages follow this repo's existing `scope: short description`
  style (see `git log --oneline`, e.g. `tui: give status-line right side
  priority over hints`).

---

## Task 1: `internal/marks` — mark persistence package

**Files:**
- Create: `internal/marks/marks.go`
- Create: `internal/marks/marks_test.go`

**Interfaces:**
- Produces: `type Marks map[rune]string`; `func Load(path string) (Marks, error)`;
  `func Save(path string, m Marks) error`; `func DefaultPath() (string, error)`.
  Consumed by Task 2 (`Model.New`, wired via `marksPkg "thicket/internal/marks"`)
  and Task 3 (`update.go`'s mark-set/mark-delete handlers call `marksPkg.Save`).

- [ ] **Step 1: Write the failing tests**

Create `internal/marks/marks_test.go`:

```go
package marks_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"thicket/internal/marks"
)

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLoad_MissingFileReturnsEmptyMarks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marks")

	m, err := marks.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m == nil {
		t.Fatal("Load returned nil Marks for a missing file, want non-nil empty")
	}
	if len(m) != 0 {
		t.Fatalf("Load returned %d marks for a missing file, want 0", len(m))
	}
}

func TestLoad_MalformedLineSkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marks")
	mustWriteFile(t, path, strings.Join([]string{
		"a\t/only-valid", // the one valid line
		"toofewfields",   // wrong field count (1)
		"c\td\te",        // wrong field count (3)
		"1\t/non-letter-key",
		"ab\t/multi-rune-key",
	}, "\n")+"\n")

	m, err := marks.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(m) != 1 {
		t.Fatalf("Load returned %d marks, want 1 (malformed lines skipped): %+v", len(m), m)
	}
	if m['a'] != "/only-valid" {
		t.Fatalf("m['a'] = %q, want /only-valid", m['a'])
	}
}

func TestLoad_PermissionDeniedReturnsError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits")
	}
	path := filepath.Join(t.TempDir(), "marks")
	mustWriteFile(t, path, "a\t/x\n")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0o644)

	if _, err := marks.Load(path); err == nil {
		t.Fatal("expected an error reading a permission-denied marks file")
	}
}

func TestSaveThenLoad_RoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "marks")
	want := marks.Marks{'a': "/home/user/projects", 'Z': "/var/log"}

	if err := marks.Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := marks.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d marks, want %d: %+v", len(got), len(want), got)
	}
	for r, p := range want {
		if got[r] != p {
			t.Fatalf("got[%q] = %q, want %q", r, got[r], p)
		}
	}
}

func TestSave_CreatesParentDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "state")
	path := filepath.Join(dir, "marks")

	if err := marks.Save(path, marks.Marks{'a': "/x"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("parent directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", dir)
	}
}

func TestSave_SortsByLetterAscending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marks")
	m := marks.Marks{'B': "/b", 'a': "/a", 'A': "/cap-a", 'z': "/z"}

	if err := marks.Save(path, m); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "a\t/a\nz\t/z\nA\t/cap-a\nB\t/b\n"
	if string(data) != want {
		t.Fatalf("Save wrote:\n%q\nwant:\n%q", data, want)
	}
}

func TestDefaultPath_UsesXDGStateHomeWhenSet(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)

	got, err := marks.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	want := filepath.Join(xdg, "thicket", "marks")
	if got != want {
		t.Fatalf("DefaultPath() = %q, want %q", got, want)
	}
}

func TestDefaultPath_FallsBackToHomeLocalState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := marks.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	want := filepath.Join(home, ".local", "state", "thicket", "marks")
	if got != want {
		t.Fatalf("DefaultPath() = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/marks/... -v`
Expected: FAIL to build — `package marks does not exist` (the package
doesn't exist yet).

- [ ] **Step 3: Write the implementation**

Create `internal/marks/marks.go`:

```go
// Package marks persists thicket's directory marks (bookmarks): a table
// mapping a single letter to an absolute directory path. It is pure disk
// I/O, parallel to internal/fsutil in the dependency graph — no Bubble
// Tea/TUI concerns live here.
package marks

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Marks is a letter -> absolute-directory-path table. Only 'a'-'z' and
// 'A'-'Z' are valid keys; callers are responsible for validating the rune
// before inserting (internal/tui's Update() does this at the keystroke
// boundary).
type Marks map[rune]string

// Load reads path and returns its Marks table. A missing file is not an
// error — it returns an empty, non-nil Marks. Malformed individual lines
// (wrong field count, non-letter key, multi-rune key) are skipped, not
// fatal, mirroring fsutil.classify's per-entry error tolerance. Any other
// read error (e.g. permission denied on an existing file) is returned to
// the caller.
func Load(path string) (Marks, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Marks{}, nil
		}
		return nil, err
	}
	m := Marks{}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 2 {
			continue
		}
		letters := []rune(fields[0])
		if len(letters) != 1 || !isMarkLetter(letters[0]) {
			continue
		}
		m[letters[0]] = fields[1]
	}
	return m, nil
}

// Save writes m to path as sorted `letter\tpath\n` lines — lowercase
// letters before uppercase, alphabetical within each case (vim/ranger's
// `:marks` convention; NOT plain ascending rune order, since 'A' (65) <
// 'a' (97) in ASCII — ascending-rune sort would put A-Z first). Creates
// the parent directory (mode 0o700) if absent. Directory paths containing
// an embedded tab or newline byte will not round-trip correctly through
// this format — Load skips the resulting malformed line silently, same as
// any other corrupt line. Accepted as an unsupported edge case.
func Save(path string, m Marks) error {
	letters := make([]rune, 0, len(m))
	for r := range m {
		letters = append(letters, r)
	}
	sort.Slice(letters, func(i, j int) bool {
		return letterLess(letters[i], letters[j])
	})

	var b strings.Builder
	for _, r := range letters {
		b.WriteRune(r)
		b.WriteByte('\t')
		b.WriteString(m[r])
		b.WriteByte('\n')
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

// DefaultPath returns $XDG_STATE_HOME/thicket/marks, falling back to
// $HOME/.local/state/thicket/marks when XDG_STATE_HOME is unset.
func DefaultPath() (string, error) {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "thicket", "marks"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "thicket", "marks"), nil
}

// isMarkLetter reports whether r is a valid mark key: exactly one ASCII
// letter, matching the 52-slot bound (a-z, A-Z) — not unicode.IsLetter,
// which would accept non-ASCII letters and break that bound.
func isMarkLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// letterLess orders a before b lowercase-before-uppercase, then
// alphabetically within each case — see Save.
func letterLess(a, b rune) bool {
	aLower := a >= 'a' && a <= 'z'
	bLower := b >= 'a' && b <= 'z'
	if aLower != bLower {
		return aLower
	}
	return a < b
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/marks/... -v`
Expected: PASS (8 tests).

- [ ] **Step 5: Verify build and vet**

Run: `go build ./... && go vet ./...`
Expected: no errors (the new package isn't imported anywhere yet, so the
rest of the module is untouched).

- [ ] **Step 6: Commit**

```bash
git add internal/marks
git commit -m "marks: add directory-marks persistence package"
```

---

## Task 2: Wire `internal/marks` into `Model` and `main`

**Files:**
- Create: `internal/tui/marks.go`
- Modify: `internal/tui/model.go`
- Modify: `cmd/thicket/main.go`
- Modify: `internal/tui/update_test.go`
- Modify: `internal/tui/render_test.go`

**Interfaces:**
- Consumes: `marks.Marks`, `marks.Load`, `marks.Save`, `marks.DefaultPath` (Task 1).
- Produces: `Model.markTable marksPkg.Marks`, `Model.marksPath string`,
  `Model.markSetPending bool`, `Model.markJumpPending bool`,
  `Model.marksListMode bool`, `Model.marksCursor int` (all consumed by
  Task 3's key handling and Task 4's rendering); new signature
  `New(startPath, marksPath string) (Model, error)` (consumed by
  `cmd/thicket/main.go` and every test in the package); pure functions
  `sortedMarkLetters(m marksPkg.Marks) []rune` and
  `marksListCursorFor(m marksPkg.Marks) int` in `internal/tui/marks.go`
  (consumed by Task 3's key handling and Task 4's rendering — signatures
  are final as written here; `marksListCursorFor` is used ONLY for the
  "list just opened" case — see Global Constraints' ruling on why a
  delete does not reuse it).

- [ ] **Step 1: Write the failing tests**

In `internal/tui/update_test.go`, change the import block from:

```go
import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"thicket/internal/fsutil"
)
```

to:

```go
import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"thicket/internal/fsutil"
	marksPkg "thicket/internal/marks"
)
```

Change `newTestModel` from:

```go
func newTestModel(t *testing.T, path string) Model {
	t.Helper()
	m, err := New(path)
	if err != nil {
		t.Fatalf("New(%q): %v", path, err)
	}
	m.height = 20
	m.width = 100
	return m
}
```

to:

```go
func newTestModel(t *testing.T, path string) Model {
	t.Helper()
	m, err := New(path, filepath.Join(t.TempDir(), "marks"))
	if err != nil {
		t.Fatalf("New(%q): %v", path, err)
	}
	m.height = 20
	m.width = 100
	return m
}
```

There are 6 other call sites in `internal/tui/update_test.go` of the exact
shape `New(root)` (each immediately followed by `if err != nil { t.Fatalf("New(%q): %v", root, err) }`)
— every one of them in `TestUpdate_PageDownMovesCursorByVisibleRows`,
`TestUpdate_PageUpMovesCursorByVisibleRows`,
`TestUpdate_PageDownClampsAtLastEntry`, `TestUpdate_PageUpClampsAtFirstEntry`,
`TestUpdate_HomeJumpsToFirstEntry`, and `TestUpdate_EndJumpsToLastEntry`.
Change each `New(root)` to `New(root, filepath.Join(t.TempDir(), "marks"))`.

Append these new tests to the end of `internal/tui/update_test.go`:

```go
func TestSortedMarkLetters_LowercaseBeforeUppercaseAlphabeticalWithinCase(t *testing.T) {
	m := marksPkg.Marks{'Z': "/z", 'b': "/b", 'a': "/a", 'A': "/cap-a"}
	got := sortedMarkLetters(m)
	want := []rune{'a', 'b', 'A', 'Z'}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sortedMarkLetters = %q, want %q", string(got), string(want))
	}
}

func TestSortedMarkLetters_EmptyReturnsEmptySlice(t *testing.T) {
	if got := sortedMarkLetters(marksPkg.Marks{}); len(got) != 0 {
		t.Fatalf("sortedMarkLetters(empty) = %v, want empty", got)
	}
}

func TestMarksListCursorFor_ZeroWhenNonEmpty(t *testing.T) {
	if got := marksListCursorFor(marksPkg.Marks{'a': "/x"}); got != 0 {
		t.Fatalf("marksListCursorFor(non-empty) = %d, want 0", got)
	}
}

func TestMarksListCursorFor_NegativeOneWhenEmpty(t *testing.T) {
	if got := marksListCursorFor(marksPkg.Marks{}); got != -1 {
		t.Fatalf("marksListCursorFor(empty) = %d, want -1", got)
	}
}

func TestNew_LoadsExistingMarksFromDisk(t *testing.T) {
	root := setupFixture(t)
	marksPath := filepath.Join(t.TempDir(), "marks")
	if err := marksPkg.Save(marksPath, marksPkg.Marks{'a': root}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	m, err := New(root, marksPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if m.markTable['a'] != root {
		t.Fatalf("markTable['a'] = %q, want %q", m.markTable['a'], root)
	}
	if m.marksCursor != 0 {
		t.Fatalf("marksCursor = %d, want 0 (non-empty markTable)", m.marksCursor)
	}
}

func TestNew_NoMarksFileGivesNegativeOneCursor(t *testing.T) {
	root := setupFixture(t)
	marksPath := filepath.Join(t.TempDir(), "marks")

	m, err := New(root, marksPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if m.marksCursor != -1 {
		t.Fatalf("marksCursor = %d, want -1 (no marks file yet)", m.marksCursor)
	}
}
```

In `internal/tui/render_test.go`, in `TestView_ActiveColumnScrollTracksActiveScroll`,
change `New(root)` to `New(root, filepath.Join(t.TempDir(), "marks"))` (this
is the one direct `New(root)` call site in this file).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go build ./...`
Expected: FAIL — `New(path)` (1 argument) no longer matches `New`'s
signature everywhere it's called (compile error), and `sortedMarkLetters`/
`marksListCursorFor`/`markTable` are undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/tui/marks.go`:

```go
package tui

import (
	"sort"

	marksPkg "thicket/internal/marks"
)

// sortedMarkLetters returns the letters of m sorted lowercase-before-
// uppercase, alphabetical within each case (vim/ranger's `:marks`
// convention — NOT Go's default ascending-rune sort, which would put
// 'A'-'Z' before 'a'-'z'), for stable, deterministic list-screen row
// order.
func sortedMarkLetters(m marksPkg.Marks) []rune {
	letters := make([]rune, 0, len(m))
	for r := range m {
		letters = append(letters, r)
	}
	sort.Slice(letters, func(i, j int) bool {
		return markLetterLess(letters[i], letters[j])
	})
	return letters
}

// markLetterLess orders a before b lowercase-before-uppercase, then
// alphabetically within each case — see sortedMarkLetters. Deliberately
// independent of internal/marks's own letterLess: two different packages,
// two different data shapes (bare letters here vs. letter-path pairs
// there), no shared exported symbol to hang a common helper off of.
func markLetterLess(a, b rune) bool {
	aLower := a >= 'a' && a <= 'z'
	bLower := b >= 'a' && b <= 'z'
	if aLower != bLower {
		return aLower
	}
	return a < b
}

// marksListCursorFor returns 0 if m is non-empty, -1 if empty — the
// initial cursor position used when marksListMode opens (spec §5's '
// key). Deleting an entry does NOT reuse this function: a delete
// preserves marksCursor when it's still in range for the shrunk table,
// matching the reload()-style clamp convention in update.go (see Global
// Constraints), and this function has no parameter to preserve a
// previous cursor value with anyway.
func marksListCursorFor(m marksPkg.Marks) int {
	if len(m) == 0 {
		return -1
	}
	return 0
}
```

In `internal/tui/model.go`, change the import block from:

```go
import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"thicket/internal/fsutil"
)
```

to:

```go
import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"thicket/internal/fsutil"
	marksPkg "thicket/internal/marks"
)
```

Change the `Model` struct from:

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
	// helpMode: in-app help screen state (? toggles it open/closed; see
	// internal/tui/help.go). Mutually exclusive with searchMode — Update's
	// early-return dispatch order guarantees only one is ever active.
	helpMode   bool
	width      int
	height     int
	quitting   bool
	selected   bool
	chosenPath string
}
```

to:

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
	// helpMode: in-app help screen state (? toggles it open/closed; see
	// internal/tui/help.go). Mutually exclusive with searchMode — Update's
	// early-return dispatch order guarantees only one is ever active.
	helpMode bool
	// markTable/marksPath/markSetPending/markJumpPending/marksListMode/
	// marksCursor: directory marks (bookmarks) state (spec
	// docs/superpowers/specs/2026-08-16-thicket-directory-marks-design.md
	// §4). markTable and marksPath are populated once in New(); the rest
	// are zero-valued at construction, same as searchMode/helpMode.
	// markSetPending/markJumpPending/marksListMode are mutually exclusive
	// with each other and with searchMode/helpMode — Update's early-return
	// dispatch order guarantees only one mode is ever active. The field is
	// named markTable, not marks, to avoid colliding with the marksPkg
	// import alias used throughout this package.
	markTable       marksPkg.Marks
	marksPath       string
	markSetPending  bool
	markJumpPending bool
	marksListMode   bool
	marksCursor     int // -1 when markTable is empty
	width           int
	height          int
	quitting        bool
	selected        bool
	chosenPath      string
}
```

Change `New` from:

```go
// New builds a Model rooted at startPath. startPath must be readable;
// a missing/inaccessible starting directory is a construction error
// (cmd/thicket exits 2 on this, per spec §4).
func New(startPath string) (Model, error) {
	abs, err := filepath.Abs(startPath)
	if err != nil {
		return Model{}, err
	}
	m := Model{activePath: abs}
	entries, err := fsutil.ListDir(m.activePath, m.showHidden)
	if err != nil {
		return Model{}, err
	}
	m.activeEntries = entries
	if len(entries) == 0 {
		m.activeCursor = -1
	}
	return m, nil
}
```

to:

```go
// New builds a Model rooted at startPath. startPath must be readable;
// a missing/inaccessible starting directory is a construction error
// (cmd/thicket exits 2 on this, per spec §4). marksPath is where the
// directory-marks table (internal/marks) is loaded from and saved to. An
// existing-but-unreadable marks file is also a construction error, for the
// same reason a bad startPath is: silently starting with an empty
// in-memory mark table would let the very next m<letter> press silently
// overwrite marks that already exist on disk.
func New(startPath, marksPath string) (Model, error) {
	abs, err := filepath.Abs(startPath)
	if err != nil {
		return Model{}, err
	}
	m := Model{activePath: abs}
	entries, err := fsutil.ListDir(m.activePath, m.showHidden)
	if err != nil {
		return Model{}, err
	}
	m.activeEntries = entries
	if len(entries) == 0 {
		m.activeCursor = -1
	}
	m.marksPath = marksPath
	loaded, err := marksPkg.Load(marksPath)
	if err != nil {
		return Model{}, err
	}
	m.markTable = loaded
	m.marksCursor = marksListCursorFor(loaded)
	return m, nil
}
```

In `cmd/thicket/main.go`, change the import block from:

```go
import (
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"thicket/internal/tui"
)
```

to:

```go
import (
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"thicket/internal/marks"
	"thicket/internal/tui"
)
```

Change the `tui.New` call in `main()` from:

```go
	m, err := tui.New(start)
	if err != nil {
		fmt.Fprintf(os.Stderr, "thicket: %v\n", err)
		os.Exit(2)
	}
```

to:

```go
	marksPath, err := marks.DefaultPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "thicket: %v\n", err)
		os.Exit(2)
	}

	m, err := tui.New(start, marksPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "thicket: %v\n", err)
		os.Exit(2)
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./... -v`
Expected: PASS across `internal/marks`, `internal/tui`, `cmd/thicket` (no
`cmd/thicket` tests exist, so that package reports `ok` with no test
output).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/marks.go internal/tui/model.go cmd/thicket/main.go internal/tui/update_test.go internal/tui/render_test.go
git commit -m "tui: load and thread directory-marks table through Model"
```

---

## Task 3: Key handling — `m`, `` ` ``, `'` and the marks-list keys

**Files:**
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/update_test.go`

**Interfaces:**
- Consumes: `Model.markTable`/`marksPath`/`markSetPending`/`markJumpPending`/
  `marksListMode`/`marksCursor` (Task 2), `sortedMarkLetters`,
  `marksListCursorFor` (Task 2), `marksPkg.Save` (Task 1).
- Produces: `(m *Model) jumpToMark(r rune, target string) bool` — consumed
  by Task 4 only insofar as Task 4 renders the `statusErr`/`activePath`
  state this method sets; no other task calls it directly.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/update_test.go`:

```go
func TestUpdate_MSetsMarkSetPending(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = updated.(Model)

	if !m.markSetPending {
		t.Fatal("expected markSetPending == true after m")
	}
}

func TestUpdate_MarkSetPendingLetterSavesMarkAndClearsPending(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(Model)

	if m.markSetPending {
		t.Fatal("expected markSetPending == false after letter")
	}
	if m.markTable['a'] != root {
		t.Fatalf("markTable['a'] = %q, want %q", m.markTable['a'], root)
	}
	saved, err := marksPkg.Load(m.marksPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if saved['a'] != root {
		t.Fatalf("persisted mark['a'] = %q, want %q", saved['a'], root)
	}
}

func TestUpdate_MarkSetPendingNonLetterCancelsWithoutMutation(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if m.markSetPending {
		t.Fatal("expected markSetPending == false after Esc")
	}
	if len(m.markTable) != 0 {
		t.Fatalf("expected no mark set, got %+v", m.markTable)
	}
}

func TestUpdate_MarkSetOverwritesExistingLetterSilently(t *testing.T) {
	root := setupFixture(t)
	sub := filepath.Join(root, "sub")
	m := newTestModel(t, root)
	m.markTable['a'] = sub // pre-existing mark

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(Model)

	if m.markTable['a'] != root {
		t.Fatalf("markTable['a'] = %q, want overwritten to %q", m.markTable['a'], root)
	}
}

func TestUpdate_BacktickSetsMarkJumpPending(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("`")})
	m = updated.(Model)

	if !m.markJumpPending {
		t.Fatal("expected markJumpPending == true after `")
	}
}

func TestUpdate_MarkJumpPendingKnownLetterNavigatesAndClearsPending(t *testing.T) {
	root := setupFixture(t)
	sub := filepath.Join(root, "sub")
	m := newTestModel(t, root)
	m.markTable['a'] = sub

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("`")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(Model)

	if m.markJumpPending {
		t.Fatal("expected markJumpPending == false after letter")
	}
	if m.activePath != sub {
		t.Fatalf("activePath = %q, want %q", m.activePath, sub)
	}
}

func TestUpdate_MarkJumpPendingUnknownLetterSetsStatusErr(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("`")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	m = updated.(Model)

	if m.markJumpPending {
		t.Fatal("expected markJumpPending == false")
	}
	if m.statusErr != "no mark: z" {
		t.Fatalf("statusErr = %q, want %q", m.statusErr, "no mark: z")
	}
}

func TestUpdate_MarkJumpPendingDeletedTargetSetsStatusErrKeepsPath(t *testing.T) {
	root := setupFixture(t)
	gone := filepath.Join(t.TempDir(), "gone")
	mustMkdir(t, gone)
	m := newTestModel(t, root)
	m.markTable['a'] = gone
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("`")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(Model)

	if m.activePath != root {
		t.Fatalf("activePath changed despite deleted mark target: %q", m.activePath)
	}
	if m.statusErr == "" {
		t.Fatal("expected statusErr to be set")
	}
}

func TestUpdate_MarkJumpPendingNonLetterCancelsWithoutMutation(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.markTable['a'] = root

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("`")})
	m = updated.(Model)
	prevPath := m.activePath
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if m.markJumpPending {
		t.Fatal("expected markJumpPending == false after Esc")
	}
	if m.activePath != prevPath {
		t.Fatalf("activePath changed on cancel: %q", m.activePath)
	}
	if m.statusErr != "" {
		t.Fatalf("statusErr = %q, want untouched empty", m.statusErr)
	}
}

func TestUpdate_QuoteOpensMarksListMode(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.markTable['a'] = root

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("'")})
	m = updated.(Model)

	if !m.marksListMode {
		t.Fatal("expected marksListMode == true after '")
	}
	if m.marksCursor != 0 {
		t.Fatalf("marksCursor = %d, want 0", m.marksCursor)
	}
}

func TestUpdate_MarksListUpDownClampsAtBothEnds(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.markTable['a'] = root
	m.markTable['b'] = root
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("'")})
	m = updated.(Model) // sorted letters: a, b; marksCursor starts at 0

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.marksCursor != 0 {
		t.Fatalf("marksCursor = %d, want clamped to 0", m.marksCursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.marksCursor != 1 {
		t.Fatalf("marksCursor = %d, want clamped to 1 (last index)", m.marksCursor)
	}
}

func TestUpdate_MarksListEnterNavigatesAndClosesList(t *testing.T) {
	root := setupFixture(t)
	sub := filepath.Join(root, "sub")
	m := newTestModel(t, root)
	m.markTable['a'] = sub
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("'")})
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if m.marksListMode {
		t.Fatal("expected marksListMode == false after Enter")
	}
	if m.activePath != sub {
		t.Fatalf("activePath = %q, want %q", m.activePath, sub)
	}
}

func TestUpdate_MarksListEnterOnDeletedTargetSetsStatusErrStaysOpen(t *testing.T) {
	root := setupFixture(t)
	gone := filepath.Join(t.TempDir(), "gone")
	mustMkdir(t, gone)
	m := newTestModel(t, root)
	m.markTable['a'] = gone
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("'")})
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if !m.marksListMode {
		t.Fatal("expected marksListMode to stay open on error")
	}
	if m.statusErr == "" {
		t.Fatal("expected statusErr to be set")
	}
}

func TestUpdate_MarksListDDeletesHighlightedMarkAndPreservesInRangeCursor(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.markTable['a'] = root
	m.markTable['b'] = root
	m.markTable['c'] = root
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("'")})
	m = updated.(Model) // sorted: a, b, c; cursor starts 0

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model) // cursor 1 -> highlights 'b'

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = updated.(Model)

	if _, ok := m.markTable['b']; ok {
		t.Fatal("expected mark 'b' deleted")
	}
	if len(m.markTable) != 2 {
		t.Fatalf("markTable = %+v, want 2 remaining", m.markTable)
	}
	// marksCursor (1) was still in range for the shrunk table (a, c: len
	// 2) -> preserved, now highlighting 'c', the entry that shifted up
	// into that slot. This is a clamp, not a reset (see Global
	// Constraints' ruling) — it must NOT jump back to 0.
	if m.marksCursor != 1 {
		t.Fatalf("marksCursor = %d, want 1 (preserved, in range)", m.marksCursor)
	}
	saved, err := marksPkg.Load(m.marksPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(saved) != 2 {
		t.Fatalf("persisted marks = %+v, want 2 remaining", saved)
	}
}

func TestUpdate_MarksListDOnLastRowResetsCursorToTop(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.markTable['a'] = root
	m.markTable['b'] = root
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("'")})
	m = updated.(Model) // sorted: a, b; cursor 0

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model) // cursor 1 -> highlights 'b', the last row

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = updated.(Model)

	// marksCursor (1) is now out of range for the shrunk table (a: len 1)
	// -> resets to 0, same as reload()'s activeCursor convention.
	if m.marksCursor != 0 {
		t.Fatalf("marksCursor = %d, want 0 (old cursor now out of range)", m.marksCursor)
	}
}

func TestUpdate_MarksListDOnEmptyListIsNoop(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("'")})
	m = updated.(Model) // empty list, cursor -1

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = updated.(Model)

	if !m.marksListMode {
		t.Fatal("expected marksListMode still open")
	}
	if m.marksCursor != -1 {
		t.Fatalf("marksCursor = %d, want -1", m.marksCursor)
	}
}

func TestUpdate_MarksListQAndEscCloseWithoutMutation(t *testing.T) {
	root := setupFixture(t)
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("q")},
		{Type: tea.KeyEsc},
	} {
		m := newTestModel(t, root)
		m.markTable['a'] = root
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("'")})
		m = updated.(Model)

		updated, _ = m.Update(key)
		m = updated.(Model)

		if m.marksListMode {
			t.Fatalf("expected marksListMode == false after %v", key)
		}
		if len(m.markTable) != 1 {
			t.Fatalf("expected mark untouched after %v, got %+v", key, m.markTable)
		}
	}
}

func TestUpdate_CtrlCQuitsFromEveryMarksMode(t *testing.T) {
	root := setupFixture(t)
	enterKeys := []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("m")}, // -> markSetPending
		{Type: tea.KeyRunes, Runes: []rune("`")}, // -> markJumpPending
		{Type: tea.KeyRunes, Runes: []rune("'")}, // -> marksListMode
	}
	for _, enterKey := range enterKeys {
		m := newTestModel(t, root)
		updated, _ := m.Update(enterKey)
		m = updated.(Model)

		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		m = updated.(Model)

		if cmd == nil {
			t.Fatalf("Ctrl-C after %v must quit (cmd != nil)", enterKey)
		}
		if !m.quitting || m.selected {
			t.Fatalf("Ctrl-C after %v: quitting=%v selected=%v, want quitting=true selected=false", enterKey, m.quitting, m.selected)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/... -run 'TestUpdate_M|TestUpdate_Backtick|TestUpdate_Quote|TestUpdate_CtrlCQuitsFromEveryMarksMode' -v`
Expected: FAIL — pressing `m`/`` ` ``/`'` has no effect yet (`markSetPending`
etc. never become true).

- [ ] **Step 3: Write the implementation**

In `internal/tui/update.go`, change the import block from:

```go
import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"thicket/internal/fsutil"
)
```

to:

```go
import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"thicket/internal/fsutil"
	marksPkg "thicket/internal/marks"
)
```

Replace the entire `Update` function with:

```go
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.clampScroll()
		return m, nil
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			m.quitting = true
			m.selected = false
			return m, tea.Quit
		}
		if m.helpMode {
			switch msg.String() {
			case "?", "q", "esc":
				m.helpMode = false
			}
			return m, nil
		}
		if m.marksListMode {
			m.handleMarksListKey(msg)
			return m, nil
		}
		if m.searchMode {
			m.handleSearchKey(msg)
			return m, nil
		}
		if m.markSetPending {
			m.handleMarkSetKey(msg)
			return m, nil
		}
		if m.markJumpPending {
			m.handleMarkJumpKey(msg)
			return m, nil
		}
		switch msg.String() {
		case "up", "k":
			m.moveCursor(-1)
		case "down", "j":
			m.moveCursor(1)
		case "pgup":
			m.moveCursor(-m.visibleRows())
		case "pgdown":
			m.moveCursor(m.visibleRows())
		case "home":
			m.moveCursor(-len(m.activeEntries))
		case "end":
			m.moveCursor(len(m.activeEntries))
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
		case "?":
			m.helpMode = true
		case "m":
			m.markSetPending = true
		case "`":
			m.markJumpPending = true
		case "'":
			m.enterMarksListMode()
		}
	}
	return m, nil
}
```

Append these new methods to the end of `internal/tui/update.go` (after
`applySearchMatch`):

```go
// enterMarksListMode opens the full-screen marks list (spec §5, ').
// marksCursor is recomputed every time the list opens, mirroring the
// -1-when-empty convention activeCursor already uses.
func (m *Model) enterMarksListMode() {
	m.marksListMode = true
	m.marksCursor = marksListCursorFor(m.markTable)
}

// singleLetterRune reports whether msg carries exactly one ASCII letter
// rune (a-z or A-Z) via tea.KeyRunes — the shape a single mark-letter
// keystroke takes. Discriminating on msg.Type, not msg.String(), mirrors
// handleSearchKey's convention (see Global Constraints): String() returns
// a name for every recognized key ("tab", "pgdown", ...), which a
// string-based check could mistake for a letter-shaped case.
func singleLetterRune(msg tea.KeyMsg) (rune, bool) {
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		return 0, false
	}
	r := msg.Runes[0]
	if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
		return 0, false
	}
	return r, true
}

// handleMarkSetKey processes one key while markSetPending is true (spec
// §5's mark-set table). Any key that isn't a single a-z/A-Z rune cancels
// without mutation.
func (m *Model) handleMarkSetKey(msg tea.KeyMsg) {
	m.markSetPending = false
	r, ok := singleLetterRune(msg)
	if !ok {
		return
	}
	m.markTable[r] = m.activePath
	if err := marksPkg.Save(m.marksPath, m.markTable); err != nil {
		m.statusErr = err.Error()
	}
}

// handleMarkJumpKey processes one key while markJumpPending is true (spec
// §5's mark-jump table).
func (m *Model) handleMarkJumpKey(msg tea.KeyMsg) {
	m.markJumpPending = false
	r, ok := singleLetterRune(msg)
	if !ok {
		return
	}
	target, known := m.markTable[r]
	if !known {
		m.statusErr = "no mark: " + string(r)
		return
	}
	m.jumpToMark(r, target)
}

// jumpToMark navigates to target (the directory marked by r) on success,
// or sets statusErr (prefixed with the mark letter, same shape as
// handleRight's permission-denied handling) and leaves activePath
// untouched on failure. Reports whether the jump succeeded so callers can
// decide whether to close their own mode — markJumpPending always clears
// regardless, but the marks list (spec §5) only closes on success.
func (m *Model) jumpToMark(r rune, target string) bool {
	entries, err := fsutil.ListDir(target, m.showHidden)
	if err != nil {
		m.statusErr = "mark " + string(r) + ": " + err.Error()
		return false
	}
	m.activePath = target
	m.activeEntries = entries
	m.activeCursor = 0
	if len(entries) == 0 {
		m.activeCursor = -1
	}
	m.activeScroll = 0
	m.statusErr = ""
	return true
}

// handleMarksListKey processes one key while marksListMode is true (spec
// §5's marks-list table). Any msg.String() not matched below is an
// explicit no-op — Go's switch already does nothing when no case matches.
func (m *Model) handleMarksListKey(msg tea.KeyMsg) {
	switch msg.String() {
	case "up", "k":
		m.moveMarksCursor(-1)
	case "down", "j":
		m.moveMarksCursor(1)
	case "enter":
		m.activateMarksListEntry()
	case "d":
		m.deleteMarksListEntry()
	case "q", "esc":
		m.marksListMode = false
	}
}

func (m *Model) moveMarksCursor(delta int) {
	n := len(sortedMarkLetters(m.markTable))
	if n == 0 {
		return
	}
	m.marksCursor += delta
	if m.marksCursor < 0 {
		m.marksCursor = 0
	}
	if last := n - 1; m.marksCursor > last {
		m.marksCursor = last
	}
}

func (m *Model) activateMarksListEntry() {
	letters := sortedMarkLetters(m.markTable)
	if m.marksCursor < 0 || m.marksCursor >= len(letters) {
		return
	}
	r := letters[m.marksCursor]
	if m.jumpToMark(r, m.markTable[r]) {
		m.marksListMode = false
	}
}

// deleteMarksListEntry deletes the highlighted mark and persists the
// change. On a Save error, the deletion stays in memory (matching the
// disk/memory-diverges tradeoff handleMarkSetKey already accepts) and
// marksCursor is left untouched so the error is visible against the row
// that's still selected. On success, marksCursor is clamped the same way
// reload() clamps activeCursor elsewhere in this file: preserved if it's
// still a valid index into the shrunk table, reset to 0 only if the
// deleted row was its last valid position (see Global Constraints'
// ruling — this is deliberately not a call to marksListCursorFor, which
// has no way to preserve a previous position).
func (m *Model) deleteMarksListEntry() {
	letters := sortedMarkLetters(m.markTable)
	if m.marksCursor < 0 || m.marksCursor >= len(letters) {
		return
	}
	r := letters[m.marksCursor]
	delete(m.markTable, r)
	if err := marksPkg.Save(m.marksPath, m.markTable); err != nil {
		m.statusErr = err.Error()
		return
	}
	m.statusErr = ""
	if len(m.markTable) == 0 {
		m.marksCursor = -1
	} else if m.marksCursor >= len(m.markTable) {
		m.marksCursor = 0
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/... -v`
Expected: PASS (all existing tests plus the new ones from this task).

- [ ] **Step 5: Verify build and vet**

Run: `go build ./... && go vet ./...`
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/update.go internal/tui/update_test.go
git commit -m "tui: add m/\`/' key handling for directory marks"
```

---

## Task 4: Rendering — marks list screen and status-line prompts

**Files:**
- Modify: `internal/tui/render.go`
- Modify: `internal/tui/render_test.go`

**Interfaces:**
- Consumes: `Model.markTable`/`markSetPending`/`markJumpPending`/
  `marksListMode`/`marksCursor` (Task 2), `sortedMarkLetters` (Task 2).
- Produces: `(m Model) renderMarksList(rows int) string` — not consumed by
  any later task; wired directly into `View()` in this same task.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/render_test.go`:

```go
func TestView_MarksListEmptyShowsNoMarksSetMessage(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.marksListMode = true
	m.marksCursor = -1

	out := m.View()

	if !strings.Contains(out, "no marks set") {
		t.Fatalf("View() missing empty-marks message:\n%s", out)
	}
}

func TestView_MarksListShowsSortedLetterPathRows(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.markTable['b'] = "/second"
	m.markTable['a'] = "/first"
	m.marksListMode = true
	m.marksCursor = 0

	out := m.View()

	aIdx := strings.Index(out, "/first")
	bIdx := strings.Index(out, "/second")
	if aIdx == -1 || bIdx == -1 || bIdx < aIdx {
		t.Fatalf("expected 'a' row before 'b' row (sorted):\n%s", out)
	}
}

func TestView_MarksListHighlightsCursorRow(t *testing.T) {
	restoreColorProfile(t)
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.markTable['a'] = "/first"
	m.marksListMode = true
	m.marksCursor = 0

	out := m.renderMarksList(m.visibleRows())

	want := selectedStyle.Render(truncate(fmt.Sprintf("%c  %s", 'a', "/first"), m.width-paneBorderWidth))
	if !strings.Contains(out, want) {
		t.Fatalf("renderMarksList() missing highlighted row:\n%s\nwant substring:\n%s", out, want)
	}
}

func TestView_StatusLineShowsMarkSetPrompt(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 150
	m.markSetPending = true

	if !strings.Contains(m.statusLine(), "mark: _") {
		t.Fatalf("statusLine() missing mark-set prompt: %q", m.statusLine())
	}
}

func TestView_StatusLineShowsMarkJumpPrompt(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 150
	m.markJumpPending = true

	if !strings.Contains(m.statusLine(), "jump to mark: _") {
		t.Fatalf("statusLine() missing mark-jump prompt: %q", m.statusLine())
	}
}

func TestView_StatusLineShowsMarksListHints(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 150
	m.marksListMode = true

	got := m.statusLine()
	for _, want := range []string{"move", "jump", "delete", "close"} {
		if !strings.Contains(got, want) {
			t.Fatalf("statusLine() missing %q: %q", want, got)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/... -run 'TestView_MarksList|TestView_StatusLineShowsMark' -v`
Expected: FAIL — `m.marksListMode`/`m.markSetPending`/`m.markJumpPending`
have no effect on `View()`/`statusLine()` yet, and `renderMarksList` is
undefined.

- [ ] **Step 3: Write the implementation**

In `internal/tui/render.go`, add a `marksListMode` branch to `View()`.
Change:

```go
func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	rows := m.visibleRows()
	header := truncate(m.activePath, m.width)
	if m.helpMode {
		return header + "\n" + m.renderHelp(rows) + "\n" + m.statusLine()
	}
	cols := m.buildColumns(rows)
```

to:

```go
func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	rows := m.visibleRows()
	header := truncate(m.activePath, m.width)
	if m.helpMode {
		return header + "\n" + m.renderHelp(rows) + "\n" + m.statusLine()
	}
	if m.marksListMode {
		return header + "\n" + m.renderMarksList(rows) + "\n" + m.statusLine()
	}
	cols := m.buildColumns(rows)
```

Append `renderMarksList` to the end of `internal/tui/render.go` (it stays
in this file, not `help.go`, since it's the marks-list counterpart of the
column-layout rendering already here, not part of the help-screen source
of truth):

```go
// renderMarksList draws the full-screen marks browser shown while
// Model.marksListMode is true (spec §7). It replaces the column layout
// entirely — full-screen replacement, not an overlay — mirroring
// renderHelp in help.go.
func (m Model) renderMarksList(rows int) string {
	width := m.width - paneBorderWidth
	if width < 0 {
		width = 0
	}
	letters := sortedMarkLetters(m.markTable)
	var content string
	if len(letters) == 0 {
		content = "no marks set"
	} else {
		lines := make([]string, len(letters))
		for i, r := range letters {
			line := truncate(fmt.Sprintf("%c  %s", r, m.markTable[r]), width)
			if i == m.marksCursor {
				line = selectedStyle.Render(line)
			}
			lines[i] = line
		}
		content = strings.Join(lines, "\n")
	}
	return activePaneStyle.Width(width).Height(rows).Render(content)
}
```

Change `statusLine` from:

```go
func (m Model) statusLine() string {
	hints := "↑/k ↓/j move · PgUp/PgDn page · Home/End top/bottom · →/l open · ←/h up · Enter cd+exit · . hidden · r refresh · / search · ? help · q quit"
	left := hints
	right := m.activePath
	isErr := m.statusErr != ""
	if isErr {
		right = m.statusErr
	}
	if m.helpMode {
		left = "? / Esc: close help"
		right = m.activePath
		isErr = false
	} else if m.searchMode {
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

to:

```go
func (m Model) statusLine() string {
	hints := "↑/k ↓/j move · PgUp/PgDn page · Home/End top/bottom · →/l open · ←/h up · Enter cd+exit · . hidden · r refresh · / search · ? help · q quit · m mark · ` jump · ' marks"
	left := hints
	right := m.activePath
	isErr := m.statusErr != ""
	if isErr {
		right = m.statusErr
	}
	if m.helpMode {
		left = "? / Esc: close help"
		right = m.activePath
		isErr = false
	} else if m.searchMode {
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
	} else if m.markSetPending {
		left = "mark: _"
	} else if m.markJumpPending {
		left = "jump to mark: _"
	} else if m.marksListMode {
		// Right side keeps its default statusErr-or-activePath precedence
		// here, unlike helpMode/searchMode above — spec §7 requires this:
		// activateMarksListEntry can set statusErr and deliberately stay
		// in marksListMode, and that error must still surface.
		left = "↑/k ↓/j move · enter jump · d delete · q/esc close"
	}
	return composeStatusLine(left, right, isErr, m.width)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/... -v`
Expected: PASS (all tests in the package).

- [ ] **Step 5: Verify build and vet**

Run: `go build ./... && go vet ./...`
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/render.go internal/tui/render_test.go
git commit -m "tui: render marks list screen and status-line prompts"
```

---

## Task 5: Documentation — help table, README, man page, AGENTS.md

**Files:**
- Modify: `internal/tui/help.go`
- Modify: `README.md`
- Modify: `man/thicket.1`
- Modify: `AGENTS.md`

**Interfaces:** none — documentation only, no code.

- [ ] **Step 1: Update `internal/tui/help.go`'s `Keybindings` table**

Change the `Keybindings` slice from:

```go
var Keybindings = []KeyBinding{
	{"↑, k", "Move selection up"},
	{"↓, j", "Move selection down"},
	{"PgUp, PgDn", "Move selection by a full page"},
	{"Home, End", "Jump to the first/last entry"},
	{"→, l", "Open selected directory, move focus right (no-op on files)"},
	{"←, h", "Go up one directory, move focus left (no-op at /)"},
	{"Enter", "Choose: cd to the selected directory, or to the active directory if a file/empty directory is selected; exit"},
	{"q, Esc, Ctrl-C", "Quit without changing directory"},
	{".", "Toggle hidden (dotfile) visibility (default off)"},
	{"r", "Refresh the active directory's listing"},
	{"/", "Type-ahead search the active column"},
	{"?", "Toggle this help screen"},
}
```

to:

```go
var Keybindings = []KeyBinding{
	{"↑, k", "Move selection up"},
	{"↓, j", "Move selection down"},
	{"PgUp, PgDn", "Move selection by a full page"},
	{"Home, End", "Jump to the first/last entry"},
	{"→, l", "Open selected directory, move focus right (no-op on files)"},
	{"←, h", "Go up one directory, move focus left (no-op at /)"},
	{"Enter", "Choose: cd to the selected directory, or to the active directory if a file/empty directory is selected; exit"},
	{"q, Esc, Ctrl-C", "Quit without changing directory"},
	{".", "Toggle hidden (dotfile) visibility (default off)"},
	{"r", "Refresh the active directory's listing"},
	{"/", "Type-ahead search the active column"},
	{"?", "Toggle this help screen"},
	{"m", "Bookmark the active directory under a letter"},
	{"`", "Jump to a bookmarked directory by letter"},
	{"'", "Open the marks list"},
	{"d (in marks list)", "Delete the highlighted mark"},
}
```

- [ ] **Step 2: Update `README.md`'s keybinding table**

Change:

```markdown
| `/` | Type-ahead search the active column: type to jump to the first matching entry; `Enter` keeps the match, `Esc` cancels back to where the cursor was |
| `?` | Toggle the in-app help screen |

Navigation only in v1 — no file create/rename/delete/copy/move, no config
file.
```

to:

```markdown
| `/` | Type-ahead search the active column: type to jump to the first matching entry; `Enter` keeps the match, `Esc` cancels back to where the cursor was |
| `?` | Toggle the in-app help screen |
| `m` | Bookmark the active directory under a letter |
| `` ` `` | Jump to a bookmarked directory by letter |
| `'` | Open the marks list |
| `d` (in marks list) | Delete the highlighted mark |

Navigation only in v1 — no file create/rename/delete/copy/move, no config
file.
```

- [ ] **Step 3: Update `man/thicket.1`'s KEYS section**

Change:

```troff
.TP
.B ?
Toggle the in-app help screen (this key table). Any other key is ignored
while the help screen is open; press
.B ?
or
.B Esc
to close it.
.SH EXIT STATUS
```

to:

```troff
.TP
.B ?
Toggle the in-app help screen (this key table). Any other key is ignored
while the help screen is open; press
.B ?
or
.B Esc
to close it.
.TP
.B m
Bookmark the active directory under a letter (
.BR a - z ,
.BR A - Z ).
.TP
.B `
Jump straight to a bookmarked directory by letter.
.TP
.B '
Open the full-screen marks list: 
.BR Up / k
and
.BR Down / j
move,
.B Enter
jumps to the highlighted mark,
.B d
deletes it,
.BR q / Esc
closes the list.
.SH EXIT STATUS
```

- [ ] **Step 4: Update `AGENTS.md`**

Change the "Project Overview" section's closing sentence, from:

```markdown
v1 scope is intentionally locked to navigation only — no file
create/rename/delete/copy/move, no config file, no mouse support, no
filesystem watching (see `docs/superpowers/specs/2026-08-15-thicket-tui-file-browser-design.md`
§3). A `/`-triggered type-ahead cursor search within the active column was
added afterward, per `docs/superpowers/specs/2026-08-16-thicket-type-ahead-search-design.md`,
which amends that one non-goal — every other v1 non-goal in §3 still holds.
```

to:

```markdown
v1 scope is intentionally locked to navigation only — no file
create/rename/delete/copy/move, no config file, no mouse support, no
filesystem watching (see `docs/superpowers/specs/2026-08-15-thicket-tui-file-browser-design.md`
§3). Two amendments to that non-goal list have shipped since: a
`/`-triggered type-ahead cursor search within the active column
(`docs/superpowers/specs/2026-08-16-thicket-type-ahead-search-design.md`),
and vim/ranger-style directory marks/bookmarks
(`docs/superpowers/specs/2026-08-16-thicket-directory-marks-design.md`).
Every other v1 non-goal in §3 still holds.
```

Change the "Architecture & Data Flow" section's dependency-direction line
and numbered list. Change:

```markdown
Three-layer module, strict dependency direction `cmd/thicket → internal/tui → internal/fsutil`:

1. **`internal/fsutil`** — pure filesystem I/O. `ListDir(dir, showHidden) ([]Entry, error)`
```

to:

```markdown
Three-layer module, strict dependency direction `cmd/thicket → internal/tui → {internal/fsutil, internal/marks}`:

1. **`internal/fsutil`** — pure filesystem I/O. `ListDir(dir, showHidden) ([]Entry, error)`
```

Change the paragraph immediately following item 1's closing sentence
("No caching, no state.") — insert a new item 2 for `internal/marks` and
renumber the existing items 2-4 to 3-5. Change:

```markdown
   which would freeze the whole TUI since preview reads run inline on the
   Bubble Tea event loop. No caching, no state.
2. **`internal/tui`** — Bubble Tea MVU (Model-Update-View / Elm-architecture).
```

to:

```markdown
   which would freeze the whole TUI since preview reads run inline on the
   Bubble Tea event loop. No caching, no state.
2. **`internal/marks`** — pure disk I/O for directory marks (bookmarks): a
   `letter -> absolute-path` table persisted as sorted `letter\tpath\n`
   lines. `Load(path)`/`Save(path, m)` mirror `fsutil`'s per-entry error
   tolerance (a malformed line is skipped, not fatal) and no-caching
   design; `DefaultPath()` resolves `$XDG_STATE_HOME/thicket/marks`
   (falling back to `$HOME/.local/state/thicket/marks`).
3. **`internal/tui`** — Bubble Tea MVU (Model-Update-View / Elm-architecture).
```

Renumber the remaining two list items (`cmd/thicket` and the shell
wrappers) from `3.`/`4.` to `4.`/`5.` — no other text in those two items
changes.

Change the ASCII diagram from:

```
th (shell function) → thicket binary (/dev/tty for UI) → stdout: chosen path
                                │
                    cmd/thicket/main.go
                                │
                         internal/tui  (Model/Update/View, Bubble Tea)
                                │
                        internal/fsutil (ListDir, ReadFilePreview)
```

to:

```
th (shell function) → thicket binary (/dev/tty for UI) → stdout: chosen path
                                │
                    cmd/thicket/main.go
                                │
                         internal/tui  (Model/Update/View, Bubble Tea)
                                │
                  ┌─────────────┴─────────────┐
        internal/fsutil (ListDir,     internal/marks (Load,
        ReadFilePreview)              Save, DefaultPath)
```

In the "Key Directories" table, add a row after the `internal/tui/` row:

```markdown
| `internal/marks/` | Pure directory-marks (bookmark) persistence — letter→path table, load/save, no TUI concerns |
```

In the "Important Files" table, add two rows after the `internal/tui/search.go`
row:

```markdown
| `internal/marks/marks.go` | `Marks`, `Load`, `Save`, `DefaultPath` — the letter→path bookmark table and its on-disk format |
| `internal/tui/marks.go` | `sortedMarkLetters`, `marksListCursorFor` — pure helpers behind the marks list screen and `` ` ``/`'` navigation |
```

In the "Testing & QA" section's "Coverage shape" bullet, append a sentence.
Change:

```markdown
- **Coverage shape**: `internal/fsutil` and `internal/tui/update.go` get
  pure-function unit tests against real temp-dir fixtures (including
  broken symlinks and unreadable directories); `internal/tui/render.go` is
  covered by string-content assertions on `View()` output at specific
  terminal widths (regression tests explicitly reference past bugs, e.g.
  `TestView_StatusLineAt80ColumnsStillShowsError`). Shell integration
  (`thicket` wrapper + `/dev/tty` behavior) is **not automated** — it's a
  manual smoke test only, since it requires a real terminal.
```

to:

```markdown
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
```

- [ ] **Step 5: Verify build, vet, and full test suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS everywhere (this task touches no `.go` logic, only a data
table literal in `help.go` plus prose files, but confirms the doc task
didn't break the build).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/help.go README.md man/thicket.1 AGENTS.md
git commit -m "docs: document directory marks keybindings and non-goal amendment"
```

---

## Manual Verification (after Task 5)

Automated tests cover every keystroke transition and the on-disk file
format in isolation, but cross-process persistence — the entire point of
marks existing on disk instead of as session-only `Model` state — cannot
be exercised by an in-process `go test` run. Run once by hand:

1. `go build -o thicket ./cmd/thicket`
2. `mkdir -p /tmp/mark-test-a /tmp/mark-test-b`
3. `XDG_STATE_HOME=/tmp/thicket-xdg-test ./thicket /tmp/mark-test-a`
4. Press `m`, then `a`. Confirm the status line briefly shows no error.
   Press `q` to quit.
5. `XDG_STATE_HOME=/tmp/thicket-xdg-test ./thicket /tmp/mark-test-b` (a
   fresh process, started from a different directory).
6. Press `` ` ``, then `a`. Confirm the header line now reads
   `/tmp/mark-test-a` — the mark survived the process boundary.
7. Press `'`. Confirm the marks list shows exactly one row: `a` and
   `/tmp/mark-test-a`, highlighted.
8. Press `d`. Confirm the row disappears and the screen shows
   `no marks set`.
9. Press `q` to close the list, then `q` again to quit.
10. `cat /tmp/thicket-xdg-test/thicket/marks` — confirm the file is now
    empty (the delete persisted).
