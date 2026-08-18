# Clipboard Yank Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `y` (yank highlighted entry's path) and `Y` (yank active directory's path) normal-mode keys that copy an absolute path to the system clipboard via a new `internal/clipboard` subprocess package, without exiting thicket or touching the filesystem.

**Architecture:** New leaf package `internal/clipboard` shells out to whichever of `wl-copy`/`xclip`/`xsel` is on `$PATH` (stdlib `os/exec` only, zero new `go.mod` entries), trying every installed mechanism in preference order until one succeeds. `internal/tui` wires `y`/`Y` as direct-action normal-mode keys (no new modal state) that call `clipboard.Copy` through an injectable package-level var, setting a transient self-dismissing status-line notice on success or `statusErr` on failure — both patterns already established by the marks feature and the update-check toast, respectively.

**Tech Stack:** Go 1.24.6, `os/exec` (stdlib), existing `bubbletea`/`lipgloss` TUI stack. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-08-18-thicket-clipboard-yank-design.md`

## Global Constraints

- Zero new `go.mod` entries — `internal/clipboard` uses only stdlib `os/exec`.
- No goroutines/channels anywhere in `internal/tui` — the clipboard write itself runs synchronously inline (same as `marks.Save`/`fsutil.ListDir`); only the yank-notice *dismissal* uses `tea.Tick`, per `AGENTS.md`'s Concurrency bullet.
- `Update()` must remain unit-testable without a real display server: `clipboardCopyFunc` is a package-level var in `internal/tui`, reassigned to a stub in tests, mirroring `latestTagFunc`.
- `internal/clipboard.Copy` must be testable without a real X11/Wayland session: tests exercise the real subprocess path via fake executable stub scripts prepended onto a test-local `$PATH`.
- Each candidate mechanism attempt is bounded by `copyTimeout` (1s, `const`, not injectable — see spec §10) individually, not the whole `Copy` call.
- Every doc edit point (`internal/tui/help.go` Keybindings, `README.md`, `man/thicket.1`, `AGENTS.md`) gets the new keys — these are hand-synced per the existing project convention, not generated from one source beyond `help.go`↔`cmd/thicket --help`.

---

## File Structure

- Create: `internal/clipboard/clipboard.go` — `Copy`, `candidateCommands`, `runCopy`, `ErrNoMechanism`, `copyTimeout`.
- Create: `internal/clipboard/clipboard_test.go` — mechanism-selection/fallback/timeout/error tests via PATH-stub scripts.
- Create: `internal/tui/clipboard.go` — `clipboardCopyFunc` var, `yankNoticeDuration` const, `clearYankNoticeMsg` type, `yankEntryPath`/`yankEntry`/`yankDir`/`yank`/`dismissYankNoticeCmd`.
- Modify: `internal/tui/model.go` — one new `yankNotice string` field.
- Modify: `internal/tui/update.go` — two new normal-mode key cases (`y`, `Y`), one new `clearYankNoticeMsg` case.
- Modify: `internal/tui/update_test.go` — new `y`/`Y` behavior tests, `withStubClipboardCopy` helper.
- Modify: `internal/tui/render.go` — `statusLine()` gains a `yankNotice` precedence tier; hint string gains `· y yank · Y yank dir`.
- Modify: `internal/tui/render_test.go` — new status-line precedence tests, new help-screen content test.
- Modify: `internal/tui/help.go` — `Keybindings` gains two rows (`y`, `Y`).
- Modify: `README.md`, `man/thicket.1`, `AGENTS.md` — doc sync (no automated test; manual convention per `AGENTS.md`'s Testing & QA section).

---

### Task 1: `internal/clipboard` package

**Files:**
- Create: `internal/clipboard/clipboard.go`
- Create: `internal/clipboard/clipboard_test.go`

**Interfaces:**
- Produces: `clipboard.Copy(text string) error`, `clipboard.ErrNoMechanism` — consumed by Task 2's `internal/tui/clipboard.go`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/clipboard/clipboard_test.go
package clipboard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeStub creates an executable shell script named name inside dir,
// running body as its only statement.
func writeStub(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	content := "#!/bin/sh\n" + body
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func TestCopy_PrefersWlCopyWhenWaylandDisplaySet(t *testing.T) {
	binDir := t.TempDir()
	wlOut := filepath.Join(t.TempDir(), "wl-out")
	xclipOut := filepath.Join(t.TempDir(), "xclip-out")
	writeStub(t, binDir, "wl-copy", fmt.Sprintf("cat > %q\n", wlOut))
	writeStub(t, binDir, "xclip", fmt.Sprintf("cat > %q\n", xclipOut))
	t.Setenv("PATH", binDir)
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")

	if err := Copy("hello"); err != nil {
		t.Fatalf("Copy() error = %v, want nil", err)
	}
	got, err := os.ReadFile(wlOut)
	if err != nil {
		t.Fatalf("wl-copy was not invoked: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("wl-copy stdin = %q, want %q", got, "hello")
	}
	if _, err := os.Stat(xclipOut); !os.IsNotExist(err) {
		t.Fatalf("xclip should not have been invoked when wl-copy succeeds")
	}
}

func TestCopy_PrefersXclipWhenWaylandDisplayUnset(t *testing.T) {
	binDir := t.TempDir()
	wlOut := filepath.Join(t.TempDir(), "wl-out")
	xclipOut := filepath.Join(t.TempDir(), "xclip-out")
	writeStub(t, binDir, "wl-copy", fmt.Sprintf("cat > %q\n", wlOut))
	writeStub(t, binDir, "xclip", fmt.Sprintf("cat > %q\n", xclipOut))
	t.Setenv("PATH", binDir)
	t.Setenv("WAYLAND_DISPLAY", "")

	if err := Copy("hello"); err != nil {
		t.Fatalf("Copy() error = %v, want nil", err)
	}
	if _, err := os.ReadFile(xclipOut); err != nil {
		t.Fatalf("xclip was not invoked: %v", err)
	}
	if _, err := os.Stat(wlOut); !os.IsNotExist(err) {
		t.Fatalf("wl-copy should not have been invoked when WAYLAND_DISPLAY is unset")
	}
}

func TestCopy_FallsBackWhenPreferredMechanismExitsNonZero(t *testing.T) {
	binDir := t.TempDir()
	xclipOut := filepath.Join(t.TempDir(), "xclip-out")
	writeStub(t, binDir, "wl-copy", "exit 1\n")
	writeStub(t, binDir, "xclip", fmt.Sprintf("cat > %q\n", xclipOut))
	t.Setenv("PATH", binDir)
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")

	if err := Copy("hello"); err != nil {
		t.Fatalf("Copy() error = %v, want nil (should fall back to xclip)", err)
	}
	got, err := os.ReadFile(xclipOut)
	if err != nil || string(got) != "hello" {
		t.Fatalf("xclip stdin = %q, %v, want %q, nil", got, err, "hello")
	}
}

func TestCopy_FallsBackWhenPreferredMechanismTimesOut(t *testing.T) {
	binDir := t.TempDir()
	xclipOut := filepath.Join(t.TempDir(), "xclip-out")
	writeStub(t, binDir, "wl-copy", "sleep 5\n")
	writeStub(t, binDir, "xclip", fmt.Sprintf("cat > %q\n", xclipOut))
	t.Setenv("PATH", binDir)
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")

	if err := Copy("hello"); err != nil {
		t.Fatalf("Copy() error = %v, want nil (should fall back to xclip after wl-copy times out)", err)
	}
	if got, err := os.ReadFile(xclipOut); err != nil || string(got) != "hello" {
		t.Fatalf("xclip stdin = %q, %v, want %q, nil", got, err, "hello")
	}
}

func TestCopy_AllMechanismsFailReturnsJoinedError(t *testing.T) {
	binDir := t.TempDir()
	writeStub(t, binDir, "wl-copy", "exit 1\n")
	writeStub(t, binDir, "xclip", "exit 1\n")
	t.Setenv("PATH", binDir)
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")

	err := Copy("hello")
	if err == nil {
		t.Fatal("Copy() error = nil, want non-nil when every found mechanism fails")
	}
	if errors.Is(err, ErrNoMechanism) {
		t.Fatalf("Copy() error = %v, want a run failure, not ErrNoMechanism (mechanisms were found)", err)
	}
}

func TestCopy_NoMechanismOnPathReturnsErrNoMechanism(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	err := Copy("hello")
	if !errors.Is(err, ErrNoMechanism) {
		t.Fatalf("Copy() error = %v, want ErrNoMechanism", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/clipboard/... -v`
