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
- Provide a `thicket update` (equivalently `thicket-bin update`) subcommand
  that installs the latest release in place, respecting `PREFIX` and the
  sudo-elevation behavior `scripts/install.sh` already implements.
- Respect the "no config file" v1 constraint: opt-out is an environment
  variable, not a config file.
- Keep `internal/tui`'s synchronous-only invariant (no goroutines,
  channels, or `tea.Cmd`) untouched — all update-check machinery lives in
  `cmd/thicket` and a new `internal/update` package, never inside
  `internal/tui`.

## Non-goals

- No new interactive confirmation/progress UI — `thicket update` is a
  CLI-only, stdio-inherited subprocess invocation of the existing
  `scripts/install.sh`, not a reimplementation of its download/verify/swap
  logic in Go.
- No checksum verification beyond what `scripts/install.sh` already does
  today (it does not verify against `checksums.txt`; that gap is out of
  scope for this change and unaffected by it).
- No caching of the "latest release" result across invocations.

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
  https://api.github.com/repos/freethewhat/Thicket/releases/latest`,
  decode JSON, return `tag_name`. Uses `encoding/json` (install.sh's
  grep/sed extraction is a shell-only constraint that doesn't apply here).
  Respects `ctx`'s deadline via `http.NewRequestWithContext`.
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

Runs standalone — never starts `tea.NewProgram`.

**Update-check on launch** (top of `main()`, TUI path only):

```go
var updateCh chan string
if os.Getenv("THICKET_NO_UPDATE_CHECK") == "" && version != "dev" {
    updateCh = make(chan string, 1)
    go func() {
        ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
        defer cancel()
        if tag, err := update.LatestTag(ctx); err == nil && update.IsNewer(version, tag) {
            updateCh <- tag
        }
    }()
}
```

The goroutine lives in `cmd/thicket`, outside `internal/tui`; it does not
touch the Bubble Tea model or violate the TUI's synchronous-only
invariant. Startup proceeds immediately into `tea.NewProgram` without
waiting on this goroutine — it races the user's TUI session in the
background.

**Surfacing the notice** (after `p.Run()` returns, TUI alt-screen already
torn down, before/after `writeSelection`):

```go
if updateCh != nil {
    select {
    case tag := <-updateCh:
        fmt.Fprintf(os.Stderr, "thicket: update available: %s — run 'thicket-bin update' to upgrade\n", tag)
    case <-time.After(300 * time.Millisecond):
    }
}
```

By the time a user has finished navigating and quit the TUI, the 2s-bounded
check has almost always already completed; the 300ms grace wait is a
belt-and-suspenders non-blocking cap, not the primary wait mechanism. On
timeout, network failure, malformed response, opt-out, or `version ==
"dev"`, nothing is printed and nothing else changes — completely silent,
never an error, never affects the exit code chosen by `finalModel.Result()`.

### Error handling

| Condition | Behavior |
|---|---|
| Network unreachable / slow / malformed JSON during check | Silent — no stderr, no exit code effect |
| `THICKET_NO_UPDATE_CHECK` set (any non-empty value) | Check never runs |
| `version == "dev"` | Check never runs |
| `thicket update` — curl/tar/install/sudo failure inside `install.sh` | Child process's own `err()` writes to stderr (inherited), `thicket-bin update` exits 1 |
| `thicket update` — success | `install.sh`'s existing success messages print (inherited stdout/stderr), `thicket-bin update` exits 0 |

### Testing

- `internal/update`:
  - `IsNewer`: table test — equal versions, older, newer (major/minor/patch
    each), malformed tags on either side, `current == "dev"`.
  - `LatestTag`: `httptest.Server` — happy path (valid JSON), 404,
    malformed JSON body, context already expired/short timeout.
  - `Run`: not unit-tested (shells out, installs, requires network/sudo in
    the general case) — covered by a manual smoke test: `go build -o
    thicket-bin ./cmd/thicket && PREFIX=$(mktemp -d) ./thicket-bin update`,
    confirm the binary lands under `$PREFIX/bin/thicket-bin`. Consistent
    with `AGENTS.md`'s existing carve-out that shell/tty integration is
    manual-only.
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

## Acceptance criteria (from issue #28)

- [ ] Running an out-of-date `thicket` surfaces a visible-but-non-blocking
  "update available: vX.Y.Z" notice on stderr after the TUI session ends.
- [ ] `thicket update` downloads and installs the latest release for the
  current OS/arch, matching what `scripts/install.sh` would install for the
  same environment (same `PREFIX` and sudo-elevation behavior, since it
  literally runs that script).
- [ ] No network call blocks TUI startup or the Bubble Tea event loop; a
  failed/slow check degrades silently.
- [ ] `THICKET_NO_UPDATE_CHECK` disables the update check entirely.
- [ ] `README.md` and `man/thicket.1` document the new subcommand and the
  opt-out.
</content>
