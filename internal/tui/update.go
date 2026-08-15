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
	rows := m.height - 2 // breadcrumb line + status line
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
	m.activePath = parent
	m.activeEntries = entries
	idx := fsutil.IndexOfName(entries, child)
	if len(entries) == 0 {
		idx = -1
	} else if idx < 0 {
		idx = 0
	}
	m.activeCursor = idx
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
