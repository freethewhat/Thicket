# Beta Release Channel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an opt-in beta update channel (`THICKET_CHANNEL=beta`) so a `vX.Y.Z-beta.N` prerelease tag can be discovered and installed by `thicket-bin update`, `scripts/install.sh`, and the on-launch update-check toast, without changing default (stable) behavior.

**Architecture:** No CI/release-pipeline changes — goreleaser already marks a semver-prerelease tag as a GitHub prerelease automatically, and `release.yml` already triggers on any `v*` tag. All work is client-side: `internal/update.LatestTag` gains a channel argument that switches between `GET .../releases/latest` (stable; GitHub excludes prereleases here by design) and `GET .../releases` (the newest-first list, which includes prereleases); `internal/update.IsNewer` gains prerelease-aware semver comparison so `-beta.N` tags sort correctly; `internal/tui` threads a channel through the existing update-check `tea.Cmd` and labels a prerelease toast as a beta offer; `scripts/install.sh` gets the same channel toggle for install/update-from-script; docs get one new env var.

**Tech Stack:** Go 1.24 stdlib (`net/http`, `encoding/json`, `strconv`, `strings`), Bubble Tea `tea.Cmd`, POSIX `sh`.

**Spec:** [issue #31 — Add opt-in Beta release channel](https://github.com/freethewhat/Thicket/issues/31)

## Global Constraints

- Default behavior (no `THICKET_CHANNEL` set, or set to anything other than exactly `beta`) is byte-for-byte unchanged: stable channel via `/releases/latest`, exactly as today.
- One single env var name (`THICKET_CHANNEL`) with two recognized values (`stable` default, `beta`) is used consistently by `install.sh`, `thicket-bin update`, and the on-launch toast — no separate `CHANNEL`-vs-`THICKET_CHANNEL` split.
- No changes to `.github/workflows/release.yml` or `.goreleaser.yaml` — tagging `vX.Y.Z-beta.N` already produces a correctly-flagged GitHub prerelease today.
- No new dependencies.
- `GET /repos/.../releases` (list endpoint) calls use the first page only (newest-first) — no pagination.
- **Sequencing note:** `internal/update.LatestTag`'s signature changes in Task 2, but its `internal/tui` consumer (`latestTagFunc`, `checkUpdateCmd`, `Model.Init`) isn't adapted until Task 3, because those consumer changes must land in one atomic commit (see Task 3's rationale) rather than split further. Between Task 2's commit and Task 3's commit, `go build ./...`/`go test ./...` at the repo root **will fail** — this is expected. During that window, verify only with the scoped command each task lists (`go test ./internal/update/...`), not a whole-module build. This window closes as soon as Task 3 lands; do not stop or hand off mid-window.

---

### Task 1: `internal/update` — prerelease-aware version comparison

**Files:**
- Modify: `internal/update/check.go`
- Test: `internal/update/check_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `IsNewer(current, latest string) bool` keeps its existing signature but now correctly orders `-beta.N` suffixes; unexported `parseSemver`, `isNewerSemver`, `comparePrerelease` replace the old unexported `parseVersion`.

This task is fully self-contained and non-breaking: `IsNewer`'s signature doesn't change, so nothing outside `internal/update` is affected. It's sequenced first (ahead of Task 2's breaking signature change) so the repo stays green for one extra commit.

- [ ] **Step 1: Write the failing tests**

Extend the `cases` table in `TestIsNewer` (`internal/update/check_test.go`) with these entries (append to the existing slice, don't remove any existing case):

```go
		{"beta older than later beta, same core", "v1.0.0-beta.1", "v1.0.0-beta.2", true},
		{"beta not newer than earlier beta, same core", "v1.0.0-beta.2", "v1.0.0-beta.1", false},
		{"equal betas", "v1.0.0-beta.1", "v1.0.0-beta.1", false},
		{"stable promotion from beta of same core is newer", "v1.0.0-beta.1", "v1.0.0", true},
		{"beta of same core as an already-stable current is not newer", "v1.0.0", "v1.0.0-beta.1", false},
		{"beta of a newer core beats stable of an older core", "v1.0.0", "v1.1.0-beta.1", true},
		{"numeric beta ordinal compares numerically, not lexically", "v1.0.0-beta.9", "v1.0.0-beta.10", true},
```

Also add a dedicated test for the comparator's numeric-vs-lexical behavior in isolation:

```go
func TestComparePrerelease_NumericFieldsCompareNumerically(t *testing.T) {
	if got := comparePrerelease("beta.9", "beta.10"); got >= 0 {
		t.Errorf("comparePrerelease(beta.9, beta.10) = %d, want < 0", got)
	}
	if got := comparePrerelease("beta.10", "beta.9"); got <= 0 {
		t.Errorf("comparePrerelease(beta.10, beta.9) = %d, want > 0", got)
	}
	if got := comparePrerelease("beta.1", "beta.1"); got != 0 {
		t.Errorf("comparePrerelease(beta.1, beta.1) = %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/update/... -run 'TestIsNewer|TestComparePrerelease' -v`
Expected: FAIL — the new `TestIsNewer` cases fail because the current `parseVersion` rejects any `-beta.N` suffix (returns `ok=false`, so `IsNewer` returns `false` for every new case expecting `true`); `TestComparePrerelease_NumericFieldsCompareNumerically` fails with `undefined: comparePrerelease`.

- [ ] **Step 3: Replace `parseVersion`/`IsNewer` with prerelease-aware versions**

In `internal/update/check.go`, delete the existing `parseVersion` function and the body of `IsNewer`, replacing with:

```go
// semver holds a parsed "vX.Y.Z" or "vX.Y.Z-pre" version string.
type semver struct {
	major, minor, patch int
	pre                  string // e.g. "beta.1"; empty for a stable release
}

// IsNewer reports whether latest is a newer version than current. Both
// are expected in "vX.Y.Z" or "vX.Y.Z-pre" form (a leading "v" is optional
// and stripped before parsing). current == "dev" (an unbuilt/local source
// run) always returns false, and either string failing to parse also
// returns false — an unparsable latest is never treated as newer.
func IsNewer(current, latest string) bool {
	if current == "dev" {
		return false
	}
	c, ok := parseSemver(current)
	if !ok {
		return false
	}
	l, ok := parseSemver(latest)
	if !ok {
		return false
	}
	return isNewerSemver(c, l)
}

// parseSemver parses "vX.Y.Z" or "vX.Y.Z-pre" (leading "v" optional; pre
// is kept as a raw dot-separated string, not further validated here).
func parseSemver(v string) (semver, bool) {
	var out semver
	v = strings.TrimPrefix(v, "v")
	core := v
	if i := strings.IndexByte(v, '-'); i >= 0 {
		core = v[:i]
		out.pre = v[i+1:]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return out, false
	}
	var nums [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		nums[i] = n
	}
	out.major, out.minor, out.patch = nums[0], nums[1], nums[2]
	return out, true
}

// isNewerSemver reports whether latest has higher precedence than
// current, per semver 2.0.0 §11: the numeric core compares first; for an
// equal core, a release with no prerelease outranks one with a
// prerelease, and two prereleases compare field-by-field via
// comparePrerelease.
func isNewerSemver(current, latest semver) bool {
	if current.major != latest.major {
		return latest.major > current.major
	}
	if current.minor != latest.minor {
		return latest.minor > current.minor
	}
	if current.patch != latest.patch {
		return latest.patch > current.patch
	}
	if current.pre == latest.pre {
		return false
	}
	if current.pre == "" {
		return false // current is already a stable release of this core version
	}
	if latest.pre == "" {
		return true // latest promotes the same core version out of prerelease
	}
	return comparePrerelease(current.pre, latest.pre) < 0
}

// comparePrerelease orders two dot-separated prerelease strings (e.g.
// "beta.1" vs "beta.2") per semver 2.0.0 §11.4: identifiers are compared
// left to right, numeric identifiers as integers (so "9" < "10"), other
// identifiers as strings; if one is a strict prefix of the other, the
// longer one has higher precedence. Returns -1, 0, or 1.
func comparePrerelease(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] == bs[i] {
			continue
		}
		an, aErr := strconv.Atoi(as[i])
		bn, bErr := strconv.Atoi(bs[i])
		if aErr == nil && bErr == nil {
			if an < bn {
				return -1
			}
			return 1
		}
		if as[i] < bs[i] {
			return -1
		}
		return 1
	}
	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	default:
		return 0
	}
}
```

`strconv` and `strings` are already imported in this file (the original `parseVersion` used both) — no import changes needed.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/update/... -v`
Expected: PASS, all `TestIsNewer` cases (old and new) and `TestComparePrerelease_NumericFieldsCompareNumerically` green. `go build ./...` from the repo root also still succeeds (this task changes no signature anything else calls).

- [ ] **Step 5: Commit**

```bash
git add internal/update/check.go internal/update/check_test.go
git commit -m "feat(update): prerelease-aware semver comparison for beta tags"
```

---

### Task 2: `internal/update` — channel-aware `LatestTag`

**Files:**
- Modify: `internal/update/check.go`
- Test: `internal/update/check_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `LatestTag(ctx context.Context, channel string) (string, error)` (signature change — was `LatestTag(ctx context.Context) (string, error)`); exported constants `ChannelStable = "stable"`, `ChannelBeta = "beta"`; unexported `fetchLatestFromList(ctx context.Context, url string) (string, error)`.

`LatestTag` is exported and consumed by `internal/tui` (`var latestTagFunc = update.LatestTag`). Changing its signature here means `internal/tui` and, transitively, `cmd/thicket` will not build again until Task 3 lands — see the Global Constraints sequencing note. This task's own scoped test command still passes; only the whole-module build is affected.

- [ ] **Step 1: Write the failing tests for `fetchLatestFromList`**

Add to `internal/update/check_test.go`:

```go
func TestFetchLatestFromList_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]string{
			{"tag_name": "v1.3.0-beta.1"},
			{"tag_name": "v1.2.0"},
		})
	}))
	defer srv.Close()

	tag, err := fetchLatestFromList(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetchLatestFromList: %v", err)
	}
	if tag != "v1.3.0-beta.1" {
		t.Errorf("tag = %q, want v1.3.0-beta.1", tag)
	}
}

func TestFetchLatestFromList_EmptyListIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]string{})
	}))
	defer srv.Close()

	if _, err := fetchLatestFromList(context.Background(), srv.URL); err == nil {
		t.Fatal("fetchLatestFromList: want error on empty list, got nil")
	}
}

func TestFetchLatestFromList_404IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := fetchLatestFromList(context.Background(), srv.URL); err == nil {
		t.Fatal("fetchLatestFromList: want error on 404, got nil")
	}
}

func TestFetchLatestFromList_MalformedJSONIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	if _, err := fetchLatestFromList(context.Background(), srv.URL); err == nil {
		t.Fatal("fetchLatestFromList: want error on malformed JSON, got nil")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/update/... -run TestFetchLatestFromList -v`
Expected: FAIL with `undefined: fetchLatestFromList`

- [ ] **Step 3: Implement `fetchLatestFromList` and channel-aware `LatestTag`**

In `internal/update/check.go`, add below the existing `releasesURL` const and rewrite `LatestTag`:

```go
const releasesListURL = "https://api.github.com/repos/freethewhat/Thicket/releases"

// ChannelStable and ChannelBeta name the two update channels LatestTag
// accepts. Any channel value other than ChannelBeta (including "") is
// treated as ChannelStable.
const (
	ChannelStable = "stable"
	ChannelBeta   = "beta"
)

// LatestTag fetches the tag_name of the latest release on channel.
// ChannelStable uses GET .../releases/latest, which GitHub's API defines
// to exclude prereleases and drafts. ChannelBeta uses GET .../releases
// (the list endpoint, confirmed newest-first by a live query against this
// repo during design) and takes the first entry, which may itself be a
// stable release if that's what was most recently cut — a beta-channel
// user always tracks whatever is newest, prerelease or not.
func LatestTag(ctx context.Context, channel string) (string, error) {
	if channel == ChannelBeta {
		return fetchLatestFromList(ctx, releasesListURL)
	}
	return fetchTag(ctx, releasesURL)
}

// fetchLatestFromList is LatestTag's ChannelBeta implementation with an
// injectable URL, so tests can point it at an httptest.Server. Mirrors
// fetchTag's error handling: a non-200 status or malformed JSON body both
// error out before decoding, so neither can be mistaken for "no releases
// exist yet".
func fetchLatestFromList(ctx context.Context, url string) (string, error) {
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

	var body []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("update: decoding release list response: %w", err)
	}
	if len(body) == 0 || body[0].TagName == "" {
		return "", fmt.Errorf("update: release list response had no releases")
	}
	return body[0].TagName, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/update/... -v`
Expected: PASS. Do **not** run `go build ./...`/`go test ./...` at the repo root as a gate for this task — per the Global Constraints sequencing note, `internal/tui` and `cmd/thicket` are expected to fail to build until Task 3 lands next; that failure is not this task's regression.

- [ ] **Step 5: Commit**

```bash
git add internal/update/check.go internal/update/check_test.go
git commit -m "feat(update): add channel-aware LatestTag for beta releases"
```

---

### Task 3: `internal/tui` — adapt to channel-aware `LatestTag`, thread the channel through, label beta toast

**Files:**
- Modify: `internal/tui/update_check.go`
- Modify: `internal/tui/model.go`
- Test: `internal/tui/update_check_test.go`

**Interfaces:**
- Consumes: `update.LatestTag(ctx context.Context, channel string) (string, error)`, `update.IsNewer(current, latest string) bool`, `update.ChannelStable`, `update.ChannelBeta` (all from Tasks 1/2).
- Produces: `checkUpdateCmd(current, channel string) tea.Cmd` (signature change — was `checkUpdateCmd(current string) tea.Cmd`); `updateNoticeText(tag string) string` keeps its signature but now labels a hyphenated (prerelease) tag as a beta offer; `Model.WithChannel(channel string) Model`; `Model.checkChannel string` field.

This task is deliberately one commit, not two: `checkUpdateCmd`'s signature and `Model.Init()`'s call to it live in the same package and must change together for `internal/tui` to compile at all — there's no way to split them across separately-buildable commits without a throwaway shim, which would violate the no-shims delivery rule for no real benefit here (this isn't a public API, it's a same-package private call site). This is the task that closes the whole-module breakage window opened by Task 2.

- [ ] **Step 1: Write the failing tests**

In `internal/tui/update_check_test.go`, update `withStubLatestTag`'s stub signature to match the new `latestTagFunc` type (it can still ignore the channel argument — stable-channel tests don't care about it):

```go
func withStubLatestTag(t *testing.T, tag string, err error) {
	t.Helper()
	orig := latestTagFunc
	latestTagFunc = func(ctx context.Context, channel string) (string, error) { return tag, err }
	t.Cleanup(func() { latestTagFunc = orig })
}
```

Update the three existing `checkUpdateCmd("v1.0.0")` call sites in this file (`TestCheckUpdateCmd_NewerReleaseReturnsUpdateAvailableMsg`, `TestCheckUpdateCmd_NoNewerReleaseReturnsNil`, `TestCheckUpdateCmd_FetchErrorReturnsNil`) to pass a channel argument — use `""` (stable) for all three: `checkUpdateCmd("v1.0.0", "")`.

Add new tests:

```go
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
```

Add `"strings"` to this test file's imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/... -run 'TestCheckUpdateCmd|TestUpdateNoticeText|TestModel_WithChannel|TestModel_InitPassesConfiguredChannel|TestModel_InitDefaultsToStableChannel' -v`
Expected: FAIL to compile — `checkUpdateCmd` still takes one argument, `m.checkChannel`/`m.WithChannel` don't exist yet, `updateNoticeText`'s beta-labeling doesn't exist yet.

- [ ] **Step 3: Update `checkUpdateCmd`, `updateNoticeText`, and `Model`**

In `internal/tui/update_check.go`:

```go
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
```

```go
// updateNoticeText formats the toast shown in the status line's right
// slot when updateAvailableMsg arrives. A tag containing "-" is a
// prerelease — the project's tagging convention never puts a hyphen in a
// stable "vX.Y.Z" tag, so this is a safe, sufficient signal — and is
// labeled as a beta offer to avoid confusing a beta-channel user about
// why they're seeing a pre-release version string.
func updateNoticeText(tag string) string {
	label := "update available"
	if strings.Contains(tag, "-") {
		label = "beta update available"
	}
	return fmt.Sprintf("%s: %s — run 'thicket-bin update'", label, tag)
}
```

Add `"strings"` to `internal/tui/update_check.go`'s imports.

In `internal/tui/model.go`, add the field next to `checkVersion`/`updateNotice` (inside the existing doc-commented block, lines ~21-31):

```go
	checkVersion string
	updateNotice string
	// checkChannel selects which update channel Init's checkUpdateCmd
	// queries (update.ChannelStable or update.ChannelBeta); empty means
	// ChannelStable. Set once via WithChannel before the Bubble Tea
	// program starts, same lifecycle as checkVersion.
	checkChannel string
```

Add the method after `WithUpdateCheck` (around line 127):

```go
// WithChannel returns a copy of m configured to check the given update
// channel (update.ChannelStable or update.ChannelBeta; empty means
// ChannelStable) instead of the default stable channel. Has no effect
// unless WithUpdateCheck was also called with a real version — Init's
// nil-Cmd short-circuit on an empty/"dev" checkVersion runs regardless of
// channel. cmd/thicket computes the channel once from THICKET_CHANNEL
// before calling this method.
func (m Model) WithChannel(channel string) Model {
	m.checkChannel = channel
	return m
}
```

Update `Init()`:

```go
func (m Model) Init() tea.Cmd {
	if m.checkVersion == "" || m.checkVersion == "dev" {
		return nil
	}
	return checkUpdateCmd(m.checkVersion, m.checkChannel)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/... -v`
Expected: PASS — every test in the package, old and new.
Then run: `go build ./...` and `go test ./...` from the repo root — both succeed again; this closes the breakage window opened by Task 2.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/update_check.go internal/tui/update_check_test.go internal/tui/model.go
git commit -m "feat(tui): adapt to channel-aware LatestTag, add Model.WithChannel, label beta toast"
```

---

### Task 4: `cmd/thicket` — read `THICKET_CHANNEL`

**Files:**
- Modify: `cmd/thicket/main.go`

**Interfaces:**
- Consumes: `update.ChannelStable`, `update.ChannelBeta` (Task 2); `Model.WithChannel(channel string) Model` (Task 3).
- Produces: nothing new exported — this is process wiring only.

- [ ] **Step 1: Wire `THICKET_CHANNEL` into the Model**

In `cmd/thicket/main.go`, immediately after the existing `checkVersion`/`THICKET_NO_UPDATE_CHECK` block (around line 57-61):

```go
	checkVersion := version
	if os.Getenv("THICKET_NO_UPDATE_CHECK") != "" {
		checkVersion = ""
	}
	channel := update.ChannelStable
	if os.Getenv("THICKET_CHANNEL") == update.ChannelBeta {
		channel = update.ChannelBeta
	}
	m = m.WithUpdateCheck(checkVersion).WithChannel(channel)
```

(This replaces the existing single-line `m = m.WithUpdateCheck(checkVersion)`.)

There is no dedicated unit test for this block — the existing `THICKET_NO_UPDATE_CHECK` env-var read two lines above it is likewise untested at this layer (`internal/tui`'s `Model`-level tests from Task 3 already cover `WithChannel`/`Init` behavior; this step is pure env-to-argument wiring). Verify it compiles and behaves correctly via the manual smoke test in Step 2.

- [ ] **Step 2: Manual smoke test**

Run: `go build -o /tmp/thicket-bin ./cmd/thicket`
Then: `THICKET_CHANNEL=beta go run ./cmd/thicket --help` — confirm it runs without error (the `--help` path exits before `WithChannel` is even reached, so this just confirms the build compiles cleanly; the channel is only exercised in the real TUI launch path, which requires a real tty and isn't scriptable here).
Also run: `go vet ./...` and `go build ./...` from the repo root — confirm the whole module still compiles.

- [ ] **Step 3: Commit**

```bash
git add cmd/thicket/main.go
git commit -m "feat(cmd): read THICKET_CHANNEL and wire it into the update check"
```

---

### Task 5: `scripts/install.sh` — channel-aware `VERSION` resolution

**Files:**
- Modify: `scripts/install.sh`
- Modify: `internal/update/run.go` (doc comment only)

**Interfaces:**
- Consumes: nothing new (shell script; reads `THICKET_CHANNEL` from its inherited environment).
- Produces: nothing new exported.

- [ ] **Step 1: Add channel-aware VERSION resolution to `install.sh`**

In `scripts/install.sh`, inside `main()`, change:

```sh
	REPO="freethewhat/Thicket"
	PREFIX="${PREFIX:-/usr/local}"
	VERSION="${VERSION:-${1:-}}"
```

to:

```sh
	REPO="freethewhat/Thicket"
	PREFIX="${PREFIX:-/usr/local}"
	CHANNEL="${THICKET_CHANNEL:-stable}"
	VERSION="${VERSION:-${1:-}}"
```

And change the existing VERSION-resolution block:

```sh
	if [ -z "$VERSION" ]; then
		VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" |
			grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
		[ -n "$VERSION" ] || err "could not determine the latest release; pass a version explicitly"
	fi
```

to:

```sh
	if [ -z "$VERSION" ]; then
		if [ "$CHANNEL" = "beta" ]; then
			releases_url="https://api.github.com/repos/${REPO}/releases"
		else
			releases_url="https://api.github.com/repos/${REPO}/releases/latest"
		fi
		VERSION="$(curl -fsSL "$releases_url" |
			grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
		[ -n "$VERSION" ] || err "could not determine the latest release; pass a version explicitly"
	fi
```

Also update the script's top-of-file usage comment to document the new env var — change:

```sh
# Env overrides:
#   PREFIX   install root for the binary and man page (default: /usr/local)
#   VERSION  release tag to install, e.g. v0.1.0 (default: latest)
```

to:

```sh
# Env overrides:
#   PREFIX          install root for the binary and man page (default: /usr/local)
#   VERSION         release tag to install, e.g. v0.1.0 (default: latest on the selected channel)
#   THICKET_CHANNEL "stable" (default) or "beta" — beta resolves VERSION against
#                    the full release list (prereleases included, newest first)
#                    instead of GitHub's "latest" endpoint, which excludes them
```

- [ ] **Step 2: Update `internal/update/run.go`'s doc comment**

`update.Run` needs no code change (it already inherits the whole process environment, including `THICKET_CHANNEL`, into the `sh` subprocess unchanged) — only its doc comment should say so explicitly. In `internal/update/run.go`, extend the existing doc comment on `Run` (currently ending "...install.sh already reads them itself, no special-casing needed here.") by appending one sentence:

```go
// PREFIX/VERSION and all other environment variables are inherited
// unchanged from the current process — install.sh already reads them
// itself, no special-casing needed here. This includes THICKET_CHANNEL,
// which install.sh reads to pick between the stable and beta release
// lists when VERSION isn't explicitly pinned.
func Run(ctx context.Context) error {
```

- [ ] **Step 3: Manual smoke test**

Run: `sh -n scripts/install.sh` (POSIX syntax check only, no network) — expect no output/exit 0.
Run against a real tag to confirm the stable path still resolves correctly (safe, read-only): `curl -fsSL https://api.github.com/repos/freethewhat/Thicket/releases/latest | grep -m1 '"tag_name"'` — confirm it prints a `v*` tag, proving the unchanged stable-path grep/sed still matches GitHub's current response shape.
If any `-beta.N` tag already exists on the repo by this point, additionally run: `THICKET_CHANNEL=beta sh -c 'curl -fsSL https://api.github.com/repos/freethewhat/Thicket/releases | grep -m1 "\"tag_name\""'` and confirm it prints the *newest* release tag on the repo — that's the beta tag only if nothing newer has been cut since; if a stable release was tagged after the beta, the newest-first list correctly returns that stable tag instead (ChannelBeta's designed "track whatever is newest, prerelease or not" semantics — see Task 2). Don't read a stable result here as a failure. If no beta tag exists yet, skip this and note it as unverified until one is cut (the stable-path smoke test above already proves the grep/sed logic against real GitHub API output, and the beta branch is byte-identical except for the URL).

- [ ] **Step 4: Commit**

```bash
git add scripts/install.sh internal/update/run.go
git commit -m "feat(install): add THICKET_CHANNEL support to install.sh"
```

---

### Task 6: Documentation — README and man page

**Files:**
- Modify: `README.md`
- Modify: `man/thicket.1`

**Interfaces:**
- Consumes: nothing (docs only).
- Produces: nothing (docs only).

- [ ] **Step 1: Update README's "Updating" section**

In `README.md`, after the existing paragraph ending "...Set `THICKET_NO_UPDATE_CHECK=1` to disable this check entirely.", add:

```markdown

Set `THICKET_CHANNEL=beta` to track pre-release builds (tagged `vX.Y.Z-beta.N`)
instead of only stable releases — this affects `thicket-bin update`, the
on-launch check above, and `scripts/install.sh` alike. A beta offer is
labeled "beta update available" in the status line so it's clear you're
being offered a pre-release. Unset, or set to anything other than `beta`,
tracks stable only (the default).
```

- [ ] **Step 2: Update man page's ENVIRONMENT section**

In `man/thicket.1`, after the existing `.TP` block for `THICKET_NO_UPDATE_CHECK` (lines 162-167) and before the `PREFIX` `.TP` block, insert:

```troff
.TP
.B THICKET_CHANNEL
Set to
.I beta
to track pre-release builds (tagged
.IR vX.Y.Z-beta.N )
instead of only stable releases. Affects
.BR "thicket update" ,
the on-launch check, and
.IR scripts/install.sh
alike. Unset, or any value other than
.IR beta ,
tracks stable only (the default).
```

- [ ] **Step 3: Verify**

Run: `man --warnings -E UTF-8 -Tutf8 man/thicket.1 >/dev/null` (or `groff -man -Tutf8 man/thicket.1 >/dev/null` if `man --warnings` isn't available) — confirm no troff warnings/errors are printed.
Read back both edited files to confirm the new prose fits the existing tone and doesn't duplicate the on-launch-check paragraph already above it.

- [ ] **Step 4: Commit**

```bash
git add README.md man/thicket.1
git commit -m "docs: document THICKET_CHANNEL beta release channel"
```

---

## Final Verification

After all 6 tasks are committed:

- [ ] Run `go build ./...` from the repo root — full module compiles.
- [ ] Run `go vet ./...` — no findings.
- [ ] Run `go test ./...` — full suite passes.
- [ ] Manually re-read the acceptance criteria in issue #31 and confirm each is met:
  - `THICKET_CHANNEL=beta thicket-bin update` installs the newest tagged `-beta.N` (or newer stable) release; unset/`stable` behaves exactly as today. *(Task 5 install.sh logic + Task 1/2 IsNewer/LatestTag)*
  - `CHANNEL=beta curl .../install.sh | sh` installs the latest beta for a fresh install — **note the issue's original acceptance text said `CHANNEL`; this plan standardizes on `THICKET_CHANNEL` everywhere per the Global Constraints single-env-var decision. Use `THICKET_CHANNEL=beta curl .../install.sh | sh` instead; flag this wording delta when closing the issue.**
  - `internal/update.IsNewer`/semver parsing have test coverage for prerelease ordering. *(Task 1)*
  - On-launch update toast clearly labels a beta offer as such. *(Task 3)*
  - README and man page document `THICKET_CHANNEL`. *(Task 6)*
