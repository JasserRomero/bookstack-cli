#!/bin/sh
# Install bsk (bookstack-cli) on Linux or macOS.
#
#   sh install.sh                 install the latest release
#   sh install.sh --uninstall     remove the installed binary
#
# Environment:
#   BSK_VERSION       release to install (v0.1.0 or 0.1.0); default: latest
#   BSK_INSTALL_DIR   install directory; default: $HOME/.local/bin
#   GITHUB_TOKEN, GH_TOKEN
#                     token used with the GitHub API when gh is unavailable
#
# Download strategy, in order: gh (installed and authenticated), the GitHub API
# with GITHUB_TOKEN/GH_TOKEN, anonymous HTTPS (public repository only).
#
# Test-only overrides (unset in normal use; they never change default behavior):
#   BSK_INSTALL_OS        replaces `uname -s` (e.g. Linux, Darwin)
#   BSK_INSTALL_ARCH      replaces `uname -m` (e.g. x86_64, arm64)
#   BSK_INSTALL_BASE_URL  forces anonymous downloads from
#                         $BSK_INSTALL_BASE_URL/<tag>/<file>, skipping gh and tokens
#   BSK_INSTALL_API_URL   GitHub API base URL (default https://api.github.com)
set -eu

REPO="JasserRomero/bookstack-cli"
API_URL="${BSK_INSTALL_API_URL:-https://api.github.com}"
BASE_URL="${BSK_INSTALL_BASE_URL:-}"
INSTALL_DIR="${BSK_INSTALL_DIR:-${HOME}/.local/bin}"

say() {
	printf '%s\n' "$*"
}

die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

usage() {
	say "Usage: install.sh [--uninstall]"
	say ""
	say "Installs bsk to \$BSK_INSTALL_DIR (default \$HOME/.local/bin)."
	say "Set BSK_VERSION (e.g. v0.1.0) to pin a release; default is the latest."
}

has() {
	command -v "$1" >/dev/null 2>&1
}

