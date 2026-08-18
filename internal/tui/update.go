package tui

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"thicket/internal/fsutil"
	marksPkg "thicket/internal/marks"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.clampScroll()
		if m.findMode {
			m.clampFindScroll(m.findEntryRows(m.visibleRows()))
		}
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			m.quitting = true
			m.selected = false
			return m, tea.Quit
		}
		if m.helpMode {
			switch msg.String() {
			case "?", "q", "esc":
				m.helpMode = false
			}
			return m, nil
		}
		if m.marksListMode {
			m.handleMarksListKey(msg)
			return m, nil
		}
		if m.searchMode {
			m.handleSearchKey(msg)
			return m, nil
		}
		if m.findMode {
			m.handleFindKey(msg)
			return m, nil
		}
		if m.markSetPending {
			m.handleMarkSetKey(msg)
			return m, nil
		}
		if m.markJumpPending {
			m.handleMarkJumpKey(msg)
			return m, nil
		}
		switch msg.String() {
		case "up", "k":
			m.moveCursor(-1)
		case "down", "j":
			m.moveCursor(1)
		case "pgup":
			m.moveCursor(-m.visibleRows())
		case "pgdown":
			m.moveCursor(m.visibleRows())
		case "home":
			m.moveCursor(-len(m.activeEntries))
		case "end":
			m.moveCursor(len(m.activeEntries))
		case "right", "l":
			m.handleRight()
		case "left", "h":
			m.handleLeft()
		case "enter":
			m.handleEnter()
			return m, tea.Quit
		case "q", "esc":
			m.quitting = true
			m.selected = false
			return m, tea.Quit
		case ".":
			m.showHidden = !m.showHidden
			m.reload()
		case "r":
			m.reload()
		case "/":
			m.enterSearchMode()
		case "f":
			m.enterFindMode()
		case "?":
			m.helpMode = true
		case "m":
			m.markSetPending = true
		case "`":
			m.markJumpPending = true
		case "'":
			m.enterMarksListMode()
		}
	case updateAvailableMsg:
		m.updateNotice = updateNoticeText(msg.tag)
		return m, dismissNoticeCmd(updateNoticeDuration)
	case clearUpdateNoticeMsg:
		m.updateNotice = ""
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
	m.cursorMemory[m.activePath] = m.activeCursor
	remembered, ok := m.cursorMemory[newPath]
	if !ok {
		remembered = 0
	}
	m.activePath = newPath
	m.activeEntries = entries
	switch {
	case len(entries) == 0:
		m.activeCursor = -1
	case remembered < 0:
		m.activeCursor = 0
	case remembered >= len(entries):
		// Directory shrank since cursorMemory[newPath] was recorded
		// (e.g. an entry was deleted externally) — land on the last
		// row rather than resetting to 0 or indexing out of range.
		m.activeCursor = len(entries) - 1
	default:
		m.activeCursor = remembered
	}
	m.activeScroll = 0
	m.clampScroll()
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

	m.cursorMemory[m.activePath] = m.activeCursor
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
	m.chosenPath = m.selectedDirPath()
}

// selectedDirPath returns the directory the cursor is highlighting: the
// entry under activeCursor when it's a directory (matching what the
// preview column shows), otherwise activePath itself (cursor on a file,
// or the active directory is empty). Shared by handleEnter (Enter picks
// this path) and handleMarkSetKey (m<letter> bookmarks this path) so
// both agree with what's visually highlighted rather than the listing
// one level up.
func (m *Model) selectedDirPath() string {
	if m.activeCursor >= 0 && m.activeCursor < len(m.activeEntries) && m.activeEntries[m.activeCursor].IsDir {
		return filepath.Join(m.activePath, m.activeEntries[m.activeCursor].Name)
	}
	return m.activePath
}

// enterSearchMode opens the type-ahead search prompt (spec §4). It never
// touches activeEntries/activePath — only the query state and a saved
// cursor to restore on cancel.
func (m *Model) enterSearchMode() {
	m.searchMode = true
	m.searchQuery = ""
	m.searchNoMatch = false
	m.searchPrevCursor = m.activeCursor
}

