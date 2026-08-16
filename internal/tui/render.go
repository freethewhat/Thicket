package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"thicket/internal/fsutil"
)

const (
	minColWidth = 15
	// paneBorderWidth/paneBorderHeight are the screen cells each pane's
	// border consumes on top of its content (left+right, top+bottom).
	paneBorderWidth  = 2
	paneBorderHeight = 2
)

var (
	dirStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	symlinkStyle  = lipgloss.NewStyle().Faint(true)
	selectedStyle = lipgloss.NewStyle().Reverse(true)
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))

	// activePaneStyle marks the pane the user is currently browsing (spec
	// pane-separation addendum): a thick, bright-yellow border sets it
	// apart from the thin, dim border on ancestor/preview panes.
	activePaneStyle   = lipgloss.NewStyle().Border(lipgloss.ThickBorder()).BorderForeground(lipgloss.Color("11")).Bold(true)
	inactivePaneStyle = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("8"))
)

// SetRenderer rebinds every package-level style to r. lipgloss.NewStyle()
// captures a reference to the *current* default renderer at call time, and
// the styles above are built as package vars — during Go's init phase,
// before main() can point lipgloss at the real terminal (thicket renders to
// a dedicated /dev/tty handle, not os.Stdout). Without this, later calls to
// lipgloss.SetDefaultRenderer have no effect on these already-built styles:
// they keep rendering against whatever renderer was default at init, which
// detects color support from os.Stdout — a pipe when invoked through the
// th wrapper's `dir=$(command thicket ...)`, so all color/border output
// silently drops even though the profile reported elsewhere is correct.
func SetRenderer(r *lipgloss.Renderer) {
	dirStyle = dirStyle.Renderer(r)
	symlinkStyle = symlinkStyle.Renderer(r)
	selectedStyle = selectedStyle.Renderer(r)
	errStyle = errStyle.Renderer(r)
	activePaneStyle = activePaneStyle.Renderer(r)
	inactivePaneStyle = inactivePaneStyle.Renderer(r)
}

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
	// active marks the pane the user is currently browsing — always the
	// current-directory column (or the sole column in single-pane mode) —
	// so renderColumn can give it a visually distinct border.
	active bool
}

func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	rows := m.visibleRows()
	header := truncate(m.activePath, m.width)
	if m.helpMode {
		return header + "\n" + m.renderHelp(rows) + "\n" + m.statusLine()
	}
	cols := m.buildColumns(rows)

	colWidth := m.width/len(cols) - paneBorderWidth
	if colWidth < minColWidth {
		colWidth = minColWidth
	}

	rendered := make([]string, len(cols))
	for i, c := range cols {
		rendered[i] = renderColumn(c, colWidth, rows)
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)

	return header + "\n" + body + "\n" + m.statusLine()
}

func (m Model) buildColumns(rows int) []column {
	maxCols := m.width / (minColWidth + paneBorderWidth)
	if maxCols < 1 {
		maxCols = 1
	}

	active := column{entries: m.activeEntries, highlightIdx: m.activeCursor, start: m.activeScroll, active: true}
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
	paneStyle := inactivePaneStyle
	if c.active {
		paneStyle = activePaneStyle
	}
	if c.unreadable {
		inner := lipgloss.NewStyle().Width(width).Height(rows).Render("[permission denied]")
		return paneStyle.Render(inner)
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
		reservedForSymlink := 0
		if e.IsSymlink {
			reservedForSymlink = 1
		}
		highlightedDir := i == c.highlightIdx && e.IsDir
		reservedForMarker := 0
		if highlightedDir {
			reservedForMarker = 2
		}
		nameWidth := width - reservedForSymlink - reservedForMarker
		text := truncate(e.Name, nameWidth)
		nameLen := len([]rune(text))
		if e.IsDir {
			text = dirStyle.Render(text)
		}
		if e.IsSymlink {
			text += symlinkStyle.Render("@")
		}
		if highlightedDir {
			// Right-align the '->' marker flush against the pane's inner
			// edge, however short the name is, rather than butting it
			// straight up against the name.
			pad := width - nameLen - reservedForSymlink - 2
			text += strings.Repeat(" ", pad) + dirStyle.Render("->")
		}
		if i == c.highlightIdx {
			text = selectedStyle.Render(text)
		}
		b.WriteString(text)
	}
	// MaxHeight/MaxWidth bound the rendered block even if a truncated
	// double-width name still occupies more display cells than its rune
	// count suggests, so one column can never grow the whole frame past
	// height/width and push the header off-screen. The border is applied
	// last so it wraps the sized content block rather than being counted
	// as part of it (spec: borders cost paneBorderWidth/Height on top of
	// content dimensions).
	inner := lipgloss.NewStyle().Width(width).MaxWidth(width).MaxHeight(rows).Render(b.String())
	return paneStyle.Render(inner)
}

func (m Model) statusLine() string {
	hints := "↑/k ↓/j move · PgUp/PgDn page · Home/End top/bottom · →/l open · ←/h up · Enter cd+exit · . hidden · r refresh · / search · ? help · q quit"
	left := hints
	right := m.activePath
	isErr := m.statusErr != ""
	if isErr {
		right = m.statusErr
	}
	if m.helpMode {
		left = "? / Esc: close help"
		right = m.activePath
		isErr = false
	} else if m.searchMode {
		// The right slot is dedicated to search state for the whole
		// session — a stale statusErr from before / was pressed is not
		// displayed (spec §6); the statusErr field itself is untouched.
		left = "/" + m.searchQuery
		right = m.activePath
		isErr = false
		if m.searchNoMatch {
			right = "no match"
			isErr = true
		}
	}
	return composeStatusLine(left, right, isErr, m.width)
}

// composeStatusLine lays hints on the left and statusErr-or-activePath on
// the right within width cells (spec §6). The right side is the
// higher-priority information — it's the only way navigation errors reach
// the user — so it always gets first claim on available space; the left
// hints are truncated (down to nothing if necessary) to make room.
func composeStatusLine(left, right string, rightIsErr bool, width int) string {
	if width <= 0 {
		return ""
	}
	right = truncate(right, width)
	rightLen := len([]rune(right))
	rendered := right
	if rightIsErr {
		rendered = errStyle.Render(right)
	}
	avail := width - rightLen - 1 // reserve one separating space
	if avail < 0 {
		avail = 0
	}
	left = truncate(left, avail)
	leftLen := len([]rune(left))
	gap := width - leftLen - rightLen
	if gap < 0 {
		gap = 0
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
