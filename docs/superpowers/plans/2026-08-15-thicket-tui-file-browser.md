# thicket TUI File Browser Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `thicket`, a Linux CLI tool that replaces manual `cd`/`ls` navigation with a Miller-column TUI: browse with arrow/vi keys, and on Enter the calling shell actually `cd`s into the chosen directory.

**Architecture:** A Go module with three internal layers — `internal/fsutil` (pure filesystem reading: listing, sorting, hidden-file filtering, symlink classification, file-content preview with binary detection), `internal/tui` (a Bubble Tea `Model`/`Update`/`View` implementing the Miller-column navigation state machine described below), and `cmd/thicket` (wires the model to a real terminal via `/dev/tty` so the program's actual `stdout` stays free to emit the chosen path for the shell wrapper to `cd` into).

**Tech Stack:** Go (module `thicket`), `github.com/charmbracelet/bubbletea` v1.x (pinned — v2 is still release-candidate), `github.com/charmbracelet/lipgloss` for styling.

**Spec:** `docs/superpowers/specs/2026-08-15-thicket-tui-file-browser-design.md`

## Global Constraints

- Go module path: `thicket` (bare local path; not yet published).
- Binary name: `thicket`. Suggested shell wrapper function name: `th`.
- Bubble Tea must be pinned to the v1.x line (`github.com/charmbracelet/bubbletea`, latest stable is v1.3.10) — v2 (`charm.land/bubbletea/v2`) is release-candidate only, do not use it.
- No config file, no file operations (create/rename/delete/copy/move), no search/filter, no mouse support, no filesystem watching — navigation only (spec §3).
- Linux is the only supported/tested target.
- Sort order for every directory listing: directories before files, then case-insensitive lexicographic by name (spec §6).
- Hidden files (`.`-prefixed) excluded by default; toggled with `.` key (spec §5, §8).
- No `..` pseudo-row in any listing — ascending is exclusively the Left key (spec §5).

---

### Task 1: Project scaffold + directory listing (`internal/fsutil`)

**Files:**
- Create: `go.mod`
- Create: `internal/fsutil/entry.go`
- Create: `internal/fsutil/listing.go`
- Create: `internal/fsutil/listing_test.go`
- Create: `internal/fsutil/helpers_test.go`

**Interfaces:**
- Produces: `fsutil.Entry{Name string, IsDir bool, IsSymlink bool, Broken bool, Size int64, ModTime time.Time}`, `fsutil.ListDir(dir string, showHidden bool) ([]Entry, error)`, `fsutil.IndexOfName(entries []Entry, name string) int`. Later tasks (3, 4) consume all three.

- [ ] **Step 1: Initialize the module**

Run: `go mod init thicket` at the repo root, then `go get github.com/charmbracelet/bubbletea@v1.3.10` and `go get github.com/charmbracelet/lipgloss@latest`.

- [ ] **Step 2: Write `entry.go`**

```go
package fsutil

import "time"

// Entry describes one file-tree entry as shown in a browser pane.
type Entry struct {
	Name      string
	IsDir     bool // resolved: follows symlinks
	IsSymlink bool
	Broken    bool // symlink whose target could not be stat'd
	Size      int64
	ModTime   time.Time
}
```

- [ ] **Step 3: Write the failing tests for `ListDir`/`IndexOfName`**

```go
// internal/fsutil/helpers_test.go
package fsutil_test

import (
	"os"
	"testing"
)

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
```

```go
// internal/fsutil/listing_test.go
package fsutil_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"thicket/internal/fsutil"
)

func TestListDir_SortsDirsFirstAndFiltersHidden(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "zdir"))
	mustMkdir(t, filepath.Join(dir, "adir"))
	mustWriteFile(t, filepath.Join(dir, "bfile.txt"), "hello")
	mustWriteFile(t, filepath.Join(dir, ".hidden"), "secret")

	entries, err := fsutil.ListDir(dir, false)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	want := []string{"adir", "zdir", "bfile.txt"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("got %v, want %v", names, want)
	}
}

func TestListDir_ShowHiddenIncludesDotfiles(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".hidden"), "x")

	entries, err := fsutil.ListDir(dir, true)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != ".hidden" {
		t.Fatalf("got %+v", entries)
	}
}

func TestListDir_ClassifiesSymlinks(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "target"))
	if err := os.Symlink(filepath.Join(dir, "target"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "broken")); err != nil {
		t.Fatal(err)
	}

	entries, err := fsutil.ListDir(dir, false)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	byName := map[string]fsutil.Entry{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	link := byName["link"]
	if !link.IsSymlink || !link.IsDir || link.Broken {
		t.Fatalf("link entry wrong: %+v", link)
	}
	broken := byName["broken"]
	if !broken.IsSymlink || !broken.Broken {
		t.Fatalf("broken entry wrong: %+v", broken)
	}
}

func TestListDir_PermissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits")
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "locked")
	mustMkdir(t, sub)
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(sub, 0o755)

	if _, err := fsutil.ListDir(sub, false); err == nil {
		t.Fatal("expected error for unreadable dir")
	}
}

func TestIndexOfName(t *testing.T) {
	entries := []fsutil.Entry{{Name: "a"}, {Name: "b"}}
	if got := fsutil.IndexOfName(entries, "b"); got != 1 {
		t.Fatalf("got %d, want 1", got)
	}
	if got := fsutil.IndexOfName(entries, "z"); got != -1 {
		t.Fatalf("got %d, want -1", got)
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./internal/fsutil/...`
Expected: build failure (`ListDir`, `IndexOfName` undefined) — confirms the tests actually exercise not-yet-written code.

- [ ] **Step 5: Write `listing.go`**

```go
package fsutil

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListDir reads dir, classifies each entry, filters dotfiles unless
// showHidden is set, and sorts directories before files (case-insensitive
// alphabetical within each group).
func ListDir(dir string, showHidden bool) ([]Entry, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(dirEntries))
	for _, de := range dirEntries {
		name := de.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		entries = append(entries, classify(dir, name))
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, nil
}

func classify(dir, name string) Entry {
	full := filepath.Join(dir, name)
	lst, err := os.Lstat(full)
	if err != nil {
		return Entry{Name: name, Broken: true}
	}
	if lst.Mode()&os.ModeSymlink == 0 {
		return Entry{Name: name, IsDir: lst.IsDir(), Size: lst.Size(), ModTime: lst.ModTime()}
	}
	st, err := os.Stat(full) // follows the symlink
	if err != nil {
		return Entry{Name: name, IsSymlink: true, Broken: true}
	}
	return Entry{Name: name, IsDir: st.IsDir(), IsSymlink: true, Size: st.Size(), ModTime: st.ModTime()}
}

// IndexOfName returns the index of the entry named name, or -1 if absent.
func IndexOfName(entries []Entry, name string) int {
	for i, e := range entries {
		if e.Name == name {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/fsutil/...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/fsutil
git commit -m "feat: directory listing with sorting, hidden-file filter, symlink classification"
```

---

### Task 2: File-content preview (`internal/fsutil/preview.go`)

**Files:**
- Create: `internal/fsutil/preview.go`
- Create: `internal/fsutil/preview_test.go`

**Interfaces:**
- Consumes: nothing from Task 1 directly (standalone file I/O).
- Produces: `fsutil.FilePreview{Binary bool, Lines []string, Size int64}`, `fsutil.ReadFilePreview(path string) (FilePreview, error)`. Consumed by Task 4 (render.go) for the file-preview pane.

- [ ] **Step 1: Write the failing tests**

```go
// internal/fsutil/preview_test.go
package fsutil_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"thicket/internal/fsutil"
)

func TestReadFilePreview_TextFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	content := "line1\nline2\nline3"
	mustWriteFile(t, path, content)

	p, err := fsutil.ReadFilePreview(path)
	if err != nil {
		t.Fatalf("ReadFilePreview: %v", err)
	}
	if p.Binary {
		t.Fatal("expected non-binary")
	}
	want := []string{"line1", "line2", "line3"}
	if !reflect.DeepEqual(p.Lines, want) {
		t.Fatalf("got %v, want %v", p.Lines, want)
	}
	if p.Size != int64(len(content)) {
		t.Fatalf("size = %d, want %d", p.Size, len(content))
	}
}

func TestReadFilePreview_BinaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.bin")
	data := append([]byte("PNG"), 0x00, 0x01, 0x02)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := fsutil.ReadFilePreview(path)
	if err != nil {
		t.Fatalf("ReadFilePreview: %v", err)
	}
	if !p.Binary {
		t.Fatal("expected binary detection")
	}
	if p.Size != int64(len(data)) {
		t.Fatalf("size = %d, want %d", p.Size, len(data))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/fsutil/...`
Expected: build failure (`FilePreview`, `ReadFilePreview` undefined).

- [ ] **Step 3: Write `preview.go`**

```go
package fsutil

import (
	"bytes"
	"io"
	"os"
	"strings"
)

const (
	previewReadLimit = 64 * 1024
	binarySniffLen    = 8000
)

// FilePreview is the content shown in the preview pane for a regular file.
type FilePreview struct {
	Binary bool
	Lines  []string
	Size   int64
}

// ReadFilePreview reads up to previewReadLimit bytes from path and detects
// binary content via a NUL byte in the first binarySniffLen bytes.
func ReadFilePreview(path string) (FilePreview, error) {
	info, err := os.Stat(path)
	if err != nil {
		return FilePreview{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return FilePreview{}, err
	}
	defer f.Close()

	buf := make([]byte, previewReadLimit)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return FilePreview{}, err
	}
	buf = buf[:n]

	sniff := buf
	if len(sniff) > binarySniffLen {
		sniff = sniff[:binarySniffLen]
	}
	if bytes.IndexByte(sniff, 0) != -1 {
		return FilePreview{Binary: true, Size: info.Size()}, nil
	}

	return FilePreview{Lines: strings.Split(string(buf), "\n"), Size: info.Size()}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/fsutil/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/fsutil/preview.go internal/fsutil/preview_test.go
git commit -m "feat: file-content preview with binary detection"
```

---

### Task 3: Navigation state machine (`internal/tui`)

**Files:**
- Create: `internal/tui/model.go`
- Create: `internal/tui/update.go`
- Create: `internal/tui/update_test.go`

**Interfaces:**
- Consumes: `fsutil.Entry`, `fsutil.ListDir`, `fsutil.IndexOfName` (Task 1).
- Produces: `tui.Model` (implements `tea.Model`: `Init() tea.Cmd`, `Update(tea.Msg) (tea.Model, tea.Cmd)`, `View() string` — `View` stubbed to return `""` in this task, implemented in Task 4), `tui.New(startPath string) (Model, error)`, `func (m Model) Result() (path string, ok bool)`. Consumed by Task 4 (adds `View`) and Task 5 (`cmd/thicket`).

- [ ] **Step 1: Write `model.go`**

```go
package tui

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"thicket/internal/fsutil"
)

// Model is the Bubble Tea model for the Miller-column browser. Navigation
// state is derived from activePath plus two integers (activeCursor,
// activeScroll) rather than a cached tree of panes — see spec §5.
type Model struct {
	activePath    string
	activeEntries []fsutil.Entry
	activeCursor  int // -1 when the active directory is empty
	activeScroll  int
	showHidden    bool
	statusErr     string
	width         int
	height        int
	quitting      bool
	selected      bool
	chosenPath    string
}

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

func (m Model) Init() tea.Cmd {
	return nil
}

// Result reports the outcome after the program quits: ok is true only if
// the user pressed Enter (as opposed to q/Esc/Ctrl-C).
func (m Model) Result() (path string, ok bool) {
	return m.chosenPath, m.selected
}
```

- [ ] **Step 2: Write the failing tests for `Update`**

```go
// internal/tui/update_test.go
package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// setupFixture builds:
//   root/
//     sub/
//       grand/
//       leaf.txt
//     file.txt
//     .hidden
func setupFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "sub"))
	mustMkdir(t, filepath.Join(root, "sub", "grand"))
	mustWriteFile(t, filepath.Join(root, "sub", "leaf.txt"), "hi")
	mustWriteFile(t, filepath.Join(root, "file.txt"), "hi")
	mustWriteFile(t, filepath.Join(root, ".hidden"), "hi")
	return root
}

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

func TestUpdate_RightEntersDirectory(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)

	if want := filepath.Join(root, "sub"); m.activePath != want {
		t.Fatalf("activePath = %q, want %q", m.activePath, want)
	}
	if m.activeCursor != 0 {
		t.Fatalf("activeCursor = %d, want 0", m.activeCursor)
	}
}

func TestUpdate_RightNoOpOnFile(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown}) // sub -> file.txt
	m = updated.(Model)
	before := m.activePath

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)

	if m.activePath != before {
		t.Fatalf("activePath changed on file selection: %q -> %q", before, m.activePath)
	}
}

func TestUpdate_LeftAtRootIsNoOp(t *testing.T) {
	m := newTestModel(t, "/")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(Model)

	if m.activePath != "/" {
		t.Fatalf("activePath = %q, want /", m.activePath)
	}
}

func TestUpdate_LeftReturnsCursorToChildJustLeft(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight}) // into "sub"
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft}) // back to root
	m = updated.(Model)

	if m.activePath != root {
		t.Fatalf("activePath = %q, want %q", m.activePath, root)
	}
	if m.activeCursor < 0 || m.activeEntries[m.activeCursor].Name != "sub" {
		t.Fatalf("cursor not restored to 'sub': cursor=%d entries=%+v", m.activeCursor, m.activeEntries)
	}
}

func TestUpdate_UpDownClamping(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.activeCursor != 0 {
		t.Fatalf("cursor = %d, want 0 (clamped at top)", m.activeCursor)
	}

	last := len(m.activeEntries) - 1
	for i := 0; i < len(m.activeEntries)+2; i++ {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(Model)
	}
	if m.activeCursor != last {
		t.Fatalf("cursor = %d, want %d (clamped at end)", m.activeCursor, last)
	}
}

func TestUpdate_UpDownOnEmptyDirectoryIsNoOp(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "empty"))
	m := newTestModel(t, filepath.Join(root, "empty"))

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)

	if m.activeCursor != -1 {
		t.Fatalf("activeCursor = %d, want -1 for empty dir", m.activeCursor)
	}
}

func TestUpdate_EnterOnDirectoryChoosesChildPath(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	path, ok := m.Result()
	if !ok {
		t.Fatal("expected selected == true")
	}
	if want := filepath.Join(root, "sub"); path != want {
		t.Fatalf("chosenPath = %q, want %q", path, want)
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestUpdate_EnterOnFileChoosesActiveDirectory(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown}) // move onto file.txt
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	path, ok := m.Result()
	if !ok || path != root {
		t.Fatalf("Result() = (%q, %v), want (%q, true)", path, ok, root)
	}
}

func TestUpdate_EnterOnEmptyDirectoryChoosesActiveDirectory(t *testing.T) {
	root := t.TempDir()
	empty := filepath.Join(root, "empty")
	mustMkdir(t, empty)
	m := newTestModel(t, empty)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	path, ok := m.Result()
	if !ok || path != empty {
		t.Fatalf("Result() = (%q, %v), want (%q, true)", path, ok, empty)
	}
}

func TestUpdate_QuitKeysDoNotSelect(t *testing.T) {
	root := setupFixture(t)
	keys := []tea.KeyMsg{
		{Type: tea.KeyEsc},
		{Type: tea.KeyCtrlC},
		{Type: tea.KeyRunes, Runes: []rune("q")},
	}
	for _, key := range keys {
		m := newTestModel(t, root)
		updated, cmd := m.Update(key)
		m = updated.(Model)
		if _, ok := m.Result(); ok {
			t.Fatalf("key %v: expected selected == false", key)
		}
		if cmd == nil {
			t.Fatalf("key %v: expected tea.Quit command", key)
		}
	}
}

func TestUpdate_ToggleHiddenShowsDotfiles(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	before := len(m.activeEntries)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(".")})
	m = updated.(Model)

	if len(m.activeEntries) != before+1 {
		t.Fatalf("entries after toggle = %d, want %d", len(m.activeEntries), before+1)
	}
	found := false
	for _, e := range m.activeEntries {
		if e.Name == ".hidden" {
			found = true
		}
	}
	if !found {
		t.Fatal(".hidden not present after toggling showHidden")
	}
}

func TestUpdate_RightIntoPermissionDeniedSetsStatusErrAndKeepsPath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits")
	}
	root := setupFixture(t)
	locked := filepath.Join(root, "locked")
	mustMkdir(t, locked)
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0o755)

	m := newTestModel(t, root) // "locked" sorts before "sub" alphabetically -> cursor 0

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)

	if m.activePath != root {
		t.Fatalf("activePath changed despite permission error: %q", m.activePath)
	}
	if m.statusErr == "" {
		t.Fatal("expected statusErr to be set")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/tui/...`
Expected: build failure (`Update` not implemented / `View` missing to satisfy `tea.Model` — add a temporary `func (m Model) View() string { return "" }` in `model.go` for this task so the package compiles; Task 4 replaces it).

- [ ] **Step 4: Write `update.go`**

```go
package tui

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"thicket/internal/fsutil"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
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
		}
	}
	return m, nil
}

// reload re-reads the active directory's listing in place, clamping the
// cursor if the entry count changed.
func (m *Model) reload() {
	entries, err := fsutil.ListDir(m.activePath, m.showHidden)
	if err != nil {
		m.statusErr = err.Error()
		return
	}
	m.activeEntries = entries
	m.statusErr = ""
	if len(entries) == 0 {
		m.activeCursor = -1
	} else if m.activeCursor < 0 || m.activeCursor >= len(entries) {
		m.activeCursor = 0
	}
	m.clampScroll()
}

func (m *Model) moveCursor(delta int) {
	if len(m.activeEntries) == 0 {
		return
	}
	m.activeCursor += delta
	if m.activeCursor < 0 {
		m.activeCursor = 0
	}
	if last := len(m.activeEntries) - 1; m.activeCursor > last {
		m.activeCursor = last
	}
	m.clampScroll()
}

func (m *Model) clampScroll() {
	rows := m.visibleRows()
	if rows <= 0 || m.activeCursor < 0 {
		return
	}
	if m.activeCursor < m.activeScroll {
		m.activeScroll = m.activeCursor
	}
	if m.activeCursor >= m.activeScroll+rows {
		m.activeScroll = m.activeCursor - rows + 1
	}
	if m.activeScroll < 0 {
		m.activeScroll = 0
	}
}

func (m *Model) visibleRows() int {
	rows := m.height - 2 // breadcrumb line + status line
	if rows < 1 {
		rows = 1
	}
	return rows
}

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
	m.activePath = parent
	m.activeEntries = entries
	idx := fsutil.IndexOfName(entries, child)
	if len(entries) == 0 {
		idx = -1
	} else if idx < 0 {
		idx = 0
	}
	m.activeCursor = idx
	m.activeScroll = 0
	m.clampScroll()
	m.statusErr = ""
}

func (m *Model) handleEnter() {
	m.quitting = true
	m.selected = true
	if m.activeCursor >= 0 && m.activeCursor < len(m.activeEntries) && m.activeEntries[m.activeCursor].IsDir {
		m.chosenPath = filepath.Join(m.activePath, m.activeEntries[m.activeCursor].Name)
		return
	}
	m.chosenPath = m.activePath
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/update.go internal/tui/update_test.go
git commit -m "feat: Miller-column navigation state machine"
```

---

### Task 4: Rendering (`internal/tui/render.go`)

**Files:**
- Modify: `internal/tui/model.go` (remove the temporary stub `View` from Task 3)
- Create: `internal/tui/render.go`
- Create: `internal/tui/render_test.go`

**Interfaces:**
- Consumes: `Model` fields from Task 3, `fsutil.Entry`/`ListDir`/`ReadFilePreview`.
- Produces: `func (m Model) View() string`, satisfying `tea.Model` fully. Consumed by Task 5 (`cmd/thicket` via `tea.NewProgram`).

- [ ] **Step 1: Remove the Task-3 stub**

Delete the temporary `func (m Model) View() string { return "" }` from `internal/tui/model.go`.

- [ ] **Step 2: Write the failing tests**

Rendering tests assert on the *content and left-to-right order* of literal names/text within the output, not exact byte-for-byte layout — exact ANSI-styled output is environment-dependent and not worth pinning.

```go
// internal/tui/render_test.go
package tui

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestView_ShowsActiveDirectoryEntries(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	out := m.View()

	for _, name := range []string{"sub", "file.txt"} {
		if !strings.Contains(out, name) {
			t.Fatalf("View() missing %q:\n%s", name, out)
		}
	}
	if strings.Contains(out, ".hidden") {
		t.Fatalf("View() should hide dotfiles by default:\n%s", out)
	}
}

func TestView_PreviewPaneShowsChildDirectoryContents(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root) // cursor starts on "sub"

	out := m.View()

	// "sub" contains "grand" and "leaf.txt" — the preview pane, to the
	// right of the active column, must show them.
	subIdx := strings.Index(out, "sub")
	grandIdx := strings.Index(out, "grand")
	if subIdx == -1 || grandIdx == -1 || grandIdx < subIdx {
		t.Fatalf("expected preview contents after active column:\n%s", out)
	}
}

func TestView_ShowsAncestorColumnAfterDescending(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(rightKey())
	m = updated.(Model)

	out := m.View()
	rootName := filepath.Base(root)
	if !strings.Contains(out, rootName) {
		t.Fatalf("expected ancestor column showing %q:\n%s", rootName, out)
	}
}

func TestView_PermissionDeniedColumnShowsMarker(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	m.statusErr = "open /x: permission denied"
	out := m.View()
	if !strings.Contains(out, "permission denied") {
		t.Fatalf("expected statusErr surfaced in status line:\n%s", out)
	}
}

func TestView_NarrowTerminalShowsActiveColumnOnly(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 20 // below the 30-column preview threshold

	out := m.View()
	if !strings.Contains(out, "sub") {
		t.Fatalf("expected active column contents even when narrow:\n%s", out)
	}
}
```

Add a small unexported helper next to the tests (used above and reused by future tests) — put it in `update_test.go` from Task 3... instead, since it's render-specific, add directly in `render_test.go`:

```go
func rightKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRight} }
```

(add `tea "github.com/charmbracelet/bubbletea"` to the imports of `render_test.go`.)

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/tui/...`
Expected: build failure (`View` removed, not yet re-implemented).

- [ ] **Step 4: Write `render.go`**

```go
package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"thicket/internal/fsutil"
)

