package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"thicket/internal/fsutil"
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

// TestView_PermissionDeniedColumnShowsMarker covers §6: the status line
// composes hints (left) with statusErr (right) — the error needs enough
// width alongside the hints to appear in full, unlike a bare status line.
func TestView_PermissionDeniedColumnShowsMarker(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 130

	m.statusErr = "open /x: permission denied"
	out := m.View()
	if !strings.Contains(out, "permission denied") {
		t.Fatalf("expected statusErr surfaced in status line:\n%s", out)
	}
}

// TestView_StatusLineAt80ColumnsStillShowsError is a regression test: the
// key-hint string is exactly 80 runes wide, so at the common 80-column
// terminal width the hints alone used to consume the entire status line,
// silently dropping statusErr (spec §6 says the right side — statusErr or
// activePath — is the higher-priority information and must never be
// suppressed by the left-side hints).
func TestView_StatusLineAt80ColumnsStillShowsError(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 80

	m.statusErr = "open /x: permission denied"
	out := m.View()
	if !strings.Contains(out, "permission denied") {
		t.Fatalf("expected statusErr surfaced at width 80:\n%s", out)
	}
}

// TestView_StatusLineNarrowWidthStillShowsError checks the same priority
// holds at an even tighter width, where the hints must be truncated away
// entirely (not just shortened) to make room for the error.
func TestView_StatusLineNarrowWidthStillShowsError(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 40

	m.statusErr = "open /x: permission denied"
	out := m.View()
	if !strings.Contains(out, "permission denied") {
		t.Fatalf("expected statusErr surfaced at width 40:\n%s", out)
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

func TestRenderColumn_SelectedDirectoryKeepsBoldStyle(t *testing.T) {
	restoreColorProfile(t)

	got := renderColumn(column{
		entries:      []fsutil.Entry{{Name: "directory", IsDir: true}},
		highlightIdx: 0,
	}, 20, 1)
	want := selectedStyle.Render(dirStyle.Render("directory") + strings.Repeat(" ", 9) + dirStyle.Render("->"))
	if !strings.Contains(got, want) {
		t.Fatalf("selected directory lost its bold styling:\n%q", got)
	}
}

func TestRenderColumn_SelectedDirectoryGetsRightAlignedArrowMarker(t *testing.T) {
	restoreColorProfile(t)

	got := renderColumn(column{
		entries:      []fsutil.Entry{{Name: "dir", IsDir: true}},
		highlightIdx: 0,
	}, 20, 1)
	// Content width is 20: "dir" (3) + 15 spaces of padding + "->" (2) = 20,
	// flush against the pane's right inner edge regardless of name length.
	want := selectedStyle.Render(dirStyle.Render("dir") + strings.Repeat(" ", 15) + dirStyle.Render("->"))
	if !strings.Contains(got, want) {
		t.Fatalf("expected '->' marker right-aligned to pane width:\n%q", got)
	}
}

func TestRenderColumn_SelectedFileHasNoTrailingArrowMarker(t *testing.T) {
	restoreColorProfile(t)

	got := renderColumn(column{
		entries:      []fsutil.Entry{{Name: "file.txt", IsDir: false}},
		highlightIdx: 0,
	}, 20, 1)
	if strings.Contains(got, ">") {
		t.Fatalf("expected highlighted file to have no trailing '>' marker:\n%q", got)
	}
}

func TestRenderColumn_DirectoryUsesExplicitBlueColor(t *testing.T) {
	restoreColorProfile(t)

	got := renderColumn(column{
		entries:      []fsutil.Entry{{Name: "directory", IsDir: true}},
		highlightIdx: -1,
	}, 20, 1)
	want := dirStyle.Render("directory")
	if !strings.Contains(got, want) {
		t.Fatalf("expected directory rendered with explicit color, independent of terminal bold-as-bright defaults:\n%q", got)
	}
}

func TestRenderColumn_SelectedSymlinkKeepsFaintSuffix(t *testing.T) {
	restoreColorProfile(t)

	got := renderColumn(column{
		entries:      []fsutil.Entry{{Name: "link", IsSymlink: true}},
		highlightIdx: 0,
	}, 20, 1)
	want := selectedStyle.Render("link" + symlinkStyle.Render("@"))
	if !strings.Contains(got, want) {
		t.Fatalf("selected symlink lost its faint suffix:\n%q", got)
	}
}

func TestRenderColumn_LongSymlinkFitsColumn(t *testing.T) {
	restoreColorProfile(t)

	got := renderColumn(column{
		entries:      []fsutil.Entry{{Name: "long-link", IsSymlink: true}},
		highlightIdx: -1,
	}, 5, 1)
	if !strings.Contains(got, "@") {
		t.Fatalf("truncated symlink lost its suffix: %q", got)
	}
	// Content width 5 plus the pane's left/right border (paneBorderWidth).
	want := 5 + paneBorderWidth
	if width := lipgloss.Width(got); width != want {
		t.Fatalf("symlink row width = %d, want %d: %q", width, want, got)
	}
}

func restoreColorProfile(t *testing.T) {
	t.Helper()
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
}

func rightKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRight} }