Expected: FAIL — package `internal/clipboard` does not exist yet (build failure).

- [ ] **Step 3: Write the implementation**

```go
// internal/clipboard/clipboard.go
package clipboard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ErrNoMechanism is returned when none of wl-copy/xclip/xsel is found on
// $PATH.
var ErrNoMechanism = errors.New("no clipboard mechanism found (install xclip, wl-copy, or xsel)")

// copyTimeout bounds each individual mechanism attempt, not the whole
// Copy call — see Copy's doc comment.
const copyTimeout = 1 * time.Second

// candidateCommands returns the ordered list of clipboard-helper argv
// slices to try, based on whether $WAYLAND_DISPLAY is set. Every
// mechanism found on $PATH is attempted in this order, not just the
// first — see Copy's doc comment.
func candidateCommands(waylandDisplay string) [][]string {
	wl := []string{"wl-copy"}
	xclip := []string{"xclip", "-selection", "clipboard"}
	xsel := []string{"xsel", "--clipboard", "--input"}
	if waylandDisplay != "" {
		return [][]string{wl, xclip, xsel}
	}
	return [][]string{xclip, xsel, wl}
}

// Copy writes text to the system clipboard, trying every mechanism found
// on PATH in preference order until one succeeds: wl-copy, xclip, xsel
// when $WAYLAND_DISPLAY is set, else xclip, xsel, wl-copy. A
// preferred-but-non-functional helper (e.g. wl-copy present via XWayland
// with no running Wayland compositor) does not mask a working fallback —
// mixed X11/Wayland setups are the common case this guards against. Each
// candidate is bounded individually by copyTimeout, so one hung mechanism
// yields to the next rather than blocking the whole call indefinitely.
// Returns ErrNoMechanism if none of the three binaries is on PATH;
// otherwise, if every found mechanism's attempt failed, returns their
// combined errors via errors.Join.
func Copy(text string) error {
	commands := candidateCommands(os.Getenv("WAYLAND_DISPLAY"))
	var found bool
	var errs []error
	for _, argv := range commands {
		path, err := exec.LookPath(argv[0])
		if err != nil {
			continue
		}
		found = true
		if err := runCopy(path, argv[1:], text); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", argv[0], err))
			continue
		}
		return nil
	}
	if !found {
		return ErrNoMechanism
	}
	return errors.Join(errs...)
}

// runCopy runs path with the given args, writing text to its stdin,
// bounded by copyTimeout.
func runCopy(path string, args []string, text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), copyTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/clipboard/... -v`
Expected: PASS (all six tests). `TestCopy_FallsBackWhenPreferredMechanismTimesOut` takes ~1s of real wall-clock time — expected, per the spec's decision not to add an injectable-timeout seam.

