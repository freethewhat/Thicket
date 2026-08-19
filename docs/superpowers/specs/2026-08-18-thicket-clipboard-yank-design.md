# thicket — yank active/selected entry path to clipboard

**Status:** Approved for planning
**Date:** 2026-08-18
**Issue:** [github.com/freethewhat/Thicket#9](https://github.com/freethewhat/Thicket/issues/9)
**Relates to:** `docs/superpowers/specs/2026-08-15-thicket-tui-file-browser-design.md` §3.
This is a fourth amendment in the same spirit as the type-ahead-search,
directory-marks, and recursive-find amendments already listed in
`AGENTS.md`'s opening paragraph — it adds a new normal-mode key, but does
not reverse or narrow any line in §3's non-goal list. In particular it
does **not** conflict with "no file create/rename/delete/copy/move":
yanking reads a path and calls an external clipboard mechanism; nothing on
disk changes.

## 1. Summary

Add two normal-mode keys: `y` copies the currently highlighted entry's
absolute path to the system clipboard (or the active directory's own path,
if the active directory is empty), and `Y` always copies the active
directory's own path regardless of what's highlighted. Neither key exits
`thicket` or changes `activePath`/`activeCursor`. A transient status-line
confirmation (`yanked: <path>`) appears on success and self-dismisses,
mirroring the existing update-available toast; a clipboard failure sets
`statusErr`, the same as every other error path in `internal/tui`.

## 2. Non-goals

- No configurable/detectable "which mechanism was used" indicator beyond
  the status-line text — `yanked: <path>` doesn't say `xclip` vs
  `wl-copy`.
- No primary-selection (X11 middle-click paste) support — clipboard only.
- No OSC 52 (terminal-native, SSH-friendly clipboard escape sequence) —
  considered and rejected; see §3.
- No retry against a *lower-preference* mechanism once the call has
  already succeeded via a higher-preference one — fallback (§3) only
  engages when a candidate's own attempt fails, never as a "try them all
  and pick the best" strategy.
- No file-content copying, only path strings — `y`/`Y` never read the
  target file/directory's contents.

## 3. Dependency decision: subprocess (xclip/wl-copy/xsel)

Two approaches were considered:

- **Subprocess (chosen):** shell out to whichever of `wl-copy`/`xclip`/
  `xsel` is on `$PATH`, via stdlib `os/exec` only. Zero new `go.mod`
  entries. Deterministic success/failure — "no mechanism installed" is a
  real, detectable error, and it's fully unit-testable via PATH-stub
  scripts (no real X11/Wayland session needed in CI; see §9).
- **OSC 52 escape sequence (rejected):** `termenv.Output.Copy` (already a
  *direct* dependency, wrapping `go-osc52`, already an *indirect*
  dependency — so this too would need zero new `go.mod` entries) writes
  the clipboard escape sequence straight to the tty thicket already holds
  open. Works over SSH/tmux with no subprocess. Rejected because OSC 52
  is fire-and-forget: there is no ack channel, so an unsupported or
  security-locked-down terminal silently does nothing, indistinguishable
  from success. That directly conflicts with the issue's acceptance
  criterion "clear error when no clipboard mechanism is available" — the
  subprocess approach can honestly satisfy that; OSC 52 cannot.

Both approaches turn out to need zero new dependencies — the issue's
framing of this as "the first feature that needs... a real new
dependency" does not hold under either choice; `go.mod` is unchanged by
this feature either way.

## 4. New package: `internal/clipboard`

A new leaf package, parallel to `internal/fsutil`/`internal/marks` in the
dependency graph (`cmd/thicket → internal/tui → {internal/fsutil,
internal/marks, internal/clipboard}`). Pure subprocess I/O; no Bubble
Tea/TUI concerns.

```go
package clipboard

import (
	"context"
	"errors"
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

(`os` and `fmt` imports added alongside the others above; elided from
the snippet for brevity.)

## 5. Model changes (`internal/tui/model.go`)

One new field, zero-valued at construction like `searchMode`/`helpMode`:

```go
// yankNotice: transient status-line confirmation shown after a
// successful y/Y (spec docs/superpowers/specs/2026-08-18-thicket-clipboard-yank-design.md).
// Self-clears via a tea.Tick-scheduled clearYankNoticeMsg, mirroring
// updateNotice's shape (§7). Unlike searchMode/helpMode/findMode/
// markSetPending/markJumpPending/marksListMode, this is not a modal
// state — y/Y are direct-action keys with no follow-up keystroke to
// capture, so no *Pending/*Mode field is needed alongside it.
yankNotice string
```

`internal/tui/clipboard.go` (§7) declares a package-level injectable var
mirroring `update_check.go`'s `latestTagFunc` pattern (tests reassign it
to a stub, avoiding a real subprocess call) — see §7 for the exact
declaration; it is not a `Model` field.

## 6. Key handling (`internal/tui/update.go`)

Two new cases in the existing normal-mode `switch msg.String()` block —
no dispatch-chain changes, since `y`/`Y` are direct-action keys reachable
only when no other mode (`helpMode`/`searchMode`/`findMode`/
`markSetPending`/`markJumpPending`/`marksListMode`) is intercepting keys,
same as `.`/`r`/`m`/`` ` ``/`'` today:

```go
case "y":
    return m, m.yankEntry()
case "Y":
    return m, m.yankDir()
```

This is the first normal-mode case to return a non-nil `tea.Cmd` from
inside the `switch`; every existing case mutates `m` and falls through to
`Update`'s final `return m, nil`. `y`/`Y` return directly instead, since
the `tea.Cmd` (the notice-dismiss timer) must be attached to this specific
keystroke's response, not queued for the shared fallthrough return.

## 7. New file `internal/tui/clipboard.go`

Mirrors `update_check.go`'s self-contained-feature-file pattern (msg
types, dismiss-timer constructor, and the feature's own methods, all in
one file):