// exitSearchMode closes the prompt. If restoreCursor is true, activeCursor
// is reset to the value it had before / was pressed (Esc / empty-backspace
// behavior, spec §4); otherwise activeCursor is left wherever the search
// moved it (Enter behavior).
func (m *Model) exitSearchMode(restoreCursor bool) {
	m.searchMode = false
	m.searchQuery = ""
	m.searchNoMatch = false
	if restoreCursor {
		m.activeCursor = m.searchPrevCursor
		m.clampScroll()
	}
}

// enterFindMode runs a one-time recursive walk of activePath's subtree
// (spec §5) and opens the full-screen find-mode result list. On a walk
// error (activePath itself unreadable), statusErr is set and find mode is
// never entered — mirrors handleRight's treatment of a failed ListDir.
func (m *Model) enterFindMode() {
	results, truncated, err := fsutil.WalkSubtree(m.activePath, m.showHidden)
	if err != nil {
		m.statusErr = err.Error()
		return
	}
	m.findMode = true
	m.findQuery = ""
	m.findResults = results
	m.findTruncated = truncated
	m.findCursor = -1
	m.findScroll = 0
	if len(results) > 0 {
		m.findCursor = 0
	}
	m.statusErr = ""
}

// exitFindMode closes find mode without touching activePath/activeCursor.
// commitFindSelection (Task 5) relocates separately, before calling this;
// every other exit path (Esc, empty-backspace) never relocates.
func (m *Model) exitFindMode() {
	m.findMode = false
	m.findQuery = ""
}

// handleFindKey processes one key while findMode is true (spec §5).
// Discriminates on msg.Type, not msg.String() — see Global Constraints.
// Any msg.Type not matched by a case below (arrows other than Up/Down,
// Tab, Ctrl-U, Home, PageUp, F-keys, ...) is an explicit no-op.
func (m *Model) handleFindKey(msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyRunes:
		m.appendFindQuery(msg.Runes...)
	case tea.KeySpace:
		m.appendFindQuery(' ')
	case tea.KeyBackspace:
		if m.findQuery == "" {
			m.exitFindMode()
			return
		}
		runes := []rune(m.findQuery)
		m.findQuery = string(runes[:len(runes)-1])
		m.applyFindFilter()
	case tea.KeyUp:
		m.moveFindCursor(-1)
	case tea.KeyDown:
		m.moveFindCursor(1)
	case tea.KeyEnter:
		m.commitFindSelection()
	case tea.KeyEsc:
		m.exitFindMode()
	}
}

// appendFindQuery adds runes to findQuery and re-runs the filter. A single
// KeyMsg can carry more than one rune (bracketed paste, composed input),
// so callers pass every rune from one message, not just the first.
func (m *Model) appendFindQuery(runes ...rune) {
	m.findQuery += string(runes)
	m.applyFindFilter()
}

// applyFindFilter resets findCursor against filterWalk(findResults,
// findQuery): 0 if the filtered view is non-empty, else -1. findScroll
// resets to 0 alongside it — the filtered view just changed, so the old
// scroll window no longer corresponds to anything meaningful.
func (m *Model) applyFindFilter() {
	filtered := filterWalk(m.findResults, m.findQuery)
	m.findScroll = 0
	if len(filtered) == 0 {
		m.findCursor = -1
		return
	}
	m.findCursor = 0
}

// moveFindCursor moves findCursor by delta within the current filtered
// view, clamped at both ends, then nudges findScroll just enough to keep
// it visible. No-op if the filtered view is empty.
func (m *Model) moveFindCursor(delta int) {
	filtered := filterWalk(m.findResults, m.findQuery)
	if len(filtered) == 0 {
		return
	}
	m.findCursor += delta
	if m.findCursor < 0 {
		m.findCursor = 0
	}
	if last := len(filtered) - 1; m.findCursor > last {
		m.findCursor = last
	}
	m.clampFindScroll(m.findEntryRows(m.visibleRows()))
}

// clampFindScroll mirrors clampScroll's persisted-scroll convention for
// findScroll/findCursor: nudge the window just enough to keep findCursor
// visible within rows rows, rather than scrollStartFor's pin-to-bottom
// behavior used by the derived ancestor/preview columns, which have no
// user-driven cursor of their own.
func (m *Model) clampFindScroll(rows int) {
	if rows <= 0 || m.findCursor < 0 {
		return
	}
	if m.findCursor < m.findScroll {
		m.findScroll = m.findCursor
	}
	if m.findCursor >= m.findScroll+rows {
		m.findScroll = m.findCursor - rows + 1
	}
	if m.findScroll < 0 {
		m.findScroll = 0
	}
}

