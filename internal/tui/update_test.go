package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"thicket/internal/fsutil"
	marksPkg "thicket/internal/marks"
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
	m, err := New(path, filepath.Join(t.TempDir(), "marks"))
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

func TestUpdate_QuestionMarkOpensHelpMode(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("opening help must not produce a command")
	}
	if !m.helpMode {
		t.Fatal("expected helpMode = true after ?")
	}
}

func TestUpdate_QuestionMarkTogglesHelpModeClosed(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)

	if m.helpMode {
		t.Fatal("expected second ? to close help mode")
	}
}

func TestUpdate_EscAndQCloseHelpModeWithoutQuitting(t *testing.T) {
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyEsc},
		{Type: tea.KeyRunes, Runes: []rune("q")},
	} {
		root := setupFixture(t)
		m := newTestModel(t, root)

		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
		m = updated.(Model)

		updated, cmd := m.Update(key)
		m = updated.(Model)

		if cmd != nil {
			t.Fatalf("key %v must close help, not quit the program", key)
		}
		if m.helpMode {
			t.Fatalf("key %v should have closed help mode", key)
		}
		if m.quitting || m.selected {
			t.Fatalf("key %v must not set quitting/selected while closing help", key)
		}
	}
}

func TestUpdate_HelpModeSwallowsNavigationKeys(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)
	beforeCursor := m.activeCursor

	for _, kt := range []tea.KeyType{tea.KeyDown, tea.KeyUp, tea.KeyRight, tea.KeyLeft} {
		updated, cmd := m.Update(tea.KeyMsg{Type: kt})
		m = updated.(Model)
		if cmd != nil {
			t.Fatalf("key %v produced a command while helpMode", kt)
		}
	}
	if m.activeCursor != beforeCursor || !m.helpMode {
		t.Fatal("navigation keys must be no-ops while helpMode is active")
	}
}

func TestUpdate_CtrlCQuitsEvenDuringHelp(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("expected Ctrl-C to quit even while helpMode is active")
	}
	if m.selected {
		t.Fatal("Ctrl-C must not select a path")
	}
}

func TestUpdate_PageDownMovesCursorByVisibleRows(t *testing.T) {
	root := t.TempDir()
	for i := range 30 {
		mustWriteFile(t, filepath.Join(root, fmt.Sprintf("n%02d", i)), "x")
	}
	m, err := New(root, filepath.Join(t.TempDir(), "marks"))
	if err != nil {
		t.Fatalf("New(%q): %v", root, err)
	}
	m.height = 10 // visibleRows() = 6
	m.width = 20

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = updated.(Model)

	if m.activeCursor != 6 {
		t.Fatalf("activeCursor = %d, want 6 (0 + visibleRows)", m.activeCursor)
	}
}

