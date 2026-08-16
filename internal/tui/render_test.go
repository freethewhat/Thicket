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
