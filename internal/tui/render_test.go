package tui

import (
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
