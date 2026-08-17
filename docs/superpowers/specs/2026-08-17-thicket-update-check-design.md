# Thicket update check + `thicket update` — design

Issue: [freethewhat/Thicket#28](https://github.com/freethewhat/Thicket/issues/28)
Date: 2026-08-17

## Problem

`thicket` has no way to discover or install newer releases short of
re-running `scripts/install.sh` by hand. `cmd/thicket/main.go` only handles
`-h`/`--help`, `-v`/`--version`, and a positional start path; `version` is a
build-time `ldflags` string with no runtime awareness of newer releases.

## Goals

- Detect that a newer release exists, without adding noticeable startup
  latency or ever hanging on a slow/unreachable network.
- Surface the notice as a transient, self-dismissing toast inside the TUI
  session itself (not only after the user has already quit) — visible for
  5 seconds, then gone, replaced by the normal status line.
- Provide a `thicket update` (equivalently `thicket-bin update`) subcommand
  that installs the latest release in place, respecting `PREFIX` and the
  sudo-elevation behavior `scripts/install.sh` already implements.
- Never install automatically — `thicket update` is always a separate,
  user-initiated command. The on-launch check only ever informs; it never
  triggers a download.
- Respect the "no config file" v1 constraint: opt-out is an environment
  variable, not a config file.

## Invariant change: `internal/tui` gains `tea.Cmd`

`AGENTS.md` and this repo's earlier design specs document `internal/tui`
as synchronous-only: "no goroutines, channels, or `tea.Cmd`... anywhere in
the codebase." A toast that appears *during* the session and needs a timed
auto-dismiss cannot be done from `cmd/thicket` alone — it requires the
Bubble Tea model to receive an async result and a timer tick. This design
deliberately introduces `tea.Cmd`/`tea.Tick` to `internal/tui`, scoped
narrowly to the update-check-and-dismiss pair described below. Nothing
else in `internal/tui` becomes async — all navigation, search, find, and
marks handling remain the synchronous `Update()` key-dispatch they are
today. `AGENTS.md`'s Concurrency bullet must be updated (see Documentation
below) to state this precise, narrow exception instead of the current
unconditional "none."

## Non-goals

- No new interactive confirmation/progress UI — `thicket update` is a
  CLI-only, stdio-inherited subprocess invocation of the existing
  `scripts/install.sh`, not a reimplementation of its download/verify/swap
  logic in Go.
- No checksum verification beyond what `scripts/install.sh` already does
  today (it does not verify against `checksums.txt`; that gap is out of
  scope for this change and unaffected by it).
- No caching of the "latest release" result across invocations.
- No general-purpose async infrastructure in `internal/tui` — no worker
  pool, no cancellation plumbing beyond the check's own context timeout,
  no reuse of the toast mechanism for anything other than this notice.

## Design

### `scripts/embed.go` (new)

```go
package scripts

import _ "embed"

//go:embed install.sh
var InstallSh []byte
```

Go's `//go:embed` directive cannot traverse `..`, so the embedding source
file must live in `scripts/`, next to `install.sh`. `install.sh` itself
does not move — the `curl .../scripts/install.sh | sh` URL documented in
`README.md` and referenced by `scripts/install.sh`'s own usage comment
stays valid and unaffected.

### `internal/update` (new package)

