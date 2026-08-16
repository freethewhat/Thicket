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
// start is the index of the first visible row. The active column's start
// tracks Model.activeScroll (its own persisted scroll state); ancestor and
// preview columns carry no scroll state of their own, so their start is
// derived from highlightIdx at build time (pinning the highlighted row
// into view).
type column struct {
	entries      []fsutil.Entry
	highlightIdx int
	unreadable   bool
	start        int
}

func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	rows := m.visibleRows()
	cols := m.buildColumns(rows)

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

func (m Model) buildColumns(rows int) []column {
	maxCols := m.width / minColWidth
	if maxCols < 1 {
		maxCols = 1
	}

	active := column{entries: m.activeEntries, highlightIdx: m.activeCursor, start: m.activeScroll}
	if maxCols < 2 {
		return []column{active}
	}

	preview := m.buildPreview()
	ancestors := m.buildAncestors(maxCols-2, rows)

	cols := make([]column, 0, len(ancestors)+2)
	cols = append(cols, ancestors...)
	cols = append(cols, active, preview)
	return cols
}

func (m Model) buildAncestors(max, rows int) []column {
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
		chain = append([]column{{entries: entries, highlightIdx: idx, start: scrollStartFor(idx, rows)}}, chain...)
		path = parent
	}
	return chain
}

// scrollStartFor derives a start row that keeps highlightIdx visible within
// rows rows, pinning it to the bottom once it would otherwise scroll off —
// the behavior ancestor/preview columns use since they have no persisted
// scroll state of their own.
func scrollStartFor(highlightIdx, rows int) int {
	if highlightIdx >= rows {
		return highlightIdx - rows + 1
	}
	return 0
}

func (m Model) buildPreview() column {
	if m.activeCursor < 0 || m.activeCursor >= len(m.activeEntries) {
		return column{highlightIdx: -1, entries: []fsutil.Entry{{Name: "(empty)"}}}
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
	if len(entries) == 0 {
		entries = []fsutil.Entry{{Name: "(empty)"}}
	}
	return column{entries: entries, highlightIdx: -1}
}

func (m Model) buildFilePreviewColumn(entry fsutil.Entry) column {
	fullPath := filepath.Join(m.activePath, entry.Name)
	preview, err := fsutil.ReadFilePreview(fullPath)
	if err != nil {
		return column{unreadable: true, highlightIdx: -1}
	}
	if preview.Special {
		return column{
			entries:      []fsutil.Entry{{Name: "<special file>"}},
			highlightIdx: -1,
		}
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
	var b strings.Builder
	for row := range rows {
		if row > 0 {
			b.WriteByte('\n')
		}
		i := c.start + row
		if i < 0 || i >= len(c.entries) {
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
	}
	// MaxHeight/MaxWidth bound the rendered block even if a truncated
	// double-width name still occupies more display cells than its rune
	// count suggests, so one column can never grow the whole frame past
	// height/width and push the header off-screen.
	return lipgloss.NewStyle().Width(width).MaxWidth(width).MaxHeight(rows).Render(b.String())
}

func (m Model) statusLine() string {
	hints := "↑/k ↓/j move · →/l open · ←/h up · Enter cd+exit · . hidden · r refresh · q quit"
	right := m.activePath
	isErr := m.statusErr != ""
	if isErr {
		right = m.statusErr
	}
	return composeStatusLine(hints, right, isErr, m.width)
}

// composeStatusLine lays hints on the left and statusErr-or-activePath on
// the right within width cells (spec §6), truncating the right side first
// when space is tight, and the left side too if that still doesn't fit.
func composeStatusLine(left, right string, rightIsErr bool, width int) string {
	if width <= 0 {
		return ""
	}
	leftLen := len([]rune(left))
	if leftLen >= width {
		return truncate(left, width)
	}
	avail := width - leftLen - 1 // reserve one separating space
	right = truncate(right, avail)
	rightLen := len([]rune(right))
	gap := width - leftLen - rightLen
	if gap < 0 {
		gap = 0
	}
	rendered := right
	if rightIsErr {
		rendered = errStyle.Render(right)
	}
	return left + strings.Repeat(" ", gap) + rendered
}

func truncate(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width <= 1 {
		if width <= 0 {
			return ""
		}
		return string(r[:width])
	}
	return string(r[:width-1]) + "…"
}
