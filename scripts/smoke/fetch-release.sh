#!/bin/sh
# Downloads and verifies one tool's pinned release for the smoke matrix, in
# snapshot.sh's release mode (PLAN.md M4, K1):
#
#   sh scripts/smoke/fetch-release.sh NAME TAG IDENTITY OUT_DIR
#
# NAME is tokvera or sievant, TAG and IDENTITY come from bundled-tools.sh
# (IDENTITY is the exact keyless identity of that tag's release run). Every
# step refuses rather than continues:
#   1. checksums.txt and checksums.txt.sigstore.json from the release;
#      `cosign verify-blob` against IDENTITY and GitHub's OIDC issuer (cosign
#      is required here, unlike in the installers: this script exists to
#      check the real thing);
#   2. checksums.txt must be strict "<sha256>  <file name>" lines;
#   3. the files the channels install (six archives, two debs, two rpms, two
#      apks), each listed in checksums.txt and matching it;
#   4. the tool's cask (rajasatyajit/homebrew-tap, Casks/), Scoop manifest
#      (rajasatyajit/scoop-bucket) and winget manifests (microsoft/winget-pkgs,
#      manifests/h/Halotrcer/<Name>/<version>/): each must name this version,
#      and every SHA-256 in it must be one checksums.txt lists, so a manifest
#      can only point at the verified files;
#   5. the tool's apk public key from its repository at TAG (where both tools'
#      docs/install.md point apk users); the apk channel then proves apk
#      verifies the packages against it.
# Writes OUT_DIR/releases/NAME/TAG/, casks/NAME.rb, scoop/NAME.json,
# winget/<Id>/ and apk-keys/. SMOKE_GITHUB_DOWNLOAD and SMOKE_GITHUB_RAW
# replace https://github.com and https://raw.githubusercontent.com (tests).
# Needs curl, cosign, and sha256sum or shasum.
set -eu

name="${1:?usage: fetch-release.sh NAME TAG IDENTITY OUT_DIR}"
tag="${2:?usage: fetch-release.sh NAME TAG IDENTITY OUT_DIR}"
identity="${3:?usage: fetch-release.sh NAME TAG IDENTITY OUT_DIR}"
out="${4:?usage: fetch-release.sh NAME TAG IDENTITY OUT_DIR}"
dl="${SMOKE_GITHUB_DOWNLOAD:-https://github.com}"
raw="${SMOKE_GITHUB_RAW:-https://raw.githubusercontent.com}"
issuer=https://token.actions.githubusercontent.com
proto='=https'
# Plain http only for a test's loopback server.
if [ -n "${SMOKE_GITHUB_DOWNLOAD:-}${SMOKE_GITHUB_RAW:-}" ]; then proto='=https,http'; fi

fail() {
	echo "fetch-release.sh: $name $tag: $*" >&2
	exit 1
}
case "$name" in
tokvera) id=Halotrcer.Tokvera ;;
sievant) id=Halotrcer.Sievant ;;
*) fail "unknown tool (tokvera or sievant)" ;;
esac
printf '%s\n' "$tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$' || fail "not a release tag"
version="${tag#v}"
command -v curl >/dev/null 2>&1 || fail "needs curl"
command -v cosign >/dev/null 2>&1 || fail "needs cosign: the release mode verifies every signature"
if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
	fail "needs sha256sum or shasum"
fi
get() { # get URL FILE
	curl -fsSL --retry 3 --proto "$proto" -o "$2" "$1" || fail "download failed: $1"
}

rel="$out/releases/$name/$tag"
mkdir -p "$rel" "$out/casks" "$out/scoop" "$out/winget/$id" "$out/apk-keys"
base="$dl/rajasatyajit/$name/releases/download/$tag"

# 1. The signed checksums.
get "$base/checksums.txt" "$rel/checksums.txt"
get "$base/checksums.txt.sigstore.json" "$rel/checksums.txt.sigstore.json"
cosign verify-blob --bundle "$rel/checksums.txt.sigstore.json" \
	--certificate-identity "$identity" --certificate-oidc-issuer "$issuer" \
	"$rel/checksums.txt" >/dev/null 2>&1 ||
	fail "cosign could not verify checksums.txt against $identity"