config_dir() {
	case "${XDG_CONFIG_HOME:-}" in
	/*) say "${XDG_CONFIG_HOME}/bookstack-cli" ;;
	*) say "${HOME}/.config/bookstack-cli" ;;
	esac
}

uninstall() {
	target="${INSTALL_DIR}/bsk"
	if [ -e "$target" ]; then
		rm -f "$target"
		say "Removed ${target}"
	else
		say "bsk is not installed in ${INSTALL_DIR}"
	fi
	say "Configuration (including credentials) was kept in $(config_dir); delete it manually if you no longer need it."
}

detect_platform() {
	os_raw="${BSK_INSTALL_OS:-$(uname -s)}"
	arch_raw="${BSK_INSTALL_ARCH:-$(uname -m)}"
	case "$os_raw" in
	Linux | linux) OS=linux ;;
	Darwin | darwin) OS=darwin ;;
	*) die "unsupported operating system: ${os_raw} (supported: Linux, macOS; on Windows use install.ps1)" ;;
	esac
	case "$arch_raw" in
	x86_64 | amd64) ARCH=amd64 ;;
	aarch64 | arm64) ARCH=arm64 ;;
	*) die "unsupported architecture: ${arch_raw} (supported: x86_64/amd64, aarch64/arm64)" ;;
	esac
}

# fetch URL OUTPUT: anonymous download.
fetch() {
	if has curl; then
		curl -fsSL --retry 3 -o "$2" "$1"
	elif has wget; then
		wget -q -t 3 -O "$2" "$1"
	else
		die "curl or wget is required"
	fi
}

# api_fetch URL OUTPUT ACCEPT: authenticated GitHub API request. The token is
# written to a private header file so it never appears in arguments or output.
api_fetch() {
	has curl || die "curl is required to download with GITHUB_TOKEN/GH_TOKEN"
	curl -fsSL --retry 3 -o "$2" -H @"${TMP}/auth.header" -H "Accept: $3" "$1"
}

# asset_url NAME < release.json: API URL of the release asset called NAME.
asset_url() {
	awk -v want="$1" '
		{ s = s $0 }
		END {
			while (match(s, /"(url|name)"[[:space:]]*:[[:space:]]*"[^"]*"/)) {
				m = substr(s, RSTART, RLENGTH)
				s = substr(s, RSTART + RLENGTH)
				v = m
				sub(/^"[a-z]+"[[:space:]]*:[[:space:]]*"/, "", v)
				sub(/"$/, "", v)
				if (m ~ /^"url"/) {
					if (v ~ /\/releases\/assets\/[0-9]+$/) u = v
				} else if (v == want && u != "") {
					print u
					exit
				}
			}
		}'
}

tag_name() {
	sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$1" | head -n 1
}

private_hint() {
	say "the repository may be private: install and authenticate the GitHub CLI (gh auth login), or set GITHUB_TOKEN or GH_TOKEN to a token that can read ${REPO}"
}

# download_gh, download_api, download_anon: fetch ${ARCHIVE} and checksums.txt
# into ${TMP}; TAG is empty for the latest release and set on return.
download_gh() {
	if [ -z "$TAG" ]; then
		TAG="$(gh release view -R "$REPO" --json tagName -q .tagName)" ||
			die "gh could not find the latest release of ${REPO}"
		set_archive
	fi
	say "Downloading ${ARCHIVE} (${TAG}) with gh..."
	attempt=1
	until gh release download "$TAG" -R "$REPO" -p "$ARCHIVE" -p checksums.txt -D "$TMP" --clobber; do
		[ "$attempt" -lt 3 ] || die "gh could not download release ${TAG} of ${REPO}"
		attempt=$((attempt + 1))
		sleep 2
	done
}

download_api() {
	(
		umask 077
		printf 'Authorization: Bearer %s\n' "$TOKEN" >"${TMP}/auth.header"
	)
	if [ -n "$TAG" ]; then
		release_url="${API_URL}/repos/${REPO}/releases/tags/${TAG}"
	else
		release_url="${API_URL}/repos/${REPO}/releases/latest"
	fi
	api_fetch "$release_url" "${TMP}/release.json" "application/vnd.github+json" ||
		die "could not read release ${TAG:-latest} of ${REPO} with the provided token (missing release or no access)"
	if [ -z "$TAG" ]; then
		TAG="$(tag_name "${TMP}/release.json")"
		[ -n "$TAG" ] || die "could not determine the latest release of ${REPO}"
		set_archive
	fi
	say "Downloading ${ARCHIVE} (${TAG}) with the GitHub API..."
	for name in "$ARCHIVE" checksums.txt; do
		url="$(asset_url "$name" <"${TMP}/release.json")"
		[ -n "$url" ] || die "release ${TAG} has no asset named ${name}"
		api_fetch "$url" "${TMP}/${name}" "application/octet-stream" ||
			die "could not download ${name} from release ${TAG}"
	done
}

download_anon() {
	if [ -z "$TAG" ]; then
		fetch "${API_URL}/repos/${REPO}/releases/latest" "${TMP}/release.json" ||
			die "could not find the latest release of ${REPO}; $(private_hint)"
		TAG="$(tag_name "${TMP}/release.json")"
		[ -n "$TAG" ] || die "could not determine the latest release of ${REPO}"
		set_archive
	fi
	base="${BASE_URL:-https://github.com/${REPO}/releases/download}"
	say "Downloading ${ARCHIVE} (${TAG})..."
	for name in "$ARCHIVE" checksums.txt; do
		fetch "${base}/${TAG}/${name}" "${TMP}/${name}" ||
			die "could not download ${name} from release ${TAG}; $(private_hint)"
	done
}

set_archive() {
	ARCHIVE="bsk_${TAG#v}_${OS}_${ARCH}.tar.gz"
}

sha256() {
	if has sha256sum; then
		sha256sum "$1" | awk '{ print $1 }'
	elif has shasum; then
		shasum -a 256 "$1" | awk '{ print $1 }'
	else
		die "sha256sum or shasum is required to verify the download"
	fi
}

verify() {
	expected="$(awk -v f="$ARCHIVE" '$2 == f || $2 == "*" f { print $1; exit }' "${TMP}/checksums.txt")"
	[ -n "$expected" ] || die "checksums.txt has no entry for ${ARCHIVE}; refusing to install"
	actual="$(sha256 "${TMP}/${ARCHIVE}")"
	[ "$expected" = "$actual" ] ||
		die "checksum mismatch for ${ARCHIVE} (expected ${expected}, got ${actual}); refusing to install"
	say "Checksum verified (SHA-256 ${actual})"
}

install_binary() {
	mkdir -p "${TMP}/extract"
	tar -xzf "${TMP}/${ARCHIVE}" -C "${TMP}/extract"
	[ -f "${TMP}/extract/bsk" ] || die "${ARCHIVE} does not contain bsk"
	mkdir -p "$INSTALL_DIR"
	staged="${INSTALL_DIR}/.bsk.new.$$"
	cp "${TMP}/extract/bsk" "$staged"
	chmod 755 "$staged"
	mv -f "$staged" "${INSTALL_DIR}/bsk"
	say "Installed ${INSTALL_DIR}/bsk"
	"${INSTALL_DIR}/bsk" --version
	case ":${PATH}:" in
	*":${INSTALL_DIR}:"*) ;;
	*)
		say ""
		say "Note: ${INSTALL_DIR} is not in your PATH. Add it to your shell profile:"
		say "  export PATH=\"${INSTALL_DIR}:\$PATH\""
		;;
	esac
}

main() {
	case "${1:-}" in
	"") ;;
	--uninstall)
		uninstall
		return
		;;
	-h | --help)
		usage
		return
		;;
	*)
		usage >&2
		die "unknown argument: $1"
		;;
	esac

	detect_platform
	TMP="$(mktemp -d 2>/dev/null || mktemp -d -t bsk)"
	trap 'rm -rf "$TMP"' EXIT
	trap 'exit 1' HUP INT TERM

	TAG=""
	if [ -n "${BSK_VERSION:-}" ]; then
		TAG="v${BSK_VERSION#v}"
		set_archive
	fi

	TOKEN="${GITHUB_TOKEN:-${GH_TOKEN:-}}"
	if [ -n "$BASE_URL" ]; then
		download_anon
	elif has gh && gh auth token >/dev/null 2>&1; then
		download_gh
	elif [ -n "$TOKEN" ]; then
		download_api
	else
		download_anon
	fi

	verify
	install_binary
}

main "$@"