// commitFindSelection relocates activePath/activeCursor to the selected
// match's parent directory (spec §5's Enter row) and exits find mode. A
// no-op when findCursor is -1 (empty walk, or the current query matches
// nothing) — find mode stays open so the user can keep typing. If the
// relocation target's ListDir fails (the walk snapshot went stale — the
// match's parent directory was removed or its permissions changed after
// the walk but before Enter), statusErr is set and find mode is exited
// anyway, unlike the marks list's analogous Enter failure which stays
// open: re-showing the now-stale find results would be misleading, so
// this deliberately drops back to the plain listing instead.
func (m *Model) commitFindSelection() {
	filtered := filterWalk(m.findResults, m.findQuery)
	if m.findCursor < 0 || m.findCursor >= len(filtered) {
		return
	}
	we := filtered[m.findCursor]
	relDir := filepath.Dir(we.RelPath)
	newPath := m.activePath
	if relDir != "." {
		newPath = filepath.Join(m.activePath, relDir)
	}
	entries, err := fsutil.ListDir(newPath, m.showHidden)
	if err != nil {
		m.statusErr = err.Error()
		m.exitFindMode()
		return
	}
	m.activePath = newPath
	m.activeEntries = entries
	m.activeCursor = fsutil.IndexOfName(entries, we.Name)
	if m.activeCursor < 0 {
		m.activeCursor = 0
		if len(entries) == 0 {
			m.activeCursor = -1
		}
	}
	m.activeScroll = 0
	m.clampScroll()
	m.statusErr = ""
	m.exitFindMode()
}

// handleSearchKey processes one key while searchMode is true (spec §4).
// Discriminates on msg.Type, not msg.String() — see Global Constraints.
// Any msg.Type not matched by a case below (arrows, Tab, Ctrl-U, Home,
// PageUp, F-keys, ...) is an explicit no-op: Go's switch already does
// nothing when no case matches, which is exactly the spec §4 catch-all
// rule.
func (m *Model) handleSearchKey(msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyRunes:
		m.appendQuery(msg.Runes...)
	case tea.KeySpace:
		m.appendQuery(' ')
	case tea.KeyBackspace:
		if m.searchQuery == "" {
			m.exitSearchMode(true)
			return
		}
		runes := []rune(m.searchQuery)
		m.searchQuery = string(runes[:len(runes)-1])
		m.applySearchMatch()
	case tea.KeyEnter:
		m.exitSearchMode(false)
	case tea.KeyEsc:
		m.exitSearchMode(true)
	}
}

// appendQuery adds runes to searchQuery and re-runs the match. A single
// KeyMsg can carry more than one rune (bracketed paste, composed input),
// so callers pass every rune from one message, not just the first.
func (m *Model) appendQuery(runes ...rune) {
	m.searchQuery += string(runes)
	m.applySearchMatch()
}

// applySearchMatch re-jumps activeCursor to firstMatch(activeEntries,
// searchQuery), always searching from the top of the list (spec §5). On no
// match, activeCursor is left unchanged and searchNoMatch is set.
func (m *Model) applySearchMatch() {
	i := firstMatch(m.activeEntries, m.searchQuery)
	if i < 0 {
		m.searchNoMatch = true
		return
	}
	m.activeCursor = i
	m.searchNoMatch = false
	m.clampScroll()
}

// enterMarksListMode opens the full-screen marks list (spec §5, ').
// marksCursor is recomputed every time the list opens, mirroring the
// -1-when-empty convention activeCursor already uses.
func (m *Model) enterMarksListMode() {
	m.marksListMode = true
	m.marksCursor = marksListCursorFor(m.markTable)
}

// singleLetterRune reports whether msg carries exactly one ASCII letter
// rune (a-z or A-Z) via tea.KeyRunes — the shape a single mark-letter
// keystroke takes. Discriminating on msg.Type, not msg.String(), mirrors
// handleSearchKey's convention (see Global Constraints): String() returns
// a name for every recognized key ("tab", "pgdown", ...), which a
// string-based check could mistake for a letter-shaped case.
func singleLetterRune(msg tea.KeyMsg) (rune, bool) {
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		return 0, false
	}
	r := msg.Runes[0]
	if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
		return 0, false
	}
	return r, true
}

