#!/bin/sh
# Publishes a release's packages to the signed apt and yum repositories on
# the gh-pages branch of rajasatyajit/packages, shared with Tokvera and
# Sievant (the release workflow's publish phase):
#
#   sh scripts/release/publish-repos.sh TAG PACKAGES_DIR
#
# Clones gh-pages (or starts it, on the first publish of any product), adds
# the packages and rebuilds and re-signs the metadata with
# package-repos-docker.sh, commits "halotrcer TAG" when anything changed,
# and pushes. A re-run for the same tag changes nothing and pushes nothing.
#
# Tokvera's and Sievant's release workflows push to the same branch, so a
# push can be refused because the branch moved since the clone. The attempt
# then starts over from the new tip: a fresh clone, the packages added again
# and the metadata rebuilt and re-signed over everything the branch now
# holds (two signed InRelease files cannot be rebased textually). At most
# REPO_PUSH_ATTEMPTS attempts (default 3), with 5 s then 10 s between them
# (doubling beyond). Any other failure stops at once.
#
# Environment: REPO_REMOTE (default https://github.com/rajasatyajit/packages.git),
# PUBLISHER_TOKEN (sent as an HTTP header, never in the URL or on disk),
# REPO_PUSH_ATTEMPTS, and the REPO_* variables package-repos.sh reads.
set -eu

tag="${1:?usage: publish-repos.sh TAG PACKAGES_DIR}"
pkgs="${2:?usage: publish-repos.sh TAG PACKAGES_DIR}"
remote="${REPO_REMOTE:-https://github.com/rajasatyajit/packages.git}"
here="$(cd "$(dirname "$0")" && pwd)"
site="$(mktemp -d)"
trap 'rm -rf "$site"' EXIT

g() {
	if [ -n "${PUBLISHER_TOKEN:-}" ]; then
		git -c http.extraheader="AUTHORIZATION: basic $(printf 'x-access-token:%s' "$PUBLISHER_TOKEN" | base64 | tr -d '\n')" "$@"
	else
		git "$@"
	fi
}

attempts="${REPO_PUSH_ATTEMPTS:-3}"
case "$attempts" in '' | *[!0-9]* | 0)
	echo "publish-repos.sh: REPO_PUSH_ATTEMPTS must be a positive number" >&2
	exit 2
	;;
esac

# attempt N: one clone, build, commit and push. It runs in a subshell with
# set -e (a function called from `||` would run with set -e suspended).
# Exit status: 0 done (pushed, or nothing to push); 75 the push was refused
# because gh-pages moved (try again); anything else failed for good.
attempt() {
	rm -rf "$site"
	mkdir -p "$site"
	# An unreachable remote fails here; a reachable one without gh-pages is
	# the first publish.
	heads="$(g ls-remote --heads "$remote" gh-pages)" || {
		echo "publish-repos.sh: cannot reach $remote" >&2
		exit 1
	}
	if [ -n "$heads" ]; then
		g clone --quiet --depth 1 --branch gh-pages "$remote" "$site"
	else
		echo "publish-repos.sh: $remote has no gh-pages branch yet; starting it" >&2
		git init --quiet -b gh-pages "$site"
		git -C "$site" remote add origin "$remote"
	fi

	sh "$here/package-repos-docker.sh" "$site" "$pkgs"
	# GitHub Pages would hide paths starting with an underscore from Jekyll.
	touch "$site/.nojekyll"

	git -C "$site" add -A
	if git -C "$site" diff --cached --quiet; then
		echo "publish-repos.sh: the repositories already carry halotrcer $tag; nothing to push" >&2
		exit 0
	fi
	git -C "$site" -c user.name="Satyajit Rajaraman" -c user.email="satyajit.rajaraman@halotrcer.com" \
		commit --quiet -m "halotrcer $tag"
	if out="$(g -C "$site" push --quiet --porcelain origin gh-pages 2>&1)"; then
		echo "publish-repos.sh: pushed halotrcer $tag to $remote gh-pages (attempt $1 of $attempts)" >&2
		exit 0
	fi
	case "$out" in
	*"[rejected]"* | *"non-fast-forward"* | *"fetch first"* | *"stale info"*)
		echo "publish-repos.sh: attempt $1 of $attempts: gh-pages moved since the clone (another publish); starting over from its new tip" >&2
		exit 75
		;;
	esac
	echo "publish-repos.sh: push failed: $out" >&2
	exit 1
}

n=1
while :; do
	set +e
	(
		set -e
		attempt "$n"
	)
	status=$?
	set -e
	[ "$status" -eq 75 ] || exit "$status"
	if [ "$n" -ge "$attempts" ]; then
		echo "publish-repos.sh: gh-pages kept moving; gave up after $attempts attempts. Nothing of this run was pushed; run the job again." >&2
		exit 1
	fi
	sleep "$((5 << (n - 1)))" # backoff: 5 s, then 10 s
	n=$((n + 1))
done
