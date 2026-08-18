<!--
Thanks for contributing to thicket. Keep this description tight — reviewers
should be able to understand the change without reading the diff first.
Delete any section that truly doesn't apply.
-->

## Summary

<!-- One or two sentences: what changed and why. -->

## Related issue

<!-- Closes #123, or "None" if this wasn't filed as an issue first. -->

## Changes

<!--
Bullet the actual changes, not the diff. Call out anything a reviewer
wouldn't expect from the summary alone (e.g. a rename that touches many
files, a behavior change hiding inside a refactor).
-->

-

## Testing

<!--
`go test ./...` covers regressions but not everything — shell integration,
cross-process marks, and terminal rendering are manual-smoke-test-only per
AGENTS.md. Describe what you actually ran, not what should theoretically
pass.
-->

- [ ] `go build ./... && go vet ./... && go test ./...` pass locally
- [ ] Manually exercised the changed behavior in a real terminal (describe below)

<!-- What you did to verify, and what you saw: -->

## Screenshots / recording

<!-- Required for any change to render.go or the TUI's visible output. Delete if not applicable. -->

## Checklist

- [ ] Change stays within v1 scope (no file create/rename/delete/copy/move,
      no config file, no mouse support, no filesystem watching — see
      `docs/superpowers/specs/2026-08-15-thicket-tui-file-browser-design.md` §3)
      unless this PR is itself an approved amendment to that list
- [ ] No new goroutines/channels introduced under `internal/tui` (only
      `tea.Cmd`/`tea.Tick` are permitted — see the Concurrency bullet in
      `AGENTS.md`), or `AGENTS.md` is updated alongside this PR if that changed
- [ ] `internal/tui/model.go`'s single-source-of-truth invariant
      (`activePath` + cursor/scroll only; panes recomputed on every `View()`)
      still holds, or `AGENTS.md` is updated alongside this PR if that changed
- [ ] New/changed keybindings are reflected in `internal/tui/help.go`,
      `README.md`, and `man/thicket.1`
- [ ] Tests added/updated for the new behavior (`TestXxx_Behavior` style, in
      the existing `*_test.go` file for the package unless this is a new package)
- [ ] `AGENTS.md` updated if this changes architecture, conventions, or key files
