#!/bin/sh
# Halotrcer installer for Linux and macOS: halotrcer, Tokvera and Sievant.
#
#   curl -fsSL https://www.halotrcer.com/install.sh | sh
#   curl -fsSL https://www.halotrcer.com/install.sh | sh -s -- --only tokvera
#
# What it does, in order (every step is commented below):
#   1. detects the OS and CPU (Apple Silicon under Rosetta gets the native
#      arm64 build; musl and glibc Linux get the same static binary);
#   2. resolves the halotrcer version (the latest published release unless
#      --version);
#   3. downloads halotrcer's release archive and checksums.txt from GitHub,
#      unless that version is already installed (safe to run again);
#   4. verifies checksums.txt's cosign signature when cosign is installed
#      (required with --require-cosign), then the archive's SHA-256;
#   5. installs halotrcer atomically (with --only, it is kept in a scratch
#      directory instead and removed at the end);
#   6. has that halotrcer install Tokvera and Sievant into the same
#      directory: each from its own release, verified the same way against
#      its own release workflow's identity, at the newest stable version
#      inside that halotrcer's compatibility matrix. A tool already installed
#      elsewhere on PATH (brew, a package, go install) is left alone;
#   7. prints a PATH hint if needed, and the next command.
#
# It never runs sudo: --global into a directory you cannot write stops and
# tells you how to run it as root yourself. It contacts only GitHub
# (api.github.com for versions, github.com for the release files), never
# asks for credentials, and with --dry-run contacts nothing at all. POSIX
# sh; shellcheck-clean. Source and docs: https://github.com/rajasatyajit/halotrcer
# (docs/install.md).
#
# Exit codes: 0 installed (or already installed, or dry run), 1 failure,
# 2 usage error.

set -eu

# --- Constants --------------------------------------------------------------

OWNER_URL="https://github.com/rajasatyajit"
# The release files and the GitHub API, for every repository of the owner.
# Overridable for a mirror or a test server (halotrcer reads the same two
# variables for Tokvera and Sievant); an override must be https, or plain
# http to a loopback host only.
DOWNLOAD_BASE="${HALOTRCER_INSTALL_DOWNLOAD_BASE:-$OWNER_URL}"
API_BASE="${HALOTRCER_INSTALL_API_BASE:-https://api.github.com/repos/rajasatyajit}"
DOWNLOAD_BASE="${DOWNLOAD_BASE%/}"
API_BASE="${API_BASE%/}"
# The identity cosign must see on the checksum file's signature: the release
# workflow of this repository, running on a version tag.
COSIGN_IDENTITY_REGEXP='^https://github\.com/rajasatyajit/halotrcer/\.github/workflows/release\.yml@refs/tags/v'
COSIGN_OIDC_ISSUER='https://token.actions.githubusercontent.com'

# --- Messages ---------------------------------------------------------------

# Progress goes to stderr, so `sh install.sh 2>/dev/null` is quiet.
say() { printf 'halotrcer-install: %s\n' "$*" >&2; }
fail() {
	say "error: $*"
	exit 1
}
usage_error() {
	say "error: $*"
	say "run with --help for usage"
	exit 2
}

