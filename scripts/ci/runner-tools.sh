#!/bin/sh
# Checks that a self-hosted Linux runner has the host tools a job uses:
#
#   sh scripts/ci/runner-tools.sh TOOL...
#
# GitHub's ubuntu-24.04 image has them all (ci.yml installs the PDF
# checkers there); a self-hosted runner may not, and the PDF tests skip
# when a tool is missing, so a bare runner would pass CI while testing less.
# The workflows run this first, only when runner.environment is
# self-hosted, and it fails naming every missing tool. docs/development.md
# lists each job's tools and the packages that provide them.
#
# docker-daemon is a check, not a program: `docker info` answers, so the
# daemon runs and this user may use it.
set -eu

[ "$#" -gt 0 ] || {
	echo "usage: runner-tools.sh TOOL..." >&2
	exit 2
}
missing=""
for tool in "$@"; do
	case "$tool" in
	docker-daemon) docker info >/dev/null 2>&1 || missing="$missing $tool" ;;
	*) command -v "$tool" >/dev/null 2>&1 || missing="$missing $tool" ;;
	esac
done
if [ -n "$missing" ]; then
	echo "runner-tools: this runner lacks:$missing" >&2
	echo "runner-tools: install them (docs/development.md lists the packages), or unset the LINUX_RUNNER variable to run on GitHub-hosted runners" >&2
	exit 1
fi
echo "runner-tools: all $# present"
