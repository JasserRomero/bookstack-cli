#!/usr/bin/env bash
# Tests for install.sh and install.ps1.
#
#   test/install/run.sh
#
# Requires docker (koalaman/shellcheck:stable, mcr.microsoft.com/powershell),
# python3, curl, tar, sha256sum and network access. The real-release checks
# also require gh authenticated with read access to JasserRomero/bookstack-cli.
# Every install goes to a temporary directory; nothing touches the real PATH
# or ~/.local/bin. Prints PASS/FAIL per check and exits non-zero on any FAIL.
set -uo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
installer="$repo/install.sh"
release_repo="JasserRomero/bookstack-cli"
real_version="0.1.0"

work="$(mktemp -d)"
server_pid=""
cleanup() {
	[ -n "$server_pid" ] && kill "$server_pid" 2>/dev/null
	rm -rf "$work"
}
trap cleanup EXIT

fails=0
check() {
	local name="$1"
	shift
	if "$@"; then
		echo "PASS $name"
	else
		echo "FAIL $name"
		fails=$((fails + 1))
	fi
}

contains() { [[ "$1" == *"$2"* ]]; }
not_contains() { ! [[ "$1" == *"$2"* ]]; }

# --- fixtures ---------------------------------------------------------------

srv="$work/srv"
mkdir -p "$srv/dl" "$srv/api/repos/$release_repo/releases"
printf '{"tag_name": "v9.9.9", "name": "v9.9.9"}\n' >"$srv/api/repos/$release_repo/releases/latest"

stage="$work/stage"
mkdir -p "$stage"
printf '#!/bin/sh\necho "bsk version 9.9.9"\n' >"$stage/bsk"
chmod 755 "$stage/bsk"
printf 'fake bsk.exe 9.9.9\n' >"$stage/bsk.exe"
printf 'readme\n' >"$stage/README.md"

good="$srv/dl/v9.9.9"
mkdir -p "$good"
for platform in linux_amd64 linux_arm64 darwin_amd64 darwin_arm64; do
	tar -czf "$good/bsk_9.9.9_${platform}.tar.gz" -C "$stage" bsk README.md
done
for platform in windows_amd64 windows_arm64; do
	(cd "$stage" && python3 -I -m zipfile -c "$good/bsk_9.9.9_${platform}.zip" bsk.exe README.md)
