#!/bin/sh
# Adds a release's deb and rpm packages to the signed apt and yum repositories
# on rajasatyajit/packages gh-pages, and regenerates and signs their metadata.
# The repositories are shared with Tokvera and Sievant; this follows their
# shared repository contract (Sievant PLAN.md §6l) exactly:
#
#   sh scripts/release/package-repos.sh SITE PACKAGES_DIR
#
# Runs inside the pinned Ubuntu 24.04 image (package-repos-docker.sh installs
# apt-ftparchive, createrepo_c and gpg there at pinned versions). SITE is the
# gh-pages checkout. It keeps every earlier release of every product, and the
# indexes are rebuilt from every package in the pool, any product's, for
# every architecture present. Nothing here deletes or rewrites a package.
#
# The repositories' description (Origin, Label, Description and the apt
# architectures) is read from SITE/repo-meta.env, never hard-coded. The first
# publish of any product seeds it from packaging/repo-meta.env
# (REPO_META_SEED), byte-identical in the three repositories. It is parsed as
# strict KEY=value lines and never sourced, because it comes from a remote
# branch. Unknown keys are ignored (forward compatible); anything malformed
# is refused before the site is changed. The apt indexes cover the union of
# REPO_ARCHITECTURES and every architecture of a deb in the pool ("all"
# excluded). When the signed Release disagrees with repo-meta.env, the
# metadata is rebuilt and re-signed even if no package is new, so the
# products converge on the file.
#
# Environment:
#   REPO_GPG_KEY_FILE    the armored OpenPGP private key (the PKG_GPG_KEY secret)
#   REPO_GPG_PASSPHRASE  its passphrase (may be empty for an unprotected key)
#   REPO_GPG_PUB         the public key users install (packaging/keys/halotrcer-packages.asc);
#                        its fingerprint must be the signing key's
#   REPO_BASE_URL        where SITE is served (default https://rajasatyajit.github.io/packages),
#                        written into rpm/halotrcer.repo
#   REPO_META_SEED       the repo-meta.env an empty site is seeded with (default
#                        packaging/repo-meta.env of this checkout)
#
# Layout under SITE:
#   repo-meta.env                          the shared settings
#   halotrcer-packages.asc                 the public key
#   apt/pool/main/h/halotrcer/*.deb        every Halotrcer release's packages
#                                          (Tokvera's in t/tokvera, Sievant's in s/sievant)
#   apt/dists/stable/{Release,InRelease,Release.gpg}
#   apt/dists/stable/main/binary-<arch>/Packages{,.gz}, for every architecture
#   rpm/*.rpm, rpm/repodata/ (repomd.xml signed as repomd.xml.asc), rpm/halotrcer.repo
#
# A package already in SITE with different bytes is refused: a published
# version never changes under its users. When nothing is new and the Release
# agrees with repo-meta.env, the signed metadata is left byte for byte after
# it verifies, so a re-run changes nothing. Every signature is checked with
# gpgv against REPO_GPG_PUB before the script finishes. The key is never
# printed.
set -eu

site="${1:?usage: package-repos.sh SITE PACKAGES_DIR}"
pkgs="${2:?usage: package-repos.sh SITE PACKAGES_DIR}"
: "${REPO_GPG_KEY_FILE:?package-repos.sh: REPO_GPG_KEY_FILE is not set}"
: "${REPO_GPG_PUB:?package-repos.sh: REPO_GPG_PUB is not set}"
base="${REPO_BASE_URL:-https://rajasatyajit.github.io/packages}"
for tool in apt-ftparchive createrepo_c dpkg-deb gpg gpgv gzip; do
	command -v "$tool" >/dev/null 2>&1 || {
		echo "package-repos.sh: $tool is not installed" >&2
		exit 1
	}
done

# --- the shared settings, validated before anything is written ----------------------
meta="$site/repo-meta.env"
seeded=0
read_from="$meta"
if [ ! -f "$meta" ]; then
	read_from="${REPO_META_SEED:-$(cd "$(dirname "$0")/../.." && pwd)/packaging/repo-meta.env}"
	[ -f "$read_from" ] || {
		echo "package-repos.sh: no repo-meta.env in the site and no seed at $read_from" >&2
		exit 1
	}
	seeded=1
