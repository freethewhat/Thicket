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
		m.clampScroll()
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
	// header/status lines (2) + every pane's top/bottom border (2) — see
	// paneBorderHeight in render.go.
	rows := m.height - 2 - paneBorderHeight
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

	// The child-index lookup must not use the showHidden-filtered listing
	// on its own: if activePath is itself a dotdirectory and hidden files
	// are off, child is filtered out of entries entirely and the lookup
	// would always miss. Resolve existence against an unfiltered listing
	// of the parent while still displaying/storing the filtered one.
	known := entries
	if !m.showHidden {
		if all, aerr := fsutil.ListDir(parent, true); aerr == nil {
			known = all
		}
	}

	m.activePath = parent
	m.activeEntries = entries
	switch {
	case len(entries) == 0:
		m.activeCursor = -1
	case fsutil.IndexOfName(known, child) < 0:
		// Absent even from the unfiltered listing: deleted externally.
		m.activeCursor = 0
	default:
		// Present in the parent, but only highlightable when it also
		// appears in the filtered display; a hidden dotdirectory just
		// left with showHidden off has no visible row to highlight.
		m.activeCursor = fsutil.IndexOfName(entries, child)
	}
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