```go
package tui

import (
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"thicket/internal/clipboard"
)

// clipboardCopyFunc is clipboard.Copy by default; tests reassign it to a
// stub.
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
// or activePath itself when activeCursor == -1 (empty directory), per
// issue #9 acceptance criterion 2.
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
// every other statusErr-setting path in update.go (e.g. handleRight's
// permission-denied case).
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
// after d elapses, via tea.Tick — extracted the same way
// dismissNoticeCmd (update_check.go) is, so tests can schedule a short
// dismissal instead of blocking on the real yankNoticeDuration.
func dismissYankNoticeCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return clearYankNoticeMsg{} })
}
```

`Update()` gains one new case, paralleling the existing
`clearUpdateNoticeMsg` one:

```go
case clearYankNoticeMsg:
    m.yankNotice = ""
```

## 8. Rendering (`internal/tui/render.go`)

**Status line** (`statusLine()`) — new precedence tier between `statusErr`
and `updateNotice`:

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

Rationale: an explicit action's confirmation (`y`/`Y`) outranks a passive
background toast (update-available), but a real error always wins. Only
reachable in normal mode — `helpMode`/`searchMode`/`marksListMode`/
`findMode` already force `right` in their own branches (unchanged), and
`y`/`Y` cannot fire while any of those modes intercepts keys, so no
interaction with those branches is needed.

Hint string (`statusLine`'s `hints` literal) gains `· y yank · Y yank dir`
appended after the existing `... · m mark · ` \` jump · ' marks` suffix.

## 9. Documentation updates

- `internal/tui/help.go`'s `Keybindings` gains two rows: `y` ("Copy the
  highlighted entry's path to the clipboard"), `Y` ("Copy the active
  directory's own path to the clipboard"). `KeyColWidth` (currently sized
  for `"d (in marks list)"`, 19) does not need to grow — both new `Keys`
  strings are shorter than that.
- `README.md`'s keybinding table and `man/thicket.1` get the same two
  rows, per the existing manually-synced-three-ways convention already
  documented on `cmd/thicket/main.go`'s `helpText()`.
- `AGENTS.md`'s opening paragraph, which currently lists three shipped
  amendments to the v1 non-goal list (type-ahead search, marks, recursive
  find), gains a fourth sentence for this feature, referencing this spec
  file. Per §1 above, this amendment does not reverse any non-goal line —
  the sentence should say so explicitly, matching how §1 is phrased here.
