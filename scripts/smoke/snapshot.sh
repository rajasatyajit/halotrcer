#!/bin/sh
# Builds the snapshot the smoke matrix installs from (PLAN.md M4) and lays it
# out in OUT_DIR for testdata/mirror and the channel scripts:
#
#   sh scripts/smoke/snapshot.sh OUT_DIR
#
#   OUT_DIR/TAG                          halotrcer's snapshot tag
#   OUT_DIR/TOOLS                        "stand-in" (K1) or "release"
#   OUT_DIR/releases/<repo>/<tag>/...    every release file, per repository:
#                                        halotrcer's snapshot and the tools'
#   OUT_DIR/casks/<name>.rb, scoop/<name>.json, winget/<Id>/*.yaml
#   OUT_DIR/apk-keys/satyajit.rajaraman@halotrcer.com-<name>.rsa.pub
#   OUT_DIR/repo/ and repo-key.asc       the signed apt and yum repositories
#   OUT_DIR/mirror-<os>-<arch>[.exe]     testdata/mirror for every smoke runner
#
# Like Tokvera's and Sievant's snapshot.sh: halotrcer's goreleaser --snapshot
# runs over a copy of .goreleaser.yaml without the cosign blocks (keyless
# signing would publish to Sigstore's transparency log), the SBOMs and the
# image; `--skip=sign` is not used, because it also drops the package
# signatures. Packages are signed with throwaway keys made here: one RSA key
# for every apk (its public half under each project's key name) and one
# OpenPGP key for the deb and rpm packages and the repositories. Nothing is
# published.
#
# The tools (K1): with packaging/bundled-tools.env empty, the stand-ins in
# testdata/standin are released the same way under the real tools' names, at
# v0.2.0. Once the tools' releases are pinned there, this script refuses to
# run until the release mode (downloading and verifying the pinned releases'
# files) is written: a stand-in must never stand in for a real release.
#
# The repositories hold the stand-ins' packages where the tools' own
# publishes would leave them (apt/pool/main/t/tokvera, s/sievant, rpm/), then
# halotrcer's, added by the release workflow's own
# scripts/release/package-repos-docker.sh.
#
# Needs: go, goreleaser v2.18.3, openssl, gpg, docker. Run from anywhere; it
# uses the checkout it sits in, and writes dist/ there.
set -eu

out="${1:?usage: snapshot.sh OUT_DIR}"
repo="$(cd "$(dirname "$0")/../.." && pwd)"
mkdir -p "$out"
out="$(cd "$out" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# Real releases pinned: not built yet (see above).
if grep -Eq '^(TOKVERA|SIEVANT)_VERSION=.+' "$repo/packaging/bundled-tools.env"; then
	echo "snapshot.sh: packaging/bundled-tools.env pins the tools' releases; the release mode (PLAN.md K1) is not written yet, and stand-ins must not replace real releases" >&2
	exit 1
fi
echo stand-in >"$out/TOOLS"

# A throwaway apk signing key (PKCS#8 PEM, what nfpm's RSA signer reads); its
# public half under each project's key name, which apk looks up.
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "$work/apk.rsa" 2>/dev/null
mkdir -p "$out/apk-keys"
for p in halotrcer tokvera sievant; do
	openssl pkey -in "$work/apk.rsa" -pubout -out "$out/apk-keys/satyajit.rajaraman@halotrcer.com-$p.rsa.pub"
done

# A throwaway OpenPGP key (passphrase-protected, as the release's is) for the
# deb and rpm signatures and the repository metadata.
gnupg="$work/gnupg"
mkdir -m 700 "$gnupg"
printf 'snapshot-passphrase' >"$work/gpg-pass"
GNUPGHOME="$gnupg" gpg --batch --quiet --pinentry-mode loopback --passphrase-file "$work/gpg-pass" \
	--quick-gen-key 'halotrcer snapshot <snapshot@invalid>' rsa3072 sign 2d 2>/dev/null
GNUPGHOME="$gnupg" gpg --batch --quiet --pinentry-mode loopback --passphrase-file "$work/gpg-pass" \
	--armor --export-secret-keys snapshot@invalid >"$work/gpg.asc"
GNUPGHOME="$gnupg" gpg --batch --quiet --armor --export snapshot@invalid >"$out/repo-key.asc"

