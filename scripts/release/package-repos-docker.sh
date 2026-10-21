#!/bin/sh
# Runs scripts/release/package-repos.sh inside the pinned Ubuntu 24.04 image,
# with its tools at pinned versions, the same way locally, in the tests and
# in the release workflow:
#
#   sh scripts/release/package-repos-docker.sh SITE PACKAGES_DIR
#
# The REPO_* variables package-repos.sh reads are passed through by name,
# never on the command line; REPO_GPG_KEY_FILE, REPO_GPG_PUB and the seed are
# mounted read-only. SITE is written and handed back to the calling user.
set -eu

site="${1:?usage: package-repos-docker.sh SITE PACKAGES_DIR}"
pkgs="${2:?usage: package-repos-docker.sh SITE PACKAGES_DIR}"
: "${REPO_GPG_KEY_FILE:?package-repos-docker.sh: REPO_GPG_KEY_FILE is not set}"
: "${REPO_GPG_PUB:?package-repos-docker.sh: REPO_GPG_PUB is not set}"
mkdir -p "$site"
site="$(cd "$site" && pwd)"
pkgs="$(cd "$pkgs" && pwd)"
abs() { printf '%s/%s\n' "$(cd "$(dirname "$1")" && pwd)" "$(basename "$1")"; }
key="$(abs "$REPO_GPG_KEY_FILE")"
pub="$(abs "$REPO_GPG_PUB")"
repo="$(cd "$(dirname "$0")/../.." && pwd)"
seed="$(abs "${REPO_META_SEED:-$repo/packaging/repo-meta.env}")"

# ubuntu:24.04 and noble's package versions: the pins Tokvera's and
# Sievant's package-repos-docker.sh use, so the three products build the
# shared metadata with the same tools (apt-utils must equal the image's apt).
image="ubuntu@sha256:68ee22004a9109b44973313b02ff7b1de250f1de0f5ecb628d73f579aaff33ac"
packages="apt-utils=2.8.3 createrepo-c=0.17.3-2ubuntu3 gnupg=2.4.4-2ubuntu17.6"

export REPO_GPG_PASSPHRASE="${REPO_GPG_PASSPHRASE:-}"
docker run --rm \
	-v "$repo/scripts/release/package-repos.sh:/package-repos.sh:ro" \
	-v "$site:/site" -v "$pkgs:/pkgs:ro" -v "$key:/key.asc:ro" -v "$pub:/pub.asc:ro" \
	-v "$seed:/repo-meta.env:ro" -e REPO_META_SEED=/repo-meta.env \
	-e REPO_GPG_PASSPHRASE -e REPO_BASE_URL \
	-e REPO_GPG_KEY_FILE=/key.asc -e REPO_GPG_PUB=/pub.asc \
	-e HOST_IDS="$(id -u):$(id -g)" \
	"$image" sh -c "
		set -eu
		export DEBIAN_FRONTEND=noninteractive
		apt-get update -qq >/dev/null
		apt-get install -y -qq --no-install-recommends $packages >/dev/null
		status=0
		sh /package-repos.sh /site /pkgs || status=\$?
		chown -R \"\$HOST_IDS\" /site
		exit \$status
	"