- `AGENTS.md`'s Concurrency bullet ("Do not introduce further
  `tea.Cmd`/goroutine/channel use elsewhere in `internal/tui` without
  updating this bullet again") gets one clause noting `dismissYankNoticeCmd`
  is the second `tea.Tick` site, alongside the update-check's
  `dismissNoticeCmd`. Still zero goroutines/channels — `clipboard.Copy`
  runs synchronously inline in `yank`, on the Bubble Tea event loop, same
  as `marks.Save`/`fsutil.ListDir` elsewhere; only the *notice dismissal*
  uses `tea.Tick`, not the clipboard write itself.
- `AGENTS.md`'s Important Files table gains rows for
  `internal/clipboard/clipboard.go` and `internal/tui/clipboard.go`.

## 10. Testing strategy

Follows the existing `TestXxx_Behavior` convention; new test file only for
the new package, per `AGENTS.md`'s rule.

**`internal/clipboard/clipboard_test.go`** (new file, new package) — every
case uses a `t.TempDir()` containing fake executable shell-script stubs
(e.g. a script named `xclip` that appends its stdin to a file) prepended
onto a test-local `$PATH` via `t.Setenv("PATH", ...)`, exercising the real
subprocess path with no actual X11/Wayland session required:

- `TestCopy_PrefersWlCopyWhenWaylandDisplaySet` — both `wl-copy` and
  `xclip` stubs succeed (and record their stdin to separate files);
  `$WAYLAND_DISPLAY` set; assert only `wl-copy`'s file was written (proves
  preference order, not just presence).
- `TestCopy_PrefersXclipWhenWaylandDisplayUnset` — same two stubs,
  `$WAYLAND_DISPLAY` unset; assert only `xclip`'s file was written.
- `TestCopy_FallsBackWhenPreferredMechanismExitsNonZero` — `wl-copy` stub
  exits 1, `xclip` stub succeeds; `$WAYLAND_DISPLAY` set; assert `xclip`'s
  file has the text and `Copy` returns `nil`.
- `TestCopy_FallsBackWhenPreferredMechanismTimesOut` — `wl-copy` stub
  sleeps past `copyTimeout`, `xclip` stub succeeds fast; assert fallback
  still runs and `Copy` returns `nil`. `copyTimeout` is a `const`, not an
  injectable var — this test tolerates the real ~1s wall-clock wait
  rather than adding a seam solely for one test case; 1s is not slow
  enough to justify the extra indirection.
- `TestCopy_AllMechanismsFailReturnsJoinedError` — every found stub exits
  non-zero; assert a non-nil error is returned (not `nil`), and that
  `errors.Is`/`errors.As` against the joined error surfaces at least one
  wrapped exit-status error.
- `TestCopy_NoMechanismOnPathReturnsErrNoMechanism` — PATH set to an empty
  temp dir with no stubs; assert `errors.Is(err, clipboard.ErrNoMechanism)`.

**`internal/tui/update_test.go`** — new cases reassigning
`clipboardCopyFunc` to a stub `func(string) error`, restored via
`t.Cleanup`:

- `TestUpdate_YCopiesHighlightedEntryPath`
- `TestUpdate_YCopiesFileEntryPath` (distinguishing from `selectedDirPath`
  — a highlighted *file*, not just a directory, is yanked)
- `TestUpdate_YOnEmptyDirectoryCopiesActivePath` (`activeCursor == -1`)
- `TestUpdate_CapitalYAlwaysCopiesActivePathRegardlessOfCursor`
- `TestUpdate_YSetsYankNoticeAndClearsStatusErr`
- `TestUpdate_YFailureSetsStatusErrAndClearsYankNotice`
- `TestUpdate_ClearYankNoticeMsgClearsYankNotice`

**`internal/tui/render_test.go`** — new cases:

- `TestStatusLine_YankNoticeTakesPrecedenceOverUpdateNotice`
- `TestStatusLine_StatusErrTakesPrecedenceOverYankNotice`
- `TestView_HelpScreenListsYankKeybindings` (mirrors existing help-screen
  content assertions for other keys)

**Not automated** (manual smoke test only, per acceptance criterion 1 and
the existing "shell integration... not automated" convention): pressing
`y`/`Y` in a real terminal on both X11 (verified via `xclip -o`) and
Wayland (verified via `wl-paste`).
