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

# fetch wraps every network call so the retry and timeout policy lives in one
# place instead of being repeated, and inconsistently, at each call site.
#
# Retries matter: a connection dropped part way through a download is common
# enough on hotel wifi, mobile tethering and congested CI networks that failing
# on the first one turns a working install into a support question. The
# timeouts still bound how long a genuinely dead connection can hang, which is
# what stops `curl … | sh` waiting forever on a socket nobody will answer.
#
# Usage: fetch <max-seconds> [curl args...]
fetch() {
	fetch_timeout=$1
	shift
	curl -fsSL \
		--retry 3 --retry-delay 2 --retry-connrefused \
		--connect-timeout 10 --max-time "$fetch_timeout" \
		"$@"
}

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
	resolved=$(fetch 60 -o /dev/null -w '%{url_effective}' "$latest_url" 2>/dev/null) ||
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

# verify_checksum compares the downloaded archive against the checksums.txt
# published alongside every release.
#
# Without this the script would download a binary and put it on the user's PATH
# on the strength of TLS alone. TLS proves the bytes came from GitHub; it says
# nothing about whether the artifact GitHub is serving is the one the
# maintainer built. A compromised release token would otherwise install
# silently and run with the user's full privileges.
#
# Verification is required, not best-effort: a missing checksums.txt or a
# missing hashing tool aborts the install rather than continuing unverified.
verify_checksum() {
	work_dir=$1
	archive=$2
	version=$3

	if command -v sha256sum >/dev/null 2>&1; then
		actual=$(sha256sum "$work_dir/$archive" | cut -d' ' -f1)
	elif command -v shasum >/dev/null 2>&1; then
		actual=$(shasum -a 256 "$work_dir/$archive" | cut -d' ' -f1)
	else
		fail "neither sha256sum nor shasum was found; cannot verify the download"
	fi

	sums_url="https://github.com/${REPO}/releases/download/${version}/checksums.txt"
	fetch 60 -o "$work_dir/checksums.txt" "$sums_url" ||
		fail "could not download $sums_url; refusing to install an unverified binary"

	expected=$(grep " $archive\$" "$work_dir/checksums.txt" | cut -d' ' -f1)
	[ -n "$expected" ] ||
		fail "$archive is not listed in checksums.txt; refusing to install"

	[ "$actual" = "$expected" ] ||
		fail "checksum mismatch for $archive (expected $expected, got $actual); refusing to install"

	log "Checksum verified"
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
	# Timeouts matter more than usual here: this script is meant to be piped
	# into sh, where a connection that is accepted but never delivers data
	# would otherwise hang the install forever with nothing to report.
	fetch 300 -o "$work_dir/$archive" "$url" ||
		fail "download failed: $url (no partial install was performed)"

	verify_checksum "$work_dir" "$archive" "$version"

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
