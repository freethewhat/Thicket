# Beta Channel Follow-ups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the 3 deferred polish items from #31's whole-branch review (issue #33): derive the update-toast's "beta" label from the actual channel instead of a bare hyphen check, add a semver-aware downgrade guard to `install.sh`'s beta-channel VERSION resolution, and rename `install.sh`'s internal `CHANNEL` local to lowercase `channel` so it can't be confused with the public `THICKET_CHANNEL` env var.

**Architecture:** Two independent tasks, no shared state. Task 1 is a pure Go signature change (`updateNoticeText` gains a `channel` parameter) confined to `internal/tui`. Task 2 is a POSIX `sh` change confined to `scripts/install.sh`: hoists the three `PREFIX`-derived directory vars above the VERSION-resolution block, adds a self-contained `is_newer()` awk-based semver comparator (ported 1:1 from `internal/update.IsNewer`/`isNewerSemver`/`comparePrerelease`), and uses it to skip installing an auto-resolved VERSION that is not actually newer than the already-installed `thicket-bin`. The `CHANNEL`→`channel` rename (item 3) rides along in Task 2 since it touches the exact lines Task 2 already edits.

**Tech Stack:** Go 1.24 stdlib (`strings`, `testing`), POSIX `sh` + `awk` (no bash-isms, no GNU-only flags — `scripts/install.sh` targets Linux and Darwin).