- [ ] **Step 5: Commit**

```bash
git add internal/clipboard/
git commit -m "feat: add internal/clipboard package for yank feature"
```

---

### Task 2: `internal/tui` model field, clipboard.go, key handling

**Files:**
- Create: `internal/tui/clipboard.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/update_test.go`

**Interfaces:**
- Consumes: `clipboard.Copy(text string) error` (Task 1).
- Produces: `Model.yankNotice` field, `clipboardCopyFunc` var, `(m Model) yankEntryPath() string`, `(m *Model) yankEntry() tea.Cmd`, `(m *Model) yankDir() tea.Cmd` — consumed by Task 3's `statusLine()`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/update_test.go`:

```go
// withStubClipboardCopy replaces clipboardCopyFunc for the duration of
// the test and restores it afterward, avoiding a real subprocess call.
func withStubClipboardCopy(t *testing.T, fn func(string) error) {
	t.Helper()
	orig := clipboardCopyFunc
	clipboardCopyFunc = fn
	t.Cleanup(func() { clipboardCopyFunc = orig })
}

func TestUpdate_YCopiesHighlightedEntryPath(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	var got string
	withStubClipboardCopy(t, func(text string) error {
		got = text
		return nil
	})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = updated.(Model)

	want := filepath.Join(root, "sub")
	if got != want {
		t.Fatalf("clipboardCopyFunc got %q, want %q", got, want)
	}
	if m.yankNotice == "" {
		t.Fatal("yankNotice: want non-empty after successful yank")
	}
}

func TestUpdate_YCopiesFileEntryPath(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.activeCursor = fsutil.IndexOfName(m.activeEntries, "file.txt")
	var got string
	withStubClipboardCopy(t, func(text string) error {
		got = text
		return nil
	})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = updated.(Model)

	want := filepath.Join(root, "file.txt")
	if got != want {
		t.Fatalf("clipboardCopyFunc got %q, want %q (must yank the file itself, unlike selectedDirPath)", got, want)
	}
}

func TestUpdate_YOnEmptyDirectoryCopiesActivePath(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	grand := filepath.Join(root, "sub", "grand")
	m.activePath = grand
	m.activeEntries = nil
	m.activeCursor = -1
	var got string
	withStubClipboardCopy(t, func(text string) error {
		got = text
		return nil
	})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = updated.(Model)

	if got != grand {
		t.Fatalf("clipboardCopyFunc got %q, want %q (activePath fallback on empty dir)", got, grand)
	}
}

func TestUpdate_CapitalYAlwaysCopiesActivePathRegardlessOfCursor(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root) // default cursor is on "sub", a directory
	var got string
	withStubClipboardCopy(t, func(text string) error {
		got = text
		return nil
	})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Y")})
	m = updated.(Model)

	if got != root {
		t.Fatalf("clipboardCopyFunc got %q, want %q (Y always yanks activePath)", got, root)
	}
}

func TestUpdate_YSetsYankNoticeAndClearsStatusErr(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.statusErr = "stale error"
	withStubClipboardCopy(t, func(string) error { return nil })

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = updated.(Model)

	if m.statusErr != "" {
		t.Fatalf("statusErr = %q, want empty after successful yank", m.statusErr)
	}
	if m.yankNotice == "" {
		t.Fatal("yankNotice: want non-empty after successful yank")
	}
	if cmd == nil {
		t.Fatal("Update(y): want non-nil tea.Cmd (the dismiss tea.Tick)")
	}
}

func TestUpdate_YFailureSetsStatusErrAndClearsYankNotice(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.yankNotice = "old notice"
	withStubClipboardCopy(t, func(string) error { return errFakeNetwork })

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = updated.(Model)

	if m.yankNotice != "" {
		t.Fatalf("yankNotice = %q, want empty after failed yank", m.yankNotice)
	}
	if !strings.Contains(m.statusErr, "yank:") || !strings.Contains(m.statusErr, errFakeNetwork.Error()) {
		t.Fatalf("statusErr = %q, want it to contain %q and %q", m.statusErr, "yank:", errFakeNetwork.Error())
	}
	if cmd != nil {
		t.Fatal("Update(y) on failure: want nil tea.Cmd")
	}
}

func TestUpdate_ClearYankNoticeMsgClearsYankNotice(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.yankNotice = "yanked: /some/path"

	updated, _ := m.Update(clearYankNoticeMsg{})
	m = updated.(Model)

	if m.yankNotice != "" {
		t.Errorf("yankNotice = %q, want empty after clearYankNoticeMsg", m.yankNotice)
	}
}

func TestYankNoticeDuration_IsThreeSeconds(t *testing.T) {
	if yankNoticeDuration != 3*time.Second {
		t.Errorf("yankNoticeDuration = %s, want 3s", yankNoticeDuration)
	}
}

func TestDismissYankNoticeCmd_ProducesClearYankNoticeMsgAfterDuration(t *testing.T) {
	msg := dismissYankNoticeCmd(time.Millisecond)()
	if _, ok := msg.(clearYankNoticeMsg); !ok {
		t.Fatalf("dismissYankNoticeCmd(...)() = %#v, want clearYankNoticeMsg", msg)
	}
}
```

