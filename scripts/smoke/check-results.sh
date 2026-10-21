#!/bin/sh
# Decides a smoke job: one RESULT line per expected channel (PLAN.md M4):
#
#   sh scripts/smoke/check-results.sh RESULTS_FILE CHANNEL...
#
# A channel passes with "RESULT <channel> PASS ..." or is skipped with
# "RESULT <channel> SKIP <reason>" (the reason is required). Any FAIL line,
# or a missing, duplicated or reasonless line, fails the job, so a channel
# that silently did nothing cannot look green. Prints a table, also to
# $GITHUB_STEP_SUMMARY when set.
set -eu

file="${1:?usage: check-results.sh RESULTS_FILE CHANNEL...}"
shift
[ "$#" -gt 0 ] || {
	echo "check-results.sh: no channels given" >&2
	exit 2
}
[ -f "$file" ] || {
	echo "check-results.sh: no results file $file" >&2
	exit 1
}

status=0
table="| Channel | Result |
| --- | --- |"
for ch in "$@"; do
	n="$(grep -c "^RESULT $ch " "$file" || true)"
	line="$(grep "^RESULT $ch " "$file" | head -n 1 || true)"
	verdict="${line#"RESULT $ch "}"
	failed="$(grep "^RESULT $ch FAIL" "$file" | head -n 1 || true)"
	if [ -n "$failed" ]; then
		n=fail
		verdict="${failed#"RESULT $ch "}"
	fi
	case "$n" in
	fail) status=1 ;;
	1)
		case "$verdict" in
		PASS*) ;;
		"SKIP "?*) ;;
		*)
			verdict="INVALID: '$verdict' (want PASS, or SKIP with a reason)"
			status=1
			;;
		esac
		;;
	0)
		verdict="MISSING: the channel reported nothing"
		status=1
		;;
	*)
		verdict="INVALID: $n RESULT lines"
		status=1
		;;
	esac
	printf '%-16s %s\n' "$ch" "$verdict"
	table="$table
| \`$ch\` | $verdict |"
done
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
	printf '%s\n\n' "$table" >>"$GITHUB_STEP_SUMMARY"
fi
exit "$status"
