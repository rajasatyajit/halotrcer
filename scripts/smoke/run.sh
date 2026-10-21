#!/bin/sh
# Runs one smoke channel and never stops the job (PLAN.md M4):
#
#   sh scripts/smoke/run.sh CHANNEL COMMAND [ARG...]
#
# The channel writes its own RESULT line to $SMOKE_RESULTS. A channel that
# wrote none (it died first, or exited 0 without one), or exited non-zero
# without a FAIL, gets a FAIL line here: the RESULT line decides, never the
# exit code alone. Always exits 0, so every channel in the job runs;
# scripts/smoke/check-results.sh alone decides the job.
set -u
channel="${1:?usage: run.sh CHANNEL COMMAND [ARG...]}"
shift
: "${SMOKE_RESULTS:?set SMOKE_RESULTS}"
touch "$SMOKE_RESULTS"
echo "::group::$channel"
"$@"
code=$?
echo "::endgroup::"
failed=0
[ "$code" -eq 0 ] || failed=1
if ! grep -q "^RESULT $channel " "$SMOKE_RESULTS"; then
	printf 'RESULT %s FAIL exited %s without reporting a result\n' "$channel" "$code" >>"$SMOKE_RESULTS"
	failed=1
elif [ "$code" -ne 0 ] && ! grep -q "^RESULT $channel FAIL" "$SMOKE_RESULTS"; then
	printf 'RESULT %s FAIL exited %s after reporting a result\n' "$channel" "$code" >>"$SMOKE_RESULTS"
fi
if [ "$failed" -eq 1 ]; then
	echo "::error::smoke channel $channel failed (exit $code); the job's last step reports it"
fi
exit 0