usage() {
	cat <<'EOF'
Install halotrcer, Tokvera and Sievant (https://github.com/rajasatyajit/halotrcer).

usage: install.sh [options]

  --only TOOL         install only tokvera or only sievant (halotrcer is used
                      to install it, then removed)
  --version VERSION   the halotrcer release (e.g. v0.2.0); default: the latest.
                      The tools' versions come from that release's
                      compatibility matrix.
  --dir DIR           install into DIR (default: $HOME/.local/bin)
  --global            install into /usr/local/bin (run the script as root for
                      that; it never runs sudo itself)
  --dry-run           print what would happen; download and change nothing
  --no-verify         skip the SHA-256 and signature checks (not recommended)
  --require-cosign    fail unless cosign is installed and every signature verifies
  -h, --help          show this help

Exit codes: 0 installed, already installed or dry run; 1 failure; 2 usage.
EOF
}

# --- Step 0: read the options -------------------------------------------------

only=""
version=""
dir=""
global=0
dry_run=0
verify=1
require_cosign=0

while [ $# -gt 0 ]; do
	case "$1" in
	--only)
		[ $# -ge 2 ] || usage_error "--only needs a value"
		only="$2"
		shift 2
		;;
	--only=*)
		only="${1#--only=}"
		shift
		;;
	--version)
		[ $# -ge 2 ] || usage_error "--version needs a value"
		version="$2"
		shift 2
		;;
	--version=*)
		version="${1#--version=}"
		shift
		;;
	--dir)
		[ $# -ge 2 ] || usage_error "--dir needs a value"
		dir="$2"
		shift 2
		;;
	--dir=*)
		dir="${1#--dir=}"
		shift
		;;
	--global)
		global=1
		shift
		;;
	--dry-run)
		dry_run=1
		shift
		;;
	--no-verify)
		verify=0
		shift
		;;
	--require-cosign)
		require_cosign=1
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*) usage_error "unknown option: $1" ;;
	esac
done

# Contradictory or malformed options are refused before anything else runs.
case "$only" in
"" | tokvera | sievant) ;;
*) usage_error "--only must be tokvera or sievant, got: $only" ;;
esac
if [ "$global" -eq 1 ] && [ -n "$dir" ]; then
	usage_error "--dir and --global are mutually exclusive"
fi
if [ "$verify" -eq 0 ] && [ "$require_cosign" -eq 1 ]; then
	usage_error "--no-verify and --require-cosign are mutually exclusive"
fi
if [ -n "$version" ]; then
	# Accept "0.2.0" as well as "v0.2.0"; the release tags carry the v.
	case "$version" in v*) ;; *) version="v$version" ;; esac
	# A strict pattern keeps the value safe to put in a URL and a file name.
	if ! printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'; then
		usage_error "--version must look like v1.2.3 or v1.2.3-rc.1, got: $version"
	fi
fi

# An overridden endpoint must not send the download in the clear to another
# machine: https, or http to this machine only.
check_endpoint() {
	case "$1" in
	https://*) ;;
	http://127.0.0.1:* | http://127.0.0.1/* | http://localhost:* | http://localhost/* | "http://[::1]:"* | "http://[::1]/"*) ;;
	*) usage_error "$2 must be an https URL (or http to a loopback host), got: $1" ;;
	esac
}
check_endpoint "$DOWNLOAD_BASE" HALOTRCER_INSTALL_DOWNLOAD_BASE
check_endpoint "$API_BASE" HALOTRCER_INSTALL_API_BASE

# What gets installed, for the messages and for halotrcer install.
if [ -n "$only" ]; then
	tools="$only"
	what="$only"
else
	tools="tokvera sievant"
	what="halotrcer, tokvera and sievant"
fi

# --- Step 1: detect the platform ----------------------------------------------

# The OS. Windows shells (Git Bash, MSYS2, Cygwin) are pointed at install.ps1.
case "$(uname -s)" in
Linux) os=linux ;;
Darwin) os=darwin ;;
MINGW* | MSYS* | CYGWIN*) fail "this is the Linux/macOS installer; on Windows use install.ps1 (see docs/install.md)" ;;
*) fail "unsupported OS: $(uname -s) (release builds exist for Linux, macOS and Windows)" ;;
esac

# The CPU. Release builds exist for amd64 and arm64 only.
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) fail "unsupported CPU: $(uname -m) (release builds exist for amd64 and arm64)" ;;
esac

# Apple Silicon running this shell under Rosetta 2 reports x86_64; the native
# arm64 build is the right one there. sysctl.proc_translated is 1 only inside
# a translated process (and absent on Intel Macs). halotrcer applies the same
# rule to the tools.
if [ "$os" = darwin ] && [ "$arch" = amd64 ]; then
	if [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || true)" = 1 ]; then
		arch=arm64
		say "Rosetta 2 detected: installing the native arm64 builds"
	fi