`errFakeNetwork`/`errFake` already exist in `internal/tui/update_check_test.go` (package-level, reusable as-is). `internal/tui/update_test.go`'s current import block (`fmt`, `os`, `path/filepath`, `reflect`, `strings`, `testing`, plus `tea`/`fsutil`/`marksPkg`) does **not** include `"time"` — add it as a new stdlib import for `TestYankNoticeDuration_IsThreeSeconds`/`TestDismissYankNoticeCmd_ProducesClearYankNoticeMsgAfterDuration`'s `3*time.Second`/`time.Millisecond` usage.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/... -run 'TestUpdate_Y|TestUpdate_CapitalY|TestYankNoticeDuration|TestDismissYankNoticeCmd|TestUpdate_ClearYankNoticeMsg' -v`
Expected: FAIL — `clipboardCopyFunc`, `yankNotice`, `clearYankNoticeMsg`, `yankNoticeDuration`, `dismissYankNoticeCmd` all undefined.

- [ ] **Step 3: Write the implementation**

In `internal/tui/model.go`, add the field to the `Model` struct (immediately after the `marksCursor` field, before `width`):

```go
	// yankNotice: transient status-line confirmation shown after a
	// successful y/Y (spec docs/superpowers/specs/2026-08-18-thicket-clipboard-yank-design.md).
	// Self-clears via a tea.Tick-scheduled clearYankNoticeMsg, mirroring
	// updateNotice's shape. Unlike searchMode/helpMode/findMode/
	// markSetPending/markJumpPending/marksListMode, this is not a modal
	// state — y/Y are direct-action keys with no follow-up keystroke to
	// capture, so no *Pending/*Mode field is needed alongside it.
	yankNotice string
