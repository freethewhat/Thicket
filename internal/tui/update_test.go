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
//
//	root/
//	  sub/
//	    grand/
//	    leaf.txt
//	  file.txt
//	  .hidden
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
