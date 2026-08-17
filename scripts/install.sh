#!/bin/sh
# Installs the latest (or a pinned) thicket release from GitHub Releases.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/freethewhat/Thicket/master/scripts/install.sh | sh
#   curl -fsSL .../install.sh | sh -s -- v0.1.0     # pin a version
#
# Env overrides:
#   PREFIX   install root for the binary and man page (default: /usr/local)
#   VERSION  release tag to install, e.g. v0.1.0 (default: latest)
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
	VERSION="${VERSION:-${1:-}}"

	need curl
	need tar
	need install

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
		VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" |
			grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
		[ -n "$VERSION" ] || err "could not determine the latest release; pass a version explicitly"
	fi

	version_num="${VERSION#v}"
	archive="thicket_${version_num}_${os}_${arch}.tar.gz"
	url="https://github.com/${REPO}/releases/download/${VERSION}/${archive}"

	workdir="$(mktemp -d)"
	trap 'rm -rf "$workdir"' EXIT

	echo "install.sh: downloading ${url}" >&2
	curl -fsSL "$url" -o "${workdir}/${archive}" || err "download failed: ${url}"

	tar -xzf "${workdir}/${archive}" -C "$workdir"

	bin_dir="${PREFIX}/bin"
	man_dir="${PREFIX}/share/man/man1"
	shell_dir="${PREFIX}/share/thicket/shell"

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

main "$@"
