#!/bin/sh
# Prints the one digest GoReleaser pushed for IMAGE, read from its
# artifacts.json, for the image's signature check and provenance:
#
#   sh scripts/release/image-subject.sh dist/goreleaser/artifacts.json ghcr.io/OWNER/NAME
#
# dockers_v2 records a "Docker Image" artifact per pushed tag, named
# IMAGE:TAG, with the index digest in extra.Digest (GoReleaser v2.18.3,
# internal/pipe/docker/v2). Every tag of IMAGE must carry the same sha256
# digest and there must be at least one tag; anything else is refused.
set -eu

file="${1:?usage: image-subject.sh ARTIFACTS_JSON IMAGE}"
image="${2:?usage: image-subject.sh ARTIFACTS_JSON IMAGE}"
digests="$(jq -r --arg image "$image" '
	[.[] | select(.type == "Docker Image" and (.name | startswith($image + ":"))) | .extra.Digest // ""]
	| unique | .[]' "$file")"
count="$(printf '%s' "$digests" | grep -c . || true)"
[ "$count" -eq 1 ] || {
	echo "image-subject.sh: want one digest for $image in $file, found $count: $(printf '%s' "$digests" | tr '\n' ' ')" >&2
	exit 1
}
printf '%s\n' "$digests" | grep -Eq '^sha256:[0-9a-f]{64}$' || {
	echo "image-subject.sh: '$digests' is not a sha256 digest" >&2
	exit 1
}
printf '%s\n' "$digests"
