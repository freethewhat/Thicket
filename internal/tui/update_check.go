package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"thicket/internal/update"
)

// updateCheckTimeout bounds how long the on-launch update check waits for
// a response before giving up silently.
const updateCheckTimeout = 2 * time.Second

// updateNoticeDuration is how long the update-available toast stays in
// the status line before auto-dismissing.
const updateNoticeDuration = 5 * time.Second

// latestTagFunc is update.LatestTag by default; tests reassign it to a
// stub to avoid a real network call. Deliberately package-level (not a
// Model field) since it configures a process-wide capability, not
// per-session state.
var latestTagFunc = update.LatestTag

// updateAvailableMsg carries the newer release's tag once checkUpdateCmd's
// check succeeds and finds current out of date.
type updateAvailableMsg struct{ tag string }

// clearUpdateNoticeMsg dismisses the toast set by updateAvailableMsg,
// delivered by the tea.Tick started when the toast is shown.
type clearUpdateNoticeMsg struct{}

// checkUpdateCmd returns a tea.Cmd that checks GitHub for a release newer
// than current, bounded by updateCheckTimeout. A failed check (offline,
// timeout, bad response) or a check that finds no newer release both
// return a nil tea.Msg — bubbletea treats a nil Msg from a Cmd as a
// no-op, so the failure is silent by construction, not by a separate
// error-swallowing branch grafted on afterward.
func checkUpdateCmd(current string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
		defer cancel()
		tag, err := latestTagFunc(ctx)
		if err != nil || !update.IsNewer(current, tag) {
			return nil
		}
		return updateAvailableMsg{tag: tag}
	}
}

// updateNoticeText formats the toast shown in the status line's right
// slot when updateAvailableMsg arrives.
func updateNoticeText(tag string) string {
	return fmt.Sprintf("update available: %s — run 'thicket-bin update'", tag)
}
