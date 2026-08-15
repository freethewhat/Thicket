package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"thicket/internal/fsutil"
)

const minColWidth = 15

var (
	dirStyle      = lipgloss.NewStyle().Bold(true)
	symlinkStyle  = lipgloss.NewStyle().Faint(true)
	selectedStyle = lipgloss.NewStyle().Reverse(true)
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

// column is a single rendered pane: an ancestor, the active directory, or
// the preview. highlightIdx is -1 when the pane has no highlighted row
// (ancestors and previews don't carry their own cursor — see spec §5).
type column struct {
	entries      []fsutil.Entry
	highlightIdx int
	unreadable   bool
}

func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	rows := m.visibleRows()
	cols := m.buildColumns()

	colWidth := m.width / len(cols)
	if colWidth < minColWidth {
		colWidth = minColWidth
	}

	rendered := make([]string, len(cols))
	for i, c := range cols {
		rendered[i] = renderColumn(c, colWidth, rows)
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)

	header := truncate(m.activePath, m.width)
	return header + "\n" + body + "\n" + m.statusLine()
}

func (m Model) buildColumns() []column {
	maxCols := m.width / minColWidth
	if maxCols < 1 {
		maxCols = 1
	}

	active := column{entries: m.activeEntries, highlightIdx: m.activeCursor}
	if maxCols < 2 {
		return []column{active}
	}

	preview := m.buildPreview()
	ancestors := m.buildAncestors(maxCols - 2)

	cols := make([]column, 0, len(ancestors)+2)
	cols = append(cols, ancestors...)
	cols = append(cols, active, preview)
	return cols
}

func (m Model) buildAncestors(max int) []column {
	if max <= 0 {
		return nil
	}
	var chain []column
	path := m.activePath
	for path != "/" && len(chain) < max {
		child := filepath.Base(path)
		parent := filepath.Dir(path)
		entries, err := fsutil.ListDir(parent, m.showHidden)
		if err != nil {
			chain = append([]column{{unreadable: true, highlightIdx: -1}}, chain...)
			path = parent
			continue
		}
		idx := fsutil.IndexOfName(entries, child)
		chain = append([]column{{entries: entries, highlightIdx: idx}}, chain...)
		path = parent
	}
	return chain
}

func (m Model) buildPreview() column {
	if m.activeCursor < 0 || m.activeCursor >= len(m.activeEntries) {
		return column{highlightIdx: -1}
	}
	entry := m.activeEntries[m.activeCursor]
	if !entry.IsDir {
		return m.buildFilePreviewColumn(entry)
	}

	childPath := filepath.Join(m.activePath, entry.Name)
	entries, err := fsutil.ListDir(childPath, m.showHidden)
	if err != nil {
		return column{unreadable: true, highlightIdx: -1}
	}
	const previewCap = 1000
	if len(entries) > previewCap {
		more := fsutil.Entry{Name: fmt.Sprintf("… %d more", len(entries)-previewCap)}
		entries = append(append([]fsutil.Entry{}, entries[:previewCap]...), more)
	}
	return column{entries: entries, highlightIdx: -1}
}

func (m Model) buildFilePreviewColumn(entry fsutil.Entry) column {
	fullPath := filepath.Join(m.activePath, entry.Name)
	preview, err := fsutil.ReadFilePreview(fullPath)
	if err != nil {
		return column{unreadable: true, highlightIdx: -1}
	}
	if preview.Binary {
		return column{
			entries:      []fsutil.Entry{{Name: fmt.Sprintf("<binary file, %d bytes>", preview.Size)}},
			highlightIdx: -1,
		}
	}
	entries := make([]fsutil.Entry, len(preview.Lines))
	for i, line := range preview.Lines {
		entries[i] = fsutil.Entry{Name: line}
	}
	return column{entries: entries, highlightIdx: -1}
}

func renderColumn(c column, width, rows int) string {
	if c.unreadable {
		return lipgloss.NewStyle().Width(width).Height(rows).Render("[permission denied]")
	}
	start := 0
	if c.highlightIdx >= rows {
		start = c.highlightIdx - rows + 1
	}
	var b strings.Builder
	for i := start; i < start+rows; i++ {
		if i >= len(c.entries) {
			b.WriteString("\n")
			continue
		}
		e := c.entries[i]
		nameWidth := width
		if e.IsSymlink {
			nameWidth--
		}
		text := truncate(e.Name, nameWidth)
		if e.IsDir {
			text = dirStyle.Render(text)
		}
		if e.IsSymlink {
			text += symlinkStyle.Render("@")
		}
		if i == c.highlightIdx {
			text = selectedStyle.Render(text)
		}
		b.WriteString(text)
		b.WriteString("\n")
	}
	return lipgloss.NewStyle().Width(width).Render(b.String())
}

func (m Model) statusLine() string {
	if m.statusErr != "" {
		return errStyle.Render(truncate(m.statusErr, m.width))
	}
	hints := "↑/k ↓/j move · →/l open · ←/h up · Enter cd+exit · . hidden · r refresh · q quit"
	return truncate(hints, m.width)
}

func truncate(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width <= 1 {
		return string(r[:width])
	}
	return string(r[:width-1]) + "…"
}
