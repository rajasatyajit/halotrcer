#!/bin/sh
# Packages the umbrella chart (charts/halotrcer) for a release, with
# Tokvera's and Sievant's released charts vendored into it:
#
#   TOKVERA_CHART_VERSION=X SIEVANT_CHART_VERSION=Y \
#     sh scripts/release/package-chart.sh vX.Y.Z TOKVERA.tgz SIEVANT.tgz OUT_DIR
#
# TOKVERA.tgz and SIEVANT.tgz are the charts fetch-chart.sh fetched by the
# digests packaging/bundled-tools.env pins (bundled-tools.sh prints the two
# versions). Each must be that tool's chart at exactly that version. The
# umbrella's dependency versions are set to them, the charts are copied into
# its charts/ directory, it is linted with both tools off (the default) and
# both on, and packaged with the tag's version as version and appVersion.
# Helm never resolves a dependency itself, so what ships is what was
# verified. Writes OUT_DIR/halotrcer-X.Y.Z.tgz and nothing else; the
# checkout's chart is never modified (the edits happen in a copy). Pushing
# it is the release workflow's job, in the publish phase only.
set -eu

tag="${1:?usage: package-chart.sh TAG TOKVERA.tgz SIEVANT.tgz OUT_DIR}"
tokvera_tgz="${2:?usage: package-chart.sh TAG TOKVERA.tgz SIEVANT.tgz OUT_DIR}"
sievant_tgz="${3:?usage: package-chart.sh TAG TOKVERA.tgz SIEVANT.tgz OUT_DIR}"
out="${4:?usage: package-chart.sh TAG TOKVERA.tgz SIEVANT.tgz OUT_DIR}"
: "${TOKVERA_CHART_VERSION:?package-chart.sh: TOKVERA_CHART_VERSION is not set}"
: "${SIEVANT_CHART_VERSION:?package-chart.sh: SIEVANT_CHART_VERSION is not set}"

semver='^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'
version="${tag#v}"
if [ "$tag" != "v$version" ] || ! printf '%s\n' "$version" | grep -Eq "$semver"; then
	echo "package-chart.sh: $tag is not a vX.Y.Z tag" >&2
	exit 2
fi
for v in "$TOKVERA_CHART_VERSION" "$SIEVANT_CHART_VERSION"; do
	printf '%s\n' "$v" | grep -Eq "$semver" || {
		echo "package-chart.sh: '$v' is not a chart version X.Y.Z" >&2
		exit 2
	}
done

# chart_field TGZ NAME FIELD: FIELD of NAME/Chart.yaml inside TGZ.
chart_field() { tar -xzOf "$1" "$2/Chart.yaml" 2>/dev/null | sed -n "s/^$3: *//p" | head -n 1; }
for pair in "tokvera:$tokvera_tgz:$TOKVERA_CHART_VERSION" "sievant:$sievant_tgz:$SIEVANT_CHART_VERSION"; do
	name="${pair%%:*}"
	rest="${pair#*:}"
	tgz="${rest%:*}"
	want="${rest##*:}"
	[ -f "$tgz" ] || {
		echo "package-chart.sh: no $tgz" >&2
		exit 1
	}
	got_name="$(chart_field "$tgz" "$name" name)"
	got_version="$(chart_field "$tgz" "$name" version)"
	if [ "$got_name" != "$name" ] || [ "$got_version" != "$want" ]; then
		echo "package-chart.sh: $tgz is chart '${got_name:-unreadable}' version '${got_version:-unreadable}', not $name $want" >&2
		exit 1
	fi
done

root="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cp -R "$root/charts/halotrcer" "$work/halotrcer"
chart="$work/halotrcer/Chart.yaml"

# Each dependency's one placeholder version line becomes the pinned version.
awk -v tv="$TOKVERA_CHART_VERSION" -v sv="$SIEVANT_CHART_VERSION" '
	/^  - name: / { dep = $3 }
	/^    version: 0\.0\.0-dev$/ && dep == "tokvera" { print "    version: " tv; t++; next }
	/^    version: 0\.0\.0-dev$/ && dep == "sievant" { print "    version: " sv; s++; next }
	{ print }
	END { if (t != 1 || s != 1) exit 1 }
' "$chart" >"$chart.new" || {
	echo "package-chart.sh: want one '    version: 0.0.0-dev' line under each of tokvera and sievant in Chart.yaml" >&2
	exit 1
}
mv "$chart.new" "$chart"
mkdir -p "$work/halotrcer/charts"
cp "$tokvera_tgz" "$work/halotrcer/charts/tokvera-$TOKVERA_CHART_VERSION.tgz"
cp "$sievant_tgz" "$work/halotrcer/charts/sievant-$SIEVANT_CHART_VERSION.tgz"

helm lint "$work/halotrcer" --strict
# Both on, with the settings each tool's own release lints its chart with.
helm lint "$work/halotrcer" --strict \
	--set tokvera.enabled=true --set tokvera.scan.confirm=true --set tokvera.scan.target=http://release-check.invalid/v1 \
	--set sievant.enabled=true --set-string 'sievant.gateway.config.inline=server: {transport: http}'
mkdir -p "$out"
helm package "$work/halotrcer" --version "$version" --app-version "$version" --destination "$out"