// TestView_ActiveColumnScrollTracksActiveScroll covers the regression
// where renderColumn re-derived its own scroll window from highlightIdx
// instead of Model.activeScroll, permanently pinning the cursor to the
// bottom row and hiding everything below it once the cursor passed the
// pane height.
func TestView_ActiveColumnScrollTracksActiveScroll(t *testing.T) {
	root := t.TempDir()
	for i := range 30 {
		mustWriteFile(t, filepath.Join(root, fmt.Sprintf("n%02d", i)), "x")
	}
	m, err := New(root, filepath.Join(t.TempDir(), "marks"))
	if err != nil {
		t.Fatalf("New(%q): %v", root, err)
	}
	m.height = 10 // visibleRows() = 6 data rows (border rows eat 2 more)
	m.width = 20  // below the preview threshold: active-only

	down := tea.KeyMsg{Type: tea.KeyDown}
	up := tea.KeyMsg{Type: tea.KeyUp}

	for range 12 {
		updated, _ := m.Update(down)
		m = updated.(Model)
	}
	if m.activeCursor != 12 || m.activeScroll != 7 {
		t.Fatalf("after 12 downs: cursor=%d scroll=%d, want cursor=12 scroll=7", m.activeCursor, m.activeScroll)
	}
	out := m.View()
	for i := 7; i <= 12; i++ {
		name := fmt.Sprintf("n%02d", i)
		if !strings.Contains(out, name) {
			t.Fatalf("expected %q visible in window 7-12:\n%s", name, out)
		}
	}
	for _, i := range []int{0, 4, 6, 13, 29} {
		name := fmt.Sprintf("n%02d", i)
		if strings.Contains(out, name) {
			t.Fatalf("did not expect %q visible in window 7-12:\n%s", name, out)
		}
	}

	// Move back up without leaving the current window: the window must
	// stay put (not stay pinned so the cursor is always the bottom row),
	// then keep moving up until it must shift back up.
	for range 9 {
		updated, _ := m.Update(up)
		m = updated.(Model)
	}
	if m.activeCursor != 3 || m.activeScroll != 3 {
		t.Fatalf("after 9 ups: cursor=%d scroll=%d, want cursor=3 scroll=3 (window must shift back up)", m.activeCursor, m.activeScroll)
	}
	out = m.View()
	for i := 3; i <= 8; i++ {
		name := fmt.Sprintf("n%02d", i)
		if !strings.Contains(out, name) {
			t.Fatalf("expected %q visible in window 3-8 after scrolling up:\n%s", name, out)
		}
	}
	for _, i := range []int{0, 2, 9, 29} {
		name := fmt.Sprintf("n%02d", i)
		if strings.Contains(out, name) {
			t.Fatalf("did not expect %q visible in window 3-8 after scrolling up:\n%s", name, out)
		}
	}
}

// TestView_ActivePaneUsesThickBorder covers the new pane-separation UI:
// the active (current-directory) pane must render with a thick border so
// the user can tell at a glance which pane they're navigating, distinct
// from the thin border on ancestor/preview panes.
func TestView_ActivePaneUsesThickBorder(t *testing.T) {
	restoreColorProfile(t)
	root := setupFixture(t)
	m := newTestModel(t, root)

	out := m.View()
	if !strings.Contains(out, "┏") {
		t.Fatalf("expected active pane to use a thick-border corner (┏):\n%s", out)
	}
}