done
(cd "$good" && sha256sum ./*.tar.gz ./*.zip | sed 's# \./# #') >"$good/checksums.txt"

# v9.9.8: same archives, wrong checksums for the amd64 archives.
mkdir -p "$srv/dl/v9.9.8"
for f in "$good"/bsk_*; do
	cp "$f" "$srv/dl/v9.9.8/$(basename "$f" | sed 's/9\.9\.9/9.9.8/')"
done
(cd "$srv/dl/v9.9.8" && sha256sum ./*.tar.gz ./*.zip | sed 's# \./# #') |
	sed '/_amd64\./s/^[0-9a-f]\{8\}/00000000/' >"$srv/dl/v9.9.8/checksums.txt"

# v9.9.7: checksums.txt without entries for the amd64 archives.
mkdir -p "$srv/dl/v9.9.7"
for f in "$good"/bsk_*; do
	cp "$f" "$srv/dl/v9.9.7/$(basename "$f" | sed 's/9\.9\.9/9.9.7/')"
done
(cd "$srv/dl/v9.9.7" && sha256sum ./*.tar.gz ./*.zip | sed 's# \./# #') |
	grep -v '_amd64\.' >"$srv/dl/v9.9.7/checksums.txt"

port="$(python3 -I -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')"
python3 -I -m http.server --bind 127.0.0.1 --directory "$srv" "$port" >"$work/http.log" 2>&1 &
server_pid=$!
base_url="http://127.0.0.1:$port/dl"
api_url="http://127.0.0.1:$port/api"
for _ in $(seq 50); do
	curl -fsS -o /dev/null "$base_url/v9.9.9/checksums.txt" 2>/dev/null && break
	sleep 0.1
done

# --- helpers ----------------------------------------------------------------

out=""
rc=0
n=0
# run_local NAME_VARS... -- ARGS...: run install.sh against the fake server with
# a fresh HOME and TMPDIR; sets out, rc, tmpdir.
run_local() {
	n=$((n + 1))
	home="$work/home$n"
	tmpdir="$work/tmp$n"
	mkdir -p "$home" "$tmpdir"
	local vars=()
	while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do
		vars+=("$1")
		shift
	done
	[ "$#" -gt 0 ] && shift
	out="$(env -u GITHUB_TOKEN -u GH_TOKEN -u BSK_VERSION -u BSK_INSTALL_DIR \
		HOME="$home" TMPDIR="$tmpdir" PATH="/usr/bin:/bin" \
		BSK_INSTALL_BASE_URL="$base_url" BSK_INSTALL_API_URL="$api_url" \
		BSK_INSTALL_OS=Linux BSK_INSTALL_ARCH=x86_64 \
		"${vars[@]}" sh "$installer" "$@" 2>&1)"
	rc=$?
}

empty_dir() { [ -z "$(ls -A "$1")" ]; }
old_binary() {
	mkdir -p "$1"
	printf '#!/bin/sh\necho "bsk version old"\n' >"$1/bsk"
	chmod 755 "$1/bsk"
}

# --- static checks ----------------------------------------------------------

sc_out="$(docker run --rm -v "$installer:/mnt/install.sh:ro" koalaman/shellcheck:stable --shell=sh /mnt/install.sh 2>&1)"
check "shellcheck install.sh (POSIX sh)" [ $? -eq 0 ]
[ -n "$sc_out" ] && echo "$sc_out"
sc_out="$(docker run --rm -v "$here/run.sh:/mnt/run.sh:ro" koalaman/shellcheck:stable /mnt/run.sh 2>&1)"
check "shellcheck test/install/run.sh" [ $? -eq 0 ]
[ -n "$sc_out" ] && echo "$sc_out"

# --- install.sh against the fake server -------------------------------------

dir="$work/i1"
run_local BSK_VERSION=9.9.9 BSK_INSTALL_DIR="$dir" --
check "sh: success exits 0" [ "$rc" -eq 0 ]
check "sh: checksum verified" contains "$out" "Checksum verified"
check "sh: downloads linux_amd64 for Linux/x86_64" contains "$out" "bsk_9.9.9_linux_amd64.tar.gz (v9.9.9)"
check "sh: prints installed version" contains "$out" "bsk version 9.9.9"
check "sh: installed binary runs" [ "$("$dir/bsk" --version)" = "bsk version 9.9.9" ]
check "sh: PATH hint when dir not in PATH" contains "$out" "$dir is not in your PATH"
check "sh: temp dir removed" empty_dir "$tmpdir"
check "sh: only bsk in install dir" [ "$(ls -A "$dir")" = "bsk" ]

dir="$work/i2"
run_local BSK_INSTALL_DIR="$dir" BSK_INSTALL_OS=Darwin BSK_INSTALL_ARCH=aarch64 --
check "sh: latest release resolved through the API" contains "$out" "(v9.9.9)"
check "sh: Darwin/aarch64 maps to darwin_arm64" contains "$out" "bsk_9.9.9_darwin_arm64.tar.gz"
check "sh: latest install exits 0" [ "$rc" -eq 0 ]

dir="$work/i3"
old_binary "$dir"
run_local BSK_VERSION=v9.9.9 BSK_INSTALL_DIR="$dir" PATH="$dir:/usr/bin:/bin" --
check "sh: replace existing exits 0" [ "$rc" -eq 0 ]
check "sh: existing binary replaced" [ "$("$dir/bsk" --version)" = "bsk version 9.9.9" ]
check "sh: no staged file left" [ "$(ls -A "$dir")" = "bsk" ]
check "sh: no PATH hint when dir in PATH" not_contains "$out" "not in your PATH"

dir="$work/i4"
old_binary "$dir"
run_local BSK_VERSION=v9.9.8 BSK_INSTALL_DIR="$dir" --
check "sh: checksum mismatch exits non-zero" [ "$rc" -ne 0 ]
check "sh: checksum mismatch message" contains "$out" "checksum mismatch for bsk_9.9.8_linux_amd64.tar.gz"
check "sh: checksum mismatch keeps existing binary" [ "$("$dir/bsk" --version)" = "bsk version old" ]
check "sh: checksum mismatch leaves no staged file" [ "$(ls -A "$dir")" = "bsk" ]
check "sh: temp dir removed on failure" empty_dir "$tmpdir"

dir="$work/i5"
run_local BSK_VERSION=v9.9.7 BSK_INSTALL_DIR="$dir" --
check "sh: missing checksum entry exits non-zero" [ "$rc" -ne 0 ]
check "sh: missing checksum entry message" contains "$out" "checksums.txt has no entry for bsk_9.9.7_linux_amd64.tar.gz"
check "sh: missing checksum entry installs nothing" [ ! -e "$dir" ]

dir="$work/i6"
run_local BSK_VERSION=v9.9.7 BSK_INSTALL_DIR="$dir" BSK_INSTALL_ARCH=arm64 --
check "sh: arm64 entry present installs" [ "$rc" -eq 0 ]

run_local BSK_VERSION=v9.9.9 BSK_INSTALL_DIR="$work/i7" BSK_INSTALL_OS=FreeBSD --
check "sh: unsupported OS exits non-zero" [ "$rc" -ne 0 ]
check "sh: unsupported OS message" contains "$out" "unsupported operating system: FreeBSD"
run_local BSK_VERSION=v9.9.9 BSK_INSTALL_DIR="$work/i7" BSK_INSTALL_ARCH=i686 --
check "sh: unsupported arch exits non-zero" [ "$rc" -ne 0 ]
check "sh: unsupported arch message" contains "$out" "unsupported architecture: i686"
check "sh: unsupported platform installs nothing" [ ! -e "$work/i7" ]

run_local BSK_INSTALL_DIR="$work/i8" -- --bogus
check "sh: unknown argument exits non-zero" [ "$rc" -ne 0 ]

dir="$work/i9"
old_binary "$dir"
printf 'keep\n' >"$dir/other"
cfg_home="$work/cfg-home"
mkdir -p "$cfg_home/.config/bookstack-cli"
printf 'token = "x"\n' >"$cfg_home/.config/bookstack-cli/config.toml"
run_local BSK_INSTALL_DIR="$dir" HOME="$cfg_home" -- --uninstall
check "sh: uninstall exits 0" [ "$rc" -eq 0 ]
check "sh: uninstall removes bsk" [ ! -e "$dir/bsk" ]
check "sh: uninstall keeps other files" [ -f "$dir/other" ]
check "sh: uninstall keeps config" [ -f "$cfg_home/.config/bookstack-cli/config.toml" ]
check "sh: uninstall names config dir" contains "$out" "kept in $cfg_home/.config/bookstack-cli"
run_local BSK_INSTALL_DIR="$dir" -- --uninstall
check "sh: uninstall when absent exits 0" [ "$rc" -eq 0 ]
check "sh: uninstall when absent message" contains "$out" "bsk is not installed in $dir"

# --- install.sh against the real release ------------------------------------

# PATH without gh: symlinks to every /usr/bin command except gh.
nogh="$work/nogh"
mkdir -p "$nogh"
for f in /usr/bin/*; do
	[ "$(basename "$f")" = gh ] || ln -s "$f" "$nogh/$(basename "$f")"
done

dir="$work/real-gh"
out="$(env -u GITHUB_TOKEN -u GH_TOKEN BSK_VERSION="v$real_version" BSK_INSTALL_DIR="$dir" sh "$installer" 2>&1)"
rc=$?
check "real gh: exits 0" [ "$rc" -eq 0 ]
check "real gh: used gh" contains "$out" "with gh..."
check "real gh: checksum verified" contains "$out" "Checksum verified"
check "real gh: bsk --version" [ "$("$dir/bsk" --version 2>&1)" = "bsk version $real_version" ]
[ "$rc" -eq 0 ] || echo "$out"

token="$(gh auth token 2>/dev/null)"
dir="$work/real-token"
# GH_TOKEN is exported inside the substitution so it never appears in argv.
out="$(
	export GH_TOKEN="$token"
	env -u GITHUB_TOKEN PATH="$nogh" BSK_VERSION="$real_version" BSK_INSTALL_DIR="$dir" sh "$installer" 2>&1
)"
rc=$?
check "real token: exits 0" [ "$rc" -eq 0 ]
check "real token: used the GitHub API" contains "$out" "with the GitHub API..."
check "real token: checksum verified" contains "$out" "Checksum verified"
check "real token: bsk --version" [ "$("$dir/bsk" --version 2>&1)" = "bsk version $real_version" ]
check "real token: token never printed" not_contains "$out" "$token"
[ "$rc" -eq 0 ] || echo "$out"

dir="$work/real-anon"
private="$(gh repo view "$release_repo" --json isPrivate -q .isPrivate 2>/dev/null)"
out="$(env -u GITHUB_TOKEN -u GH_TOKEN PATH="$nogh" BSK_VERSION="v$real_version" BSK_INSTALL_DIR="$dir" sh "$installer" 2>&1)"
rc=$?
if [ "$private" = "true" ]; then
	check "real anonymous (private repo): exits non-zero" [ "$rc" -ne 0 ]
	check "real anonymous (private repo): explains gh option" contains "$out" "gh auth login"
	check "real anonymous (private repo): explains token option" contains "$out" "GITHUB_TOKEN or GH_TOKEN"
	check "real anonymous (private repo): installs nothing" [ ! -e "$dir" ]
else
	check "real anonymous (public repo): exits 0" [ "$rc" -eq 0 ]
	check "real anonymous (public repo): bsk --version" [ "$("$dir/bsk" --version 2>&1)" = "bsk version $real_version" ]
fi

# --- install.ps1 in the PowerShell image ------------------------------------

ps_out="$(GH_TOKEN="$token" docker run --rm --network host -e GH_TOKEN \
	-e BSK_TEST_BASE_URL="$base_url" -e BSK_TEST_API_URL="$api_url" -e BSK_TEST_REAL_VERSION="$real_version" \
	-v "$repo/install.ps1:/src/install.ps1:ro" -v "$here/install_test.ps1:/src/install_test.ps1:ro" \
	mcr.microsoft.com/powershell pwsh -NoProfile -File /src/install_test.ps1 2>&1)"
ps_rc=$?
unset token
echo "$ps_out"
ps_fails="$(grep -c '^FAIL ' <<<"$ps_out")"
fails=$((fails + ps_fails))
if [ "$ps_rc" -ne 0 ] && [ "$ps_fails" -eq 0 ]; then
	echo "FAIL ps1: test container exited $ps_rc"
	fails=$((fails + 1))
fi

echo
if [ "$fails" -eq 0 ]; then
	echo "All installer checks passed."
else
	echo "$fails installer check(s) failed."
	exit 1
fi
