#!/bin/sh
# Installs the latest (or a pinned) thicket release from GitHub Releases.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/freethewhat/Thicket/master/scripts/install.sh | sh
#   curl -fsSL .../install.sh | sh -s -- v0.1.0     # pin a version
#
# Env overrides:
#   PREFIX          install root for the binary and man page (default: /usr/local)
#   VERSION         release tag to install, e.g. v0.1.0 (default: latest on the selected channel)
#   THICKET_CHANNEL "stable" (default) or "beta" — beta resolves VERSION against
#                    the full release list (prereleases included, newest first)
#                    instead of GitHub's "latest" endpoint, which excludes them
set -eu

# The whole body runs inside main(), called only on the final line. POSIX
# shells must read a function definition through its closing brace before
# they can act on it, so by the time main() runs (and can shell out to
# sudo, which touches the controlling tty) the interpreter has already
# consumed the entire piped script. Without this, `curl URL | sh` can race:
# sh finishes executing and closes its end of the pipe while curl is still
# flushing, producing a harmless but alarming "curl: (23) Failure writing
# output to destination".
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

	os="$(uname -s)"
	case "$os" in
	Linux) os="linux" ;;
	Darwin) os="darwin" ;;
	*) err "unsupported OS: $os (thicket ships linux/darwin builds only)" ;;
	esac

	arch="$(uname -m)"
	case "$arch" in
	x86_64 | amd64) arch="amd64" ;;
	arm64 | aarch64) arch="arm64" ;;
	*) err "unsupported architecture: $arch (thicket ships amd64/arm64 builds only)" ;;
	esac

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

	version_num="${VERSION#v}"
	archive="thicket_${version_num}_${os}_${arch}.tar.gz"
	url="https://github.com/${REPO}/releases/download/${VERSION}/${archive}"

	workdir="$(mktemp -d)"
	trap 'rm -rf "$workdir"' EXIT

	echo "install.sh: downloading ${url}" >&2
	curl -fsSL "$url" -o "${workdir}/${archive}" || err "download failed: ${url}"

	tar -xzf "${workdir}/${archive}" -C "$workdir"

	writable_prefix_dir="$PREFIX"
	while [ ! -e "$writable_prefix_dir" ]; do
		writable_prefix_dir="$(dirname "$writable_prefix_dir")"
	done

	sudo_cmd=""
	if [ ! -w "$writable_prefix_dir" ] && [ "$(id -u)" -ne 0 ]; then
		need sudo
		sudo_cmd="sudo"
	fi

	$sudo_cmd install -d "$bin_dir" "$man_dir" "$shell_dir"
	$sudo_cmd install -m 755 "${workdir}/thicket-bin" "${bin_dir}/thicket-bin"
	$sudo_cmd install -m 644 "${workdir}/man/thicket.1" "${man_dir}/thicket.1"
	$sudo_cmd install -m 644 "${workdir}/shell/thicket.bash" "${shell_dir}/thicket.bash"
	$sudo_cmd install -m 644 "${workdir}/shell/thicket.zsh" "${shell_dir}/thicket.zsh"

	echo "install.sh: installed thicket-bin ${VERSION} to ${bin_dir}/thicket-bin" >&2
	echo "install.sh: source the shell wrapper to enable 'thicket' from your rc file:" >&2
	echo "  source ${shell_dir}/thicket.bash  # bash" >&2
	echo "  source ${shell_dir}/thicket.zsh   # zsh" >&2
}

err() {
	echo "install.sh: $*" >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || err "'$1' is required but not found in PATH"
}

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

main "$@"
