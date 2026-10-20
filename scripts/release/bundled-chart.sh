#!/bin/sh
# Builds the umbrella chart from the pinned, verified Tokvera and Sievant
# charts (both release phases, after verify-bundled.sh):
#
#   sh scripts/release/bundled-chart.sh BUNDLED_ENV TAG OUT_DIR
#
# BUNDLED_ENV is bundled-tools.sh's output, read as KEY=value lines, never
# sourced. Fetches each chart by its digest (fetch-chart.sh) and packages
# the umbrella with them (package-chart.sh) into OUT_DIR/halotrcer-X.Y.Z.tgz.
# The draft phase runs it to prove the chart builds before the draft exists;
# the publish phase runs it before anything is published, and pushes the
# result only after the other channels.
set -eu

file="${1:?usage: bundled-chart.sh BUNDLED_ENV TAG OUT_DIR}"
tag="${2:?usage: bundled-chart.sh BUNDLED_ENV TAG OUT_DIR}"
out="${3:?usage: bundled-chart.sh BUNDLED_ENV TAG OUT_DIR}"
here="$(cd "$(dirname "$0")" && pwd)"

tokvera_chart="" sievant_chart="" tokvera_version="" sievant_version=""
while IFS= read -r line || [ -n "$line" ]; do
	value="${line#*=}"
	case "${line%%=*}" in
	TOKVERA_CHART) tokvera_chart="$value" ;;
	SIEVANT_CHART) sievant_chart="$value" ;;
	TOKVERA_CHART_VERSION) tokvera_version="$value" ;;
	SIEVANT_CHART_VERSION) sievant_version="$value" ;;
	esac
done <"$file"
for v in "$tokvera_chart" "$sievant_chart" "$tokvera_version" "$sievant_version"; do
	[ -n "$v" ] || {
		echo "bundled-chart.sh: $file lacks a chart or chart version" >&2
		exit 1
	}
done

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
sh "$here/fetch-chart.sh" "$tokvera_chart" "$work/tokvera.tgz"
sh "$here/fetch-chart.sh" "$sievant_chart" "$work/sievant.tgz"
TOKVERA_CHART_VERSION="$tokvera_version" SIEVANT_CHART_VERSION="$sievant_version" \
	sh "$here/package-chart.sh" "$tag" "$work/tokvera.tgz" "$work/sievant.tgz" "$out"
