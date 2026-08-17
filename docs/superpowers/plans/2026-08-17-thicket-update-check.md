# Thicket Update Check + `thicket update` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detect newer thicket releases without blocking startup, surface a 5-second self-dismissing toast in the TUI status line, and add a `thicket update` (`thicket-bin update`) subcommand that installs the latest release via the existing `scripts/install.sh`.

**Architecture:** A new `internal/update` package does the GitHub Releases API check (`LatestTag`, `IsNewer`) and the actual install (`Run`, which execs `sh` with an embedded copy of `scripts/install.sh` piped to its stdin). `internal/tui` gains a narrowly-scoped `tea.Cmd`/`tea.Tick` pair — the on-launch check runs as a `tea.Cmd` from `Model.Init()`, and a successful check sets a `updateNotice` field rendered by `statusLine()`, auto-cleared 5s later by a `tea.Tick`. `cmd/thicket/main.go` wires it together: a new `update` subcommand routes to `internal/update.Run` before any TUI code runs, and the on-launch check is threaded into the `Model` via a new `Model.WithUpdateCheck(version string)` method (not a `New()` parameter — `tui.New` already has ~13 call sites across `update_test.go`/`render_test.go`; a new value-returning method avoids touching all of them, matching `Result()`'s existing by-value method style. Same externally observable behavior as the spec; this is purely an internal wiring choice).

**Tech Stack:** Go 1.24.6, `github.com/charmbracelet/bubbletea` v1.3.10 (pinned to v1.x), standard library only for the new package (`net/http`, `encoding/json`, `os/exec`, `embed`).

**Spec:** `docs/superpowers/specs/2026-08-17-thicket-update-check-design.md`

## Global Constraints

- Opt-out is the `THICKET_NO_UPDATE_CHECK` environment variable only — no config file (v1 non-goal, unchanged).
- `internal/tui`'s synchronous-only invariant is narrowed, not removed: `tea.Cmd`/`tea.Tick` are used only for the update-check-and-dismiss pair added by this plan. No other navigation/search/find/marks code becomes async.
- Update-check network timeout: 2 seconds (`context.WithTimeout`).
- Toast visible duration: 5 seconds (`tea.Tick`).
- A failed/slow/timed-out/malformed check is always silent — no stderr, no toast, no crash, no effect on exit code.
- `thicket update` never runs automatically; it is always a separate, user-initiated subcommand invocation.
- `LatestTag` treats any non-`200` HTTP response as an error before attempting JSON decode (rate-limit/outage bodies must not decode into an empty-string `tag_name` and silently look like "no update").
- `internal/update.Run` reuses `scripts/install.sh` verbatim via `//go:embed` — no reimplementation of its download/verify/swap/sudo-elevation logic in Go.
- `go vet ./...` and `go build ./...` must stay clean after every task; `go test ./...` must stay green after every task except where a task's own step explicitly expects a transient failure (TDD red step).

---

### Task 1: `internal/update` — release check (`LatestTag`, `IsNewer`)

**Files:**
- Create: `internal/update/check.go`
- Test: `internal/update/check_test.go`

**Interfaces:**
- Produces: `update.LatestTag(ctx context.Context) (string, error)`, `update.IsNewer(current, latest string) bool` — consumed by Task 3 (`internal/tui`).
- Consumes: nothing from other tasks.

- [ ] **Step 1: Write the failing test**

Create `internal/update/check_test.go`:

```go
package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{"equal versions", "v1.2.3", "v1.2.3", false},
		{"older major", "v2.0.0", "v1.9.9", false},
		{"newer major", "v1.9.9", "v2.0.0", true},
		{"newer minor", "v1.2.3", "v1.3.0", true},
		{"newer patch", "v1.2.3", "v1.2.4", true},
		{"older patch", "v1.2.4", "v1.2.3", false},
		{"malformed latest", "v1.2.3", "not-a-version", false},
		{"malformed current", "not-a-version", "v1.2.3", false},
		{"dev build never newer", "dev", "v99.0.0", false},
		{"missing v prefix still parses", "1.2.3", "1.2.4", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsNewer(tc.current, tc.latest)
			if got != tc.want {
				t.Errorf("IsNewer(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
			}
		})
	}
}

func TestFetchTag_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v1.2.3"})
	}))
	defer srv.Close()

	tag, err := fetchTag(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetchTag: %v", err)
	}
	if tag != "v1.2.3" {
		t.Errorf("tag = %q, want v1.2.3", tag)
	}
}

func TestFetchTag_404IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := fetchTag(context.Background(), srv.URL); err == nil {
		t.Fatal("fetchTag: want error on 404, got nil")
	}
}

func TestFetchTag_RateLimitBodyIsError(t *testing.T) {
	// GitHub's rate-limit response: 403 with a body that has no tag_name
	// field at all. Must be rejected at the status check, not decoded
	// into an empty-string tag that would look like "no update".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "API rate limit exceeded"})
	}))
	defer srv.Close()

	tag, err := fetchTag(context.Background(), srv.URL)
	if err == nil {
		t.Fatalf("fetchTag: want error on 403, got tag %q", tag)
	}
}

func TestFetchTag_MalformedJSONIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	if _, err := fetchTag(context.Background(), srv.URL); err == nil {
		t.Fatal("fetchTag: want error on malformed JSON, got nil")
	}
}

func TestFetchTag_ContextTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v1.2.3"})
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	_, err := fetchTag(ctx, srv.URL)
	if err == nil {
		t.Fatal("fetchTag: want error on context timeout, got nil")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") && ctx.Err() == nil {
		t.Fatalf("fetchTag: want a context-deadline error, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/update/... -v`
Expected: FAIL — `check_test.go` references `fetchTag`, `IsNewer` which don't exist yet (build failure).

- [ ] **Step 3: Write the implementation**

Create `internal/update/check.go`:

```go
// Package update checks for and installs newer thicket releases from
// GitHub Releases.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const releasesURL = "https://api.github.com/repos/freethewhat/Thicket/releases/latest"

// LatestTag fetches the tag_name of the latest GitHub release.
func LatestTag(ctx context.Context) (string, error) {
	return fetchTag(ctx, releasesURL)
}

// fetchTag is LatestTag's implementation with an injectable URL, so tests
// can point it at an httptest.Server instead of the real GitHub API.
//
// It returns an error if the request fails outright (offline, DNS
// failure, ctx deadline exceeded), or if the response status is not 200
// — a non-200 response (e.g. GitHub API rate-limiting, which returns 403
// with a {"message": "..."} body and no tag_name field) is rejected
// before any JSON decoding is attempted, so it can never be mistaken for
// "latest version is empty string".
func fetchTag(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("update: unexpected status %d from %s", resp.StatusCode, url)
	}

	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("update: decoding release response: %w", err)
	}
	if body.TagName == "" {
		return "", fmt.Errorf("update: release response had no tag_name")
	}
	return body.TagName, nil
}

// IsNewer reports whether latest is a newer version than current. Both
// are expected in "vX.Y.Z" form (a leading "v" is optional and stripped
// before parsing). current == "dev" (an unbuilt/local source run) always
// returns false, and either string failing to parse as three
// dot-separated integers also returns false — an unparsable latest is
// never treated as newer.
func IsNewer(current, latest string) bool {
	if current == "dev" {
		return false
	}
	c, ok := parseVersion(current)
	if !ok {
		return false
	}
	l, ok := parseVersion(latest)
	if !ok {
		return false
	}
	for i := range c {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/update/... -v`
Expected: PASS — all `TestIsNewer` subtests and all `TestFetchTag_*` tests green.

- [ ] **Step 5: Commit**

```bash
git add internal/update/check.go internal/update/check_test.go
git commit -m "feat: add internal/update release check (LatestTag, IsNewer)"
```

---

### Task 2: `internal/update.Run` — install via embedded `install.sh`

**Files:**
- Create: `scripts/embed.go`
- Create: `internal/update/run.go`

**Interfaces:**
- Consumes: nothing from Task 1 (independent concern within the same package).
- Produces: `update.Run(ctx context.Context) error` — consumed by Task 5 (`cmd/thicket/main.go`'s `update` subcommand).

- [ ] **Step 1: Create the embed shim**

Create `scripts/embed.go`:

```go
// Package scripts embeds install.sh so internal/update can run it without
// a runtime network fetch of the script itself. install.sh stays directly
// curl-able at its existing path/URL — this file only adds a Go-importable
// copy of the same bytes.
package scripts

import _ "embed"

//go:embed install.sh
var InstallSh []byte
```

This has no separate test — `go build ./...` in Step 3 is the verification that the embed directive resolves.

- [ ] **Step 2: Write `Run`**

Create `internal/update/run.go`:

```go
package update

import (
	"bytes"
	"context"
	"os"
	"os/exec"

	"thicket/scripts"
)

// Run installs the latest thicket release by piping the embedded
// install.sh to sh — the same "curl .../install.sh | sh" flow documented
// in README.md, minus the curl fetch of the script itself. No version
// argument is passed, so install.sh resolves "latest" itself.
//
// cmd.Stdin is the script's own bytes, not interactive input: install.sh
// makes no further reads from its stdin. cmd.Stdout/cmd.Stderr are
// inherited from the current process so curl's progress output and any
// sudo password prompt (sudo reads that from the controlling tty, not
// from this process's Stdin) behave identically to running install.sh
// directly. PREFIX/VERSION and all other environment variables are
// inherited unchanged from the current process — install.sh already
// reads them itself, no special-casing needed here.
func Run(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "sh", "-s", "--")
	cmd.Stdin = bytes.NewReader(scripts.InstallSh)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
```

- [ ] **Step 3: Build to verify the embed directive and package compile**

Run: `go build ./...`
Expected: succeeds with no errors (confirms `scripts/install.sh` embeds correctly and `internal/update` compiles).

`Run` itself is not unit-tested — it shells out, downloads, and installs for real, which needs network and (outside a writable `PREFIX`) `sudo`. It's covered by a manual smoke test once `thicket update` is wired up in Task 5.

- [ ] **Step 4: Commit**

```bash
git add scripts/embed.go internal/update/run.go
git commit -m "feat: add internal/update.Run to install via embedded install.sh"
```

---

### Task 3: `internal/tui` — update-check `tea.Cmd` and toast state

**Files:**
- Create: `internal/tui/update_check.go`
- Create: `internal/tui/update_check_test.go`
- Modify: `internal/tui/model.go:14-73` (Model struct fields), `internal/tui/model.go:107-109` (`Init`)
- Modify: `internal/tui/update.go:1-9` (imports), `internal/tui/update.go:94-95` (new `case` arms)

**Interfaces:**
- Consumes: `update.LatestTag(ctx context.Context) (string, error)`, `update.IsNewer(current, latest string) bool` (Task 1).
- Produces: `Model.WithUpdateCheck(currentVersion string) Model`, `Model.updateNotice string` field (read by Task 4's `statusLine()`) — consumed by Task 5 (`cmd/thicket/main.go`).

- [ ] **Step 1: Write the failing test**

Create `internal/tui/update_check_test.go`:

```go
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
```

(The trailing `var _ = time.Second` / `var _ tea.Msg` lines exist only to keep the `time` and `tea` imports used if you trim assertions during review — delete both once `tea.Tick`'s return type is exercised via `cmd()` above, which already uses `tea` implicitly through `tea.Model`/`tea.Cmd` types; keep the file compiling either way.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/... -run 'TestCheckUpdateCmd|TestModel_WithUpdateCheck|TestModel_Init|TestUpdate_UpdateAvailableMsg|TestUpdate_ClearUpdateNoticeMsg' -v`
Expected: FAIL — build failure: `latestTagFunc`, `checkUpdateCmd`, `updateAvailableMsg`, `clearUpdateNoticeMsg`, `updateNoticeDuration`, `Model.WithUpdateCheck`, `Model.checkVersion`, `Model.updateNotice` don't exist yet.

- [ ] **Step 3: Write `internal/tui/update_check.go`**

```go
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
```

- [ ] **Step 4: Modify `internal/tui/model.go`**

Add two fields to the `Model` struct — insert after the existing `statusErr string` field (currently `internal/tui/model.go:20`):

```go
	statusErr     string
	// checkVersion/updateNotice: on-launch update-check state (spec
	// docs/superpowers/specs/2026-08-17-thicket-update-check-design.md).
	// checkVersion is set once via WithUpdateCheck before the Bubble Tea
	// program starts; empty ("" or "dev") disables the check entirely —
	// Init returns nil and no network request is ever made. updateNotice
	// is populated by Update's updateAvailableMsg case and rendered by
	// statusLine; it self-clears via a tea.Tick-scheduled
	// clearUpdateNoticeMsg, mirroring statusErr's shape but with a timed
	// auto-dismiss statusErr does not have.
	checkVersion string
	updateNotice string
```

Add `WithUpdateCheck` after `New` (currently ending `internal/tui/model.go:105`), and replace the existing `Init`:

```go
// WithUpdateCheck returns a copy of m configured to check for a newer
// release when the Bubble Tea program starts, comparing against
// currentVersion. An empty currentVersion (or "dev") disables the check
// entirely: Init returns nil and no network request is ever made.
// cmd/thicket computes currentVersion once from the build's version and
// THICKET_NO_UPDATE_CHECK before calling this method.
func (m Model) WithUpdateCheck(currentVersion string) Model {
	m.checkVersion = currentVersion
	return m
}

func (m Model) Init() tea.Cmd {
	if m.checkVersion == "" || m.checkVersion == "dev" {
		return nil
	}
	return checkUpdateCmd(m.checkVersion)
}
```

- [ ] **Step 5: Modify `internal/tui/update.go`**

Add `"time"` to the import block (currently `internal/tui/update.go:1-9`):

```go
import (
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"thicket/internal/fsutil"
	marksPkg "thicket/internal/marks"
)
```

Add two new top-level `case` arms to `Update`'s type switch, as siblings of `case tea.KeyMsg:` — insert immediately after that case's body ends and before the switch's closing brace (currently `internal/tui/update.go:94-95`, right after the inner `switch msg.String() { ... }` closes):

```go
	case updateAvailableMsg:
		m.updateNotice = updateNoticeText(msg.tag)
		return m, tea.Tick(updateNoticeDuration, func(time.Time) tea.Msg { return clearUpdateNoticeMsg{} })
	case clearUpdateNoticeMsg:
		m.updateNotice = ""
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/tui/... -run 'TestCheckUpdateCmd|TestModel_WithUpdateCheck|TestModel_Init|TestUpdate_UpdateAvailableMsg|TestUpdate_ClearUpdateNoticeMsg' -v`
Expected: PASS.

- [ ] **Step 7: Run the full `internal/tui` suite to confirm no regressions**

Run: `go test ./internal/tui/...`
Expected: PASS (existing navigation/search/find/marks tests untouched).

- [ ] **Step 8: Commit**

```bash
git add internal/tui/update_check.go internal/tui/update_check_test.go internal/tui/model.go internal/tui/update.go
git commit -m "feat: add update-check tea.Cmd and toast state to internal/tui"
```

---

### Task 4: `internal/tui/render.go` — toast in the status line

**Files:**
- Modify: `internal/tui/render.go:268-272` (`statusLine`'s right-slot precedence)
- Test: `internal/tui/render_test.go` (append)

**Interfaces:**
- Consumes: `Model.updateNotice string` (Task 3).
- Produces: nothing new — `statusLine()`'s signature is unchanged, only its right-slot precedence gains a tier.

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/render_test.go` (matches the file's existing `TestView_StatusLine*` naming and structure — see `TestView_StatusLineMarksListSurfacesStatusErr` for the closest existing precedent):

```go
func TestView_StatusLineShowsUpdateNotice(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 150
	m.updateNotice = "update available: v9.9.9 — run 'thicket-bin update'"

	if !strings.Contains(m.statusLine(), "update available: v9.9.9") {
		t.Fatalf("statusLine() missing updateNotice: %q", m.statusLine())
	}
}

func TestView_StatusLineStatusErrTakesPrecedenceOverUpdateNotice(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 150
	m.updateNotice = "update available: v9.9.9 — run 'thicket-bin update'"
	m.statusErr = "open /x: permission denied"

	line := m.statusLine()
	if !strings.Contains(line, "permission denied") {
		t.Fatalf("statusLine() missing statusErr: %q", line)
	}
	if strings.Contains(line, "update available") {
		t.Fatalf("statusLine() should not show updateNotice while statusErr is set: %q", line)
	}
}

func TestView_StatusLineSearchSuppressesUpdateNotice(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 150
	m.updateNotice = "update available: v9.9.9 — run 'thicket-bin update'"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)

	if strings.Contains(m.statusLine(), "update available") {
		t.Fatalf("statusLine() should suppress updateNotice while searchMode is active: %q", m.statusLine())
	}
}

func TestView_StatusLineMarksListStillShowsUpdateNotice(t *testing.T) {
	root := setupFixture(t)
	m := newTestModel(t, root)
	m.width = 150
	m.marksListMode = true
	m.updateNotice = "update available: v9.9.9 — run 'thicket-bin update'"

	if !strings.Contains(m.statusLine(), "update available") {
		t.Fatalf("statusLine() should still show updateNotice during marksListMode: %q", m.statusLine())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/... -run TestView_StatusLine.*UpdateNotice -v`
Expected: FAIL — `TestView_StatusLineShowsUpdateNotice` and `TestView_StatusLineMarksListStillShowsUpdateNotice` fail (notice not yet rendered); `TestView_StatusLineStatusErrTakesPrecedenceOverUpdateNotice` and `TestView_StatusLineSearchSuppressesUpdateNotice` pass vacuously today (there's no notice logic to leak) — that's expected; they'll stay green through Step 4 and guard against future precedence regressions.

- [ ] **Step 3: Modify `statusLine()`**

Replace lines `internal/tui/render.go:269-272`:

```go
	isErr := m.statusErr != ""
	if isErr {
		right = m.statusErr
	} else if m.updateNotice != "" {
		right = m.updateNotice
	}
```

(This is placed before the existing `if m.helpMode { ... } else if m.searchMode { ... } ... else if m.findMode { ... }` chain, which already unconditionally overwrites `right = m.activePath` in the `helpMode`/`searchMode`/`findMode` branches — so the toast is suppressed there the same way `statusErr` already is, and left showing during the default view and during `marksListMode`, which keeps this default precedence per its own existing comment.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/... -run TestView_StatusLine -v`
Expected: PASS — all `StatusLine` tests, old and new.

- [ ] **Step 5: Run the full `internal/tui` suite**

Run: `go test ./internal/tui/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/render.go internal/tui/render_test.go
git commit -m "feat: render update-available toast in the status line"
```

---

### Task 5: `cmd/thicket/main.go` — `update` subcommand and check wiring

**Files:**
- Modify: `cmd/thicket/main.go:1-13` (imports), `cmd/thicket/main.go:20-45` (arg parsing, `Model` construction), `cmd/thicket/main.go:95-110` (`helpText`)

**Interfaces:**
- Consumes: `update.Run(ctx context.Context) error` (Task 2), `Model.WithUpdateCheck(currentVersion string) Model` (Task 3).
- Produces: `thicket-bin update` CLI behavior — no Go symbols consumed by later tasks.

- [ ] **Step 1: Modify imports**

Replace `cmd/thicket/main.go:1-13`:

```go
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"thicket/internal/marks"
	"thicket/internal/tui"
	"thicket/internal/update"
)
```

- [ ] **Step 2: Add the `update` subcommand and update-check wiring**

Replace `cmd/thicket/main.go:20-45` (the whole `func main()` body from `start := "."` through the `tui.New` call and its error check):

```go
func main() {
	if len(os.Args) > 1 && os.Args[1] == "update" {
		if err := update.Run(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "thicket: update failed: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	start := "."
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-h", "--help":
			fmt.Print(helpText())
			return
		case "-v", "--version":
			fmt.Printf("thicket %s\n", version)
			return
		default:
			start = os.Args[1]
		}
	}

	marksPath, err := marks.DefaultPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "thicket: %v\n", err)
		os.Exit(2)
	}

	m, err := tui.New(start, marksPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "thicket: %v\n", err)
		os.Exit(2)
	}

	checkVersion := version
	if os.Getenv("THICKET_NO_UPDATE_CHECK") != "" {
		checkVersion = ""
	}
	m = m.WithUpdateCheck(checkVersion)
```

(`WithUpdateCheck` and `Model.Init` already treat `"dev"` as disabled — see Task 3 — so `main.go` only needs to handle the env-var opt-out here, not the dev-build case a second time.)

The rest of `func main()` (from `tty, err := os.OpenFile(...)` onward) is unchanged.

- [ ] **Step 3: Document the subcommand in `--help` output**

Modify `helpText()` (currently `cmd/thicket/main.go:95-110`) — replace the `Usage:` block:

```go
	b.WriteString("Usage:\n")
	b.WriteString("  thicket [path]\n")
	b.WriteString("  thicket -h | --help\n")
	b.WriteString("  thicket -v | --version\n")
	b.WriteString("  thicket update\n\n")
```

- [ ] **Step 4: Build**

Run: `go build -o thicket-bin ./cmd/thicket`
Expected: succeeds with no errors.

- [ ] **Step 5: Manual smoke test — update check opt-out and `--help`**

Run: `THICKET_NO_UPDATE_CHECK=1 ./thicket-bin --help`
Expected: prints usage including the new `thicket update` line; exits 0; no network activity (nothing to observe directly here, but confirms the binary still runs normally with the opt-out set).

Run: `./thicket-bin --version`
Expected: prints `thicket dev` (or the built-in version) and exits 0 — confirms the `update` subcommand check (`os.Args[1] == "update"`) doesn't shadow `-v`/`--version` handling.

- [ ] **Step 6: Manual smoke test — `thicket-bin update` against a scratch prefix**

Run:
```sh
export PREFIX="$(mktemp -d)"
./thicket-bin update
```
Expected: downloads and installs the latest published release into `$PREFIX/bin/thicket-bin`, `$PREFIX/share/man/man1/thicket.1`, `$PREFIX/share/thicket/shell/*` (same layout `scripts/install.sh` produces directly); exits 0. Confirm with `ls "$PREFIX/bin"`.

(This step requires network access and an existing published GitHub release for the repo; if neither is available in the current environment, note that explicitly instead of claiming the smoke test passed — do not fabricate a result.)

- [ ] **Step 7: Commit**

```bash
git add cmd/thicket/main.go
git commit -m "feat: add thicket update subcommand and wire on-launch update check"
```

---

### Task 6: Documentation and full verification

**Files:**
- Modify: `README.md` (append "Updating" section)
- Modify: `man/thicket.1` (SYNOPSIS, OPTIONS, new ENVIRONMENT section)
- Modify: `AGENTS.md` (Concurrency bullet under `internal/tui`)

**Interfaces:**
- Consumes: nothing (documentation only).
- Produces: nothing (terminal task).

- [ ] **Step 1: Add an "Updating" section to `README.md`**

Insert before the `## License` section (currently `README.md:72`):

```markdown
## Updating

```sh
thicket-bin update
```

Downloads and installs the latest release for your OS/arch into the same
`PREFIX` (default `/usr/local`) used by the install script above,
including the same sudo-elevation behavior for a non-writable prefix.

thicket also checks for a newer release on every launch (a single
GitHub API request, 2-second timeout, fails silently if offline or
slow) and shows a 5-second "update available" notice in the status line
when one exists. Set `THICKET_NO_UPDATE_CHECK=1` to disable this check
entirely.

```

- [ ] **Step 2: Update `man/thicket.1`**

Add to `SYNOPSIS` (currently `man/thicket.1:4-12`), after the `-v`/`--version` form:

```troff
.br
.B thicket
.B update
```

Add to `OPTIONS` (currently `man/thicket.1:32-41`), after the `-v`/`--version` entry:

```troff
.TP
.B update
Download and install the latest release for the current OS/arch into
the same install prefix used at install time (see
.B ENVIRONMENT
below), then exit. Equivalent to re-running the install script.
```

Add a new `ENVIRONMENT` section after `FILES` and before `EXIT STATUS` (currently `man/thicket.1:120-132`):

```troff
.SH ENVIRONMENT
.TP
.B THICKET_NO_UPDATE_CHECK
If set to any non-empty value, disables the on-launch check for a newer
release. Has no effect on
.B thicket update
itself, which always checks.
.TP
.B PREFIX
Used only by
.B thicket update
(and by
.IR scripts/install.sh ):
the install root for the binary and man page. Defaults to
.IR /usr/local .
```

- [ ] **Step 3: Update `AGENTS.md`'s Concurrency bullet**

Find the line under **Code Conventions & Common Patterns**:

> - **Concurrency**: none. Everything runs synchronously inside Bubble Tea's
>   `Update`/`View` calls; no `tea.Cmd`, goroutines, or channels are used
>   anywhere in the codebase.

Replace with:

> - **Concurrency**: narrowly scoped. All navigation/search/find/marks
>   handling is synchronous, same as before. The one exception is the
>   on-launch update check (`internal/tui/update_check.go`): `Model.Init`
>   returns a `tea.Cmd` that does a single bounded (2s timeout) GitHub API
>   request, and a successful check schedules a `tea.Tick` to auto-dismiss
>   its status-line toast after 5s. No goroutines or channels are used
>   directly anywhere — `tea.Cmd`/`tea.Tick` are Bubble Tea's own async
>   primitives, run by its scheduler, not manually spawned. Do not
>   introduce further `tea.Cmd`/goroutine/channel use elsewhere in
>   `internal/tui` without updating this bullet again.

- [ ] **Step 4: Full verification**

Run:
```bash
gofmt -l .
go vet ./...
go build ./...
go test ./...
```
Expected: `gofmt -l .` prints nothing (no unformatted files); `go vet`, `go build`, `go test ./...` all succeed with zero failures.

- [ ] **Step 5: Commit**

```bash
git add README.md man/thicket.1 AGENTS.md
git commit -m "docs: document thicket update, THICKET_NO_UPDATE_CHECK, and narrowed concurrency invariant"
```
