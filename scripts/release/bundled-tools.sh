#!/bin/sh
# Validates packaging/bundled-tools.env (the released Tokvera and Sievant
# artifacts a release bundles) and prints the references the release
# workflow uses, for $GITHUB_ENV:
#
#   sh scripts/release/bundled-tools.sh [FILE] >> "$GITHUB_ENV"
#
# FILE defaults to packaging/bundled-tools.env. It is parsed as strict
# KEY=value lines (# comments and blank lines allowed), never sourced. All six
# keys are required, each once, and nothing else is accepted (the file is
# Halotrcer's own, so an unknown key is a mistake):
#   TOKVERA_VERSION, SIEVANT_VERSION   vMAJOR.MINOR.PATCH[-prerelease]
#   *_IMAGE_DIGEST, *_CHART_DIGEST     sha256:<64 hex>
# Prints, for TOKVERA and the same for SIEVANT:
#   TOKVERA_VERSION=v0.2.0
#   TOKVERA_IMAGE=ghcr.io/rajasatyajit/tokvera@sha256:…
#   TOKVERA_CHART=ghcr.io/rajasatyajit/charts/tokvera@sha256:…
#   TOKVERA_CHART_VERSION=0.2.0
#   TOKVERA_IDENTITY=https://github.com/rajasatyajit/tokvera/.github/workflows/release.yml@refs/tags/v0.2.0
# TOKVERA_IDENTITY is the exact keyless signing identity of that tag's
# release run, which signed both the image and the chart (the tool's
# release.yml publish job): cosign verifies each digest against it, so a
# digest from any other release, or from anything else, is refused.
# Exits 1 with every problem listed otherwise.
set -eu

file="${1:-$(cd "$(dirname "$0")/../.." && pwd)/packaging/bundled-tools.env}"
[ -f "$file" ] || { echo "bundled-tools.sh: no $file" >&2; exit 1; }

version_re='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+(\.[0-9A-Za-z]+)*)?$'
digest_re='^sha256:[0-9a-f]{64}$'
status=0
seen=" "
lineno=0
fail() {
	echo "bundled-tools.sh: $*" >&2
	status=1
}
TOKVERA_VERSION="" TOKVERA_IMAGE_DIGEST="" TOKVERA_CHART_DIGEST=""
SIEVANT_VERSION="" SIEVANT_IMAGE_DIGEST="" SIEVANT_CHART_DIGEST=""
while IFS= read -r line || [ -n "$line" ]; do
	lineno=$((lineno + 1))
	case "$line" in "" | "#"*) continue ;; esac
	key="${line%%=*}"
	value="${line#*=}"
	if [ "$key" = "$line" ]; then
		fail "line $lineno is not KEY=value"
		continue
	fi
	case "$key" in
	TOKVERA_VERSION | SIEVANT_VERSION) pattern="$version_re" ;;
	TOKVERA_IMAGE_DIGEST | TOKVERA_CHART_DIGEST | SIEVANT_IMAGE_DIGEST | SIEVANT_CHART_DIGEST) pattern="$digest_re" ;;
	*)
		fail "line $lineno: unknown key $key"
		continue
		;;
	esac
	case "$seen" in *" $key "*)
		fail "line $lineno: $key is set twice"
		continue
		;;
	esac
	seen="$seen$key "
	if [ -z "$value" ]; then
		fail "$key is empty (set it once the tool's release is published)"
		continue
	fi
	if ! printf '%s\n' "$value" | LC_ALL=C grep -Eq "$pattern"; then
		fail "line $lineno: $key must match $pattern"
		continue
	fi
	# The key is one of the six names matched above, so this assigns only them.
	eval "$key=\$value"
done <"$file"
for key in TOKVERA_VERSION TOKVERA_IMAGE_DIGEST TOKVERA_CHART_DIGEST SIEVANT_VERSION SIEVANT_IMAGE_DIGEST SIEVANT_CHART_DIGEST; do
	case "$seen" in *" $key "*) ;; *) fail "$key is missing" ;; esac
done
[ "$status" -eq 0 ] || exit 1

# emit TOOL UPPER VERSION IMAGE_DIGEST CHART_DIGEST
emit() {
	echo "$2_VERSION=$3"
	echo "$2_IMAGE=ghcr.io/rajasatyajit/$1@$4"
	echo "$2_CHART=ghcr.io/rajasatyajit/charts/$1@$5"
	echo "$2_CHART_VERSION=${3#v}"
	echo "$2_IDENTITY=https://github.com/rajasatyajit/$1/.github/workflows/release.yml@refs/tags/$3"
}
emit tokvera TOKVERA "$TOKVERA_VERSION" "$TOKVERA_IMAGE_DIGEST" "$TOKVERA_CHART_DIGEST"
emit sievant SIEVANT "$SIEVANT_VERSION" "$SIEVANT_IMAGE_DIGEST" "$SIEVANT_CHART_DIGEST"
