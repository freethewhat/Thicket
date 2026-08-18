# Per-Directory Cursor Memory Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remember the cursor row last visited in each directory during the current session, so `Right` into a directory previously left restores that row instead of always resetting to `0`.

**Architecture:** Add a `cursorMemory map[string]int` field to `tui.Model`, keyed by absolute directory path. `handleRight` and `handleLeft` (`internal/tui/update.go`) both write `cursorMemory[<directory being left>] = activeCursor` immediately before changing `activePath`. `handleRight` additionally reads `cursorMemory[<directory being entered>]` and uses it (clamped to the new listing's bounds) instead of defaulting to `0`. `handleLeft` keeps its existing child-name-lookup cursor placement unchanged — it only writes to the map, never reads from it, since child-lookup already reconstructs the correct value for the direct-backtrack case. In-memory only, per-process; never persisted to disk.

**Tech Stack:** Go 1.24.6, `github.com/charmbracelet/bubbletea` v1.x, Go standard library `testing` (no mocking/assertion libraries).

**Spec:** `docs/superpowers/specs/2026-08-15-thicket-tui-file-browser-design.md` §5 (Navigation state model — the closing paragraph explicitly anticipates this exact feature as "a contained, addressable follow-up ... not a re-architecture"); requirements and acceptance criteria from GitHub issue #8 (`https://github.com/freethewhat/Thicket/issues/8`).

## Global Constraints

- No new goroutines, `tea.Cmd`, or channels — pure in-`Update` state mutation, same as every existing transition (`AGENTS.md` Concurrency bullet).
- In-memory only; must not persist across process exits (no config/state file — v1 posture, spec §3).
- Must not change behavior for a directory visited for the first time in the session, nor change `handleLeft`'s existing child-name-lookup cursor placement.
- New tests go in the existing `internal/tui/update_test.go`, following the `TestXxx_Behavior` naming convention already used throughout that file — no new test file.
- Test fixtures use `t.TempDir()` plus the existing `mustMkdir`/`mustWriteFile`/`setupFixture`/`newTestModel` helpers already defined in `internal/tui/update_test.go`.
- A remembered cursor that no longer fits the (possibly shrunk) directory must clamp to the last valid row (`len(entries)-1`), not panic and not silently reset to `0` — this is a different clamp convention from `reload()`'s existing reset-to-`0`-on-mismatch behavior, and must stay distinct (see Task 2).

---

### Task 1: Remember cursor on Left/Right and restore it on Right (happy path)

**Files:**
- Modify: `internal/tui/model.go:11-13` (Model doc comment), `internal/tui/model.go:17-18` (field block), `internal/tui/model.go:99` (`New()` construction)
- Modify: `internal/tui/update.go:162-184` (`handleRight`), `internal/tui/update.go:186-227` (`handleLeft`)
- Test: `internal/tui/update_test.go` (append near the existing `TestUpdate_LeftReturnsCursorToChildJustLeft`, currently ending at line 117)

**Interfaces:**
- Produces: `Model.cursorMemory map[string]int` field, populated as a side effect of `handleRight`/`handleLeft` — no new exported API, no new method signatures other than the two existing pointer-receiver methods gaining a few lines each.

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/update_test.go` (after `TestUpdate_LeftReturnsCursorToChildJustLeft`, i.e. after line 117):

```go
func TestUpdate_LeftThenRightRestoresPriorCursorPosition(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	sub := filepath.Join(root, "sub")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight}) // root -> sub, cursor 0 (grand)
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // sub cursor 0 -> 1 (leaf.txt)
	m = updated.(Model)
	if m.activeEntries[m.activeCursor].Name != "leaf.txt" {
		t.Fatalf("precondition: cursor not on leaf.txt, entries=%+v cursor=%d", m.activeEntries, m.activeCursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft}) // sub -> root
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight}) // root -> sub again
	m = updated.(Model)

	if m.activePath != sub {
		t.Fatalf("activePath = %q, want %q", m.activePath, sub)
	}
	if m.activeCursor < 0 || m.activeEntries[m.activeCursor].Name != "leaf.txt" {
		t.Fatalf("cursor not restored to leaf.txt: cursor=%d entries=%+v", m.activeCursor, m.activeEntries)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/... -run TestUpdate_LeftThenRightRestoresPriorCursorPosition -v`
Expected: FAIL — cursor lands back on `grand` (index 0, today's hardcoded default) instead of `leaf.txt`.

- [ ] **Step 3: Add the `cursorMemory` field**

In `internal/tui/model.go`, the field block currently reads (lines 14-19):

```go
type Model struct {
	activePath    string
	activeEntries []fsutil.Entry
	activeCursor  int // -1 when the active directory is empty
	activeScroll  int
	showHidden    bool
```

Insert a new field and doc comment directly after `activeScroll` (before `showHidden`):

```go
type Model struct {
	activePath    string
	activeEntries []fsutil.Entry
	activeCursor  int // -1 when the active directory is empty
	activeScroll  int
	// cursorMemory remembers the last activeCursor seen in each directory
	// (keyed by absolute path), for the current process only — never
	// persisted to disk. handleRight/handleLeft (internal/tui/update.go)
	// write to it immediately before they change activePath, so it always
	// reflects wherever the cursor was left in the directory being
	// departed. handleRight consults it when re-entering a directory, to
	// restore that position instead of resetting to row 0; handleLeft
	// only writes — its own cursor placement is fully determined by
	// child-name lookup and does not need to read this map. This is the
	// "contained, addressable follow-up" spec
	// docs/superpowers/specs/2026-08-15-thicket-tui-file-browser-design.md
	// §5 anticipated: cursorMemory is itself derived (a cache of past
	// activeCursor values), not authoritative state a transition depends
	// on to be correct, so it does not change the "derived from
	// activePath plus two integers" model below.
	cursorMemory map[string]int
	showHidden   bool
```

Update the `Model` doc comment at the top of the file (lines 11-13), currently:

```go
// Model is the Bubble Tea model for the Miller-column browser. Navigation
// state is derived from activePath plus two integers (activeCursor,
// activeScroll) rather than a cached tree of panes — see spec §5.
```

to:

```go
// Model is the Bubble Tea model for the Miller-column browser. Navigation
// state is derived from activePath plus two integers (activeCursor,
// activeScroll) rather than a cached tree of panes — see spec §5.
// cursorMemory is a session-scoped cache of past activeCursor values per
// directory (spec §5's anticipated follow-up), not additional
// authoritative state; see its doc comment below.
```

In `New()` (`internal/tui/model.go:99`), currently:

```go
	m := Model{activePath: abs}
```

change to:

```go
	m := Model{activePath: abs, cursorMemory: make(map[string]int)}
```

- [ ] **Step 4: Write to `cursorMemory` in `handleRight` and `handleLeft`, and do a naive (unclamped) restore in `handleRight`**

In `internal/tui/update.go`, `handleRight` currently reads (lines 162-184):

```go
func (m *Model) handleRight() {
	if m.activeCursor < 0 || m.activeCursor >= len(m.activeEntries) {
		return
	}
	entry := m.activeEntries[m.activeCursor]
	if !entry.IsDir {
		return
	}
	newPath := filepath.Join(m.activePath, entry.Name)
	entries, err := fsutil.ListDir(newPath, m.showHidden)
	if err != nil {
		m.statusErr = err.Error()
		return
	}
	m.activePath = newPath
	m.activeEntries = entries
	m.activeCursor = 0
	if len(entries) == 0 {
		m.activeCursor = -1
	}
	m.activeScroll = 0
	m.statusErr = ""
}
```

Replace with:

```go
func (m *Model) handleRight() {
	if m.activeCursor < 0 || m.activeCursor >= len(m.activeEntries) {
		return
	}
	entry := m.activeEntries[m.activeCursor]
	if !entry.IsDir {
		return
	}
	newPath := filepath.Join(m.activePath, entry.Name)
	entries, err := fsutil.ListDir(newPath, m.showHidden)
	if err != nil {
		m.statusErr = err.Error()
		return
	}
	m.cursorMemory[m.activePath] = m.activeCursor
	remembered, ok := m.cursorMemory[newPath]
	m.activePath = newPath
	m.activeEntries = entries
	if ok {
		m.activeCursor = remembered
	} else {
		m.activeCursor = 0
	}
	if len(entries) == 0 {
		m.activeCursor = -1
	}
	m.activeScroll = 0
	m.statusErr = ""
}
```

(This is deliberately not yet safe against a shrunk directory — `remembered` can be `>= len(entries)`. Task 2 fixes that; this step's only job is to make Step 1's test pass.)

`handleLeft` currently reads (lines 186-227):

```go
func (m *Model) handleLeft() {
	if m.activePath == "/" {
		return
	}
	child := filepath.Base(m.activePath)
	parent := filepath.Dir(m.activePath)
	entries, err := fsutil.ListDir(parent, m.showHidden)
	if err != nil {
		m.statusErr = err.Error()
		return
	}

	// The child-index lookup must not use the showHidden-filtered listing
	// on its own: if activePath is itself a dotdirectory and hidden files
	// are off, child is filtered out of entries entirely and the lookup
	// would always miss. Resolve existence against an unfiltered listing
	// of the parent while still displaying/storing the filtered one.
	known := entries
	if !m.showHidden {
		if all, aerr := fsutil.ListDir(parent, true); aerr == nil {
			known = all
		}
	}

	m.activePath = parent
	m.activeEntries = entries
	switch {
	case len(entries) == 0:
		m.activeCursor = -1
	case fsutil.IndexOfName(known, child) < 0:
		// Absent even from the unfiltered listing: deleted externally.
		m.activeCursor = 0
	default:
		// Present in the parent, but only highlightable when it also
		// appears in the filtered display; a hidden dotdirectory just
		// left with showHidden off has no visible row to highlight.
		m.activeCursor = fsutil.IndexOfName(entries, child)
	}
	m.activeScroll = 0
	m.clampScroll()
	m.statusErr = ""
}
```

Add exactly one line — the memory write — right before `m.activePath = parent`:

```go
	m.cursorMemory[m.activePath] = m.activeCursor
	m.activePath = parent
	m.activeEntries = entries
```

Nothing else in `handleLeft` changes: its `switch` below still runs exactly as before.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/tui/... -run TestUpdate_LeftThenRightRestoresPriorCursorPosition -v`
Expected: PASS

- [ ] **Step 6: Run the full `internal/tui` suite to check for regressions**

Run: `go test ./internal/tui/...`
Expected: PASS (in particular `TestUpdate_RightEntersDirectory`, `TestUpdate_RightNoOpOnFile`, `TestUpdate_LeftAtRootIsNoOp`, and `TestUpdate_LeftReturnsCursorToChildJustLeft` must still pass unchanged — they exercise first-visit-defaults-to-0 and the child-lookup path this task does not touch).

- [ ] **Step 7: Commit**

```bash
git add internal/tui/model.go internal/tui/update.go internal/tui/update_test.go
git commit -m "feat: remember and restore cursor position across Left/Right"
```

---

### Task 2: Clamp restored cursor when the directory has shrunk, and sync docs

**Files:**
- Modify: `internal/tui/update.go` (the `handleRight` block Task 1 just added)
- Modify: `AGENTS.md:50-54`, `AGENTS.md:154-159`
- Test: `internal/tui/update_test.go` (append after the test added in Task 1)

**Interfaces:**
- Consumes: `Model.cursorMemory` from Task 1.
- Produces: no new public interface — this task only tightens `handleRight`'s existing restore logic and updates prose docs.

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/update_test.go`, after `TestUpdate_LeftThenRightRestoresPriorCursorPosition`:

```go
func TestUpdate_CursorMemoryClampsWhenEntryCountShrinks(t *testing.T) {
	root := t.TempDir()
	multi := filepath.Join(root, "multi")
	mustMkdir(t, multi)
	mustWriteFile(t, filepath.Join(multi, "a.txt"), "a")
	mustWriteFile(t, filepath.Join(multi, "b.txt"), "b")
	mustWriteFile(t, filepath.Join(multi, "c.txt"), "c")
	mustWriteFile(t, filepath.Join(multi, "d.txt"), "d")
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight}) // root -> multi, cursor 0 (a.txt)
	m = updated.(Model)
	for range 3 {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // -> d.txt (index 3)
		m = updated.(Model)
	}
	if m.activeEntries[m.activeCursor].Name != "d.txt" {
		t.Fatalf("precondition: cursor not on d.txt, entries=%+v cursor=%d", m.activeEntries, m.activeCursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft}) // multi -> root; remembers cursor 3 for multi
	m = updated.(Model)

	if err := os.Remove(filepath.Join(multi, "d.txt")); err != nil {
		t.Fatalf("remove d.txt: %v", err)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight}) // root -> multi again; multi now has 3 entries
	m = updated.(Model)

	if m.activePath != multi {
		t.Fatalf("activePath = %q, want %q", m.activePath, multi)
	}
	if len(m.activeEntries) != 3 {
		t.Fatalf("activeEntries len = %d, want 3", len(m.activeEntries))
	}
	if m.activeCursor != 2 {
		t.Fatalf("activeCursor = %d, want 2 (clamped to last row)", m.activeCursor)
	}
	if m.activeEntries[m.activeCursor].Name != "c.txt" {
		t.Fatalf("cursor not on last row c.txt: entries=%+v cursor=%d", m.activeEntries, m.activeCursor)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/... -run TestUpdate_CursorMemoryClampsWhenEntryCountShrinks -v`
Expected: FAIL — with Task 1's naive restore, `remembered` is `3` but `len(entries)` is now `3` (valid indices `0-2`), so `m.activeCursor` ends up `3`, and the test's own `m.activeEntries[m.activeCursor]` index either panics (`index out of range`) or the preceding assertion on `activeCursor != 2` fails first — either way, FAIL.

- [ ] **Step 3: Implement the clamp**

In `internal/tui/update.go`, `handleRight` currently has (added by Task 1):

```go
	m.cursorMemory[m.activePath] = m.activeCursor
	remembered, ok := m.cursorMemory[newPath]
	m.activePath = newPath
	m.activeEntries = entries
	if ok {
		m.activeCursor = remembered
	} else {
		m.activeCursor = 0
	}
	if len(entries) == 0 {
		m.activeCursor = -1
	}
	m.activeScroll = 0
	m.statusErr = ""
```

Replace the restore block (everything from `remembered, ok :=` through the `if len(entries) == 0` block) with a clamped version:

```go
	m.cursorMemory[m.activePath] = m.activeCursor
	remembered, ok := m.cursorMemory[newPath]
	if !ok {
		remembered = 0
	}
	m.activePath = newPath
	m.activeEntries = entries
	switch {
	case len(entries) == 0:
		m.activeCursor = -1
	case remembered < 0:
		m.activeCursor = 0
	case remembered >= len(entries):
		// Directory shrank since cursorMemory[newPath] was recorded
		// (e.g. an entry was deleted externally) — land on the last
		// row rather than resetting to 0 or indexing out of range.
		m.activeCursor = len(entries) - 1
	default:
		m.activeCursor = remembered
	}
	m.activeScroll = 0
	m.statusErr = ""
```

The full `handleRight` after this change:

```go
func (m *Model) handleRight() {
	if m.activeCursor < 0 || m.activeCursor >= len(m.activeEntries) {
		return
	}
	entry := m.activeEntries[m.activeCursor]
	if !entry.IsDir {
		return
	}
	newPath := filepath.Join(m.activePath, entry.Name)
	entries, err := fsutil.ListDir(newPath, m.showHidden)
	if err != nil {
		m.statusErr = err.Error()
		return
	}
	m.cursorMemory[m.activePath] = m.activeCursor
	remembered, ok := m.cursorMemory[newPath]
	if !ok {
		remembered = 0
	}
	m.activePath = newPath
	m.activeEntries = entries
	switch {
	case len(entries) == 0:
		m.activeCursor = -1
	case remembered < 0:
		m.activeCursor = 0
	case remembered >= len(entries):
		// Directory shrank since cursorMemory[newPath] was recorded
		// (e.g. an entry was deleted externally) — land on the last
		// row rather than resetting to 0 or indexing out of range.
		m.activeCursor = len(entries) - 1
	default:
		m.activeCursor = remembered
	}
	m.activeScroll = 0
	m.statusErr = ""
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/tui/... -run TestUpdate_CursorMemoryClampsWhenEntryCountShrinks -v`
Expected: PASS

- [ ] **Step 5: Run the full `internal/tui` suite**

Run: `go test ./internal/tui/...`
Expected: PASS, including both new tests and every pre-existing test (in particular the four named in Task 1 Step 6, plus `TestUpdate_RightIntoPermissionDeniedSetsStatusErrAndKeepsPath`, which must still leave `cursorMemory` and `activePath` untouched on a failed `ListDir`).

- [ ] **Step 6: Run `go vet`**

Run: `go vet ./...`
Expected: no issues.

- [ ] **Step 7: Sync `AGENTS.md`**

`AGENTS.md:50-54` currently reads:

```markdown
3. **`internal/tui`** — Bubble Tea MVU (Model-Update-View / Elm-architecture).
   `Model` (`internal/tui/model.go`) holds *only* `activePath` plus two
   integers (`activeCursor`, `activeScroll`) — ancestor columns and the
   preview pane are **derived** on every render by walking
   `filepath.Dir(activePath)` and calling `fsutil.ListDir`/`ReadFilePreview`
```

Change the second line to:

```markdown
3. **`internal/tui`** — Bubble Tea MVU (Model-Update-View / Elm-architecture).
   `Model` (`internal/tui/model.go`) holds `activePath` plus two integers
   (`activeCursor`, `activeScroll`) and a session-scoped `cursorMemory
   map[string]int` cache of past per-directory cursor positions (populated
   by `handleRight`/`handleLeft`, consulted by `handleRight` — spec §5's
   anticipated follow-up, not additional authoritative state) — ancestor
   columns and the preview pane are **derived** on every render by walking
   `filepath.Dir(activePath)` and calling `fsutil.ListDir`/`ReadFilePreview`
```

`AGENTS.md:154-159` currently reads:

```markdown
- **State management**: single source of truth per `Model` —
  `activePath` + `activeCursor`/`activeScroll` integers. Everything else
  (ancestor columns, preview pane contents) is *recomputed from disk on
  every `View()` call* rather than cached — see `Model` doc comment in
  `internal/tui/model.go`. Do not introduce a pane cache without updating
  that documented invariant.
```

Change to:

```markdown
- **State management**: single source of truth per `Model` —
  `activePath` + `activeCursor`/`activeScroll` integers, plus the
  `cursorMemory` cache (a `map[string]int` of past `activeCursor` values
  per directory, written by `handleRight`/`handleLeft`, read only by
  `handleRight`) described in the `Model` doc comment in
  `internal/tui/model.go`. Everything else (ancestor columns, preview pane
  contents) is *recomputed from disk on every `View()` call* rather than
  cached. Do not introduce a pane cache without updating that documented
  invariant.
```

- [ ] **Step 8: Commit**

```bash
git add internal/tui/update.go internal/tui/update_test.go AGENTS.md
git commit -m "fix: clamp restored cursor when directory has shrunk; sync docs"
```

---

## Self-Review Notes

- **Spec/issue coverage:** Design-spec §5 follow-up → Task 1 (map field) + Task 2 (clamp). Issue's four acceptance criteria: (1) same-entry-count restore → `TestUpdate_LeftThenRightRestoresPriorCursorPosition` (Task 1); (2) shrink clamps → `TestUpdate_CursorMemoryClampsWhenEntryCountShrinks` (Task 2); (3) first-time visit unchanged → covered by pre-existing `TestUpdate_RightEntersDirectory` (re-run, not re-written, in Task 1 Step 6 / Task 2 Step 5); (4) existing Left-lands-on-child test unchanged → same re-run coverage, and `handleLeft`'s cursor-selection code path is untouched by both tasks (only one line added, before its existing logic). Issue's two explicitly-named test functions are both present verbatim. No goroutines/`tea.Cmd`/persistence introduced (Global Constraints honored — nothing in either task touches `Init`/`tea.Cmd`/disk I/O).
- **Placeholder scan:** no TBD/TODO/"handle appropriately" — every step shows exact before/after code.
- **Type consistency:** `cursorMemory map[string]int` name and shape used identically in Task 1 (declaration, `New()` init, `handleRight`/`handleLeft` writes) and Task 2 (read/clamp) — no renaming drift.
