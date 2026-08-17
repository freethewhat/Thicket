package tui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// withStubLatestTag replaces latestTagFunc for the duration of the test
// and restores it afterward, avoiding a real network call.
func withStubLatestTag(t *testing.T, tag string, err error) {
	t.Helper()
	orig := latestTagFunc
	latestTagFunc = func(ctx context.Context) (string, error) { return tag, err }
	t.Cleanup(func() { latestTagFunc = orig })
}

func TestCheckUpdateCmd_NewerReleaseReturnsUpdateAvailableMsg(t *testing.T) {
	withStubLatestTag(t, "v99.0.0", nil)

	msg := checkUpdateCmd("v1.0.0")()

	got, ok := msg.(updateAvailableMsg)
	if !ok {
		t.Fatalf("checkUpdateCmd result = %#v, want updateAvailableMsg", msg)
	}
	if got.tag != "v99.0.0" {
		t.Errorf("tag = %q, want v99.0.0", got.tag)
	}
}

func TestCheckUpdateCmd_NoNewerReleaseReturnsNil(t *testing.T) {
	withStubLatestTag(t, "v1.0.0", nil)

	if msg := checkUpdateCmd("v1.0.0")(); msg != nil {
		t.Fatalf("checkUpdateCmd result = %#v, want nil", msg)
	}
}

func TestCheckUpdateCmd_FetchErrorReturnsNil(t *testing.T) {
	withStubLatestTag(t, "", errFakeNetwork)

	if msg := checkUpdateCmd("v1.0.0")(); msg != nil {
		t.Fatalf("checkUpdateCmd result = %#v, want nil", msg)
	}
}

func TestModel_WithUpdateCheckSetsField(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	m = m.WithUpdateCheck("v1.0.0")

	if m.checkVersion != "v1.0.0" {
		t.Errorf("checkVersion = %q, want v1.0.0", m.checkVersion)
	}
}

func TestModel_InitReturnsNilWithoutUpdateCheck(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	if cmd := m.Init(); cmd != nil {
		t.Fatal("Init(): want nil tea.Cmd when WithUpdateCheck was never called")
	}
}

func TestModel_InitReturnsNilForDevVersion(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root).WithUpdateCheck("dev")

	if cmd := m.Init(); cmd != nil {
		t.Fatal("Init(): want nil tea.Cmd for checkVersion == \"dev\"")
	}
}

func TestModel_InitReturnsCheckCmdWhenConfigured(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root).WithUpdateCheck("v1.0.0")

	if cmd := m.Init(); cmd == nil {
		t.Fatal("Init(): want non-nil tea.Cmd when WithUpdateCheck was called with a real version")
	}
}

func TestUpdate_UpdateAvailableMsgSetsNoticeAndSchedulesDismiss(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, cmd := m.Update(updateAvailableMsg{tag: "v9.9.9"})
	m = updated.(Model)

	if m.updateNotice == "" {
		t.Fatal("updateNotice: want non-empty after updateAvailableMsg")
	}
	if cmd == nil {
		t.Fatal("Update(updateAvailableMsg{...}): want non-nil tea.Cmd (the dismiss tea.Tick)")
	}
	msg := cmd()
	if _, ok := msg.(clearUpdateNoticeMsg); !ok {
		t.Fatalf("scheduled tea.Cmd produced %#v, want clearUpdateNoticeMsg (after waiting up to %s)", msg, updateNoticeDuration)
	}
}

func TestUpdate_ClearUpdateNoticeMsgClearsNotice(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.updateNotice = "update available: v9.9.9 — run 'thicket-bin update'"

	updated, _ := m.Update(clearUpdateNoticeMsg{})
	m = updated.(Model)

	if m.updateNotice != "" {
		t.Errorf("updateNotice = %q, want empty after clearUpdateNoticeMsg", m.updateNotice)
	}
}

var errFakeNetwork = errFake{"network unreachable"}

type errFake struct{ msg string }

func (e errFake) Error() string { return e.msg }

var _ = time.Second // silence unused import if trimmed later; remove if time is used elsewhere in this file
var _ tea.Msg
