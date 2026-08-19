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
// than current on the given channel, bounded by updateCheckTimeout. A
// failed check (offline, timeout, bad response) or a check that finds no
// newer release both return a nil tea.Msg — bubbletea treats a nil Msg
// from a Cmd as a no-op, so the failure is silent by construction, not by
// a separate error-swallowing branch grafted on afterward.
func checkUpdateCmd(current, channel string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
		defer cancel()
		tag, err := latestTagFunc(ctx, channel)
		if err != nil || !update.IsNewer(current, tag) {
			return nil
		}
		return updateAvailableMsg{tag: tag}
	}
}

// dismissNoticeCmd returns a tea.Cmd that delivers clearUpdateNoticeMsg
// after d elapses, via tea.Tick. Extracted from the updateAvailableMsg
// case in update.go so tests can schedule a short dismissal instead of
// blocking on the real updateNoticeDuration.
func dismissNoticeCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return clearUpdateNoticeMsg{} })
}

// updateNoticeText formats the toast shown in the status line's right
// slot when updateAvailableMsg arrives. The label is derived from the
// channel the check ran on (Model.checkChannel, threaded through from
// Update's updateAvailableMsg handler) rather than from the tag's shape:
// a future "-rc.N" or "-alpha.N" prerelease convention would otherwise
// be mislabeled "beta" by a bare hyphen check.
func updateNoticeText(tag, channel string) string {
	label := "update available"
	if channel == update.ChannelBeta {
		label = "beta update available"
	}
	return fmt.Sprintf("%s: %s — run 'thicket-bin update'", label, tag)
}