```

Create `internal/tui/clipboard.go`:

```go
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
// delivered by the tea.Tick started when the toast is shown.
type clearYankNoticeMsg struct{}

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
// (statusErr cleared) and a dismiss timer is scheduled. On failure,
// statusErr is set (yankNotice cleared) — mutually exclusive, matching
// every other statusErr-setting path in update.go.
func (m *Model) yank(path string) tea.Cmd {
	if err := clipboardCopyFunc(path); err != nil {
		m.statusErr = "yank: " + err.Error()
		m.yankNotice = ""
		return nil
	}
	m.statusErr = ""
	m.yankNotice = "yanked: " + path
	return dismissYankNoticeCmd(yankNoticeDuration)
}

// dismissYankNoticeCmd returns a tea.Cmd that delivers clearYankNoticeMsg
// after d elapses, via tea.Tick.
func dismissYankNoticeCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return clearYankNoticeMsg{} })
}
```

In `internal/tui/update.go`, add two cases to the existing normal-mode `switch msg.String()` block (alongside `case "'":`):

```go
	case "y":
		return m, m.yankEntry()
	case "Y":
		return m, m.yankDir()
```

And one new case in `Update`'s outer `switch msg := msg.(type)`, alongside the existing `case clearUpdateNoticeMsg:`:

```go
	case clearYankNoticeMsg:
		m.yankNotice = ""
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/... -v`
Expected: PASS for all new tests and every pre-existing test in the package (no regressions).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/clipboard.go internal/tui/model.go internal/tui/update.go internal/tui/update_test.go
git commit -m "feat: wire y/Y clipboard-yank keys into internal/tui"
```

---

### Task 3: Rendering — status line precedence, hints, help screen

**Files:**
- Modify: `internal/tui/render.go`
- Modify: `internal/tui/render_test.go`
- Modify: `internal/tui/help.go`

**Interfaces:**
- Consumes: `Model.yankNotice` (Task 2).

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/render_test.go`:

```go
func TestView_StatusLineShowsYankNotice(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 150
	m.yankNotice = "yanked: " + filepath.Join(root, "sub")

	if !strings.Contains(m.statusLine(), "yanked:") {
		t.Fatalf("statusLine() missing yankNotice: %q", m.statusLine())
	}
}

func TestView_StatusLineStatusErrTakesPrecedenceOverYankNotice(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 150
	m.yankNotice = "yanked: /some/path"
	m.statusErr = "open /x: permission denied"

	line := m.statusLine()
	if !strings.Contains(line, "permission denied") {
		t.Fatalf("statusLine() missing statusErr: %q", line)
	}
	if strings.Contains(line, "yanked:") {
		t.Fatalf("statusLine() should not show yankNotice while statusErr is set: %q", line)
	}
}

