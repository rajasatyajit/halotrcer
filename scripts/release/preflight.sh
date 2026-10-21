#!/bin/sh
# Refuses a release run that must not go ahead, before anything is built or
# signed (.github/workflows/release.yml, both phases):
#
#   sh scripts/release/preflight.sh PHASE TAG
#
# PHASE is "draft" (a v* tag was pushed) or "publish" (the owner published
# the draft). Every failed check is reported with its reason, then the
# script exits 1:
#   - TAG is vMAJOR.MINOR.PATCH with an optional -prerelease;
#   - the repository is not private (REPO_PRIVATE=true): artifact
#     attestations need a public repository on this plan, and every install
#     channel downloads release assets without credentials;
#   - TAG's commit is on MAIN_REF (default origin/main);
#   - the secrets the phase needs are set (values never printed): draft
#     PKG_GPG_KEY, PKG_GPG_PASSPHRASE, PKG_APK_KEY; publish PUBLISHER_TOKEN
#     and the same three (the OpenPGP key signs the apt and yum metadata);
#   - packaging/bundled-tools.env at TAG's commit pins released Tokvera and
#     Sievant images and charts (bundled-tools.sh validates it): the image
#     and the umbrella chart are built from them. A stable TAG must pin
#     stable tool releases.
# The same checks as Tokvera's and Sievant's preflight, plus the last.
set -eu

phase="${1:?usage: preflight.sh draft|publish TAG}"
tag="${2:?usage: preflight.sh draft|publish TAG}"
main_ref="${MAIN_REF:-origin/main}"
here="$(cd "$(dirname "$0")" && pwd)"
status=0
fail() {
	echo "preflight: $*" >&2
	status=1
}

case "$phase" in
draft) need="PKG_GPG_KEY PKG_GPG_PASSPHRASE PKG_APK_KEY" ;;
publish) need="PUBLISHER_TOKEN PKG_GPG_KEY PKG_GPG_PASSPHRASE PKG_APK_KEY" ;;
*)
	echo "preflight: phase '$phase' is not draft or publish" >&2
	exit 2
	;;
esac

if ! printf '%s\n' "$tag" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+(\.[0-9A-Za-z]+)*)?$'; then
	fail "tag '$tag' is not vMAJOR.MINOR.PATCH[-prerelease]"
fi

if [ "${REPO_PRIVATE:-}" = true ]; then
	fail "the repository is private: artifact attestations (SLSA provenance) need a public repository on this plan, and the install scripts, Homebrew, Scoop and winget download release assets without credentials. Make it public first (docs/releasing.md)."
fi

commit=""
if ! commit="$(git rev-parse -q --verify "refs/tags/$tag^{commit}")"; then
	fail "no tag $tag in this checkout"
	commit=""
elif ! git rev-parse -q --verify "$main_ref^{commit}" >/dev/null; then
	fail "no $main_ref in this checkout (fetch-depth: 0 fetches it)"
elif ! git merge-base --is-ancestor "$commit" "$main_ref"; then
	fail "tag $tag ($commit) is not on $main_ref"
fi

for name in $need; do
	eval "value=\${$name:-}"
	# shellcheck disable=SC2154 # value is assigned by the eval above
	[ -n "$value" ] || fail "secret $name is not set (the $phase phase needs it)"
done

if [ -n "$commit" ]; then
	pins="$(mktemp)"
	if git show "$commit:packaging/bundled-tools.env" >"$pins" 2>/dev/null; then
		if out="$(sh "$here/bundled-tools.sh" "$pins")"; then
			# A stable release bundles stable tools: an rc's package is
			# 0.2.0~rc.1, which the packages' ">= 0.2.0" does not accept, and
			# a stable image should not carry a release candidate.
			case "$tag" in *-*) ;; *)
				for v in $(printf '%s\n' "$out" | sed -n 's/^\(TOKVERA\|SIEVANT\)_VERSION=\(.*-.*\)$/\1 \2/p' | tr ' ' '='); do
					fail "stable $tag pins the release candidate ${v%%=*} ${v#*=}; pin a stable release"
				done
				;;
			esac
		else
			fail "packaging/bundled-tools.env at $tag does not pin released Tokvera and Sievant artifacts (above)"
		fi
	else
		fail "no packaging/bundled-tools.env at $tag"
	fi
	rm -f "$pins"
fi

[ "$status" -eq 0 ] && echo "preflight: $phase $tag ok"
exit "$status"