// TestView_InactivePanesUseNormalBorder covers the preview pane (never
// navigable) rendering with a thin, non-active border, distinguishing it
// from the active pane's thick border.
func TestView_InactivePanesUseNormalBorder(t *testing.T) {
	restoreColorProfile(t)
	root := setupFixture(t)
	m := newTestModel(t, root)

	out := m.View()
	if !strings.Contains(out, "┌") {
		t.Fatalf("expected an inactive pane to use a normal-border corner (┌):\n%s", out)
	}
}

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

func TestView_HelpModeShowsKeybindingsAndHidesColumns(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)

	out := m.View()

	if !strings.Contains(out, "Keybindings") {
		t.Fatalf("expected help screen title:\n%s", out)
	}
	if !strings.Contains(out, "Move selection up") {
		t.Fatalf("expected keybinding description in help screen:\n%s", out)
	}
	if strings.Contains(out, "sub") || strings.Contains(out, "file.txt") {
		t.Fatalf("help screen must not show the normal column layout:\n%s", out)
	}
}

// TestView_HelpScreenKeyColumnFitsWidestRow guards against a keyColWidth
// regression: the widest Keys entry ("d (in marks list)", 17 runes) must
// still get its full two-space gutter before the Action column, not be
// jammed against it because the column width fell behind after a longer
// entry was added.
func TestView_HelpScreenKeyColumnFitsWidestRow(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.height = 24 // tall enough that visibleRows() >= len(Keybindings)+2, so no row is clipped

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)

	out := m.View()

	want := "d (in marks list)  Delete the highlighted mark"
	if !strings.Contains(out, want) {
		t.Fatalf("expected gutter-separated widest row %q in help screen:\n%s", want, out)
	}
}

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

// TestView_MarksListManyMarksFillPaneToExactVisibleHeightWithBorderIntact
// covers the case where the user has accumulated more marks than fit on
// screen: lipgloss's Height() is a minimum, not a clamp, so without an
// explicit MaxHeight the pane would grow past the available rows and push
// the header off-screen (the same failure renderColumn's MaxHeight guards
// against for the normal column layout). Applying MaxHeight to the
// *bordered* style rather than a borderless inner block truncates the
// border itself away on every render, so this asserts the exact total
// line count (not just an upper bound) and that the bottom border survives.
func TestView_MarksListManyMarksFillPaneToExactVisibleHeightWithBorderIntact(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.height = 12
	m.width = 100
	for r := 'a'; r <= 'o'; r++ { // 15 marks, comfortably more than visibleRows()
		m.markTable[r] = "/" + string(r)
	}
	m.marksListMode = true
	m.marksCursor = 0

	out := m.View()

	lines := strings.Split(out, "\n")
	if len(lines) != m.height {
		t.Fatalf("View() produced %d lines, want exactly m.height (%d):\n%s", len(lines), m.height, out)
	}
	if lines[0] != truncate(m.activePath, m.width) {
		t.Fatalf("expected header as first line, got %q", lines[0])
	}
	borderLine := lines[len(lines)-2] // pane's bottom border, just above the status line
	if !strings.Contains(borderLine, lipgloss.ThickBorder().BottomLeft) {
		t.Fatalf("expected bottom border on pane's last line, got %q\nfull output:\n%s", borderLine, out)
	}
}

// TestView_MarksListFewMarksStillFillsFullHeightBorderedBox covers the
// opposite edge: with fewer marks than visibleRows(), the pane must still
// pad out to the full box height (not shrink to just the content) and
// keep its border, since Height(rows) with no MaxHeight interference is
// the floor that guarantees this.
func TestView_MarksListFewMarksStillFillsFullHeightBorderedBox(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.height = 16
	m.width = 100
	m.markTable['a'] = "/only"
	m.marksListMode = true
	m.marksCursor = 0

	out := m.View()

	lines := strings.Split(out, "\n")
	if len(lines) != m.height {
		t.Fatalf("View() produced %d lines, want exactly m.height (%d):\n%s", len(lines), m.height, out)
	}
	borderLine := lines[len(lines)-2] // pane's bottom border, just above the status line
	if !strings.Contains(borderLine, lipgloss.ThickBorder().BottomLeft) {
		t.Fatalf("expected bottom border on pane's last line, got %q\nfull output:\n%s", borderLine, out)
	}
}