func TestView_StatusLineYankNoticeTakesPrecedenceOverUpdateNotice(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 150
	m.updateNotice = "update available: v9.9.9 — run 'thicket-bin update'"
	m.yankNotice = "yanked: /some/path"

	line := m.statusLine()
	if !strings.Contains(line, "yanked:") {
		t.Fatalf("statusLine() missing yankNotice: %q", line)
	}
	if strings.Contains(line, "update available") {
		t.Fatalf("statusLine() should not show updateNotice while yankNotice is set: %q", line)
	}
}

func TestView_HelpScreenListsYankKeybindings(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)

	out := m.View()
	for _, want := range []string{"Copy the highlighted entry's path to the clipboard", "Copy the active directory's own path to the clipboard"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help screen missing %q:\n%s", want, out)
		}
	}
}
```

Also add to `cmd/thicket/main_test.go`'s `TestHelpTextContainsUsageAndKeybindings` want-list (it already asserts a subset of `Keybindings` entries render into `--help` output):

```go
	for _, want := range []string{"Usage:", "thicket [path]", "-h | --help", "Move selection up", "Toggle this help screen", "Copy the highlighted entry's path to the clipboard"} {
```

(This is the one line in that test's slice literal — add the new string as the final element.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/... ./cmd/thicket/... -run 'YankNotice|YankKeybindings|TestHelpTextContainsUsageAndKeybindings' -v`
Expected: FAIL — `yankNotice` not yet read by `statusLine()`, and the help screen/`--help` text don't contain the new descriptions yet.

- [ ] **Step 3: Write the implementation**

In `internal/tui/render.go`'s `statusLine()`, insert a new branch between the `isErr` check and the `updateNotice` check:

```go
	right := m.activePath
	isErr := m.statusErr != ""
	if isErr {
		right = m.statusErr
	} else if m.yankNotice != "" {
		right = m.yankNotice
	} else if m.updateNotice != "" {
		right = m.updateNotice
	}
```

(This replaces the existing `if isErr { ... } else if m.updateNotice != "" { ... }` two-branch chain with the three-branch version above — the `yankNotice` branch sits between `isErr` and `updateNotice`.)

In the same function's `hints` string literal, append `· y yank · Y yank dir` after the existing `... · m mark · ` \` jump · ' marks` suffix.

In `internal/tui/help.go`'s `Keybindings` slice, append two rows after the `{"d (in marks list)", "Delete the highlighted mark"}` row:

```go
	{"y", "Copy the highlighted entry's path to the clipboard"},
	{"Y", "Copy the active directory's own path to the clipboard"},
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/... ./cmd/thicket/... -v`
Expected: PASS for all tests, including every pre-existing one (no regressions to `TestView_HelpScreenKeyColumnFitsWidestRow` — both new `Keys` strings, `"y"` and `"Y"`, are shorter than `"d (in marks list)"`, so `KeyColWidth` does not need to change).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/render.go internal/tui/render_test.go internal/tui/help.go cmd/thicket/main_test.go
git commit -m "feat: render yank status-line notice and help-screen entries"
```

---

### Task 4: Documentation sync (README, man page, AGENTS.md)

**Files:**
- Modify: `README.md`
- Modify: `man/thicket.1`
- Modify: `AGENTS.md`

**Interfaces:** None — pure documentation, no code dependency on other tasks (can be done in parallel with Tasks 2-3, though it references key descriptions matching them).

- [ ] **Step 1: Update `README.md`**

In the keybinding table, insert two rows after the existing `| `d` (in marks list) | Delete the highlighted mark |` row (currently line 67), before the blank line that precedes the "Navigation only in v1..." paragraph:

```markdown
| `y` | Copy the highlighted entry's path to the clipboard |
| `Y` | Copy the active directory's own path to the clipboard |
```

- [ ] **Step 2: Update `man/thicket.1`**

In the `.SH KEYS` section, insert two new `.TP` entries after the existing `'` entry's block (after `closes the list.`, currently line 148), before `.SH FILES`:

```troff
.TP
.B y
Copy the highlighted entry's absolute path to the system clipboard
(shells out to
.BR wl-copy ,
.BR xclip ,
or
.BR xsel ,
whichever is found first); if the active directory is empty, copies its
own path instead.
.TP
.B Y
Copy the active directory's own absolute path to the system clipboard,
regardless of what is highlighted.
```

- [ ] **Step 3: Update `AGENTS.md`**

In the "Project Overview" section's opening paragraph (currently lines 13-19), change "Three amendments" to "Four amendments" and append a fourth clause before the final sentence:

```markdown
Four amendments to that non-goal list have shipped since: a
`/`-triggered type-ahead cursor search within the active column
(`docs/superpowers/specs/2026-08-16-thicket-type-ahead-search-design.md`),
vim/ranger-style directory marks/bookmarks
(`docs/superpowers/specs/2026-08-16-thicket-directory-marks-design.md`),
an `f`-triggered recursive find over the active directory's subtree
(`docs/superpowers/specs/2026-08-16-thicket-recursive-find-design.md`),
and `y`/`Y` clipboard-yank keys
(`docs/superpowers/specs/2026-08-18-thicket-clipboard-yank-design.md`) —
the last of these does not reverse or narrow any non-goal line, since it
only reads a path and calls an external clipboard mechanism; nothing on
disk changes.
Every other v1 non-goal in §3 still holds.
```

In the "Code Conventions & Common Patterns" section's **Concurrency** bullet (currently lines 166-175), append a clause after "...auto-dismiss its status-line toast after 5s.":

```markdown
  The `y`/`Y` clipboard-yank keys (`internal/tui/clipboard.go`) add a
  second `tea.Tick` site (`dismissYankNoticeCmd`), auto-dismissing their
  own status-line toast after 3s — the clipboard write itself
  (`clipboard.Copy`) still runs synchronously inline on the Bubble Tea
  event loop, same as `marks.Save`/`fsutil.ListDir` elsewhere; only the
  *notice dismissal* uses `tea.Tick`.
```

In the "Important Files" table (currently lines 187-208), add two rows after the `internal/tui/help.go` row:

```markdown
| `internal/clipboard/clipboard.go` | `Copy` — subprocess-based clipboard write trying wl-copy/xclip/xsel in preference order |
| `internal/tui/clipboard.go` | `yankEntryPath`, `yankEntry`, `yankDir`, `yank` — the `y`/`Y` keys' clipboard-write logic and status-line notice |
```

- [ ] **Step 4: Verify no automated test regresses**

Run: `go test ./... && go vet ./...`
Expected: PASS — this task touches no `.go` files with behavior, only Markdown/troff, so this step is a sanity check that nothing else broke, not new coverage. (Doc-content sync itself stays manual, per `AGENTS.md`'s Testing & QA section's existing "shell integration... not automated" convention — there is no automated cross-check between `README.md`/`man/thicket.1` and `internal/tui/help.go` today.)

- [ ] **Step 5: Commit**

```bash
git add README.md man/thicket.1 AGENTS.md
git commit -m "docs: document y/Y clipboard-yank keys"
```

---

### Task 5: Final verification

**Files:** None (verification only).

- [ ] **Step 1: Full build and test suite**

Run: `go build -o thicket-bin ./cmd/thicket && go vet ./... && go test ./...`
Expected: build succeeds, `go vet` reports nothing, all tests PASS.

- [ ] **Step 2: Manual smoke test (X11)**

On a machine with `xclip` installed and an X11 session: `./thicket-bin`, move the cursor onto any entry, press `y`, confirm the status line shows `yanked: <path>`, then in another terminal run `xclip -o -selection clipboard` and confirm it prints the same path. Press `Y` and repeat, confirming it prints the active directory's path regardless of cursor position.

- [ ] **Step 3: Manual smoke test (Wayland)**

On a machine with `wl-copy`/`wl-paste` installed and a Wayland session: repeat Step 2's `y`/`Y` sequence, verifying with `wl-paste` instead of `xclip -o`.

- [ ] **Step 4: Manual smoke test (no clipboard tooling)**

On a machine (or a `PATH`-stripped shell) with none of `wl-copy`/`xclip`/`xsel` installed: press `y`, confirm the status line shows a `yank: no clipboard mechanism found...` error rather than silently doing nothing.

- [ ] **Step 5: Remove `thicket-bin`**

```bash
rm -f thicket-bin
```

(Per `AGENTS.md`'s `.gitignore` note: the binary is not gitignored, so a build performed in-repo during Step 1 must not be committed.)
