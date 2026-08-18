package tui

import (
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"thicket/internal/clipboard"
)

// clipboardCopyFunc is clipboard.Copy by default; tests reassign it to a
// stub, mirroring update_check.go's latestTagFunc pattern.
var clipboardCopyFunc = clipboard.Copy

// yankNoticeDuration is how long the yank confirmation toast stays in the
// status line before auto-dismissing.
const yankNoticeDuration = 3 * time.Second

// clearYankNoticeMsg dismisses the toast set by a successful yank,
// delivered by the tea.Tick started when the toast is shown. gen is the
// yankGen the tick was scheduled for; update.go's handler ignores the
// message unless gen still matches the model's current yankGen, so a
// stale tick from a superseded yank cannot clear a newer notice.
type clearYankNoticeMsg struct{ gen int }

// yankEntryPath returns the highlighted entry's absolute path — any
// entry type, file or directory, unlike selectedDirPath (update.go),
// which only descends into directories and is used by Enter/mark-set —
// or activePath itself when activeCursor == -1 (empty directory).
func (m Model) yankEntryPath() string {
	if m.activeCursor == -1 {
		return m.activePath
	}
	return filepath.Join(m.activePath, m.activeEntries[m.activeCursor].Name)
}

// yankEntry copies yankEntryPath() to the clipboard (y).
func (m *Model) yankEntry() tea.Cmd {
	return m.yank(m.yankEntryPath())
}

// yankDir copies the active directory's own path to the clipboard (Y),
// regardless of cursor position.
func (m *Model) yankDir() tea.Cmd {
	return m.yank(m.activePath)
}

// yank writes path via clipboardCopyFunc. On success, yankNotice is set
// (statusErr cleared), yankGen is incremented, and a dismiss timer
// carrying the new generation is scheduled. On failure, statusErr is set
// (yankNotice cleared) — mutually exclusive, matching every other
// statusErr-setting path in update.go.
func (m *Model) yank(path string) tea.Cmd {
	if err := clipboardCopyFunc(path); err != nil {
		m.statusErr = "yank: " + err.Error()
		m.yankNotice = ""
		return nil
	}
	m.statusErr = ""
	m.yankNotice = "yanked: " + path
	m.yankGen++
	return dismissYankNoticeCmd(yankNoticeDuration, m.yankGen)
}

// dismissYankNoticeCmd returns a tea.Cmd that delivers
// clearYankNoticeMsg{gen: gen} after d elapses, via tea.Tick.
func dismissYankNoticeCmd(d time.Duration, gen int) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return clearYankNoticeMsg{gen: gen} })
}
