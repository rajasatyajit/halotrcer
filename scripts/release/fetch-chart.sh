#!/bin/sh
# Fetches one Helm chart from an OCI registry by digest, for the umbrella
# chart to vendor (the release workflow, after cosign has verified that
# digest against the tool's release identity):
#
#   sh scripts/release/fetch-chart.sh REGISTRY/REPO@sha256:<64 hex> OUT.tgz
#
# Reads the manifest at that digest, takes its one Helm chart layer
# (application/vnd.cncf.helm.chart.content.v1.tar+gzip) and writes it to
# OUT.tgz. Both digests are checked here, independently of oras: the
# manifest's bytes must hash to the digest asked for, and the chart's bytes
# to the layer digest that manifest names. So OUT.tgz is exactly the chart
# the signature covers. Needs oras and jq.
set -eu

ref="${1:?usage: fetch-chart.sh REGISTRY/REPO@sha256:DIGEST OUT.tgz}"
out="${2:?usage: fetch-chart.sh REGISTRY/REPO@sha256:DIGEST OUT.tgz}"
repo="${ref%@*}"
digest="${ref#"$repo"@}"
printf '%s\n' "$digest" | grep -Eq '^sha256:[0-9a-f]{64}$' && [ "$repo@$digest" = "$ref" ] || {
	echo "fetch-chart.sh: $ref is not REGISTRY/REPO@sha256:<64 hex>" >&2
	exit 2
}

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
sha() { sha256sum "$1" | cut -d' ' -f1; }

oras manifest fetch --output "$work/manifest.json" "$ref"
[ "sha256:$(sha "$work/manifest.json")" = "$digest" ] || {
	echo "fetch-chart.sh: the manifest fetched for $ref does not hash to $digest" >&2
	exit 1
}
layers="$(jq -r '[.layers[] | select(.mediaType == "application/vnd.cncf.helm.chart.content.v1.tar+gzip") | .digest] | .[]' "$work/manifest.json")"
[ "$(printf '%s' "$layers" | grep -c .)" -eq 1 ] || {
	echo "fetch-chart.sh: want one Helm chart layer in $ref, found: $(printf '%s' "$layers" | tr '\n' ' ')" >&2
	exit 1
}
printf '%s\n' "$layers" | grep -Eq '^sha256:[0-9a-f]{64}$' || {
	echo "fetch-chart.sh: chart layer digest '$layers' is not sha256" >&2
	exit 1
}
oras blob fetch --output "$work/chart.tgz" "$repo@$layers"
[ "sha256:$(sha "$work/chart.tgz")" = "$layers" ] || {
	echo "fetch-chart.sh: the chart fetched from $repo does not hash to its layer digest $layers" >&2
	exit 1
}
mkdir -p "$(dirname "$out")"
mv "$work/chart.tgz" "$out"
echo "fetch-chart.sh: $ref -> $out (layer $layers)" >&2
