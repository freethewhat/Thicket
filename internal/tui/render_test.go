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
	want := selectedStyle.Render(dirStyle.Render("directory"))
	if !strings.Contains(got, want) {
		t.Fatalf("selected directory lost its bold styling:\n%q", got)
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
	if width := lipgloss.Width(got); width != 5 {
		t.Fatalf("symlink row width = %d, want 5: %q", width, got)
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
	m, err := New(root)
	if err != nil {
		t.Fatalf("New(%q): %v", root, err)
	}
	m.height = 10 // visibleRows() = 8 data rows
	m.width = 20  // below the 30-column preview threshold: active-only

	down := tea.KeyMsg{Type: tea.KeyDown}
	up := tea.KeyMsg{Type: tea.KeyUp}

	for range 12 {
		updated, _ := m.Update(down)
		m = updated.(Model)
	}
	if m.activeCursor != 12 || m.activeScroll != 5 {
		t.Fatalf("after 12 downs: cursor=%d scroll=%d, want cursor=12 scroll=5", m.activeCursor, m.activeScroll)
	}
	out := m.View()
	for i := 5; i <= 12; i++ {
		name := fmt.Sprintf("n%02d", i)
		if !strings.Contains(out, name) {
			t.Fatalf("expected %q visible in window 5-12:\n%s", name, out)
		}
	}
	for _, i := range []int{0, 4, 13, 29} {
		name := fmt.Sprintf("n%02d", i)
		if strings.Contains(out, name) {
			t.Fatalf("did not expect %q visible in window 5-12:\n%s", name, out)
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
	for i := 3; i <= 10; i++ {
		name := fmt.Sprintf("n%02d", i)
		if !strings.Contains(out, name) {
			t.Fatalf("expected %q visible in window 3-10 after scrolling up:\n%s", name, out)
		}
	}
	for _, i := range []int{0, 2, 11, 29} {
		name := fmt.Sprintf("n%02d", i)
		if strings.Contains(out, name) {
			t.Fatalf("did not expect %q visible in window 3-10 after scrolling up:\n%s", name, out)
		}
	}
}