// TestView_StatusLineMarksListSurfacesStatusErr covers the deliberate
// asymmetry with helpMode/searchMode: marksListMode's statusLine branch
// leaves `right`/`isErr` at their default statusErr-or-activePath
// precedence so that activateMarksListEntry can set statusErr and stay in
// marksListMode with the error still visible.
func TestView_StatusLineMarksListSurfacesStatusErr(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 150
	m.marksListMode = true
	m.statusErr = "some error"

	if !strings.Contains(m.statusLine(), "some error") {
		t.Fatalf("statusLine() missing statusErr while marksListMode: %q", m.statusLine())
	}
}

// TestView_HelpScreenStaysWithinPaneHeight covers the same overflow bug
// renderMarksList's MaxHeight regression tests guard against: lipgloss's
// Height() is a floor, not a clamp, so without an explicit MaxHeight the
// help pane would grow past the available rows once Keybindings no longer
// fits within m.visibleRows(), pushing the header off-screen.
func TestView_HelpScreenStaysWithinPaneHeight(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.height = 12 // visibleRows() (8) < len(Keybindings)+2 (19)
	m.width = 100
	m.helpMode = true

	out := m.View()

	lines := strings.Split(out, "\n")
	if len(lines) != m.height {
		t.Fatalf("View() produced %d lines, want exactly m.height (%d):\n%s", len(lines), m.height, out)
	}
}

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

// TestView_FindModeSymlinkAtFullWidthDoesNotPushOutNextEntry covers a
// review defect: renderFind must reserve a width cell for the symlink '@'
// suffix before truncating RelPath, same as renderColumn's
// reservedForSymlink. Without it, a symlink whose RelPath reaches the full
// pane width overflows to width+1 display cells, soft-wrapping that row
// and pushing later entries out of the MaxHeight-clipped view.
func TestView_FindModeSymlinkAtFullWidthDoesNotPushOutNextEntry(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 20
	m.height = 6 // visibleRows() == 2
	m.findMode = true
	width := m.width - paneBorderWidth
	m.findResults = []fsutil.WalkEntry{
		{Entry: fsutil.Entry{Name: "link", IsSymlink: true}, RelPath: strings.Repeat("x", width)},
		{Entry: fsutil.Entry{Name: "nextentry"}, RelPath: "nextentry"},
	}
	m.findCursor = 0

	out := m.renderFind(m.visibleRows())

	if !strings.Contains(out, "nextentry") {
		t.Fatalf("symlink at full pane width pushed the next entry out of view:\n%s", out)
	}
}

// TestView_FindModeScrollsToKeepCursorVisible covers a review defect:
// findCursor is clamped against the full filtered result list (up to the
// walk cap, far larger than the visible rows), so renderFind must track
// it with a persisted, user-driven scroll window (m.findScroll, advanced
// via moveFindCursor/clampFindScroll) instead of always rendering from
// index 0.
func TestView_FindModeScrollsToKeepCursorVisible(t *testing.T) {
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
	m.moveFindCursor(9) // 0 -> 9, clamping findScroll along the way

	out := m.renderFind(m.visibleRows())

	if !strings.Contains(out, "n09") {
		t.Fatalf("expected cursor entry n09 visible after scrolling:\n%s", out)
	}
}