fi
origin="" label="" description="" meta_arches=""
lineno=0
while IFS= read -r line || [ -n "$line" ]; do
	lineno=$((lineno + 1))
	case "$line" in "" | "#"*) continue ;; esac
	key="${line%%=*}"
	value="${line#*=}"
	if [ "$key" = "$line" ]; then
		echo "package-repos.sh: repo-meta.env line $lineno is not KEY=value; fix repo-meta.env at the root of gh-pages" >&2
		exit 1
	fi
	case "$key" in
	REPO_ORIGIN | REPO_LABEL) pattern='^[A-Za-z0-9][A-Za-z0-9 ._-]*$' ;;
	REPO_DESCRIPTION) pattern='^[A-Za-z0-9][A-Za-z0-9 ._,()/:-]*$' ;;
	REPO_ARCHITECTURES) pattern='^[a-z0-9]+( [a-z0-9]+)*$' ;;
	*)
		echo "package-repos.sh: repo-meta.env line $lineno: unknown key $key, ignored" >&2
		continue
		;;
	esac
	if ! printf '%s\n' "$value" | LC_ALL=C grep -Eq "$pattern"; then
		echo "package-repos.sh: repo-meta.env line $lineno: $key must match $pattern; fix repo-meta.env at the root of gh-pages" >&2
		exit 1
	fi
	case "$key" in
	REPO_ORIGIN) origin="$value" ;;
	REPO_LABEL) label="$value" ;;
	REPO_DESCRIPTION) description="$value" ;;
	REPO_ARCHITECTURES) meta_arches="$value" ;;
	esac
done <"$read_from"
for v in REPO_ORIGIN:"$origin" REPO_LABEL:"$label" REPO_DESCRIPTION:"$description" REPO_ARCHITECTURES:"$meta_arches"; do
	[ -n "${v#*:}" ] || {
		echo "package-repos.sh: repo-meta.env has no ${v%%:*}" >&2
		exit 1
	}
done

# --- the signing key, checked before anything is written -----------------------------
gnupg="$(mktemp -d)"
trap 'rm -rf "$gnupg"' EXIT
chmod 700 "$gnupg"
fpr() { GNUPGHOME="$gnupg" gpg --batch --quiet --with-colons --import-options show-only --import "$1" 2>/dev/null | awk -F: '$1 == "fpr" { print $10; exit }'; }
want="$(fpr "$REPO_GPG_PUB")"
got="$(fpr "$REPO_GPG_KEY_FILE")"
if [ -z "$want" ] || [ "$got" != "$want" ]; then
	echo "package-repos.sh: the signing key's fingerprint '${got:-unreadable}' is not $REPO_GPG_PUB's '${want:-none}'" >&2
	exit 1
fi

