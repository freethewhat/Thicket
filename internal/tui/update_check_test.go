package tui

import (
	"context"
	"strings"
	"testing"
	"time"
)

// withStubLatestTag replaces latestTagFunc for the duration of the test
// and restores it afterward, avoiding a real network call.
func withStubLatestTag(t *testing.T, tag string, err error) {
	t.Helper()
	orig := latestTagFunc
	latestTagFunc = func(ctx context.Context, channel string) (string, error) { return tag, err }
	t.Cleanup(func() { latestTagFunc = orig })
}

func TestCheckUpdateCmd_NewerReleaseReturnsUpdateAvailableMsg(t *testing.T) {
	withStubLatestTag(t, "v99.0.0", nil)

	msg := checkUpdateCmd("v1.0.0", "")()

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

	if msg := checkUpdateCmd("v1.0.0", "")(); msg != nil {
		t.Fatalf("checkUpdateCmd result = %#v, want nil", msg)
	}
}

func TestCheckUpdateCmd_FetchErrorReturnsNil(t *testing.T) {
	withStubLatestTag(t, "", errFakeNetwork)

	if msg := checkUpdateCmd("v1.0.0", "")(); msg != nil {
		t.Fatalf("checkUpdateCmd result = %#v, want nil", msg)
	}
}

func TestCheckUpdateCmd_PassesChannelToLatestTagFunc(t *testing.T) {
	orig := latestTagFunc
	var gotChannel string
	latestTagFunc = func(ctx context.Context, channel string) (string, error) {
		gotChannel = channel
		return "", errFakeNetwork
	}
	t.Cleanup(func() { latestTagFunc = orig })

	checkUpdateCmd("v1.0.0", "beta")()

	if gotChannel != "beta" {
		t.Errorf("channel passed to latestTagFunc = %q, want beta", gotChannel)
	}
}

func TestUpdateNoticeText_StableTagHasNoLabel(t *testing.T) {
	got := updateNoticeText("v1.2.3")
	if strings.Contains(got, "beta") {
		t.Errorf("updateNoticeText(%q) = %q, want no beta label", "v1.2.3", got)
	}
	if !strings.Contains(got, "update available: v1.2.3") {
		t.Errorf("updateNoticeText(%q) = %q, missing tag", "v1.2.3", got)
	}
}

func TestUpdateNoticeText_PrereleaseTagLabelsBeta(t *testing.T) {
	got := updateNoticeText("v1.3.0-beta.1")
	if !strings.Contains(got, "beta update available: v1.3.0-beta.1") {
		t.Errorf("updateNoticeText(%q) = %q, want a beta-labeled notice", "v1.3.0-beta.1", got)
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

func TestModel_WithChannelSetsField(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	m = m.WithChannel("beta")

	if m.checkChannel != "beta" {
		t.Errorf("checkChannel = %q, want beta", m.checkChannel)
	}
}

func TestModel_InitPassesConfiguredChannelToLatestTagFunc(t *testing.T) {
	root := setupFixture(t)
	orig := latestTagFunc
	var gotChannel string
	latestTagFunc = func(ctx context.Context, channel string) (string, error) {
		gotChannel = channel
		return "", errFakeNetwork
	}
	t.Cleanup(func() { latestTagFunc = orig })

	m := newTestModel(t, root).WithUpdateCheck("v1.0.0").WithChannel("beta")
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init(): want non-nil tea.Cmd")
	}
	cmd()

	if gotChannel != "beta" {
		t.Errorf("channel passed to latestTagFunc via Init = %q, want beta", gotChannel)
	}
}

func TestModel_InitDefaultsToStableChannelWhenUnset(t *testing.T) {
	root := setupFixture(t)
	orig := latestTagFunc
	var gotChannel string
	latestTagFunc = func(ctx context.Context, channel string) (string, error) {
		gotChannel = channel
		return "", errFakeNetwork
	}
	t.Cleanup(func() { latestTagFunc = orig })

	m := newTestModel(t, root).WithUpdateCheck("v1.0.0")
	m.Init()()

	if gotChannel != "" {
		t.Errorf("channel passed to latestTagFunc via Init = %q, want empty (stable default)", gotChannel)
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
}

func TestUpdateNoticeDuration_IsFiveSeconds(t *testing.T) {
	if updateNoticeDuration != 5*time.Second {
		t.Errorf("updateNoticeDuration = %s, want 5s", updateNoticeDuration)
	}
}

func TestDismissNoticeCmd_ProducesClearUpdateNoticeMsgAfterDuration(t *testing.T) {
	msg := dismissNoticeCmd(time.Millisecond)()
	if _, ok := msg.(clearUpdateNoticeMsg); !ok {
		t.Fatalf("dismissNoticeCmd(...)() = %#v, want clearUpdateNoticeMsg", msg)
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