const minColWidth = 15

var (
	dirStyle      = lipgloss.NewStyle().Bold(true)
	symlinkStyle  = lipgloss.NewStyle().Faint(true)
	selectedStyle = lipgloss.NewStyle().Reverse(true)
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

// column is a single rendered pane: an ancestor, the active directory, or
// the preview. highlightIdx is -1 when the pane has no highlighted row
// (ancestors and previews don't carry their own cursor — see spec §5).
type column struct {
	entries      []fsutil.Entry
	highlightIdx int
	unreadable   bool
}

func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	rows := m.visibleRows()
	cols := m.buildColumns()

	colWidth := m.width / len(cols)
	if colWidth < minColWidth {
		colWidth = minColWidth
	}

	rendered := make([]string, len(cols))
	for i, c := range cols {
		rendered[i] = renderColumn(c, colWidth, rows)
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)

	header := truncate(m.activePath, m.width)
	return header + "\n" + body + "\n" + m.statusLine()
}

func (m Model) buildColumns() []column {
	maxCols := m.width / minColWidth
	if maxCols < 1 {
		maxCols = 1
	}

	active := column{entries: m.activeEntries, highlightIdx: m.activeCursor}
	if maxCols < 2 {
		return []column{active}
	}

	preview := m.buildPreview()
	ancestors := m.buildAncestors(maxCols - 2)

	cols := make([]column, 0, len(ancestors)+2)
	cols = append(cols, ancestors...)
	cols = append(cols, active, preview)
	return cols
}