**Spec:** [issue #33 — Beta release channel: 3 deferred polish items from #31's review](https://github.com/freethewhat/Thicket/issues/33)

## Global Constraints

- No behavior change for the stable channel's on-launch toast wording (`TestUpdateNoticeText_StableTagHasNoLabel` must keep passing unmodified in intent, only its call signature changes).
- `install.sh` stays a single POSIX `#!/bin/sh` script — no bashisms, no GNU-only `sort -V`/`grep -P`, since it must run correctly via `sh -s --` on both Linux and Darwin (see `internal/update/run.go`'s `Run`).
- An explicit `VERSION` (env var or positional arg) always bypasses the new downgrade guard — the guard only applies when `install.sh` auto-resolves "latest on the selected channel" itself.
- `THICKET_CHANNEL` (the public env var) is untouched by the item-3 rename; only the internal shell local changes case.

---

### Task 1: `updateNoticeText` derives the beta label from the channel, not a hyphen check

**Files:**
- Modify: `internal/tui/update_check.go:61-73`
- Modify: `internal/tui/update.go:95-96` (call site)
- Modify: `internal/tui/update_check_test.go:65-80` (existing tests' call signature)

**Interfaces:**
- Produces: `updateNoticeText(tag, channel string) string` — `channel` is one of `update.ChannelStable`/`update.ChannelBeta`/`""` (empty means stable, matching `Model.checkChannel`'s documented zero value). Replaces the previous `updateNoticeText(tag string) string`.

- [ ] **Step 1: Write the failing tests**

Replace the two existing `updateNoticeText` tests in `internal/tui/update_check_test.go:65-80` and add a new one covering the bug this fixes (an `-rc.N`/`-alpha.N`-shaped tag on the stable channel must NOT be labeled beta):

```go
func TestUpdateNoticeText_StableChannelHasNoLabel(t *testing.T) {
	got := updateNoticeText("v1.2.3", update.ChannelStable)
	if strings.Contains(got, "beta") {
		t.Errorf("updateNoticeText(%q, stable) = %q, want no beta label", "v1.2.3", got)
	}
	if !strings.Contains(got, "update available: v1.2.3") {
		t.Errorf("updateNoticeText(%q, stable) = %q, missing tag", "v1.2.3", got)
	}
}

func TestUpdateNoticeText_BetaChannelLabelsBeta(t *testing.T) {
	got := updateNoticeText("v1.3.0-beta.1", update.ChannelBeta)
	if !strings.Contains(got, "beta update available: v1.3.0-beta.1") {
		t.Errorf("updateNoticeText(%q, beta) = %q, want a beta-labeled notice", "v1.3.0-beta.1", got)
	}
}

func TestUpdateNoticeText_NonBetaPrereleaseTagOnStableChannelHasNoLabel(t *testing.T) {
	// A future "-rc.N"/"-alpha.N" tag reached on the stable channel must
	// not be mislabeled "beta" by a bare hyphen check (issue #33 item 1).
	got := updateNoticeText("v2.0.0-rc.1", update.ChannelStable)
	if strings.Contains(got, "beta") {
		t.Errorf("updateNoticeText(%q, stable) = %q, want no beta label", "v2.0.0-rc.1", got)
	}
}
```

This requires importing `"thicket/internal/update"` in `internal/tui/update_check_test.go`; check its current `import` block first (`internal/tui/update_check_test.go:1-8`) and add the import if not already present.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/... -run TestUpdateNoticeText -v`
Expected: FAIL — compile error (`too many arguments in call to updateNoticeText`) since the production signature hasn't changed yet.

- [ ] **Step 3: Update the production code**

In `internal/tui/update_check.go`, replace the `updateNoticeText` function (currently lines 61-73):

```go
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
```

In `internal/tui/update.go`, update the call site (currently line 96):

```go
	case updateAvailableMsg:
		m.updateNotice = updateNoticeText(msg.tag, m.checkChannel)
```

`internal/tui/update_check.go` already imports `"thicket/internal/update"` (used by `latestTagFunc`/`checkUpdateCmd`) — no new import needed there. `internal/tui/update.go` needs no import change since it only passes `m.checkChannel` (a `string` field), not the `update` package itself.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/... -v`
Expected: PASS — all `internal/tui` tests, including the three from Step 1 and every pre-existing test in the package (confirms the call-site change didn't break `TestModel_UpdateAvailableMsgSetsNoticeAndSchedulesDismiss`-style tests that construct `updateAvailableMsg` directly).

- [ ] **Step 5: Run static checks**

Run: `go vet ./internal/tui/...`
Expected: clean, no output.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/update_check.go internal/tui/update.go internal/tui/update_check_test.go
git commit -m "fix(tui): derive update-toast beta label from channel, not tag hyphen"
```

---

### Task 2: `install.sh` — semver downgrade guard on the beta path + `CHANNEL`→`channel` rename

**Files:**
- Modify: `scripts/install.sh` (whole `main()` function, `need()` block, and the `#   THICKET_CHANNEL` doc comment is unaffected — only the internal var name changes)

**Interfaces:**
- Produces: `is_newer <current> <latest>` — a shell function that prints `yes` to stdout if `<latest>` has higher semver precedence than `<current>` (mirrors `internal/update.IsNewer`'s exact rules, including the numeric-vs-lexical prerelease-field comparison), else prints `no`. Both arguments accept an optional leading `v`. `<current>` of `"dev"` or either argument failing to parse as `vX.Y.Z[-pre]`/`X.Y.Z[-pre]` both yield `no`.

There is no automated test harness for `install.sh` (per `AGENTS.md`: shell integration is manual-smoke-test-only — no bats/shunit2 dependency exists in the repo and none should be added for one script). Verification is a scripted manual smoke test run directly via the shell, using the exact case table from `internal/update/check_test.go`'s `TestIsNewer` so the two implementations are checked against the same ground truth.

- [ ] **Step 1: Read the current file to confirm line numbers haven't drifted**

Run: `cat -n scripts/install.sh`
Expected: lines 1-107 matching the structure described below (if it has drifted, re-derive the exact line ranges before editing — do not guess).

- [ ] **Step 2: Hoist the `PREFIX`-derived directory vars, rename `CHANNEL`, add `need awk`**

In `scripts/install.sh`, replace the `main()` function's opening block (originally lines 24-32):

```sh
main() {
	REPO="freethewhat/Thicket"
	PREFIX="${PREFIX:-/usr/local}"
	channel="${THICKET_CHANNEL:-stable}"
	VERSION="${VERSION:-${1:-}}"

	bin_dir="${PREFIX}/bin"
	man_dir="${PREFIX}/share/man/man1"
	shell_dir="${PREFIX}/share/thicket/shell"

	need curl
	need tar
	need install
	need awk
```

This hoists `bin_dir`/`man_dir`/`shell_dir` (originally defined later, at lines 71-73, right before the `install -d`/`install -m` calls) up so Step 3's downgrade guard can check for an already-installed `thicket-bin` before downloading anything. Delete the original `bin_dir`/`man_dir`/`shell_dir` block further down (originally lines 71-73, immediately after `tar -xzf "${workdir}/${archive}" -C "$workdir"` and before `writable_prefix_dir="$PREFIX"`) — it becomes a duplicate.

Rename the other `CHANNEL` reference (originally line 49, inside the VERSION-resolution `if`):

```sh
		if [ "$channel" = "beta" ]; then
```

- [ ] **Step 3: Add the downgrade guard to the VERSION-resolution block**

Replace the `if [ -z "$VERSION" ]; then ... fi` block (originally lines 48-57) with:

```sh
	if [ -z "$VERSION" ]; then
		if [ "$channel" = "beta" ]; then
			releases_url="https://api.github.com/repos/${REPO}/releases"
		else
			releases_url="https://api.github.com/repos/${REPO}/releases/latest"
		fi
		VERSION="$(curl -fsSL "$releases_url" |
			grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
		[ -n "$VERSION" ] || err "could not determine the latest release; pass a version explicitly"

		# GitHub's /releases list (the beta channel's source) is ordered by
		# creation date, not semver precedence — a stable hotfix tagged after
		# an outstanding beta would otherwise silently move a beta-channel
		# user backward. Guard only applies to an auto-resolved VERSION (an
		# explicit VERSION/positional-arg pin always wins) and only when a
		# prior thicket-bin is actually present to compare against; a "dev"
		# local build is never treated as a real prior release worth
		# protecting.
		if [ -x "${bin_dir}/thicket-bin" ]; then
			current="$("${bin_dir}/thicket-bin" -v 2>/dev/null | awk '{print $2}')"
			if [ -n "$current" ] && [ "$current" != "dev" ] && [ "$(is_newer "$current" "$VERSION")" != "yes" ]; then
				echo "install.sh: installed thicket-bin ${current} is already up to date with the latest ${channel} release (${VERSION}); nothing to do" >&2
				exit 0
			fi
		fi
	fi
```

- [ ] **Step 4: Add the `is_newer` function**

In `scripts/install.sh`, insert after the `need()` function (originally lines 103-105, right before `main "$@"`):

```sh
# is_newer prints "yes" if $2 has higher semver precedence than $1, else
# "no". Ported 1:1 from internal/update.IsNewer/isNewerSemver/
# comparePrerelease (see internal/update/check.go) so install.sh's
# downgrade guard agrees with the Go client's own update-availability
# logic. Both arguments accept an optional leading "v"; "$1" == "dev" or
# either argument failing to parse as X.Y.Z[-pre] both yield "no".
is_newer() {
	awk -v cur="$1" -v lat="$2" '
	function parse(v, out,    core, i, parts, n) {
		sub(/^v/, "", v)
		out["ok"] = 0
		i = index(v, "-")
		if (i > 0) {
			core = substr(v, 1, i-1)
			out["pre"] = substr(v, i+1)
		} else {
			core = v
			out["pre"] = ""
		}
		n = split(core, parts, ".")
		if (n != 3) return
		for (i = 1; i <= 3; i++) {
			if (parts[i] !~ /^[0-9]+$/) return
		}
		out["major"] = parts[1] + 0
		out["minor"] = parts[2] + 0
		out["patch"] = parts[3] + 0
		out["ok"] = 1
	}
	function cmp_pre(a, b,    as, bs, na, nb, i, an, bn) {
		na = split(a, as, ".")
		nb = split(b, bs, ".")
		for (i = 1; i <= na && i <= nb; i++) {
			if (as[i] == bs[i]) continue
			if (as[i] ~ /^[0-9]+$/ && bs[i] ~ /^[0-9]+$/) {
				an = as[i] + 0; bn = bs[i] + 0
				return (an < bn) ? -1 : 1
			}
			return (as[i] < bs[i]) ? -1 : 1
		}
		if (na < nb) return -1
		if (na > nb) return 1
		return 0
	}
	BEGIN {
		if (cur == "dev") { print "no"; exit }
		parse(cur, c)
		parse(lat, l)
		if (!c["ok"] || !l["ok"]) { print "no"; exit }
		if (c["major"] != l["major"]) { print (l["major"] > c["major"]) ? "yes" : "no"; exit }
		if (c["minor"] != l["minor"]) { print (l["minor"] > c["minor"]) ? "yes" : "no"; exit }
		if (c["patch"] != l["patch"]) { print (l["patch"] > c["patch"]) ? "yes" : "no"; exit }
		if (c["pre"] == l["pre"]) { print "no"; exit }
		if (c["pre"] == "") { print "no"; exit }
		if (l["pre"] == "") { print "yes"; exit }
		print (cmp_pre(c["pre"], l["pre"]) < 0) ? "yes" : "no"
	}
	'
}
```

- [ ] **Step 5: Verify `sh -n` syntax-checks the whole script**

Run: `sh -n scripts/install.sh`
Expected: no output, exit 0.

- [ ] **Step 6: Manual smoke test — `is_newer` against the Go `TestIsNewer` case table**

Extract `is_newer` into a scratch file and run it against every case from `internal/update/check_test.go:20-36`, confirming each matches Go's `IsNewer`:

```bash
sed -n '/^is_newer() {/,/^}/p' scripts/install.sh > /tmp/is_newer_check.sh
cat >> /tmp/is_newer_check.sh <<'EOF'
check() {
	got="$(is_newer "$1" "$2")"
	want="$3"
	if [ "$got" = "$want" ]; then echo "ok   $1 $2 -> $got"; else echo "FAIL $1 $2 -> $got want $want"; fi
}
check "v1.2.3" "v1.2.3" no
check "v2.0.0" "v1.9.9" no
check "v1.9.9" "v2.0.0" yes
check "v1.2.3" "v1.3.0" yes
check "v1.2.3" "v1.2.4" yes
check "v1.2.4" "v1.2.3" no
check "v1.2.3" "not-a-version" no
check "not-a-version" "v1.2.3" no
check "dev" "v99.0.0" no
check "1.2.3" "1.2.4" yes
check "v1.0.0-beta.1" "v1.0.0-beta.2" yes
check "v1.0.0-beta.2" "v1.0.0-beta.1" no
check "v1.0.0-beta.1" "v1.0.0-beta.1" no
check "v1.0.0-beta.1" "v1.0.0" yes
check "v1.0.0" "v1.0.0-beta.1" no
check "v1.0.0" "v1.1.0-beta.1" yes
check "v1.0.0-beta.9" "v1.0.0-beta.10" yes
EOF
sh /tmp/is_newer_check.sh
rm /tmp/is_newer_check.sh
```

Expected: every line prints `ok`; zero `FAIL` lines.

- [ ] **Step 7: Manual smoke test — the guard actually blocks a backward move**

Simulate the exact scenario from issue #33 item 2 (a beta-channel user's newer beta getting shadowed by an older stable hotfix tagged more recently) using a fake `thicket-bin` and a stubbed `curl`, without touching the real network or `/usr/local`:

```bash
workdir="$(mktemp -d)"
mkdir -p "$workdir/bin"
cat > "$workdir/bin/thicket-bin" <<'EOF'
#!/bin/sh
echo "thicket 1.3.0-beta.2"
EOF
chmod +x "$workdir/bin/thicket-bin"

PREFIX="$workdir" THICKET_CHANNEL=beta VERSION= \
  sh -c '
    . /dev/stdin <<SCRIPT
$(sed -n "/^is_newer() {/,/^}/p" scripts/install.sh)
SCRIPT
    bin_dir="'"$workdir"'/bin"
    current="$("$bin_dir/thicket-bin" -v | awk "{print \$2}")"
    VERSION="v1.2.1"
    if [ -n "$current" ] && [ "$current" != "dev" ] && [ "$(is_newer "$current" "$VERSION")" != "yes" ]; then
      echo "GUARD: would skip install (correct — 1.2.1 is not newer than 1.3.0-beta.2)"
    else
      echo "GUARD: would install (BUG if reached)"
    fi
  '
rm -rf "$workdir"
```

Expected: prints `GUARD: would skip install (correct — 1.2.1 is not newer than 1.3.0-beta.2)`.

- [ ] **Step 8: Run the existing Go suite to confirm no cross-package breakage**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS — `scripts/install.sh` is not Go source, but `internal/update` embeds it (`scripts/embed.go`'s `//go:embed install.sh`) via `internal/update.Run`, so confirm the embed still compiles.

- [ ] **Step 9: Commit**

```bash
git add scripts/install.sh
git commit -m "fix(install): guard beta-channel auto-update against semver downgrade; rename CHANNEL local"
```

---

## Self-Review

**Spec coverage:**
1. Bare-hyphen beta label (issue item 1) → Task 1, fixed by threading `channel` through `updateNoticeText`. ✅
2. No version-comparison guard on the beta path (issue item 2) → Task 2 Steps 2-4, `is_newer` + guard in the VERSION-resolution block, applies only to auto-resolved VERSION, skips for `dev`/unparsable current. ✅
3. `CHANNEL` local name collision risk (issue item 3) → Task 2 Step 2, renamed to `channel` at both use sites; `THICKET_CHANNEL` (the public var) and every doc comment referencing it are untouched. ✅

**Placeholder scan:** No TBD/TODO/"add error handling"/"similar to Task N" markers — every step has literal code. ✅

**Type consistency:** `updateNoticeText(tag, channel string) string` used identically at its one call site (`internal/tui/update.go:96`) and in all three Task 1 tests. `is_newer` used identically (`is_newer "$current" "$VERSION"`) in Task 2 Step 3 and both Step 6/7 smoke tests. ✅