# --- the packages ------------------------------------------------------------------
n=0
for f in "$pkgs"/*.deb "$pkgs"/*.rpm; do [ -e "$f" ] && n=$((n + 1)); done
[ "$n" -gt 0 ] || {
	echo "package-repos.sh: no .deb or .rpm in $pkgs" >&2
	exit 1
}
pool="$site/apt/pool/main/h/halotrcer"
# A replaced package is refused before anything is copied.
for f in "$pkgs"/*.deb "$pkgs"/*.rpm; do
	[ -e "$f" ] || continue
	case "$f" in *.deb) dst="$pool/$(basename "$f")" ;; *) dst="$site/rpm/$(basename "$f")" ;; esac
	if [ -e "$dst" ] && ! cmp -s "$f" "$dst"; then
		echo "package-repos.sh: $(basename "$f") is already published with different bytes; refusing to replace it" >&2
		exit 1
	fi
done
if [ "$seeded" -eq 1 ]; then
	cp "$read_from" "$meta"
	echo "package-repos.sh: the site had no repo-meta.env; seeded it from $(basename "$read_from")" >&2
fi
mkdir -p "$pool" "$site/rpm"
added=0
for f in "$pkgs"/*.deb "$pkgs"/*.rpm; do
	[ -e "$f" ] || continue
	case "$f" in *.deb) dst="$pool/$(basename "$f")" ;; *) dst="$site/rpm/$(basename "$f")" ;; esac
	if [ ! -e "$dst" ]; then
		cp "$f" "$dst"
		added=$((added + 1))
	fi
done

# Every architecture: the settings' list and every deb in the pool, any
# product's ("all" packages appear in every architecture's index).
arches="$(
	{
		printf '%s\n' "$meta_arches" | tr ' ' '\n'
		find "$site/apt/pool" -type f -name '*.deb' -exec dpkg-deb -f {} Architecture \;
	} | grep -Ev '^(all|)$' | LC_ALL=C sort -u | paste -sd' ' -
)"

GNUPGHOME="$gnupg" gpg --batch --quiet --yes --dearmor -o "$gnupg/pub.gpg" "$REPO_GPG_PUB"
rel="$site/apt/dists/stable"
# verify: every signed file checks out against the public key alone.
verify() {
	gpgv --quiet --keyring "$gnupg/pub.gpg" "$rel/InRelease" 2>/dev/null || {
		echo "package-repos.sh: InRelease does not verify" >&2
		exit 1
	}
	gpgv --quiet --keyring "$gnupg/pub.gpg" "$rel/Release.gpg" "$rel/Release" 2>/dev/null || {
		echo "package-repos.sh: Release.gpg does not verify" >&2
		exit 1
	}
	gpgv --quiet --keyring "$gnupg/pub.gpg" "$site/rpm/repodata/repomd.xml.asc" "$site/rpm/repodata/repomd.xml" 2>/dev/null || {
		echo "package-repos.sh: repomd.xml.asc does not verify" >&2
		exit 1
	}
}
# release_field NAME: NAME's value in the signed Release, if any.
release_field() { sed -n "s/^$1: //p" "$rel/Release" 2>/dev/null | head -n 1; }
described=1
if [ "$(release_field Origin)" != "$origin" ] || [ "$(release_field Label)" != "$label" ] ||
	[ "$(release_field Description)" != "$description" ] || [ "$(release_field Architectures)" != "$arches" ]; then
	described=0
fi
# Nothing new: the signed metadata stays byte for byte (apt-ftparchive stamps
# the current time into Release, so regenerating would change it).
if [ "$added" -eq 0 ] && [ "$seeded" -eq 0 ] && [ "$described" -eq 1 ] && [ -f "$rel/InRelease" ] && [ -f "$site/rpm/repodata/repomd.xml.asc" ]; then
	verify
	echo "package-repos.sh: 0 new of $n packages; the signed metadata is unchanged and verifies" >&2
	exit 0
fi

# --- apt ------------------------------------------------------------------------------
(
	cd "$site/apt"
	for arch in $arches; do
		d="dists/stable/main/binary-$arch"
		mkdir -p "$d"
		apt-ftparchive --arch "$arch" packages pool/main >"$d/Packages"
		gzip -9 -n -c "$d/Packages" >"$d/Packages.gz"
	done
	rm -f dists/stable/Release dists/stable/InRelease dists/stable/Release.gpg
	apt-ftparchive \
		-o "APT::FTPArchive::Release::Origin=$origin" \
		-o "APT::FTPArchive::Release::Label=$label" \
		-o APT::FTPArchive::Release::Suite=stable \
		-o APT::FTPArchive::Release::Codename=stable \
		-o "APT::FTPArchive::Release::Architectures=$arches" \
		-o APT::FTPArchive::Release::Components=main \
		-o "APT::FTPArchive::Release::Description=$description" \
		release dists/stable >"$site/.Release.tmp"
	mv "$site/.Release.tmp" dists/stable/Release
)

# --- yum ------------------------------------------------------------------------------
rm -rf "$site/rpm/repodata"
createrepo_c --quiet "$site/rpm"
cat >"$site/rpm/halotrcer.repo" <<REPO
[halotrcer]
name=Halotrcer
baseurl=$base/rpm
enabled=1
gpgcheck=1
repo_gpgcheck=1
gpgkey=$base/halotrcer-packages.asc
REPO

# --- sign ----------------------------------------------------------------------------
printf '%s' "${REPO_GPG_PASSPHRASE:-}" >"$gnupg/passphrase"
g() { GNUPGHOME="$gnupg" gpg --batch --quiet --yes --pinentry-mode loopback --passphrase-file "$gnupg/passphrase" "$@"; }
g --import "$REPO_GPG_KEY_FILE" 2>/dev/null
g --local-user "$want" --clearsign --digest-algo SHA512 -o "$rel/InRelease" "$rel/Release"
g --local-user "$want" --detach-sign --armor --digest-algo SHA512 -o "$rel/Release.gpg" "$rel/Release"
g --local-user "$want" --detach-sign --armor --digest-algo SHA512 -o "$site/rpm/repodata/repomd.xml.asc" "$site/rpm/repodata/repomd.xml"
cp "$REPO_GPG_PUB" "$site/halotrcer-packages.asc"

# --- check what was signed, with the public key only ----------------------------------
verify
echo "package-repos.sh: $added new of $n packages; apt ($arches) and yum metadata for every product in the pool signed by $want and verified" >&2
