#!/bin/sh
# Writes the package-signing keys from their secrets to files GoReleaser and
# package-repos.sh read, refusing keys that are not the private halves of the
# public keys users are told to trust:
#
#   sh scripts/release/write-keys.sh DIR >> "$GITHUB_ENV"
#   sh scripts/release/write-keys.sh --check
#
# Reads PKG_GPG_KEY (an armored OpenPGP private key) and PKG_APK_KEY (an
# unencrypted RSA private key, PEM). Writes DIR/pkg-gpg.asc and
# DIR/pkg-apk.rsa (mode 600) and prints PKG_GPG_KEY_FILE=... and
# PKG_APK_KEY_FILE=..., the variables .goreleaser.yaml's nfpm signatures read.
#   - The OpenPGP key's primary fingerprint must be GPG_PUB's (default
#     packaging/keys/halotrcer-packages.asc, the key shared with Tokvera and
#     Sievant).
#   - The RSA key's public half must be APK_PUB's (default
#     packaging/keys/satyajit.rajaraman@halotrcer.com-halotrcer.rsa.pub).
# --check runs the same checks in a private temporary directory, removes it
# and prints nothing for $GITHUB_ENV (the preflight steps use it).
# Key material is never printed; on a refusal both key files are removed.
set -eu

check=false
if [ "${1:-}" = --check ]; then
	check=true
	dir="$(mktemp -d)"
else
	dir="${1:?usage: write-keys.sh DIR | --check}"
fi
repo="$(cd "$(dirname "$0")/../.." && pwd)"
gpg_pub="${GPG_PUB:-$repo/packaging/keys/halotrcer-packages.asc}"
apk_pub="${APK_PUB:-$repo/packaging/keys/satyajit.rajaraman@halotrcer.com-halotrcer.rsa.pub}"
: "${PKG_GPG_KEY:?write-keys.sh: PKG_GPG_KEY is not set}"
: "${PKG_APK_KEY:?write-keys.sh: PKG_APK_KEY is not set}"

umask 077
mkdir -p "$dir"
gpg_key="$dir/pkg-gpg.asc"
apk_key="$dir/pkg-apk.rsa"
gnupg="$(mktemp -d)"
cleanup() {
	rm -rf "$gnupg"
	if [ "$check" = true ]; then rm -rf "$dir"; fi
}
trap cleanup EXIT
refuse() {
	echo "write-keys.sh: $*" >&2
	rm -f "$gpg_key" "$apk_key"
	exit 1
}

printf '%s\n' "$PKG_GPG_KEY" >"$gpg_key"
printf '%s\n' "$PKG_APK_KEY" >"$apk_key"

# fpr FILE: the first primary-key fingerprint in FILE, read without importing.
fpr() {
	GNUPGHOME="$gnupg" gpg --batch --quiet --with-colons --import-options show-only --import "$1" 2>/dev/null |
		awk -F: '$1 == "fpr" { print $10; exit }'
}
# The armor header is assembled so this script never reads as a key itself.
begin='-----BEGIN'
grep -q "^$begin PGP PRIVATE KEY BLOCK-----" "$gpg_key" || refuse "PKG_GPG_KEY is not an armored OpenPGP private key"
want="$(fpr "$gpg_pub")"
got="$(fpr "$gpg_key")"
[ -n "$want" ] || refuse "no OpenPGP key in $gpg_pub"
[ "$got" = "$want" ] || refuse "PKG_GPG_KEY's fingerprint is '${got:-unreadable}', but $gpg_pub publishes $want"

# der ARGS: sha256 of the DER public key openssl derives from ARGS.
der() { openssl pkey "$@" -pubout -outform DER 2>/dev/null | openssl dgst -sha256 -r | cut -d' ' -f1; }
grep -Eq "^$begin (RSA )?PRIVATE KEY-----" "$apk_key" || refuse "PKG_APK_KEY is not an unencrypted PEM private key"
empty="$(printf '' | openssl dgst -sha256 -r | cut -d' ' -f1)"
want_apk="$(der -pubin -in "$apk_pub")"
got_apk="$(der -in "$apk_key")"
[ "$want_apk" != "$empty" ] || refuse "no public key in $apk_pub"
[ "$got_apk" != "$empty" ] || refuse "PKG_APK_KEY could not be read as a private key"
[ "$got_apk" = "$want_apk" ] || refuse "PKG_APK_KEY's public half (sha256 $got_apk) is not $apk_pub (sha256 $want_apk)"

if [ "$check" = true ]; then
	echo "write-keys.sh: PKG_GPG_KEY is the private key of $(basename "$gpg_pub") ($want); PKG_APK_KEY is the private key of $(basename "$apk_pub") (public key sha256 $want_apk)" >&2
	exit 0
fi
echo "PKG_GPG_KEY_FILE=$gpg_key"
echo "PKG_APK_KEY_FILE=$apk_key"
echo "write-keys.sh: both keys match the published public keys" >&2
