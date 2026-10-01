#!/bin/sh
# The hop test: the candidate in $DIST (default dist/) is v1, a second
# build of the same source at the next ordinal is v2, and the candidate's
# own cn update engine must move a member from v1 to v2 through a merged
# update PR, against regstub and release/ghstub: the rehearsal's update
# mode, steps 1 to 4. release.yml runs it before sign, so a candidate that
# cannot update away from itself is never published.
# usage: release/hop.sh
set -eu
cd "$(dirname "$0")/.."
DIST=${DIST:-dist}
out=$(mktemp)
trap 'rm -f "$out"' EXIT
status=0
DIST=$DIST UPDATE_STEPS=4 sh release/rehearse.sh --mode update > "$out" 2>&1 || status=$?
cat "$out"
[ "$status" -eq 0 ] || { echo "hop: FAIL: the candidate's updater did not move a member off it" >&2; exit 1; }
from=$(sed -n 's/^rehearse: update: a member on \(.*\) with a bare origin.*/\1/p' "$out")
to=$(sed -n 's/^rehearse: update 3: landed \(.*\)$/\1/p' "$out")
if [ -z "$from" ] || [ -z "$to" ]; then
  echo "hop: FAIL: the rehearsal did not report the hop" >&2
  exit 1
fi
echo "hop: $from updated a member to $to"
