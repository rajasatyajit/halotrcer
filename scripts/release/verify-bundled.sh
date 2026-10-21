#!/bin/sh
# Verifies the Tokvera and Sievant artifacts packaging/bundled-tools.env pins,
# before anything is built from them (both release phases):
#
#   sh scripts/release/verify-bundled.sh BUNDLED_ENV
#
# BUNDLED_ENV is bundled-tools.sh's output. For each tool:
#   - the image digest must name a multi-platform image index and the chart
#     digest a Helm chart (their manifests' media types, read by digest), so
#     the two cannot be swapped: the tool's release signed both with the
#     same identity, and cosign alone cannot tell them apart;
#   - cosign must verify both against the exact identity of that tag's
#     release run (TOOL_IDENTITY), with GitHub's OIDC issuer.
# BUNDLED_ENV is read as KEY=value lines of the eight expected keys per tool,
# never sourced. Needs cosign, oras and jq.
set -eu

file="${1:?usage: verify-bundled.sh BUNDLED_ENV}"
issuer=https://token.actions.githubusercontent.com
TOKVERA_IMAGE="" TOKVERA_CHART="" TOKVERA_IDENTITY=""
SIEVANT_IMAGE="" SIEVANT_CHART="" SIEVANT_IDENTITY=""
while IFS= read -r line || [ -n "$line" ]; do
	key="${line%%=*}"
	value="${line#*=}"
	case "$key" in
	TOKVERA_IMAGE) TOKVERA_IMAGE="$value" ;;
	TOKVERA_CHART) TOKVERA_CHART="$value" ;;
	TOKVERA_IDENTITY) TOKVERA_IDENTITY="$value" ;;
	SIEVANT_IMAGE) SIEVANT_IMAGE="$value" ;;
	SIEVANT_CHART) SIEVANT_CHART="$value" ;;
	SIEVANT_IDENTITY) SIEVANT_IDENTITY="$value" ;;
	TOKVERA_VERSION | TOKVERA_CHART_VERSION | SIEVANT_VERSION | SIEVANT_CHART_VERSION) ;;
	*)
		echo "verify-bundled.sh: unexpected line in $file: $key" >&2
		exit 1
		;;
	esac
done <"$file"

# media REF: the manifest's media type and its config's, read by digest.
media() { oras manifest fetch "$1" | jq -r '[.mediaType // "", .config.mediaType // ""] | join(" ")'; }

# check NAME IMAGE CHART IDENTITY
check() {
	if [ -z "$2" ] || [ -z "$3" ] || [ -z "$4" ]; then
		echo "verify-bundled.sh: $file has no $1 image, chart or identity" >&2
		exit 1
	fi
	case "$(media "$2")" in
	"application/vnd.oci.image.index.v1+json "* | "application/vnd.docker.distribution.manifest.list.v2+json "*) ;;
	*)
		echo "verify-bundled.sh: $2 is not a multi-platform image index" >&2
		exit 1
		;;
	esac
	case "$(media "$3")" in
	*" application/vnd.cncf.helm.config.v1+json") ;;
	*)
		echo "verify-bundled.sh: $3 is not a Helm chart" >&2
		exit 1
		;;
	esac
	for ref in "$2" "$3"; do
		cosign verify "$ref" --certificate-identity "$4" --certificate-oidc-issuer "$issuer" >/dev/null
		echo "verify-bundled.sh: verified $ref ($4)"
	done
}
check tokvera "$TOKVERA_IMAGE" "$TOKVERA_CHART" "$TOKVERA_IDENTITY"
check sievant "$SIEVANT_IMAGE" "$SIEVANT_CHART" "$SIEVANT_IDENTITY"
