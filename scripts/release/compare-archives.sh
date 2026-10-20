#!/bin/sh
# The publish phase rebuilds the release and pushes the cask, the Scoop
# manifest, the winget pull request and the image from that rebuild. This
# stops it unless every archive it rebuilt is byte for byte the one the owner
# published:
#
#   sh scripts/release/compare-archives.sh BUILT_CHECKSUMS PUBLISHED_CHECKSUMS
#
# The .tar.gz and .zip lines (sha256sum format) of both files must be the
# same set: nothing rebuilt that was not published, nothing published that
# was not rebuilt. The archives are reproducible (every timestamp comes from
# the commit; TestSnapshotReproducible). Packages and SBOMs are not compared:
# package signatures carry a time and SPDX namespaces differ by design, and
# the publish phase builds neither.
set -eu

built="${1:?usage: compare-archives.sh BUILT_CHECKSUMS PUBLISHED_CHECKSUMS}"
published="${2:?usage: compare-archives.sh BUILT_CHECKSUMS PUBLISHED_CHECKSUMS}"
archives() { grep -E '^[0-9a-f]{64}  [^ ]+\.(tar\.gz|zip)$' "$1" | LC_ALL=C sort || true; }

b="$(archives "$built")"
p="$(archives "$published")"
[ -n "$b" ] || {
	echo "compare-archives.sh: $built lists no archives" >&2
	exit 1
}
if [ "$b" != "$p" ]; then
	echo "compare-archives.sh: the rebuilt archives differ from the published release; nothing was published." >&2
	echo "--- published" >&2
	printf '%s\n' "$p" >&2
	echo "+++ rebuilt" >&2
	printf '%s\n' "$b" >&2
	exit 1
fi
echo "compare-archives.sh: $(printf '%s\n' "$b" | grep -c .) archives identical to the published release"