func (m Model) buildAncestors(max int) []column {
	if max <= 0 {
		return nil
	}
	var chain []column
	path := m.activePath
	for path != "/" && len(chain) < max {
		child := filepath.Base(path)
		parent := filepath.Dir(path)
		entries, err := fsutil.ListDir(parent, m.showHidden)
		if err != nil {
			chain = append([]column{{unreadable: true, highlightIdx: -1}}, chain...)
			path = parent
			continue
		}
		idx := fsutil.IndexOfName(entries, child)
		chain = append([]column{{entries: entries, highlightIdx: idx}}, chain...)
		path = parent
	}
	return chain
}

func (m Model) buildPreview() column {
	if m.activeCursor < 0 || m.activeCursor >= len(m.activeEntries) {
		return column{highlightIdx: -1}
	}
	entry := m.activeEntries[m.activeCursor]
	if !entry.IsDir {
		return m.buildFilePreviewColumn(entry)
	}

	childPath := filepath.Join(m.activePath, entry.Name)
	entries, err := fsutil.ListDir(childPath, m.showHidden)
	if err != nil {
		return column{unreadable: true, highlightIdx: -1}
	}
	const previewCap = 1000
	if len(entries) > previewCap {
		more := fsutil.Entry{Name: fmt.Sprintf("… %d more", len(entries)-previewCap)}
		entries = append(append([]fsutil.Entry{}, entries[:previewCap]...), more)
	}
	return column{entries: entries, highlightIdx: -1}
}

