package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"thicket/internal/fsutil"
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
	m, err := New(root)
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
	m, err := New(root)
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
	m, err := New(root)
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
	m, err := New(root)
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
	m, err := New(root)
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
	m, err := New(root)
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