fi

# musl (Alpine and friends) needs nothing different: the release binaries are
# statically linked (CGO_ENABLED=0), so the same archives run on glibc and musl.
if [ "$os" = linux ]; then
	if ldd --version 2>&1 | grep -qi musl || ls /lib/ld-musl-* >/dev/null 2>&1; then
		say "musl libc detected: the release binaries are static, so the standard Linux builds run as they are"
	fi
fi

# --- Step 2: choose the destination -------------------------------------------

if [ "$global" -eq 1 ]; then
	dir=/usr/local/bin
elif [ -z "$dir" ]; then
	[ -n "${HOME:-}" ] || fail "HOME is not set; pass --dir"
	dir="$HOME/.local/bin"
fi

# The flags halotrcer install gets: always a direct download into $dir, with
# this run's verification choice.
hal_flags="--via direct --dir"
verify_flag=""
if [ "$verify" -eq 0 ]; then
	verify_flag="--no-verify"
elif [ "$require_cosign" -eq 1 ]; then
	verify_flag="--require-cosign"
fi

# --- Dry run: print the plan, contact nothing --------------------------------

if [ "$dry_run" -eq 1 ]; then
	if [ -n "$version" ]; then
		shown="$version"
		archive="halotrcer_${version#v}_${os}_${arch}.tar.gz"
	else
		shown="latest (resolved from $API_BASE/halotrcer/releases/latest at install time)"
		archive="halotrcer_<version>_${os}_${arch}.tar.gz"
	fi
	say "dry run: nothing will be downloaded or changed"
	say "installs:     $what"
	say "platform:     ${os}/${arch}"
	say "halotrcer:    $shown"
	say "archive:      $DOWNLOAD_BASE/halotrcer/releases/download/<tag>/$archive"
	if [ "$verify" -eq 1 ]; then
		say "verify:       SHA-256 against checksums.txt; cosign signature of checksums.txt if cosign is installed$([ "$require_cosign" -eq 1 ] && printf ' (required)')"
	else
		say "verify:       SKIPPED (--no-verify)"
	fi
	if [ -n "$only" ]; then
		say "halotrcer:    kept in a scratch directory, removed at the end"
	else
		say "install to:   $dir/halotrcer"
	fi
	say "then:         halotrcer install $hal_flags $dir${verify_flag:+ $verify_flag} <tool>, for each of: $tools"
	say "              (the newest stable release in halotrcer's compatibility matrix; a tool already on PATH is left alone)"
	exit 0
fi

# --- Step 3: check the tools this script needs ---------------------------------

have() { command -v "$1" >/dev/null 2>&1; }

# A downloader: curl, or wget when curl is missing.
if have curl; then
	downloader=curl
elif have wget; then
	downloader=wget
else
	fail "need curl or wget to download the release"
fi
for tool in tar mktemp; do
	have "$tool" || fail "need $tool"
done

# A SHA-256 tool: sha256sum (GNU, busybox), shasum (macOS) or openssl.
sha256_of() {
	if have sha256sum; then
		sha256sum "$1" | awk '{print $1}'
	elif have shasum; then
		shasum -a 256 "$1" | awk '{print $1}'
	elif have openssl; then
		openssl dgst -sha256 "$1" | awk '{print $NF}'
	else
		return 1
	fi
}
if [ "$verify" -eq 1 ] && ! have sha256sum && ! have shasum && ! have openssl; then
	fail "need sha256sum, shasum or openssl to verify the download (or pass --no-verify, not recommended)"
fi

# cosign is optional unless --require-cosign; checked now, before downloading.
use_cosign=0
if [ "$verify" -eq 1 ]; then
	if have cosign; then
		use_cosign=1
	elif [ "$require_cosign" -eq 1 ]; then
		fail "--require-cosign: cosign is not installed (https://docs.sigstore.dev/cosign/system_config/installation/)"
	fi
