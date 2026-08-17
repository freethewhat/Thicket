# Recursive Find Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an `f`-triggered recursive find over the subtree rooted at the active directory, replacing the Miller-column view with a full-screen, live-filtered result list; `Enter` relocates the browser to the selected match's parent directory.

**Architecture:** A new, pure `fsutil.WalkSubtree` does one synchronous, capped recursive filesystem walk when `f` is pressed, capturing files and directories under `activePath` into `Model.findResults`. Every keystroke afterward only re-filters that already-captured in-memory slice (`filterWalk`, `internal/tui/search.go`) — no repeated disk I/O. Rendering is a third full-screen mode alongside `helpMode`/`marksListMode`, replacing the column layout entirely rather than overlaying it.

**Tech Stack:** Go 1.24.6, `github.com/charmbracelet/bubbletea` v1.x, `github.com/charmbracelet/lipgloss` v1.1.0. Standard library `testing` only.

**Spec:** `docs/superpowers/specs/2026-08-16-thicket-recursive-find-design.md`

## Global Constraints

- No goroutines, channels, or `tea.Cmd` async work anywhere (`AGENTS.md`) — the walk is synchronous, bounded by `walkMaxDepth = 12` and `walkMaxEntries = 20000` (spec §3), whichever is hit first.
- Symlinked directories are never descended into during the walk, but still appear as leaf `WalkEntry` results (spec §3/§8).
- Permission-denied subdirectories are skipped silently during the walk; only a failure to list `activePath` itself (the walk root) is a reportable error (spec §3).
- `showHidden` is honored at every level of the walk exactly like `ListDir` — a skipped dotdir is never descended into (spec §3).
- Matching is plain case-insensitive substring containment — no fuzzy matching (spec §2).
- An empty query in find mode matches every captured entry (browsable immediately after `f`), deliberately diverging from `firstMatch`'s "empty query matches nothing" convention (spec §6).
- `msg.Type`, never `msg.String()`, discriminates keys while a mode is composing query text — `msg.String()` returns readable names ("tab", "pgdown") for keys that must never become query text (existing convention, `internal/tui/update.go`'s `handleSearchKey`).
- New test cases go into existing test files for existing packages (`AGENTS.md`); a new test file is only for genuinely new exported surface in an existing package (e.g. `internal/fsutil/walk_test.go`, mirroring the `preview_test.go` precedent for `ReadFilePreview`).
- Every mode (`searchMode`, `helpMode`, `markSetPending`, `markJumpPending`, `marksListMode`, and now `findMode`) is mutually exclusive; `Update`'s early-return dispatch order guarantees only one is ever active. `Ctrl-C` hard-quits unconditionally, checked before any mode branch.

---

### Task 1: `fsutil.WalkSubtree`

**Files:**
- Create: `internal/fsutil/walk.go`
- Test: `internal/fsutil/walk_test.go`

**Interfaces:**
- Consumes: `classify(dir, name string) Entry` (unexported, `internal/fsutil/listing.go`) — reused directly, same package.
- Produces: `type WalkEntry struct { Entry; RelPath string }` and `func WalkSubtree(root string, showHidden bool) (entries []WalkEntry, truncated bool, err error)` — consumed by Task 3 (`Model.enterFindMode`) and Task 6 (`filterWalk`, `renderFind`).

- [ ] **Step 1: Write the failing tests**

```go
// internal/fsutil/walk_test.go
package fsutil_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"thicket/internal/fsutil"
)

// walkFixture builds:
//
//	root/
//	  sub/
//	    grand/
//	      deep.txt
//	    leaf.txt
//	  file.txt
//	  .hidden
//	  denied/        (chmod 0o000)
//	  link -> sub     (symlink to a directory)
func walkFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "sub", "grand"))
	mustWriteFile(t, filepath.Join(root, "sub", "grand", "deep.txt"), "hi")
	mustWriteFile(t, filepath.Join(root, "sub", "leaf.txt"), "hi")
	mustWriteFile(t, filepath.Join(root, "file.txt"), "hi")
	mustWriteFile(t, filepath.Join(root, ".hidden"), "hi")
	return root
}

func TestWalkSubtree_FindsNestedFilesAndDirs(t *testing.T) {
	root := walkFixture(t)

	entries, truncated, err := fsutil.WalkSubtree(root, false)
	if err != nil {
		t.Fatalf("WalkSubtree: %v", err)
	}
	if truncated {
		t.Fatal("expected truncated == false")
	}
	want := map[string]bool{
		"sub":                false,
		"sub/grand":          false,
		filepath.Join("sub", "grand", "deep.txt"): false,
		filepath.Join("sub", "leaf.txt"):           false,
		"file.txt":                                 false,
	}
	for _, e := range entries {
		if _, ok := want[e.RelPath]; ok {
			want[e.RelPath] = true
		}
	}
	for relPath, found := range want {
		if !found {
			t.Fatalf("expected RelPath %q in results, got %+v", relPath, entries)
		}
	}
}

func TestWalkSubtree_SkipsHiddenWhenShowHiddenFalse(t *testing.T) {
	root := walkFixture(t)

	entries, _, err := fsutil.WalkSubtree(root, false)
	if err != nil {
		t.Fatalf("WalkSubtree: %v", err)
	}
	for _, e := range entries {
		if e.RelPath == ".hidden" {
			t.Fatal("expected .hidden excluded when showHidden is false")
		}
	}

	entries, _, err = fsutil.WalkSubtree(root, true)
	if err != nil {
		t.Fatalf("WalkSubtree: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.RelPath == ".hidden" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected .hidden included when showHidden is true")
	}
}

func TestWalkSubtree_DoesNotDescendIntoSymlinkedDir(t *testing.T) {
	root := walkFixture(t)
	link := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Join(root, "sub"), link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	entries, _, err := fsutil.WalkSubtree(root, false)
	if err != nil {
		t.Fatalf("WalkSubtree: %v", err)
	}
	sawLink := false
	for _, e := range entries {
		if e.RelPath == "link" {
			sawLink = true
			if !e.IsSymlink {
				t.Fatal("expected link entry to have IsSymlink == true")
			}
		}
		if e.RelPath == filepath.Join("link", "leaf.txt") {
			t.Fatal("expected walk to not descend into symlinked directory")
		}
	}
	if !sawLink {
		t.Fatal("expected link itself to appear as a leaf result")
	}
}

func TestWalkSubtree_SkipsPermissionDeniedSubdir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	root := walkFixture(t)
	denied := filepath.Join(root, "denied")
	mustMkdir(t, denied)
	mustWriteFile(t, filepath.Join(denied, "secret.txt"), "hi")
	if err := os.Chmod(denied, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(denied, 0o755) })

	entries, _, err := fsutil.WalkSubtree(root, false)
	if err != nil {
		t.Fatalf("WalkSubtree: %v", err)
	}
	sawDenied := false
	for _, e := range entries {
		if e.RelPath == "denied" {
			sawDenied = true
		}
		if e.RelPath == filepath.Join("denied", "secret.txt") {
			t.Fatal("expected walk to not surface contents of a permission-denied subdir")
		}
	}
	if !sawDenied {
		t.Fatal("expected the denied directory itself to still appear as a result")
	}
}

func TestWalkSubtree_StopsAtMaxDepthAndSetsTruncated(t *testing.T) {
	root := t.TempDir()
	dir := root
	for i := 0; i < 20; i++ {
		dir = filepath.Join(dir, "d")
		mustMkdir(t, dir)
	}

	entries, truncated, err := fsutil.WalkSubtree(root, false)
	if err != nil {
		t.Fatalf("WalkSubtree: %v", err)
	}
	if !truncated {
		t.Fatal("expected truncated == true for a 20-level-deep tree")
	}
	if len(entries) >= 20 {
		t.Fatalf("expected depth cap to bound results well under 20, got %d", len(entries))
	}
}

func TestWalkSubtree_BelowEntryCapReturnsAllUntruncated(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 200; i++ {
		mustWriteFile(t, filepath.Join(root, "f"+string(rune('a'+i%26))+string(rune('0'+i/26))+".txt"), "hi")
	}

	entries, truncated, err := fsutil.WalkSubtree(root, false)
	if err != nil {
		t.Fatalf("WalkSubtree: %v", err)
	}
	if truncated {
		t.Fatal("expected truncated == false well below the entry cap")
	}
	if len(entries) != 200 {
		t.Fatalf("got %d entries, want 200", len(entries))
	}
}

// TestWalkSubtree_StopsAtMaxEntriesAndSetsTruncated genuinely exceeds the
// real walkMaxEntries (20000) cap — the walkMaxDepth test above bounds
// worst-case cost via depth, this one via breadth, so both caps get an
// honest exercise rather than one being asserted only by name.
func TestWalkSubtree_StopsAtMaxEntriesAndSetsTruncated(t *testing.T) {
	root := t.TempDir()
	const overCap = 20050 // > walkMaxEntries (20000)
	for i := 0; i < overCap; i++ {
		name := filepath.Join(root, fmt.Sprintf("f%05d.txt", i))
		if err := os.WriteFile(name, nil, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	entries, truncated, err := fsutil.WalkSubtree(root, false)
	if err != nil {
		t.Fatalf("WalkSubtree: %v", err)
	}
	if !truncated {
		t.Fatal("expected truncated == true for a subtree over the entry cap")
	}
	if len(entries) != 20000 {
		t.Fatalf("got %d entries, want exactly the 20000 cap", len(entries))
	}
}

func TestWalkSubtree_RelPathIsRelativeToRoot(t *testing.T) {
	root := walkFixture(t)

	entries, _, err := fsutil.WalkSubtree(root, false)
	if err != nil {
		t.Fatalf("WalkSubtree: %v", err)
	}
	for _, e := range entries {
		if filepath.IsAbs(e.RelPath) {
			t.Fatalf("RelPath %q must be relative, not absolute", e.RelPath)
		}
	}
}

func TestWalkSubtree_ErrorsOnUnreadableRoot(t *testing.T) {
	_, _, err := fsutil.WalkSubtree(filepath.Join(t.TempDir(), "does-not-exist"), false)
	if err == nil {
		t.Fatal("expected an error for an unreadable root")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/fsutil/... -run TestWalkSubtree -v`
Expected: FAIL — `undefined: fsutil.WalkSubtree` (compile error, since `WalkSubtree` does not exist yet).

- [ ] **Step 3: Implement `WalkSubtree`**

```go
// internal/fsutil/walk.go
package fsutil

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// walkMaxDepth is the descent limit below a WalkSubtree root.
	walkMaxDepth = 12
	// walkMaxEntries is the total-entries-visited limit for a WalkSubtree
	// call, regardless of depth.
	walkMaxEntries = 20000
)

// WalkEntry is one node found under a WalkSubtree root.
type WalkEntry struct {
	Entry
	// RelPath is the path relative to the walk root, e.g. "sub/dir/file.go".
	RelPath string
}

// WalkSubtree recursively lists root and everything under it (files and
// directories), reusing the same per-entry classification ListDir already
// uses. showHidden is honored at every level exactly like ListDir: a
// dotfile/dotdir is skipped, and a skipped directory is never descended
// into. Symlinked directories are included as leaf WalkEntry results but
// are never traversed, regardless of depth — no symlink-cycle risk.
// Permission-denied subdirectories are skipped silently; the walk
// continues with everything else. The walk stops as soon as walkMaxDepth
// or walkMaxEntries is reached, whichever comes first; truncated reports
// whether that happened before the whole subtree was covered. err is
// non-nil only if root itself cannot be listed.
func WalkSubtree(root string, showHidden bool) ([]WalkEntry, bool, error) {
	rootEntries, err := os.ReadDir(root)
	if err != nil {
		return nil, false, err
	}

	var results []WalkEntry
	truncated := false

	var walk func(dirEntries []os.DirEntry, dir, relDir string, depth int)
	walk = func(dirEntries []os.DirEntry, dir, relDir string, depth int) {
		names := make([]string, 0, len(dirEntries))
		for _, de := range dirEntries {
			name := de.Name()
			if !showHidden && strings.HasPrefix(name, ".") {
				continue
			}
			names = append(names, name)
		}
		sort.SliceStable(names, func(i, j int) bool {
			return strings.ToLower(names[i]) < strings.ToLower(names[j])
		})

		for _, name := range names {
			if truncated {
				return
			}
			if len(results) >= walkMaxEntries {
				truncated = true
				return
			}
			e := classify(dir, name)
			relPath := name
			if relDir != "" {
				relPath = filepath.Join(relDir, name)
			}
			results = append(results, WalkEntry{Entry: e, RelPath: relPath})

			if e.IsDir && !e.IsSymlink && depth < walkMaxDepth {
				childDir := filepath.Join(dir, name)
				childEntries, err := os.ReadDir(childDir)
				if err != nil {
					continue // permission-denied subdir: skip silently
				}
				walk(childEntries, childDir, relPath, depth+1)
			}
		}
	}

	walk(rootEntries, root, "", 0)
	return results, truncated, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/fsutil/... -run TestWalkSubtree -v`
Expected: PASS (all 9 test functions).

- [ ] **Step 5: Run full package tests and vet**

Run: `go test ./internal/fsutil/... && go vet ./internal/fsutil/...`
Expected: PASS, no vet warnings.

- [ ] **Step 6: Commit**

```bash
git add internal/fsutil/walk.go internal/fsutil/walk_test.go
git commit -m "fsutil: add WalkSubtree for recursive find"
```

---

### Task 2: `filterWalk`

**Files:**
- Modify: `internal/tui/search.go` (currently 25 lines — add `filterWalk` after `firstMatch`)
- Test: `internal/tui/update_test.go` (append to end)

**Interfaces:**
- Consumes: `fsutil.WalkEntry` (Task 1).
- Produces: `func filterWalk(results []fsutil.WalkEntry, query string) []fsutil.WalkEntry` — consumed by Task 3 (`applyFindFilter`, `moveFindCursor`, `commitFindSelection`) and Task 6 (`renderFind`, `statusLine`).

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/update_test.go`:

```go
func TestFilterWalk_CaseInsensitiveSubstringOverRelPath(t *testing.T) {
	results := []fsutil.WalkEntry{
		{Entry: fsutil.Entry{Name: "Report.txt"}, RelPath: "docs/Report.txt"},
		{Entry: fsutil.Entry{Name: "other.txt"}, RelPath: "other.txt"},
	}

	got := filterWalk(results, "report")

	if len(got) != 1 || got[0].RelPath != "docs/Report.txt" {
		t.Fatalf("filterWalk(%q) = %+v, want just docs/Report.txt", "report", got)
	}
}

func TestFilterWalk_EmptyQueryMatchesEveryEntry(t *testing.T) {
	results := []fsutil.WalkEntry{
		{Entry: fsutil.Entry{Name: "a"}, RelPath: "a"},
		{Entry: fsutil.Entry{Name: "b"}, RelPath: "b"},
	}

	got := filterWalk(results, "")

	if len(got) != 2 {
		t.Fatalf("filterWalk(\"\") = %+v, want all %d entries unchanged", got, len(results))
	}
}

func TestFilterWalk_NoMatchReturnsEmpty(t *testing.T) {
	results := []fsutil.WalkEntry{
		{Entry: fsutil.Entry{Name: "a"}, RelPath: "a"},
	}

	got := filterWalk(results, "zzz")

	if len(got) != 0 {
		t.Fatalf("filterWalk(\"zzz\") = %+v, want empty", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/... -run TestFilterWalk -v`
Expected: FAIL — `undefined: filterWalk`.

- [ ] **Step 3: Implement `filterWalk`**

Append to `internal/tui/search.go`:

```go
// filterWalk returns the entries in results (in walk order) whose RelPath
// case-insensitively contains query. An empty query matches every entry
// (returns results unchanged, in walk order) — deliberately diverging
// from firstMatch's "empty query matches nothing" convention: find mode
// is a browsable list, useful immediately after f is pressed, not only
// once the user starts typing.
func filterWalk(results []fsutil.WalkEntry, query string) []fsutil.WalkEntry {
	if query == "" {
		return results
	}
	q := strings.ToLower(query)
	var matched []fsutil.WalkEntry
	for _, e := range results {
		if strings.Contains(strings.ToLower(e.RelPath), q) {
			matched = append(matched, e)
		}
	}
	return matched
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/... -run TestFilterWalk -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/search.go internal/tui/update_test.go
git commit -m "tui: add filterWalk for recursive-find result filtering"
```

---

### Task 3: Enter/exit find mode (`f`, Esc, Ctrl-C)

**Files:**
- Modify: `internal/tui/model.go:14-53` (add fields to `Model`)
- Modify: `internal/tui/update.go:11-89` (dispatch + `f` key), and append `enterFindMode`/`exitFindMode` near the other `enter*Mode`/`exit*Mode` helpers
- Test: `internal/tui/update_test.go` (append)

**Interfaces:**
- Consumes: `fsutil.WalkSubtree` (Task 1).
- Produces: `Model.findMode/findQuery/findResults/findCursor/findTruncated` fields; `func (m *Model) enterFindMode()`, `func (m *Model) exitFindMode()` — consumed by Task 4 (`handleFindKey`) and Task 6 (rendering).

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/update_test.go`:

```go
func TestUpdate_FEntersFindModeAndWalksSubtree(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("f must not quit the program")
	}
	if !m.findMode {
		t.Fatal("expected findMode == true after f")
	}
	if m.findQuery != "" {
		t.Fatalf("findQuery = %q, want empty", m.findQuery)
	}
	if len(m.findResults) == 0 {
		t.Fatal("expected findResults populated from the fixture's subtree")
	}
	if m.findCursor != 0 {
		t.Fatalf("findCursor = %d, want 0 (non-empty walk)", m.findCursor)
	}
}

func TestUpdate_FOnUnreadableRootSetsStatusErrAndSkipsFindMode(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	root := setupFixture(t)
	m := newTestModel(t, root)
	if err := os.Chmod(root, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(root, 0o755) })

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)

	if m.findMode {
		t.Fatal("expected findMode to stay false when the walk root is unreadable")
	}
	if m.statusErr == "" {
		t.Fatal("expected statusErr set")
	}
}

func TestUpdate_FOnEmptyDirectoryOpensFindModeWithNoResults(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, filepath.Join(root, "sub", "grand"))

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)

	if !m.findMode {
		t.Fatal("expected findMode == true even for an empty subtree")
	}
	if len(m.findResults) != 0 {
		t.Fatalf("findResults = %+v, want empty", m.findResults)
	}
	if m.findCursor != -1 {
		t.Fatalf("findCursor = %d, want -1", m.findCursor)
	}
}

func TestUpdate_FindEscExitsWithNoRelocation(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	prevPath, prevCursor := m.activePath, m.activeCursor

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("Esc during find must not quit the program")
	}
	if m.findMode {
		t.Fatal("expected findMode == false after Esc")
	}
	if m.activePath != prevPath || m.activeCursor != prevCursor {
		t.Fatalf("Esc must not relocate: activePath=%q activeCursor=%d, want %q/%d", m.activePath, m.activeCursor, prevPath, prevCursor)
	}
}

func TestUpdate_CtrlCQuitsEvenDuringFind(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("expected Ctrl-C to return tea.Quit")
	}
	if m.selected {
		t.Fatal("expected selected == false on Ctrl-C")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/... -run 'TestUpdate_F|TestUpdate_CtrlCQuitsEvenDuringFind' -v`
Expected: FAIL — compile error, `m.findMode` undefined.

- [ ] **Step 3: Add `Model` fields**

In `internal/tui/model.go`, insert after the `helpMode bool` field (currently line 31) and before the `markTable` comment block:

```go
	// findMode/findQuery/findResults/findCursor/findTruncated: recursive
	// find state (spec
	// docs/superpowers/specs/2026-08-16-thicket-recursive-find-design.md).
	// Zero-valued at construction, same as searchMode/helpMode. findResults
	// is captured once when f is pressed and held for the session; it is
	// never re-walked while findMode is true, only re-filtered via
	// filterWalk. Mutually exclusive with searchMode/helpMode/
	// markSetPending/markJumpPending/marksListMode — Update's early-return
	// dispatch order guarantees only one mode is ever active.
	findMode      bool
	findQuery     string
	findResults   []fsutil.WalkEntry
	findCursor    int // index into filterWalk(findResults, findQuery); -1 when that view is empty
	findTruncated bool
```

Also update the existing `helpMode` doc comment (currently line 28-30) to mention `findMode` in its mutual-exclusivity note, matching how the `markTable` block's comment already lists every other mode:

```go
	// helpMode: in-app help screen state (? toggles it open/closed; see
	// internal/tui/help.go). Mutually exclusive with searchMode/findMode/
	// markSetPending/markJumpPending/marksListMode — Update's early-return
	// dispatch order guarantees only one is ever active.
	helpMode bool
```

- [ ] **Step 4: Add `f` to the normal-mode key switch and the `findMode` dispatch branch**

In `internal/tui/update.go`, add a new early-return branch to `Update` right after the existing `if m.searchMode { ... }` block (currently lines 35-38):

```go
		if m.findMode {
			m.handleFindKey(msg)
			return m, nil
		}
```

And add a new case to the normal-mode `switch msg.String()` (currently ending around line 86, alongside `case "/":`):

```go
		case "f":
			m.enterFindMode()
```

- [ ] **Step 5: Implement `enterFindMode`/`exitFindMode`**

Append to `internal/tui/update.go`, near `enterSearchMode`/`exitSearchMode`:

```go
// enterFindMode runs a one-time recursive walk of activePath's subtree
// (spec §5) and opens the full-screen find-mode result list. On a walk
// error (activePath itself unreadable), statusErr is set and find mode is
// never entered — mirrors handleRight's treatment of a failed ListDir.
func (m *Model) enterFindMode() {
	results, truncated, err := fsutil.WalkSubtree(m.activePath, m.showHidden)
	if err != nil {
		m.statusErr = err.Error()
		return
	}
	m.findMode = true
	m.findQuery = ""
	m.findResults = results
	m.findTruncated = truncated
	m.findCursor = -1
	if len(results) > 0 {
		m.findCursor = 0
	}
	m.statusErr = ""
}

// exitFindMode closes find mode without touching activePath/activeCursor.
// commitFindSelection (Task 5) relocates separately, before calling this;
// every other exit path (Esc, empty-backspace) never relocates.
func (m *Model) exitFindMode() {
	m.findMode = false
	m.findQuery = ""
}

// handleFindKey is completed in Task 4; a placeholder that only handles
// Esc is enough for this task's tests (Ctrl-C never reaches here — it's
// intercepted earlier in Update, before any mode branch).
func (m *Model) handleFindKey(msg tea.KeyMsg) {
	if msg.Type == tea.KeyEsc {
		m.exitFindMode()
	}
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/tui/... -run 'TestUpdate_F|TestUpdate_CtrlCQuitsEvenDuringFind' -v`
Expected: PASS.

- [ ] **Step 7: Run full package tests**

Run: `go test ./internal/tui/... && go vet ./internal/tui/...`
Expected: PASS — this also confirms the new fields/dispatch branch didn't break any existing search/marks/help test.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/model.go internal/tui/update.go internal/tui/update_test.go
git commit -m "tui: enter/exit recursive find mode on f/Esc"
```

---

### Task 4: Typing, filtering, and result-cursor movement

**Files:**
- Modify: `internal/tui/update.go` (replace the Task 3 placeholder `handleFindKey`; add `appendFindQuery`, `applyFindFilter`, `moveFindCursor`)
- Test: `internal/tui/update_test.go` (append)

**Interfaces:**
- Consumes: `filterWalk` (Task 2).
- Produces: full `handleFindKey`, `func (m *Model) moveFindCursor(delta int)` — consumed by Task 5 (`commitFindSelection` reads the same `filterWalk` result shape) and exercised directly by Task 6's rendering tests.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/update_test.go`:

```go
func TestUpdate_FindTypingFiltersResultsLive(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("leaf")})
	m = updated.(Model)

	filtered := filterWalk(m.findResults, m.findQuery)
	if len(filtered) != 1 || filtered[0].RelPath != filepath.Join("sub", "leaf.txt") {
		t.Fatalf("filtered results = %+v, want just sub/leaf.txt", filtered)
	}
	if m.findCursor != 0 {
		t.Fatalf("findCursor = %d, want 0", m.findCursor)
	}
}

func TestUpdate_FindNoMatchSetsCursorToNegativeOne(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("zzzznomatch")})
	m = updated.(Model)

	if m.findCursor != -1 {
		t.Fatalf("findCursor = %d, want -1", m.findCursor)
	}
}

func TestUpdate_FindBackspaceShrinksQueryAndRefilters(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("leafx")})
	m = updated.(Model)
	if len(filterWalk(m.findResults, m.findQuery)) != 0 {
		t.Fatal("precondition: 'leafx' should match nothing")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(Model)

	if m.findQuery != "leaf" {
		t.Fatalf("findQuery = %q, want %q", m.findQuery, "leaf")
	}
	if m.findCursor != 0 {
		t.Fatalf("findCursor = %d, want 0 after backspace re-matches", m.findCursor)
	}
}

func TestUpdate_FindBackspaceOnEmptyQueryExitsWithNoRelocation(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	prevPath := m.activePath
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(Model)

	if m.findMode {
		t.Fatal("expected findMode == false after Backspace on an empty query")
	}
	if m.activePath != prevPath {
		t.Fatalf("activePath = %q, want unchanged %q", m.activePath, prevPath)
	}
}

func TestUpdate_FindSpaceKeyAppendsLiteralSpace(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	m = updated.(Model)

	if m.findQuery != "a b" {
		t.Fatalf("findQuery = %q, want %q", m.findQuery, "a b")
	}
}

func TestUpdate_FindUpDownMovesResultCursorClampedAtEnds(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)
	n := len(filterWalk(m.findResults, m.findQuery))
	if n < 2 {
		t.Fatalf("precondition: fixture must yield at least 2 walk results, got %d", n)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.findCursor != 0 {
		t.Fatalf("Up at index 0 must clamp: findCursor = %d, want 0", m.findCursor)
	}

	for i := 0; i < n+2; i++ {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(Model)
	}
	if m.findCursor != n-1 {
		t.Fatalf("Down past the end must clamp: findCursor = %d, want %d", m.findCursor, n-1)
	}
}

func TestUpdate_FindLeftRightArrowsAreNoOps(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)
	beforeCursor, beforeQuery := m.findCursor, m.findQuery

	for _, kt := range []tea.KeyType{tea.KeyLeft, tea.KeyRight} {
		updated, cmd := m.Update(tea.KeyMsg{Type: kt})
		m = updated.(Model)
		if cmd != nil {
			t.Fatalf("key %v produced a command during find", kt)
		}
	}

	if m.findCursor != beforeCursor || m.findQuery != beforeQuery {
		t.Fatalf("Left/Right must be no-ops during find: cursor=%d query=%q, want %d/%q", m.findCursor, m.findQuery, beforeCursor, beforeQuery)
	}
}

func TestUpdate_FindLettersLikeQAndHDoNotTriggerNavCommands(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("'q' during find must not quit")
	}
	if !m.findMode {
		t.Fatal("expected still in findMode")
	}
	if m.findQuery != "q" {
		t.Fatalf("findQuery = %q, want %q (q must be query text, not a command)", m.findQuery, "q")
	}
}

func TestUpdate_FindTabAndOtherControlKeysAreNoOps(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)
	beforeCursor, beforeQuery := m.findCursor, m.findQuery

	for _, kt := range []tea.KeyType{tea.KeyTab, tea.KeyCtrlU, tea.KeyHome, tea.KeyPgUp, tea.KeyF1} {
		updated, cmd := m.Update(tea.KeyMsg{Type: kt})
		m = updated.(Model)
		if cmd != nil {
			t.Fatalf("key %v produced a command during find", kt)
		}
	}

	if m.findCursor != beforeCursor || m.findQuery != beforeQuery {
		t.Fatalf("expected no state change, got cursor=%d query=%q", m.findCursor, m.findQuery)
	}
	if !m.findMode {
		t.Fatal("expected still in findMode")
	}
}

func TestUpdate_WindowResizeWorksDuringFind(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = updated.(Model)

	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 60, Height: 15})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("resize must not quit the program")
	}
	if m.width != 60 || m.height != 15 {
		t.Fatalf("width/height = %d/%d, want 60/15", m.width, m.height)
	}
	if !m.findMode || m.findQuery != "x" {
		t.Fatalf("resize must not disturb find state: findMode=%v findQuery=%q", m.findMode, m.findQuery)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/... -run 'TestUpdate_Find|TestUpdate_WindowResizeWorksDuringFind' -v`
Expected: FAIL — typing/Backspace/Up/Down/Space are no-ops under the Task 3 placeholder, so `findQuery`/`findCursor` assertions fail.

- [ ] **Step 3: Implement full `handleFindKey` and its helpers**

In `internal/tui/update.go`, replace the Task 3 placeholder `handleFindKey` with:

```go
// handleFindKey processes one key while findMode is true (spec §5).
// Discriminates on msg.Type, not msg.String() — see Global Constraints.
// Any msg.Type not matched by a case below (arrows other than Up/Down,
// Tab, Ctrl-U, Home, PageUp, F-keys, ...) is an explicit no-op.
func (m *Model) handleFindKey(msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyRunes:
		m.appendFindQuery(msg.Runes...)
	case tea.KeySpace:
		m.appendFindQuery(' ')
	case tea.KeyBackspace:
		if m.findQuery == "" {
			m.exitFindMode()
			return
		}
		runes := []rune(m.findQuery)
		m.findQuery = string(runes[:len(runes)-1])
		m.applyFindFilter()
	case tea.KeyUp:
		m.moveFindCursor(-1)
	case tea.KeyDown:
		m.moveFindCursor(1)
	case tea.KeyEnter:
		m.commitFindSelection()
	case tea.KeyEsc:
		m.exitFindMode()
	}
}

// appendFindQuery adds runes to findQuery and re-runs the filter. A single
// KeyMsg can carry more than one rune (bracketed paste, composed input),
// so callers pass every rune from one message, not just the first.
func (m *Model) appendFindQuery(runes ...rune) {
	m.findQuery += string(runes)
	m.applyFindFilter()
}

// applyFindFilter resets findCursor against filterWalk(findResults,
// findQuery): 0 if the filtered view is non-empty, else -1.
func (m *Model) applyFindFilter() {
	filtered := filterWalk(m.findResults, m.findQuery)
	if len(filtered) == 0 {
		m.findCursor = -1
		return
	}
	m.findCursor = 0
}

// moveFindCursor moves findCursor by delta within the current filtered
// view, clamped at both ends. No-op if the filtered view is empty.
func (m *Model) moveFindCursor(delta int) {
	filtered := filterWalk(m.findResults, m.findQuery)
	if len(filtered) == 0 {
		return
	}
	m.findCursor += delta
	if m.findCursor < 0 {
		m.findCursor = 0
	}
	if last := len(filtered) - 1; m.findCursor > last {
		m.findCursor = last
	}
}

// commitFindSelection is completed in Task 5; a placeholder that always
// no-ops is enough for this task's tests, none of which press Enter.
func (m *Model) commitFindSelection() {}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/... -run 'TestUpdate_Find|TestUpdate_WindowResizeWorksDuringFind' -v`
Expected: PASS.

- [ ] **Step 5: Run full package tests**

Run: `go test ./internal/tui/... && go vet ./internal/tui/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/update.go internal/tui/update_test.go
git commit -m "tui: live-filter recursive find results while typing"
```

---

### Task 5: Commit a selection (`Enter`)

**Files:**
- Modify: `internal/tui/update.go` (replace the Task 4 placeholder `commitFindSelection`)
- Test: `internal/tui/update_test.go` (append)

**Interfaces:**
- Consumes: `filterWalk` (Task 2), `fsutil.ListDir`, `fsutil.IndexOfName` (both pre-existing, already used by `handleLeft`/`jumpToMark`).
- Produces: full `commitFindSelection` — this is the plan's last behavioral piece; Task 6 only adds rendering.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/update_test.go`:

```go
func TestUpdate_FindEnterRelocatesActivePathToMatchParentAndExits(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("leaf")})
	m = updated.(Model)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("Enter committing a find selection must not quit the program (two-step commit)")
	}
	if m.findMode {
		t.Fatal("expected findMode == false after commit")
	}
	wantPath := filepath.Join(root, "sub")
	if m.activePath != wantPath {
		t.Fatalf("activePath = %q, want %q (leaf.txt's parent)", m.activePath, wantPath)
	}
	if m.activeCursor < 0 || m.activeCursor >= len(m.activeEntries) || m.activeEntries[m.activeCursor].Name != "leaf.txt" {
		t.Fatalf("expected cursor on leaf.txt, got activeCursor=%d entries=%+v", m.activeCursor, m.activeEntries)
	}
}

func TestUpdate_FindEnterOnTopLevelMatchKeepsActivePath(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("file.txt")})
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if m.activePath != root {
		t.Fatalf("activePath = %q, want unchanged root %q (file.txt lives directly in root)", m.activePath, root)
	}
	if m.activeEntries[m.activeCursor].Name != "file.txt" {
		t.Fatalf("expected cursor on file.txt, got %+v", m.activeEntries[m.activeCursor])
	}
}

func TestUpdate_FindEnterOnEmptyResultsIsNoop(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	prevPath, prevCursor := m.activePath, m.activeCursor
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("zzzznomatch")})
	m = updated.(Model)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("Enter with no results must not quit")
	}
	if !m.findMode {
		t.Fatal("expected findMode to stay true — a no-match Enter does not close the prompt")
	}
	if m.activePath != prevPath || m.activeCursor != prevCursor {
		t.Fatalf("expected no relocation on a no-match Enter: activePath=%q activeCursor=%d", m.activePath, m.activeCursor)
	}
}

func TestUpdate_FindEnterOnGrandchildMatchRelocatesTwoLevelsDeep(t *testing.T) {
	root := setupFixture(t)
	mustWriteFile(t, filepath.Join(root, "sub", "grand", "deep.txt"), "hi")
	m := newTestModel(t, root)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("deep")})
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	wantPath := filepath.Join(root, "sub", "grand")
	if m.activePath != wantPath {
		t.Fatalf("activePath = %q, want %q", m.activePath, wantPath)
	}
	if m.activeEntries[m.activeCursor].Name != "deep.txt" {
		t.Fatalf("expected cursor on deep.txt, got %+v", m.activeEntries[m.activeCursor])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/... -run TestUpdate_FindEnter -v`
Expected: FAIL — `commitFindSelection` is still the Task 4 no-op placeholder, so `activePath`/`findMode` never change.

- [ ] **Step 3: Implement `commitFindSelection`**

In `internal/tui/update.go`, replace the Task 4 placeholder:

```go
// commitFindSelection relocates activePath/activeCursor to the selected
// match's parent directory (spec §5's Enter row) and exits find mode. A
// no-op when findCursor is -1 (empty walk, or the current query matches
// nothing) — find mode stays open so the user can keep typing.
func (m *Model) commitFindSelection() {
	filtered := filterWalk(m.findResults, m.findQuery)
	if m.findCursor < 0 || m.findCursor >= len(filtered) {
		return
	}
	we := filtered[m.findCursor]
	relDir := filepath.Dir(we.RelPath)
	newPath := m.activePath
	if relDir != "." {
		newPath = filepath.Join(m.activePath, relDir)
	}
	entries, err := fsutil.ListDir(newPath, m.showHidden)
	if err != nil {
		m.statusErr = err.Error()
		m.exitFindMode()
		return
	}
	m.activePath = newPath
	m.activeEntries = entries
	m.activeCursor = fsutil.IndexOfName(entries, we.Name)
	if m.activeCursor < 0 {
		m.activeCursor = 0
		if len(entries) == 0 {
			m.activeCursor = -1
		}
	}
	m.activeScroll = 0
	m.clampScroll()
	m.statusErr = ""
	m.exitFindMode()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/... -run TestUpdate_FindEnter -v`
Expected: PASS.

- [ ] **Step 5: Run full package tests**

Run: `go test ./internal/tui/... && go vet ./internal/tui/...`
Expected: PASS — every prior task's tests still pass.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/update.go internal/tui/update_test.go
git commit -m "tui: commit recursive-find selection on Enter"
```

---

### Task 6: Rendering — full-screen result list and status line

**Files:**
- Modify: `internal/tui/render.go` (`View`, `statusLine`; append `renderFind`)
- Test: `internal/tui/render_test.go` (append)

**Interfaces:**
- Consumes: `filterWalk` (Task 2), `Model.findMode/findQuery/findResults/findCursor/findTruncated` (Task 3), the package-level `dirStyle`/`symlinkStyle`/`selectedStyle`/`activePaneStyle`/`truncate`/`paneBorderWidth` already defined in `render.go`.
- Produces: `func (m Model) renderFind(rows int) string` — terminal deliverable of this plan; nothing later consumes it.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/render_test.go`:

```go
func TestView_FindModeShowsFullScreenResultList(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)

	out := m.View()

	if !strings.Contains(out, filepath.Join("sub", "leaf.txt")) {
		t.Fatalf("View() missing walked result:\n%s", out)
	}
	if strings.Contains(out, "↑/k ↓/j move · PgUp/PgDn page") {
		t.Fatal("expected column layout's normal hints replaced while findMode is true")
	}
}

func TestView_FindModeShowsNoMatches(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("zzzznomatch")})
	m = updated.(Model)

	out := m.View()

	if !strings.Contains(out, "no matches") {
		t.Fatalf("View() missing 'no matches':\n%s", out)
	}
}

func TestView_FindModeShowsTruncatedIndicator(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.findMode = true
	m.findResults = []fsutil.WalkEntry{{Entry: fsutil.Entry{Name: "a"}, RelPath: "a"}}
	m.findCursor = 0
	m.findTruncated = true

	out := m.View()

	if !strings.Contains(out, "truncated") {
		t.Fatalf("View() missing truncated indicator:\n%s", out)
	}
}

func TestView_FindModeHighlightsCursorRow(t *testing.T) {
	restoreColorProfile(t)
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.findMode = true
	m.findResults = []fsutil.WalkEntry{
		{Entry: fsutil.Entry{Name: "a"}, RelPath: "a"},
		{Entry: fsutil.Entry{Name: "b"}, RelPath: "b"},
	}
	m.findCursor = 1

	out := m.renderFind(m.visibleRows())

	want := selectedStyle.Render(truncate("b", m.width-paneBorderWidth))
	if !strings.Contains(out, want) {
		t.Fatalf("renderFind() missing highlighted row:\n%s\nwant substring:\n%s", out, want)
	}
}

func TestView_StatusLineShowsFindPromptAndMatchCount(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 150
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("leaf")})
	m = updated.(Model)

	got := m.statusLine()

	if !strings.Contains(got, "find/leaf") {
		t.Fatalf("statusLine() missing find prompt: %q", got)
	}
	if !strings.Contains(got, "(1)") {
		t.Fatalf("statusLine() missing match count: %q", got)
	}
}

func TestView_StatusLineFindHintDiscoverable(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 200

	if !strings.Contains(m.statusLine(), "f find") {
		t.Fatalf("statusLine() missing 'f find' hint: %q", m.statusLine())
	}
}
```

`render_test.go` already imports `"thicket/internal/fsutil"` and `"path/filepath"` (used by existing tests) — no import changes needed for the new tests above.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/... -run 'TestView_Find|TestView_StatusLineShowsFindPromptAndMatchCount|TestView_StatusLineFindHintDiscoverable' -v`
Expected: FAIL — `renderFind` undefined (compile error), and once stubbed, the status-line/View assertions fail since `findMode` isn't yet handled by either.

- [ ] **Step 3: Add the `View` dispatch branch**

In `internal/tui/render.go`, add to `View` (currently lines 71-97) alongside the existing `helpMode`/`marksListMode` branches:

```go
	if m.findMode {
		return header + "\n" + m.renderFind(rows) + "\n" + m.statusLine()
	}
```

- [ ] **Step 4: Add the `statusLine` branch**

In `internal/tui/render.go`'s `statusLine` (currently lines 262-297):

1. Append `· f find` to the `hints` constant, right after `· / search`:

```go
	hints := "↑/k ↓/j move · PgUp/PgDn page · Home/End top/bottom · →/l open · ←/h up · Enter cd+exit · . hidden · r refresh · / search · f find · ? help · q quit · m mark · ` jump · ' marks"
```

2. Add a new `else if` branch, alongside the existing `markSetPending`/`markJumpPending`/`marksListMode` branches:

```go
	} else if m.findMode {
		filtered := filterWalk(m.findResults, m.findQuery)
		left = fmt.Sprintf("find/%s (%d)", m.findQuery, len(filtered))
		right = m.activePath
		isErr = false
	}
```

- [ ] **Step 5: Implement `renderFind`**

Append to `internal/tui/render.go`:

```go
// renderFind draws the full-screen recursive-find result list shown while
// Model.findMode is true (spec §7). Full-screen replacement, mirroring
// renderMarksList/renderHelp.
func (m Model) renderFind(rows int) string {
	width := m.width - paneBorderWidth
	if width < 0 {
		width = 0
	}
	filtered := filterWalk(m.findResults, m.findQuery)

	var lines []string
	if len(filtered) == 0 {
		lines = []string{"no matches"}
	} else {
		lines = make([]string, len(filtered))
		for i, we := range filtered {
			text := truncate(we.RelPath, width)
			if we.IsDir {
				text = dirStyle.Render(text)
			}
			if we.IsSymlink {
				text += symlinkStyle.Render("@")
			}
			if i == m.findCursor {
				text = selectedStyle.Render(text)
			}
			lines[i] = text
		}
	}
	if m.findTruncated {
		lines = append(lines, "… truncated, refine your query")
	}
	content := strings.Join(lines, "\n")
	inner := lipgloss.NewStyle().Width(width).Height(rows).MaxHeight(rows).Render(content)
	return activePaneStyle.Render(inner)
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/tui/... -run 'TestView_Find|TestView_StatusLineShowsFindPromptAndMatchCount|TestView_StatusLineFindHintDiscoverable' -v`
Expected: PASS.

- [ ] **Step 7: Run full package tests and vet**

Run: `go test ./... && go vet ./...`
Expected: PASS across every package — this is the full regression check for the whole feature.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/render.go internal/tui/render_test.go
git commit -m "tui: render full-screen recursive-find result list"
```

---

### Task 7: Documentation

**Files:**
- Modify: `internal/tui/help.go` (`Keybindings` table)
- Modify: `README.md` (keybinding table)
- Modify: `man/thicket.1` (KEYS section)
- Modify: `AGENTS.md` (non-goals amendment paragraph)

**Interfaces:**
- Consumes: nothing new — this task only documents Tasks 1-6's shipped behavior.
- Produces: nothing consumed elsewhere; this is the plan's final task.

- [ ] **Step 1: Add the `f` row to `internal/tui/help.go`**

In `internal/tui/help.go`'s `Keybindings` slice, insert after the `{"/", "Type-ahead search the active column"},` row:

```go
	{"f", "Recursive find: type to filter files/dirs under the active directory; Enter jumps there, Esc cancels"},
```

- [ ] **Step 2: Verify `--help` and the in-app help screen pick it up**

Run: `go test ./internal/tui/... -run TestView_HelpModeShowsKeybindingsAndHidesColumns -v`
Expected: PASS (existing test already asserts every `Keybindings` row appears — no test changes needed, this just confirms the new row didn't break the existing assertion).

Run: `go run ./cmd/thicket --help`
Expected: output includes a line starting with `f` and the new description, formatted with the existing `KeyColWidth`-aligned table.

- [ ] **Step 3: Update `README.md`**

In `README.md`'s keybinding table, insert a new row after the `/` row (currently line 46):

```markdown
| `f` | Recursive find: type to filter files/directories under the active directory; `Enter` jumps to the match's parent directory, `Esc` cancels |
```

- [ ] **Step 4: Update `man/thicket.1`**

In `man/thicket.1`'s `.SH KEYS` section, insert a new `.TP`/`.B` block after the `/` block (currently ending at line 76, right before the `?` block):

```troff
.TP
.B f
Recursive find: type to filter files and directories under the active
directory (files and directories are both matched). \c
.B Enter
jumps to the match's parent directory with the cursor on the match; \c
.B Esc
cancels with no change.
```

- [ ] **Step 5: Update `AGENTS.md`'s non-goals amendment paragraph**

Replace the current paragraph (`AGENTS.md` lines 10-18):

```markdown
v1 scope is intentionally locked to navigation only — no file
create/rename/delete/copy/move, no config file, no mouse support, no
filesystem watching (see `docs/superpowers/specs/2026-08-15-thicket-tui-file-browser-design.md`
§3). Three amendments to that non-goal list have shipped since: a
`/`-triggered type-ahead cursor search within the active column
(`docs/superpowers/specs/2026-08-16-thicket-type-ahead-search-design.md`),
vim/ranger-style directory marks/bookmarks
(`docs/superpowers/specs/2026-08-16-thicket-directory-marks-design.md`),
and an `f`-triggered recursive find over the active directory's subtree
(`docs/superpowers/specs/2026-08-16-thicket-recursive-find-design.md`).
Every other v1 non-goal in §3 still holds.
```

- [ ] **Step 6: Run the full test suite one more time**

Run: `go build ./... && go test ./... && go vet ./...`
Expected: PASS across every package, 0 failures — final regression check before the manual smoke test.

- [ ] **Step 7: Manual smoke test**

Launch `go run ./cmd/thicket` in a directory with several nested subdirectories and similarly-named files/directories. Press `f`; confirm the walk completes and shows results. Type a substring; confirm live filtering. Press `Up`/`Down`; confirm the selection moves among multiple matches. Press `Enter`; confirm the Miller-column view relocates to the match's parent directory with the cursor on the match. Press `Enter` again; confirm it `cd`s (prints the path and exits 0 when run through the `th` wrapper, or check the printed stdout line directly). Re-launch, press `f`, then `Esc` without typing; confirm `activePath` is unchanged (compare the header line before/after).

- [ ] **Step 8: Commit**

```bash
git add internal/tui/help.go README.md man/thicket.1 AGENTS.md
git commit -m "docs: document recursive find (f) across help, README, man page, AGENTS.md"
```

---

## Plan Self-Review Notes

- **Spec coverage:** §3 (`WalkSubtree`) → Task 1. §4 (Model fields) → Task 3. §5 (key handling: `f`, typing/filter, Up/Down, Enter, Esc, catch-all) → Tasks 3-5. §6 (`filterWalk`) → Task 2. §7 (rendering, status line) → Task 6. §8 (edge cases: unreadable root, empty directory, permission errors, symlinks, immediate Enter/Esc, clamping, fresh state per `f` press) → covered by tests across Tasks 1, 3, 4, 5. §9 (test names) → every named test from the spec appears above, some split further for TDD granularity (e.g. `TestUpdate_FindNoMatchSetsCursorToNegativeOne` factors out of the spec's combined typing test). §10 (docs) → Task 7.
- **Type consistency:** `WalkEntry` (Task 1) is the type every later task threads through unchanged — `filterWalk([]fsutil.WalkEntry, string) []fsutil.WalkEntry` (Task 2), `Model.findResults []fsutil.WalkEntry` (Task 3), `commitFindSelection`'s `we := filtered[m.findCursor]` (Task 5), `renderFind`'s `for i, we := range filtered` (Task 6) — same field names (`RelPath`, embedded `Entry.Name`/`IsDir`/`IsSymlink`) used consistently throughout.
- **No placeholders left unresolved:** Tasks 3 and 4 each introduce one deliberate placeholder (`handleFindKey` Esc-only in Task 3, `commitFindSelection` no-op in Task 4) — both are explicitly completed by name in a later task's Step 3, not left dangling at plan end.
