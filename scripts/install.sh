#!/bin/sh
# Install AliasDeck without Homebrew.
#
# Usage:
#   curl -sSL https://raw.githubusercontent.com/angeltonio/aliasdeck/main/scripts/install.sh | sh
#
# Environment overrides:
#   ALIASDECK_VERSION   Tag to install, e.g. "v0.1.0". Defaults to the
#                       latest published release.
#   ALIASDECK_INSTALL_DIR
#                       Directory the binary is placed in. Defaults to
#                       /usr/local/bin, falling back to $HOME/.local/bin
#                       when that is not writable.
#
# This script is intentionally POSIX sh, not bash: it must run under the
# minimal shell any container base image ships with `sh -c "curl ... | sh"`.
set -eu

REPO="angeltonio/aliasdeck"
BIN_NAME="aliasdeck"

log() {
	printf '%s\n' "$*" >&2
}

fail() {
	log "install.sh: error: $*"
	exit 1
}

detect_os() {
	uname_s=$(uname -s)
	case "$uname_s" in
	Darwin) echo "darwin" ;;
	Linux) echo "linux" ;;
	*)
		fail "unsupported operating system: $uname_s (AliasDeck ships prebuilt binaries for macOS and Linux only)"
		;;
	esac
}

detect_arch() {
	uname_m=$(uname -m)
	case "$uname_m" in
	x86_64 | amd64) echo "amd64" ;;
	arm64 | aarch64) echo "arm64" ;;
	*)
		fail "unsupported architecture: $uname_m (AliasDeck ships prebuilt binaries for amd64 and arm64 only)"
		;;
	esac
}

# resolve_version prints the tag to install: the caller's override, or the
# latest release's tag resolved from GitHub's redirect without needing the
# GitHub API or a JSON parser.
resolve_version() {
	if [ -n "${ALIASDECK_VERSION:-}" ]; then
		echo "$ALIASDECK_VERSION"
		return
	fi

	latest_url="https://github.com/${REPO}/releases/latest"
	resolved=$(curl -fsSL -o /dev/null -w '%{url_effective}' "$latest_url" 2>/dev/null) ||
		fail "could not resolve the latest release from $latest_url"

	# GitHub redirects /releases/latest to /releases/tag/<tag> only when a
	# release actually exists. Without one it redirects to the bare
	# /releases listing instead, which has no tag segment to extract.
	case "$resolved" in
	*/releases/tag/*)
		tag=${resolved##*/releases/tag/}
		;;
	*)
		fail "no published release was found for $REPO yet"
		;;
	esac
	echo "$tag"
}

main() {
	command -v curl >/dev/null 2>&1 || fail "curl is required but was not found on PATH"
	command -v tar >/dev/null 2>&1 || fail "tar is required but was not found on PATH"

	os=$(detect_os)
	arch=$(detect_arch)
	version=$(resolve_version)
	version_number=${version#v}

	install_dir="${ALIASDECK_INSTALL_DIR:-/usr/local/bin}"
	if [ ! -w "$install_dir" ] && [ -z "${ALIASDECK_INSTALL_DIR:-}" ]; then
		install_dir="$HOME/.local/bin"
	fi
	mkdir -p "$install_dir" || fail "could not create install directory: $install_dir"

	archive="${BIN_NAME}_${version_number}_${os}_${arch}.tar.gz"
	url="https://github.com/${REPO}/releases/download/${version}/${archive}"

	work_dir=$(mktemp -d) || fail "could not create a temporary directory"
	trap 'rm -rf "$work_dir"' EXIT

	log "Downloading $url"
	curl -fsSL -o "$work_dir/$archive" "$url" ||
		fail "download failed: $url (no partial install was performed)"

	tar -xzf "$work_dir/$archive" -C "$work_dir" "$BIN_NAME" ||
		fail "could not extract $BIN_NAME from $archive"

	chmod +x "$work_dir/$BIN_NAME"
	mv "$work_dir/$BIN_NAME" "$install_dir/$BIN_NAME" ||
		fail "could not install to $install_dir (try setting ALIASDECK_INSTALL_DIR to a writable directory)"

	log "Installed $BIN_NAME $version to $install_dir/$BIN_NAME"
	case ":$PATH:" in
	*":$install_dir:"*) ;;
	*) log "Note: $install_dir is not on PATH. Add it to your shell rc file to run '$BIN_NAME' directly." ;;
	esac
}

main "$@"