// handleMarkSetKey processes one key while markSetPending is true (spec
// §5's mark-set table). Any key that isn't a single a-z/A-Z rune cancels
// without mutation.
func (m *Model) handleMarkSetKey(msg tea.KeyMsg) {
	m.markSetPending = false
	r, ok := singleLetterRune(msg)
	if !ok {
		return
	}
	m.markTable[r] = m.selectedDirPath()
	if err := marksPkg.Save(m.marksPath, m.markTable); err != nil {
		m.statusErr = err.Error()
		return
	}
	m.statusErr = ""
}

// handleMarkJumpKey processes one key while markJumpPending is true (spec
// §5's mark-jump table).
func (m *Model) handleMarkJumpKey(msg tea.KeyMsg) {
	m.markJumpPending = false
	r, ok := singleLetterRune(msg)
	if !ok {
		return
	}
	target, known := m.markTable[r]
	if !known {
		m.statusErr = "no mark: " + string(r)
		return
	}
	m.jumpToMark(r, target)
}

// jumpToMark navigates to target (the directory marked by r) on success,
// or sets statusErr (prefixed with the mark letter, same shape as
// handleRight's permission-denied handling) and leaves activePath
// untouched on failure. Reports whether the jump succeeded so callers can
// decide whether to close their own mode — markJumpPending always clears
// regardless, but the marks list (spec §5) only closes on success.
func (m *Model) jumpToMark(r rune, target string) bool {
	entries, err := fsutil.ListDir(target, m.showHidden)
	if err != nil {
		m.statusErr = "mark " + string(r) + ": " + err.Error()
		return false
	}
	m.activePath = target
	m.activeEntries = entries
	m.activeCursor = 0
	if len(entries) == 0 {
		m.activeCursor = -1
	}
	m.activeScroll = 0
	m.statusErr = ""
	return true
}

// handleMarksListKey processes one key while marksListMode is true (spec
// §5's marks-list table). Any msg.String() not matched below is an
// explicit no-op — Go's switch already does nothing when no case matches.
func (m *Model) handleMarksListKey(msg tea.KeyMsg) {
	switch msg.String() {
	case "up", "k":
		m.moveMarksCursor(-1)
	case "down", "j":
		m.moveMarksCursor(1)
	case "enter":
		m.activateMarksListEntry()
	case "d":
		m.deleteMarksListEntry()
	case "q", "esc":
		m.marksListMode = false
	}
}

func (m *Model) moveMarksCursor(delta int) {
	n := len(m.markTable)
	if n == 0 {
		return
	}
	m.marksCursor += delta
	if m.marksCursor < 0 {
		m.marksCursor = 0
	}
	if last := n - 1; m.marksCursor > last {
		m.marksCursor = last
	}
}

func (m *Model) activateMarksListEntry() {
	letters := sortedMarkLetters(m.markTable)
	if m.marksCursor < 0 || m.marksCursor >= len(letters) {
		return
	}
	r := letters[m.marksCursor]
	if m.jumpToMark(r, m.markTable[r]) {
		m.marksListMode = false
	}
}

// deleteMarksListEntry deletes the highlighted mark and persists the
// change. On a Save error, the deletion stays in memory (matching the
// disk/memory-diverges tradeoff handleMarkSetKey already accepts) and
// marksCursor is left untouched so the error is visible against the row
// that's still selected. On success, marksCursor is clamped the same way
// reload() clamps activeCursor elsewhere in this file: preserved if it's
// still a valid index into the shrunk table, reset to 0 only if the
// deleted row was its last valid position (see Global Constraints'
// ruling — this is deliberately not a call to marksListCursorFor, which
// has no way to preserve a previous position).
func (m *Model) deleteMarksListEntry() {
	letters := sortedMarkLetters(m.markTable)
	if m.marksCursor < 0 || m.marksCursor >= len(letters) {
		return
	}
	r := letters[m.marksCursor]
	delete(m.markTable, r)
	if err := marksPkg.Save(m.marksPath, m.markTable); err != nil {
		m.statusErr = err.Error()
		return
	}
	m.statusErr = ""
	if len(m.markTable) == 0 {
		m.marksCursor = -1
	} else if m.marksCursor >= len(m.markTable) {
		m.marksCursor = 0
	}
}