fi

# The destination must be writable by this user: the script never runs sudo
# (a security tool does not escalate privileges on your behalf).
if ! mkdir -p "$dir" 2>/dev/null || [ ! -w "$dir" ]; then
	if [ "$global" -eq 1 ]; then
		fail "$dir is not writable by $(id -un 2>/dev/null || echo this user); this script never runs sudo. Run it as root yourself (curl -fsSL https://www.halotrcer.com/install.sh | sudo sh -s -- --global), or leave out --global to install into ~/.local/bin"
	fi
	fail "$dir is not writable; pass --dir to a directory you own (this script never runs sudo)"
fi

# download URL FILE: https only (curl also refuses an http redirect); plain http
# is reachable only through a loopback override, which check_endpoint allowed.
download() {
	case "$1" in
	https://*) proto='=https' ;;
	*) proto='=http' ;;
	esac
	if [ "$downloader" = curl ]; then
		curl -fsSL --proto "$proto" --proto-redir "$proto" -o "$2" "$1"
	else
		wget -q -O "$2" "$1"
	fi
}

# --- Step 4: resolve the version -----------------------------------------------

# A scratch directory for everything downloaded; removed on every exit path.
tmp="$(mktemp -d 2>/dev/null || mktemp -d -t halotrcer)"
trap 'rm -rf "$tmp"' EXIT
trap 'exit 1' HUP INT TERM

if [ -z "$version" ]; then
	# The latest PUBLISHED release (drafts are not visible here).
	download "$API_BASE/halotrcer/releases/latest" "$tmp/latest.json" ||
		fail "could not find the latest release at $API_BASE/halotrcer/releases/latest; pass --version"
	version="$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$tmp/latest.json" | head -n 1)"
	if ! printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'; then
		fail "the latest release has an unexpected tag ($version); pass --version"
	fi
fi
say "installing $what for ${os}/${arch} into $dir (halotrcer $version)"

# --- Step 5: use halotrcer if this version is already installed (idempotent) --

# `halotrcer version` prints "halotrcer vX.Y.Z (commit ...)" on its first line.
hal=""
if [ -x "$dir/halotrcer" ]; then
	installed="$("$dir/halotrcer" version 2>/dev/null | head -n 1 || true)"
	case "$installed" in
	"halotrcer $version "* | "halotrcer $version")
		say "halotrcer $version is already installed at $dir/halotrcer"
		hal="$dir/halotrcer"
		;;
	esac
fi