# 2. Strict lines: a file name with no path in it.
if LC_ALL=C grep -Evq '^[0-9a-f]{64}  [A-Za-z0-9][A-Za-z0-9._+-]*$' "$rel/checksums.txt"; then
	fail "checksums.txt has a line that is not '<sha256>  <file name>'"
fi
listed() { awk -v f="$1" '$2 == f { print $1 }' "$rel/checksums.txt"; }

# 3. The files the channels install, each checked against checksums.txt.
for f in "${name}_${version}_linux_amd64.tar.gz" "${name}_${version}_linux_arm64.tar.gz" \
	"${name}_${version}_darwin_amd64.tar.gz" "${name}_${version}_darwin_arm64.tar.gz" \
	"${name}_${version}_windows_amd64.zip" "${name}_${version}_windows_arm64.zip" \
	"${name}_${version}_amd64.deb" "${name}_${version}_arm64.deb" \
	"${name}-${version}-1.x86_64.rpm" "${name}-${version}-1.aarch64.rpm" \
	"${name}_${version}_x86_64.apk" "${name}_${version}_aarch64.apk"; do
	want="$(listed "$f")"
	[ -n "$want" ] || fail "checksums.txt does not list $f"
	get "$base/$f" "$rel/$f"
	got="$(sha256 "$rel/$f")"
	if [ "$got" != "$want" ]; then
		rm -f "$rel/$f"
		fail "$f: SHA-256 $got does not match checksums.txt ($want)"
	fi
done

# 4. The package managers' manifests: this version, verified files only.
# manifest FILE: every SHA-256 in it is listed, and there is at least one.
manifest() {
	hashes="$(LC_ALL=C grep -Eo '[0-9A-Fa-f]{64}' "$1" | tr 'A-F' 'a-f' | sort -u)"
	[ -n "$hashes" ] || fail "${1##*/} names no SHA-256"
	for h in $hashes; do
		awk -v h="$h" '$1 == h { found = 1 } END { exit !found }' "$rel/checksums.txt" ||
			fail "${1##*/} names $h, which checksums.txt does not list"
	done
}
get "$raw/rajasatyajit/homebrew-tap/main/Casks/$name.rb" "$out/casks/$name.rb"
grep -Eq "^  version \"$version\"\$" "$out/casks/$name.rb" ||
	fail "the tap's cask is not version $version (update packaging/bundled-tools.env to the tap's release)"
manifest "$out/casks/$name.rb"
get "$raw/rajasatyajit/scoop-bucket/main/$name.json" "$out/scoop/$name.json"
grep -Eq "^ *\"version\": \"$version\"," "$out/scoop/$name.json" ||
	fail "the bucket's manifest is not version $version (update packaging/bundled-tools.env to the bucket's release)"
manifest "$out/scoop/$name.json"
wdir="$raw/microsoft/winget-pkgs/master/manifests/h/$(printf '%s' "$id" | tr . /)/$version"
for f in "$id.yaml" "$id.installer.yaml" "$id.locale.en-US.yaml"; do
	get "$wdir/$f" "$out/winget/$id/$f"
	grep -Eq "^PackageVersion: $version\$" "$out/winget/$id/$f" || fail "winget $f is not version $version"
done
manifest "$out/winget/$id/$id.installer.yaml"

# 5. The apk public key, from the tool's repository at the tag.
key="satyajit.rajaraman@halotrcer.com-$name.rsa.pub"
get "$raw/rajasatyajit/$name/$tag/packaging/keys/$key" "$out/apk-keys/$key"
grep -q '^-----BEGIN PUBLIC KEY-----$' "$out/apk-keys/$key" || fail "$key is not a PEM public key"

echo "fetch-release.sh: $name $tag verified (cosign: $identity; 12 files; cask, Scoop and winget manifests; apk key)" >&2