func (m Model) buildFilePreviewColumn(entry fsutil.Entry) column {
	fullPath := filepath.Join(m.activePath, entry.Name)
	preview, err := fsutil.ReadFilePreview(fullPath)
	if err != nil {
		return column{unreadable: true, highlightIdx: -1}
	}
	if preview.Binary {
		return column{
			entries:      []fsutil.Entry{{Name: fmt.Sprintf("<binary file, %d bytes>", preview.Size)}},
			highlightIdx: -1,
		}
	}
	entries := make([]fsutil.Entry, len(preview.Lines))
	for i, line := range preview.Lines {
		entries[i] = fsutil.Entry{Name: line}
	}
	return column{entries: entries, highlightIdx: -1}
}

func renderColumn(c column, width, rows int) string {
	if c.unreadable {
		return lipgloss.NewStyle().Width(width).Height(rows).Render("[permission denied]")
	}
	start := 0
	if c.highlightIdx >= rows {
		start = c.highlightIdx - rows + 1
	}
	var b strings.Builder
	for i := start; i < start+rows; i++ {
		if i >= len(c.entries) {
			b.WriteString("\n")
			continue
		}
		e := c.entries[i]
		text := truncate(e.Name, width)
		switch {
		case i == c.highlightIdx:
			text = selectedStyle.Render(text)
		case e.IsSymlink:
			text = symlinkStyle.Render(text + "@")
		case e.IsDir:
			text = dirStyle.Render(text)
		}
		b.WriteString(text)
		b.WriteString("\n")
	}
	return lipgloss.NewStyle().Width(width).Render(b.String())
}