if [ -z "$hal" ]; then
	# --- Step 6: download the archive and the checksum file ---------------------

	archive="halotrcer_${version#v}_${os}_${arch}.tar.gz"
	base="$DOWNLOAD_BASE/halotrcer/releases/download/$version"
	say "downloading $base/$archive"
	download "$base/$archive" "$tmp/$archive" || fail "download failed: $base/$archive"

	# --- Step 7: verify the signature, then the checksum -----------------------

	if [ "$verify" -eq 1 ]; then
		download "$base/checksums.txt" "$tmp/checksums.txt" || fail "download failed: $base/checksums.txt"

		# The signature first: it is what makes the checksum file trustworthy.
		if [ "$use_cosign" -eq 1 ]; then
			download "$base/checksums.txt.sigstore.json" "$tmp/checksums.txt.sigstore.json" ||
				fail "download failed: $base/checksums.txt.sigstore.json"
			if ! cosign verify-blob --bundle "$tmp/checksums.txt.sigstore.json" \
				--certificate-identity-regexp "$COSIGN_IDENTITY_REGEXP" \
				--certificate-oidc-issuer "$COSIGN_OIDC_ISSUER" \
				"$tmp/checksums.txt" >/dev/null 2>&1; then
				fail "cosign could not verify checksums.txt as signed by rajasatyajit/halotrcer's release workflow; nothing was installed"
			fi
			say "cosign: checksums.txt is signed by rajasatyajit/halotrcer's release workflow"
		else
			say "cosign not found: the signature was not checked (SHA-256 only; install cosign or pass --require-cosign)"
		fi

		# Then the archive's SHA-256 against its exact line in checksums.txt.
		want="$(awk -v f="$archive" '$2 == f {print $1}' "$tmp/checksums.txt")"
		[ -n "$want" ] || fail "$archive is not listed in checksums.txt; nothing was installed"
		got="$(sha256_of "$tmp/$archive")" || fail "could not compute the SHA-256 of $archive"
		[ "$got" = "$want" ] || fail "SHA-256 mismatch for $archive (expected $want, got $got); nothing was installed"
		say "SHA-256 verified"
	else
		say "WARNING: --no-verify: the download's SHA-256 and signature were NOT checked"
	fi

	# --- Step 8: unpack and check the binary runs ------------------------------

	mkdir "$tmp/bin"
	tar -xzf "$tmp/$archive" -C "$tmp/bin" halotrcer || fail "could not unpack halotrcer from $archive"
	chmod 0755 "$tmp/bin/halotrcer"
	"$tmp/bin/halotrcer" version >/dev/null 2>&1 || fail "the downloaded binary does not run on this machine"

	# --- Step 9: install atomically (not with --only) --------------------------

	if [ -n "$only" ]; then
		hal="$tmp/bin/halotrcer"
	else
		# Write next to the destination, then rename over it: an interrupted
		# install never leaves a half-written halotrcer behind.
		cp "$tmp/bin/halotrcer" "$dir/.halotrcer.new.$$"
		chmod 0755 "$dir/.halotrcer.new.$$"
		mv -f "$dir/.halotrcer.new.$$" "$dir/halotrcer"
		hal="$dir/halotrcer"
		say "installed: $("$hal" version 2>/dev/null | head -n 1)"
	fi
fi

# --- Step 10: Tokvera and Sievant, through halotrcer --------------------------

# halotrcer finds a tool on PATH; $dir goes first so a tool installed there
# before is upgraded in place. A tool installed elsewhere (brew, a package,
# go install) is left alone: its own channel upgrades it, and
# `halotrcer doctor` checks its version. halotrcer reads the same mirror
# variables and records what it installs, so `halotrcer uninstall --all`
# can remove it later.
failed=0
for tool in $tools; do
	if [ ! -x "$dir/$tool" ] && other="$(command -v "$tool" 2>/dev/null)" && [ -n "$other" ]; then
		say "$tool is already installed at $other; leaving it as it is (halotrcer doctor checks its version)"
		continue
	fi
	say "installing $tool with halotrcer (its own release, verified against its own release workflow)"
	# shellcheck disable=SC2086 # hal_flags and verify_flag are word lists
	if PATH="$dir:$PATH" "$hal" install "$tool" $hal_flags "$dir" $verify_flag >"$tmp/$tool.out" 2>&1; then
		# halotrcer's own "next:" line is replaced by this script's, below.
		grep -v '^next: ' "$tmp/$tool.out" | sed 's/^/  /' >&2 || true
	else
		sed 's/^/  /' "$tmp/$tool.out" >&2
		say "error: $tool was not installed"
		failed=1
	fi
done
[ "$failed" -eq 0 ] || fail "not everything was installed (see above); run the script again, or halotrcer install <tool>"

# --- Step 11: PATH hint and the next command -----------------------------------

case ":${PATH:-}:" in
*":$dir:"*) ;;
*) say "$dir is not on your PATH; add it, for example: export PATH=\"$dir:\$PATH\"" ;;
esac
case "$only" in
"") say "next: halotrcer check --target <inference-url>" ;;
tokvera) say "next: tokvera scan --target <inference-url>" ;;
sievant) say "next: sievant scan" ;;
esac