- `LatestTag(ctx context.Context) (string, error)` — `GET
  https://api.github.com/repos/freethewhat/Thicket/releases/latest`. Returns
  an error if the request fails outright (offline, DNS failure, connection
  refused, `ctx` deadline exceeded), **or if the response status is not
  `200`** (e.g. GitHub API rate-limiting returns `403` with a
  `{"message": "..."}` body that has no `tag_name` field at all — this
  must not be treated as "latest version is empty string" and silently
  fall through to a parse failure; it's rejected explicitly at the status
  check, before JSON decoding). Otherwise decodes the JSON body with
  `encoding/json` and returns `tag_name` (install.sh's grep/sed extraction
  is a shell-only constraint that doesn't apply here). Respects `ctx`'s
  deadline via `http.NewRequestWithContext`.
- `IsNewer(current, latest string) bool` — strips a leading `v`, splits on
  `.`, parses each component as an int, compares
  `(major, minor, patch)` lexicographically. Returns `false` if `current
  == "dev"` (unbuilt/local source runs never claim an update is
  available) or if either string fails to parse as `vX.Y.Z`.
- `Run(ctx context.Context) error` — runs `sh` with `scripts.InstallSh`
  piped to its stdin (`sh -s --`, no version argument, so `install.sh`
  resolves "latest" itself exactly as the curl-pipe-sh flow does today).
  `Stdin` is NOT the piped script pipe for the child's own stdin — the
  script is fed via `cmd.Stdin = bytes.NewReader(scripts.InstallSh)`, and
  `cmd.Stdout`/`cmd.Stderr` are the process's real stdout/stderr so curl's
  progress output and any `sudo` password prompt (which reads from the
  controlling tty, not from `cmd.Stdin`) behave identically to running
  `scripts/install.sh` directly. Inherits `PREFIX`/`VERSION` and all other
  env vars from the current process untouched — no special-casing needed,
  `install.sh` already reads them itself.

### Update check + toast (`internal/tui`)

`tui.New` gains a `checkVersion` parameter:

```go
func New(startPath, marksPath, checkVersion string) (Model, error)
```

`cmd/thicket/main.go` computes `checkVersion` once, before calling `New`:
empty string disables the check outright (covers both the opt-out and
`dev` builds); otherwise it's the build's `version`:

```go
checkVersion := version
if version == "dev" || os.Getenv("THICKET_NO_UPDATE_CHECK") != "" {
    checkVersion = ""
}
m, err := tui.New(start, marksPath, checkVersion)
```

`Model` gains an unexported `checkVersion string` field (stored, used only
by `Init`) and an unexported `updateNotice string` field (rendered by
`statusLine`, mirrors `statusErr`'s shape).

```go
func (m Model) Init() tea.Cmd {
    if m.checkVersion == "" {
        return nil
    }
    return checkUpdateCmd(m.checkVersion)
}

type updateAvailableMsg struct{ tag string }
type clearUpdateNoticeMsg struct{}

func checkUpdateCmd(current string) tea.Cmd {
    return func() tea.Msg {
        ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
        defer cancel()
        tag, err := update.LatestTag(ctx)
        if err != nil || !update.IsNewer(current, tag) {
            return nil // bubbletea: a Cmd returning a nil Msg is a no-op
        }
        return updateAvailableMsg{tag: tag}
    }
}
```

`Update()` gets two new top-level `case` arms, siblings to the existing
`tea.WindowSizeMsg`/`tea.KeyMsg` cases — so they fire on every tick
regardless of `helpMode`/`searchMode`/`findMode`/`marksListMode`:

```go
case updateAvailableMsg:
    m.updateNotice = fmt.Sprintf("update available: %s — run 'thicket-bin update'", msg.tag)
    return m, tea.Tick(5*time.Second, func(time.Time) tea.Msg { return clearUpdateNoticeMsg{} })
case clearUpdateNoticeMsg:
    m.updateNotice = ""
```

`statusLine()`'s right-slot precedence gains a middle tier between
`statusErr` and the default `activePath`:

```go
right := m.activePath
isErr := m.statusErr != ""
if isErr {
    right = m.statusErr
} else if m.updateNotice != "" {
    right = m.updateNotice
}
```

placed before the existing `helpMode`/`searchMode`/`findMode` branches,
which already force `right = m.activePath` unconditionally — so the toast
is correctly suppressed while help/search/find is open (same treatment
`statusErr` already gets there) and correctly still shown during
`marksListMode`, which deliberately keeps the default statusErr-or-toast-
or-activePath precedence per that mode's existing comment. Rendered with
`isErr` still `false` (plain style, not the red `errStyle`) since only
`statusErr` sets `isErr = true`.

`internal/tui` now imports `internal/update` for `LatestTag`/`IsNewer`,
alongside its existing `internal/fsutil`/`internal/marks` leaf
dependencies — consistent with the documented `cmd/thicket → internal/tui
→ internal/{fsutil,marks,update}` dependency direction, not a new
direction.

### `cmd/thicket/main.go` changes

**Subcommand routing** (before existing `-h`/`-v`/positional-path parsing):

```go
if len(os.Args) > 1 && os.Args[1] == "update" {
    if err := update.Run(context.Background()); err != nil {
        fmt.Fprintf(os.Stderr, "thicket: update failed: %v\n", err)
        os.Exit(1)
    }
    os.Exit(0)
}
```

Runs standalone — never starts `tea.NewProgram`. Unchanged from the
original design: the toast revision only touches the on-launch check,
not the `update` subcommand itself.

### Error handling

| Condition | Behavior |
|---|---|
| Network unreachable / DNS failure / connection refused / `ctx` timeout during check | Silent — `LatestTag` returns an error, `checkUpdateCmd` returns a nil `Msg`, no toast, nothing else changes |
| Non-`200` HTTP response (rate-limited, GitHub outage, unexpected redirect, etc.) | Silent — `LatestTag` returns an error on the status check before attempting to decode a body, same nil-`Msg` path as above |
| Malformed/unexpected JSON body on a `200` response | Silent — `encoding/json` decode error, same nil-`Msg` path |
| `THICKET_NO_UPDATE_CHECK` set (any non-empty value) | `checkVersion == ""`, `Init` returns `nil`, no `tea.Cmd` ever runs |
| `version == "dev"` | Same as above |
| Toast shown | Auto-clears after 5s via `tea.Tick`; also naturally overwritten immediately if `statusErr` becomes non-empty in the meantime (error takes precedence) |
| `thicket update` — curl/tar/install/sudo failure inside `install.sh` | Child process's own `err()` writes to stderr (inherited), `thicket-bin update` exits 1 |
| `thicket update` — success | `install.sh`'s existing success messages print (inherited stdout/stderr), `thicket-bin update` exits 0 |

### Testing

- `internal/update`:
  - `IsNewer`: table test — equal versions, older, newer (major/minor/patch
    each), malformed tags on either side, `current == "dev"`.
  - `LatestTag`: `httptest.Server` — happy path (valid JSON), 404, 403 with
    a rate-limit-shaped body (`{"message": "..."}`, no `tag_name`,
    confirms this errors at the status check rather than decoding to an
    empty version), malformed JSON body on a 200, context
    already-expired/short timeout.
  - `Run`: not unit-tested (shells out, installs, requires network/sudo in
    the general case) — covered by a manual smoke test: `go build -o
    thicket-bin ./cmd/thicket && PREFIX=$(mktemp -d) ./thicket-bin update`,
    confirm the binary lands under `$PREFIX/bin/thicket-bin`. Consistent
    with `AGENTS.md`'s existing carve-out that shell/tty integration is
    manual-only.
- `internal/tui`: `checkUpdateCmd`'s inner `func() tea.Msg` is directly
  callable and testable without running the full Bubble Tea loop — table
  test against `httptest.Server` mirroring `internal/update`'s own
  `LatestTag` tests, confirming it returns `updateAvailableMsg{tag}` on a
  newer release and `nil` otherwise. `Update()`'s handling of
  `updateAvailableMsg`/`clearUpdateNoticeMsg` and `statusLine()`'s new
  precedence tier get ordinary `TestUpdate_Behavior`/`TestView_Behavior`
  cases matching existing conventions (construct a `Model`, feed it the
  message, assert `updateNotice`/rendered status line) — no real network
  or real timers needed since the messages are constructed directly.
- `cmd/thicket`: subcommand routing and the opt-out are exercised via a
  manual smoke test (`thicket-bin update`, `THICKET_NO_UPDATE_CHECK=1
  thicket-bin -v`) rather than an automated test, since `main()` isn't
  currently structured for testability and this change doesn't otherwise
  require refactoring it.

### Documentation

- `README.md`: new "Updating" section — `thicket-bin update`, what it does
  (installs latest release for current OS/arch to the same `PREFIX` used at
  install time), and `THICKET_NO_UPDATE_CHECK=1` to disable the on-launch
  check.
- `man/thicket.1`: `update` subcommand under SYNOPSIS/a COMMANDS section,
  and `THICKET_NO_UPDATE_CHECK` under an ENVIRONMENT section (new section
  if one doesn't already exist).
- `AGENTS.md`: update the `internal/tui` Concurrency bullet from "none...
  no `tea.Cmd`, goroutines, or channels are used anywhere in the codebase"
  to state the narrow, documented exception this design introduces
  (update-check `tea.Cmd` + toast-dismiss `tea.Tick`, nothing else) so the
  invariant statement stays accurate for future contributors.

## Acceptance criteria (from issue #28)

- [ ] Running an out-of-date `thicket` surfaces a visible, self-dismissing
  "update available: vX.Y.Z" toast in the status line during the TUI
  session (5s), not only after the session ends.
- [ ] `thicket update` downloads and installs the latest release for the
  current OS/arch, matching what `scripts/install.sh` would install for the
  same environment (same `PREFIX` and sudo-elevation behavior, since it
  literally runs that script).
- [ ] The update check never blocks TUI startup and never installs
  anything automatically — it only ever informs; `thicket update` remains
  a separate, user-initiated action.
- [ ] A failed/slow/timed-out check degrades silently — no toast, no
  error, no stderr output.
- [ ] `THICKET_NO_UPDATE_CHECK` disables the update check entirely.
- [ ] `README.md`, `man/thicket.1`, and `AGENTS.md` document the new
  subcommand, the opt-out, and the narrowed concurrency invariant.