func (m Model) statusLine() string {
	if m.statusErr != "" {
		return errStyle.Render(truncate(m.statusErr, m.width))
	}
	hints := "↑/k ↓/j move · →/l open · ←/h up · Enter cd+exit · . hidden · r refresh · q quit"
	return truncate(hints, m.width)
}

func truncate(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width <= 1 {
		return string(r[:width])
	}
	return string(r[:width-1]) + "…"
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/render.go internal/tui/render_test.go
git commit -m "feat: Miller-column rendering with ancestor/active/preview panes"
```

---

### Task 5: Entry point and shell handoff contract (`cmd/thicket`)

**Files:**
- Create: `cmd/thicket/main.go`

**Interfaces:**
- Consumes: `tui.New`, `tui.Model` (Task 3/4).
- Produces: the `thicket` binary; stdout contract consumed by Task 6's shell wrapper.

- [ ] **Step 1: Write `main.go`**

```go
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"thicket/internal/tui"
)

func main() {
	start := "."
	if len(os.Args) > 1 {
		start = os.Args[1]
	}

	m, err := tui.New(start)
	if err != nil {
		fmt.Fprintf(os.Stderr, "thicket: %v\n", err)
		os.Exit(2)
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "thicket: opening /dev/tty: %v\n", err)
		os.Exit(2)
	}
	defer tty.Close()

	p := tea.NewProgram(m, tea.WithInput(tty), tea.WithOutput(tty), tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "thicket: %v\n", err)
		os.Exit(2)
	}

	path, ok := finalModel.(tui.Model).Result()
	if !ok {
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, path)
}
```

- [ ] **Step 2: Build and smoke-check the binary compiles and rejects a bad path**

Run: `go build -o /tmp/thicket ./cmd/thicket && /tmp/thicket /nonexistent-path-xyz; echo "exit=$?"`
Expected: a `thicket: ...` error on stderr and `exit=2` (no stdout output) — confirms the non-interactive error path from spec §4 without needing a real terminal.

- [ ] **Step 3: Commit**

```bash
git add cmd/thicket/main.go
git commit -m "feat: cmd/thicket entry point with /dev/tty + stdout handoff"
```

---

### Task 6: Shell integration and documentation

**Files:**
- Create: `shell/thicket.bash`
- Create: `shell/thicket.zsh`
- Create: `README.md`

**Interfaces:**
- Consumes: the `thicket` binary's stdout/exit-code contract from Task 5 (spec §4).

- [ ] **Step 1: Write `shell/thicket.bash`**

```bash
# Source this file from ~/.bashrc:
#   source /path/to/thicket.bash
th() {
  local dir
  dir=$(command thicket "$@") && [ -n "$dir" ] && cd -- "$dir"
}
```

- [ ] **Step 2: Write `shell/thicket.zsh`**

```zsh
# Source this file from ~/.zshrc:
#   source /path/to/thicket.zsh
th() {
  local dir
  dir=$(command thicket "$@") && [ -n "$dir" ] && cd -- "$dir"
}
```

- [ ] **Step 3: Write `README.md`**

```markdown
# thicket