// TestView_FindModeSingleRowPaneWithTruncatedStillShowsEntry covers a
// review defect introduced by the cursor-scrolling fix: reserving a
// bottom row for the truncated indicator whenever entryRows > 0 could
// drive entryRows to 0 in a 1-row-tall pane, making scrollStartFor return
// an out-of-range start and rendering nothing but the truncated
// indicator — worse than before, since not even one match/cursor row was
// visible. The indicator must be dropped in favor of showing at least one
// entry when the pane is that small.
func TestView_FindModeSingleRowPaneWithTruncatedStillShowsEntry(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.height = 5 // visibleRows() == 1
	m.width = 100
	m.findMode = true
	m.findResults = []fsutil.WalkEntry{
		{Entry: fsutil.Entry{Name: "n00"}, RelPath: "n00"},
		{Entry: fsutil.Entry{Name: "n01"}, RelPath: "n01"},
	}
	m.findCursor = 0
	m.findTruncated = true

	out := m.renderFind(m.visibleRows())

	if !strings.Contains(out, "n00") {
		t.Fatalf("expected the cursor's entry visible in a 1-row truncated find pane, got only the truncated indicator:\n%s", out)
	}
}
func TestView_StatusLineShowsUpdateNotice(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 150
	m.updateNotice = "update available: v9.9.9 — run 'thicket-bin update'"

	if !strings.Contains(m.statusLine(), "update available: v9.9.9") {
		t.Fatalf("statusLine() missing updateNotice: %q", m.statusLine())
	}
}

func TestView_StatusLineStatusErrTakesPrecedenceOverUpdateNotice(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 150
	m.updateNotice = "update available: v9.9.9 — run 'thicket-bin update'"
	m.statusErr = "open /x: permission denied"

	line := m.statusLine()
	if !strings.Contains(line, "permission denied") {
		t.Fatalf("statusLine() missing statusErr: %q", line)
	}
	if strings.Contains(line, "update available") {
		t.Fatalf("statusLine() should not show updateNotice while statusErr is set: %q", line)
	}
}

func TestView_StatusLineSearchSuppressesUpdateNotice(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 150
	m.updateNotice = "update available: v9.9.9 — run 'thicket-bin update'"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)

	if strings.Contains(m.statusLine(), "update available") {
		t.Fatalf("statusLine() should suppress updateNotice while searchMode is active: %q", m.statusLine())
	}
}

func TestView_StatusLineMarksListStillShowsUpdateNotice(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 150
	m.marksListMode = true
	m.updateNotice = "update available: v9.9.9 — run 'thicket-bin update'"

	if !strings.Contains(m.statusLine(), "update available") {
		t.Fatalf("statusLine() should still show updateNotice during marksListMode: %q", m.statusLine())
	}
}

func TestView_StatusLineShowsYankNotice(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 150
	m.yankNotice = "yanked: " + filepath.Join(root, "sub")

	if !strings.Contains(m.statusLine(), "yanked:") {
		t.Fatalf("statusLine() missing yankNotice: %q", m.statusLine())
	}
}

func TestView_StatusLineStatusErrTakesPrecedenceOverYankNotice(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 150
	m.yankNotice = "yanked: /some/path"
	m.statusErr = "open /x: permission denied"

	line := m.statusLine()
	if !strings.Contains(line, "permission denied") {
		t.Fatalf("statusLine() missing statusErr: %q", line)
	}
	if strings.Contains(line, "yanked:") {
		t.Fatalf("statusLine() should not show yankNotice while statusErr is set: %q", line)
	}
}

func TestView_StatusLineYankNoticeTakesPrecedenceOverUpdateNotice(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 150
	m.updateNotice = "update available: v9.9.9 — run 'thicket-bin update'"
	m.yankNotice = "yanked: /some/path"

	line := m.statusLine()
	if !strings.Contains(line, "yanked:") {
		t.Fatalf("statusLine() missing yankNotice: %q", line)
	}
	if strings.Contains(line, "update available") {
		t.Fatalf("statusLine() should not show updateNotice while yankNotice is set: %q", line)
	}
}

// TestView_HelpScreenListsYankKeybindings covers the new y/Y rows appended
// to Keybindings. Like TestView_HelpScreenKeyColumnFitsWidestRow, it must
// override the default test height: those rows are the two last entries
// in the list, well past what newTestModel's default height leaves room
// for once the help pane's MaxHeight clips overflow (spec §7-style
// full-screen replacement, same clipping renderMarksList/renderFind use).
func TestView_HelpScreenListsYankKeybindings(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.height = 26

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)

	out := m.View()
	for _, want := range []string{"Copy the highlighted entry's path to the clipboard", "Copy the active directory's own path to the clipboard"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help screen missing %q:\n%s", want, out)
		}
	}
}
