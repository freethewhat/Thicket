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
	// searchMode/searchQuery/searchNoMatch/searchPrevCursor: type-ahead
	// search state (spec docs/superpowers/specs/2026-08-16-thicket-type-ahead-search-design.md).
	// Zero-valued at construction — no change to New()'s behavior.
	searchMode       bool
	searchQuery      string
	searchNoMatch    bool
	searchPrevCursor int
	width            int
	height           int
	quitting         bool
	selected         bool
	chosenPath       string
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
