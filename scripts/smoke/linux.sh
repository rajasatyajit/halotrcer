#!/bin/sh
# Runs one smoke channel inside a Linux distro container, locally and in CI
# alike (PLAN.md M4):
#
#   sh scripts/smoke/linux.sh IMAGE CHANNEL DIST_DIR
#
# The checkout and the snapshot are mounted read-only. The container gets
# only what a fresh install of that distro needs to run the installer: curl
# and CA certificates where the image lacks them (Alpine needs nothing:
# busybox has wget, tar, gzip and sha256sum, the installer's wget path).
set -eu

image="${1:?usage: linux.sh IMAGE CHANNEL DIST_DIR}"
channel="${2:?usage: linux.sh IMAGE CHANNEL DIST_DIR}"
dist="$(cd "${3:?usage: linux.sh IMAGE CHANNEL DIST_DIR}" && pwd)"
repo="$(cd "$(dirname "$0")/../.." && pwd)"

prep='
if command -v apt-get >/dev/null 2>&1; then
	export DEBIAN_FRONTEND=noninteractive
	apt-get update -qq >/dev/null && apt-get install -y -qq curl ca-certificates >/dev/null
elif command -v dnf >/dev/null 2>&1; then
	command -v curl >/dev/null 2>&1 || dnf install -y -q curl-minimal >/dev/null
	command -v tar >/dev/null 2>&1 || dnf install -y -q tar gzip >/dev/null
fi
'
# $SMOKE_RESULTS (smoke.sh) reaches the container as a mounted file.
results_args=""
if [ -n "${SMOKE_RESULTS:-}" ]; then
	touch "$SMOKE_RESULTS"
	results_args="-v $(cd "$(dirname "$SMOKE_RESULTS")" && pwd)/$(basename "$SMOKE_RESULTS"):/results -e SMOKE_RESULTS=/results"
fi
# shellcheck disable=SC2086 # results_args is deliberately split into words
exec docker run --rm -v "$repo:/src:ro" -v "$dist:/dist:ro" $results_args \
	-e SMOKE_DIST=/dist ${SMOKE_LIMIT:+-e SMOKE_LIMIT="$SMOKE_LIMIT"} \
	"$image" sh -c "$prep
sh /src/scripts/smoke/smoke.sh $channel"