cd "$repo"
toolchain="$(sed -n 's/^toolchain //p' go.mod)"
signing() {
	PKG_APK_KEY_FILE="$work/apk.rsa" PKG_GPG_KEY_FILE="$work/gpg.asc" NFPM_PASSPHRASE="snapshot-passphrase" \
		GOTOOLCHAIN="$toolchain" "$@"
}

# --- halotrcer: the config minus the blocks that sign, scan or build images ----
awk '
	/^[A-Za-z0-9_]+:/ { skip = ($1 == "signs:" || $1 == "docker_signs:" || $1 == "sboms:" || $1 == "dockers_v2:") }
	!skip { print }
' .goreleaser.yaml >"$work/halotrcer.yaml"
for k in signs docker_signs sboms dockers_v2; do
	if grep -q "^$k:" "$work/halotrcer.yaml"; then
		echo "snapshot.sh: could not drop $k from the config" >&2
		exit 1
	fi
done
signing goreleaser release --snapshot --clean --config "$work/halotrcer.yaml" --skip=docker
gr="$repo/dist/goreleaser"
version="$(sed -n 's/.*"version":"\([^"]*\)".*/\1/p' "$gr/metadata.json" | head -n 1)"
[ -n "$version" ] || {
	echo "snapshot.sh: no version in metadata.json" >&2
	exit 1
}
tag="v$version"
echo "$tag" >"$out/TAG"

# layout NAME TAG DIST WINGET_ID: one project's release files, its cask, its
# Scoop manifest and its winget manifests into OUT_DIR.
layout() {
	rel="$out/releases/$1/$2"
	mkdir -p "$rel" "$out/casks" "$out/scoop" "$out/winget/$4"
	cp "$3"/*.tar.gz "$3"/*.zip "$3"/*.deb "$3"/*.rpm "$3"/*.apk "$3"/checksums.txt "$rel/"
	cp "$3/homebrew/Casks/$1.rb" "$out/casks/$1.rb"
	cp "$3/scoop/$1.json" "$out/scoop/$1.json"
	id_path="$(printf '%s' "$4" | tr . /)"
	cp "$3"/winget/manifests/*/"$id_path"/*/*.yaml "$out/winget/$4/"
}
layout halotrcer "$tag" "$gr" Halotrcer.Halotrcer

# --- the stand-ins, released under the tools' names (K1) ------------------------
for spec in tokvera:Halotrcer.Tokvera sievant:Halotrcer.Sievant; do
	name="${spec%%:*}"
	id="${spec#*:}"
	sed -e "s#@TOOL@#$name#g" -e "s#@REPO@#$name#g" -e "s#@WINGET@#$id#g" -e "s#@DIST@#dist/standin-$name#g" \
		testdata/standin/goreleaser.yaml.tmpl >"$work/$name.yaml"
	signing goreleaser release --snapshot --clean --config "$work/$name.yaml"
	layout "$name" v0.2.0 "$repo/dist/standin-$name" "$id"
done

# --- the signed apt and yum repositories ---------------------------------------
# The stand-ins' packages where the tools' own publishes leave theirs, then
# halotrcer's through the release workflow's script (a first publish: it seeds
# repo-meta.env from packaging/repo-meta.env).
site="$out/repo"
mkdir -p "$site/apt/pool/main/t/tokvera" "$site/apt/pool/main/s/sievant" "$site/rpm"
cp "$out"/releases/tokvera/v0.2.0/*.deb "$site/apt/pool/main/t/tokvera/"
cp "$out"/releases/sievant/v0.2.0/*.deb "$site/apt/pool/main/s/sievant/"
cp "$out"/releases/tokvera/v0.2.0/*.rpm "$out"/releases/sievant/v0.2.0/*.rpm "$site/rpm/"
REPO_GPG_KEY_FILE="$work/gpg.asc" REPO_GPG_PASSPHRASE="snapshot-passphrase" REPO_GPG_PUB="$out/repo-key.asc" \
	sh scripts/release/package-repos-docker.sh "$site" "$gr"

# --- the mirror, for every runner the matrix uses --------------------------------
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
	goos="${target%/*}"
	goarch="${target#*/}"
	ext=""
	[ "$goos" = windows ] && ext=".exe"
	CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" GOTOOLCHAIN="$toolchain" \
		go build -trimpath -o "$out/mirror-$goos-$goarch$ext" ./testdata/mirror
done
echo "snapshot.sh: halotrcer $tag with stand-in tools in $out" >&2
