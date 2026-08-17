package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// KeyBinding is one row of the keybinding table: the key(s) that trigger an
// action and a short description of what they do. Keybindings is the single
// source of truth for the in-app help screen (renderHelp, below) and for
// cmd/thicket's --help output (thicket.HelpText via Keybindings) — keep the
// two in sync by editing this list rather than duplicating rows.
type KeyBinding struct {
	Keys   string
	Action string
}

// Keybindings mirrors the table in README.md's Usage section. Keep both in
// sync when adding/changing a binding.
var Keybindings = []KeyBinding{
	{"↑, k", "Move selection up"},
	{"↓, j", "Move selection down"},
	{"PgUp, PgDn", "Move selection by a full page"},
	{"Home, End", "Jump to the first/last entry"},
	{"→, l", "Open selected directory, move focus right (no-op on files)"},
	{"←, h", "Go up one directory, move focus left (no-op at /)"},
	{"Enter", "Choose: cd to the selected directory, or to the active directory if a file/empty directory is selected; exit"},
	{"q, Esc, Ctrl-C", "Quit without changing directory"},
	{".", "Toggle hidden (dotfile) visibility (default off)"},
	{"r", "Refresh the active directory's listing"},
	{"/", "Type-ahead search the active column"},
	{"?", "Toggle this help screen"},
	{"m", "Bookmark the active directory under a letter"},
	{"`", "Jump to a bookmarked directory by letter"},
	{"'", "Open the marks list"},
	{"d (in marks list)", "Delete the highlighted mark"},
}

// KeyColWidth is wide enough to hold the longest Keys entry
// ("d (in marks list)") plus a two-space gutter before the Action column.
// cmd/thicket's helpText reuses this constant so the in-app ? help screen
// and --help output never drift out of alignment with each other.
const KeyColWidth = 19

// renderHelp draws the full-screen keybinding reference shown while
// Model.helpMode is true. It replaces the column layout entirely (see
// View) rather than overlaying it — lipgloss has no z-index compositing,
// and a full-screen replacement matches how the app already handles other
// modal states.
func (m Model) renderHelp(rows int) string {
	lines := make([]string, 0, len(Keybindings)+2)
	lines = append(lines, "Keybindings", "")
	for _, kb := range Keybindings {
		lines = append(lines, fmt.Sprintf("%-*s%s", KeyColWidth, kb.Keys, kb.Action))
	}
	content := strings.Join(lines, "\n")
	width := m.width - paneBorderWidth
	if width < 0 {
		width = 0
	}
	inner := lipgloss.NewStyle().Width(width).Height(rows).MaxHeight(rows).Render(content)
	return activePaneStyle.Render(inner)
}
