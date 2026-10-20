#!/bin/sh
# Pushes a packaged chart (package-chart.sh) to an OCI registry and prints the
# digest it was stored under, for its signature and provenance:
#
#   sh scripts/release/push-chart.sh halotrcer-X.Y.Z.tgz oci://ghcr.io/OWNER/charts
#
# helm v4.3.0 reports a push as "Pushed: <ref>" then "Digest: sha256:<hex>"
# (the format both tools' push-chart.sh read). Anything but exactly one such
# digest line is refused, so a changed helm cannot make the release sign the
# wrong thing.
set -eu

tgz="${1:?usage: push-chart.sh CHART_TGZ OCI_REPO}"
repo="${2:?usage: push-chart.sh CHART_TGZ OCI_REPO}"
case "$repo" in
oci://*) ;;
*)
	echo "push-chart.sh: $repo is not an oci:// repository" >&2
	exit 2
	;;
esac

out="$(helm push "$tgz" "$repo" 2>&1)" || {
	printf '%s\n' "$out" >&2
	echo "push-chart.sh: helm push failed" >&2
	exit 1
}
printf '%s\n' "$out" >&2
digests="$(printf '%s\n' "$out" | sed -n 's/^Digest: \(sha256:[0-9a-f]\{64\}\)$/\1/p')"
count="$(printf '%s' "$digests" | grep -c . || true)"
[ "$count" -eq 1 ] || {
	echo "push-chart.sh: want one 'Digest: sha256:<64 hex>' line from helm push, found $count" >&2
	exit 1
}
printf '%s\n' "$digests"