A Miller-column TUI file browser for the terminal. Browse with arrow keys
(or vi keys), and press Enter to `cd` the calling shell into the selected
directory.

## Install

```sh
go build -o thicket ./cmd/thicket
sudo install -m 755 thicket /usr/local/bin/thicket
```

Then source the shell wrapper for your shell:

```sh
# bash: add to ~/.bashrc
source /path/to/thicket/shell/thicket.bash

# zsh: add to ~/.zshrc
source /path/to/thicket/shell/thicket.zsh
```

This defines a `th` shell function. Rename it in your rc file if you'd
rather use a different name — the `thicket` binary itself doesn't care
what the wrapper is called.

## Usage

Run `th` (or `th /some/path` to start elsewhere). Keys:

| Key(s) | Action |
|---|---|
| `↑`, `k` | Move selection up |
| `↓`, `j` | Move selection down |
| `→`, `l` | Open selected directory, move focus right |
| `←`, `h` | Go up one directory, move focus left |
| `Enter` | cd to the selection and exit |
| `q`, `Esc`, `Ctrl-C` | Quit without changing directory |
| `.` | Toggle hidden (dotfile) visibility |
| `r` | Refresh the active directory's listing |

Navigation only in v1 — no file create/rename/delete/copy/move, no search,
no config file.
```

- [ ] **Step 4: Commit**

```bash
git add shell/thicket.bash shell/thicket.zsh README.md
git commit -m "docs: shell integration and usage README"
```

---

### Task 7: Manual end-to-end smoke test

**Files:** none (verification only).

**Interfaces:** none — this task exercises Tasks 1–6 together through the real terminal, which is not practical to automate headlessly (spec §10).

- [ ] **Step 1: Build and install locally**

Run: `go build -o /tmp/thicket ./cmd/thicket`

- [ ] **Step 2: Source the wrapper in the current shell**

Run (in an interactive shell, not a script): `PATH="/tmp:$PATH"; source shell/thicket.bash` (or `.zsh` if using zsh).

- [ ] **Step 3: Exercise navigation**

Run `th /tmp` (or any directory with a few subdirectories). Confirm:
- Arrow keys and `hjkl` both move the selection.
- `→`/`l` on a directory opens it and shifts focus right; the previous directory reappears as an ancestor column.
- `←`/`h` moves back up and restores the cursor onto the directory just left.
- `.` toggles dotfiles on/off.
- The status line shows key hints, and shows an error message if you navigate into a directory you don't have permission to read (e.g. `sudo mkdir -m 000 /tmp/locked-test` beforehand, then `sudo rm -rf /tmp/locked-test` after).

- [ ] **Step 4: Confirm the `cd` handoff**

From the same shell, note `pwd`, run `th`, navigate into a different directory, press Enter. Run `pwd` again and confirm the shell's working directory actually changed to the selected directory. Then run `th`, press `q` immediately, and confirm `pwd` is unchanged.

- [ ] **Step 5: Record the result**

No commit for this task — it is a verification gate. If any check fails, return to the relevant task (3–6), fix, re-run that task's automated tests, and repeat this manual smoke test before considering the plan complete.

---

## Self-Review Notes

- **Spec coverage:** §2 naming → Task 5/6 (binary/module names used verbatim); §3 non-goals → enforced by simply not building those features, called out in Global Constraints and README; §4 shell integration → Task 5 (tty/stdout/exit codes) + Task 6 (wrapper scripts); §5 navigation state model → Task 3; §6 rendering/layout → Task 4; §7 fs reading/previews → Tasks 1–2; §8 keybindings → Task 3's `Update` switch, documented in Task 6's README; §9 distribution → Task 6 README; §10 testing strategy → automated tests in Tasks 1–4, manual smoke test as Task 7.
- **Type consistency check:** `fsutil.Entry`, `fsutil.ListDir`, `fsutil.IndexOfName`, `fsutil.FilePreview`, `fsutil.ReadFilePreview` are defined once (Tasks 1–2) and referenced identically in Tasks 3–4. `tui.Model`, `tui.New`, `Model.Result()` are defined once (Task 3) and referenced identically in Tasks 4–5. No renamed/aliased symbols across tasks.
- **Placeholder scan:** no TBD/TODO markers; every step has concrete code or a concrete shell command with an expected result.