func TestUpdate_PageUpMovesCursorByVisibleRows(t *testing.T) {
	root := t.TempDir()
	for i := range 30 {
		mustWriteFile(t, filepath.Join(root, fmt.Sprintf("n%02d", i)), "x")
	}
	m, err := New(root, filepath.Join(t.TempDir(), "marks"))
	if err != nil {
		t.Fatalf("New(%q): %v", root, err)
	}
	m.height = 10 // visibleRows() = 6
	m.width = 20

	for range 20 {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(Model)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = updated.(Model)

	if m.activeCursor != 14 {
		t.Fatalf("activeCursor = %d, want 14 (20 - visibleRows)", m.activeCursor)
	}
}

func TestUpdate_PageDownClampsAtLastEntry(t *testing.T) {
	root := t.TempDir()
	for i := range 30 {
		mustWriteFile(t, filepath.Join(root, fmt.Sprintf("n%02d", i)), "x")
	}
	m, err := New(root, filepath.Join(t.TempDir(), "marks"))
	if err != nil {
		t.Fatalf("New(%q): %v", root, err)
	}
	m.height = 10 // visibleRows() = 6
	m.width = 20

	for range 27 {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(Model)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = updated.(Model)

	if last := len(m.activeEntries) - 1; m.activeCursor != last {
		t.Fatalf("activeCursor = %d, want %d (clamped at last entry)", m.activeCursor, last)
	}
}

func TestUpdate_PageUpClampsAtFirstEntry(t *testing.T) {
	root := t.TempDir()
	for i := range 30 {
		mustWriteFile(t, filepath.Join(root, fmt.Sprintf("n%02d", i)), "x")
	}
	m, err := New(root, filepath.Join(t.TempDir(), "marks"))
	if err != nil {
		t.Fatalf("New(%q): %v", root, err)
	}
	m.height = 10 // visibleRows() = 6
	m.width = 20

	for range 3 {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(Model)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = updated.(Model)

	if m.activeCursor != 0 {
		t.Fatalf("activeCursor = %d, want 0 (clamped at first entry)", m.activeCursor)
	}
}

func TestUpdate_HomeJumpsToFirstEntry(t *testing.T) {
	root := t.TempDir()
	for i := range 30 {
		mustWriteFile(t, filepath.Join(root, fmt.Sprintf("n%02d", i)), "x")
	}
	m, err := New(root, filepath.Join(t.TempDir(), "marks"))
	if err != nil {
		t.Fatalf("New(%q): %v", root, err)
	}
	m.height = 10
	m.width = 20

	for range 15 {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(Model)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = updated.(Model)

	if m.activeCursor != 0 {
		t.Fatalf("activeCursor = %d, want 0", m.activeCursor)
	}
}

func TestUpdate_EndJumpsToLastEntry(t *testing.T) {
	root := t.TempDir()
	for i := range 30 {
		mustWriteFile(t, filepath.Join(root, fmt.Sprintf("n%02d", i)), "x")
	}
	m, err := New(root, filepath.Join(t.TempDir(), "marks"))
	if err != nil {
		t.Fatalf("New(%q): %v", root, err)
	}
	m.height = 10
	m.width = 20

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = updated.(Model)

	if last := len(m.activeEntries) - 1; m.activeCursor != last {
		t.Fatalf("activeCursor = %d, want %d", m.activeCursor, last)
	}
}

func TestUpdate_PageAndHomeEndKeysNoopOnEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "empty"))
	m := newTestModel(t, filepath.Join(root, "empty"))

	for _, kt := range []tea.KeyType{tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd} {
		updated, _ := m.Update(tea.KeyMsg{Type: kt})
		m = updated.(Model)
		if m.activeCursor != -1 {
			t.Fatalf("key %v: activeCursor = %d, want -1 for empty dir", kt, m.activeCursor)
		}
	}
}

func TestUpdate_SearchPageAndHomeEndKeysAreNoops(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)
	beforeCursor, beforeQuery := m.activeCursor, m.searchQuery

	for _, kt := range []tea.KeyType{tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd} {
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
	// Move cursor off the leading "sub" directory onto "file.txt" so this
	// test isolates the activePath-fallback branch of selectedDirPath;
	// the cursor-on-a-directory branch is covered separately below.
	m.activeCursor = fsutil.IndexOfName(m.activeEntries, "file.txt")

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

// TestUpdate_MarkSetBookmarksHighlightedDirectoryNotActivePath guards
// against regressing to bookmarking the listing one level up: with the
// cursor left on its default position (the first entry, "sub" — a
// directory), m<letter> must mark root/sub, the directory the cursor and
// preview column are highlighting, not root itself.
func TestUpdate_MarkSetBookmarksHighlightedDirectoryNotActivePath(t *testing.T) {
	root := setupFixture(t)
	sub := filepath.Join(root, "sub")
	m := newTestModel(t, root)
	if m.activeEntries[m.activeCursor].Name != "sub" {
		t.Fatalf("precondition: cursor on %q, want \"sub\"", m.activeEntries[m.activeCursor].Name)
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(Model)

	if m.markTable['a'] != sub {
		t.Fatalf("markTable['a'] = %q, want %q", m.markTable['a'], sub)
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
	grand := filepath.Join(sub, "grand")
	m := newTestModel(t, root)
	m.markTable['a'] = grand // pre-existing mark, distinct from the cursor's default target (sub)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(Model)

	if m.markTable['a'] != sub {
		t.Fatalf("markTable['a'] = %q, want overwritten to %q", m.markTable['a'], sub)
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

	for i := range n + 2 {
		_ = i
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(Model)
	}
	if m.findCursor != n-1 {
		t.Fatalf("Down past the end must clamp: findCursor = %d, want %d", m.findCursor, n-1)
	}
}

// TestUpdate_FindDownScrollsWindowByExactlyOneRowAtATime covers the
// find-cursor scrolling regression: renderFind used to derive its window
// start from scrollStartFor(findCursor, rows), which recomputes from
// scratch on every render and therefore always pins the cursor to the
// last visible row once it scrolls past the first screenful — for both
// Down and Up. findScroll is now a persisted offset (mirroring
// activeScroll/clampScroll for the Miller column): repeated Down presses
// should advance the window by exactly one row at a time once the cursor
// reaches the bottom of the window (not snap to some other position),
// and a single Up press afterward should move the highlight up within
// the still-stationary window rather than immediately re-pinning it to
// the bottom row again.
func TestUpdate_FindDownScrollsWindowByExactlyOneRowAtATime(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.height = 7 // visibleRows() == 3
	m.width = 100
	m.findMode = true
	entries := make([]fsutil.WalkEntry, 10)
	for i := range entries {
		name := fmt.Sprintf("n%02d", i)
		entries[i] = fsutil.WalkEntry{Entry: fsutil.Entry{Name: name}, RelPath: name}
	}
	m.findResults = entries
	m.findCursor = 0
	m.findScroll = 0

	rows := m.visibleRows() // 3
	prevScroll := m.findScroll
	for i := 1; i < len(entries); i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(Model)

		if m.findCursor != i {
			t.Fatalf("step %d: findCursor = %d, want %d", i, m.findCursor, i)
		}
		if m.findCursor < m.findScroll || m.findCursor >= m.findScroll+rows {
			t.Fatalf("step %d: cursor %d not within visible window [%d, %d)", i, m.findCursor, m.findScroll, m.findScroll+rows)
		}
		wantDelta := 0
		if m.findCursor >= rows {
			wantDelta = 1
		}
		if m.findScroll != prevScroll+wantDelta {
			t.Fatalf("step %d: findScroll = %d, want %d (advance by exactly one row once past the window, not jump)", i, m.findScroll, prevScroll+wantDelta)
		}
		prevScroll = m.findScroll

		out := m.renderFind(rows)
		if !strings.Contains(out, entries[i].RelPath) {
			t.Fatalf("step %d: expected cursor entry %q visible:\n%s", i, entries[i].RelPath, out)
		}
	}

	scrollBeforeUp := m.findScroll
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.findScroll != scrollBeforeUp {
		t.Fatalf("Up within an already-scrolled window must not re-pin it: findScroll = %d, want unchanged %d", m.findScroll, scrollBeforeUp)
	}
	if m.findCursor != len(entries)-2 {
		t.Fatalf("findCursor after Up = %d, want %d", m.findCursor, len(entries)-2)
	}
	out := m.renderFind(rows)
	if !strings.Contains(out, entries[len(entries)-2].RelPath) {
		t.Fatalf("expected cursor entry %q visible after Up:\n%s", entries[len(entries)-2].RelPath, out)
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
	if m.findQuery != "zzzznomatch" {
		t.Fatalf("expected findQuery to survive a no-match Enter so the user can keep typing, got %q", m.findQuery)
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

func TestUpdate_FindEnterOnUnreadableRelocationTargetSetsStatusErrAndExits(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits")
	}
	root := setupFixture(t)
	m := newTestModel(t, root)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("leaf")})
	m = updated.(Model)

	sub := filepath.Join(root, "sub")
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(sub, 0o755) })

	prevEntries, prevCursor := m.activeEntries, m.activeCursor
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("Enter on an unreadable relocation target must not quit")
	}
	if m.findMode {
		t.Fatal("expected findMode == false: a stale walk snapshot is not re-shown on relocation failure")
	}
	if m.statusErr == "" {
		t.Fatal("expected statusErr to be set")
	}
	if m.activePath != root {
		t.Fatalf("activePath changed despite relocation failure: %q, want unchanged %q", m.activePath, root)
	}
	if m.activeCursor != prevCursor || len(m.activeEntries) != len(prevEntries) {
		t.Fatalf("expected activeEntries/activeCursor untouched on relocation failure: cursor=%d entries=%+v", m.activeCursor, m.activeEntries)
	}
}
