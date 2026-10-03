#!/bin/sh
# The hop test: the candidate in $DIST (default dist/) is v1, a second
# build of the same source at the next ordinal is v2, and the candidate's
# own cn update engine must move a member from v1 to v2 through a merged
# update PR, against regstub and release/ghstub: the rehearsal's update
# mode, steps 1 to 4. release.yml runs it before sign, so a candidate that
# cannot update away from itself is never published.
#
# A release candidate trusts only the ceremony's roots, to which no key the
# rehearsal holds chains, so when $DIST does not trust the development root
# the hop runs on the candidate's source rebuilt at its version and package
# with REHEARSAL=1 BUILD_TAGS=devroots, which differs from it only in the
# roots it embeds.
# usage: release/hop.sh
set -eu
cd "$(dirname "$0")/.."
DIST=${DIST:-dist}
twin=
out=$(mktemp)
trap 'rm -f "$out"; [ -z "$twin" ] || rm -rf "$twin"' EXIT
devroot=$(cat license/devroots/root.pub)
if ! grep -qF "$devroot" "$DIST/bin/linux-x64/cn"; then
  twin=$(mktemp -d)
  version=$(sed -n 's/^  "version": "\(.*\)",$/\1/p' "$DIST/manifest.json")
  package=
  for d in "$DIST"/npm/*/package; do
    [ -f "$d/manifest.json" ] && package=@claudinite/$(basename "$(dirname "$d")")
  done
  if [ -z "$version" ] || [ -z "$package" ]; then echo "hop: FAIL: no version or package in $DIST" >&2; exit 1; fi
  echo "hop: rebuilding $version ($package) with the development roots"
  DIST=$twin/dist VERSION=$version PACKAGE=$package REHEARSAL=1 BUILD_TAGS=devroots sh release/build.sh > "$twin/build.out" 2>&1 \
    || { cat "$twin/build.out" >&2; echo "hop: FAIL: the development-roots build of $version" >&2; exit 1; }
  DIST=$twin/dist
fi
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
