#!/bin/sh
# Checks a release in $DIST (default dist/) is the one built: every file
# against its SHA256SUMS, and, given the hash dev/release/create/dist-id.sh
# printed for that SHA256SUMS in the build job, the list itself.
#
#   dev/release/verify/sums.sh [SUMS_SHA256]
set -eu
[ $# -le 1 ] || { echo "usage: dev/release/verify/sums.sh [SUMS_SHA256]" >&2; exit 2; }
cd "$(dirname "$0")/../../.."
DIST=${DIST:-dist}
cd "$(dirname "$DIST")"
dist=$(basename "$DIST")
[ $# -eq 0 ] || echo "$1  $dist/SHA256SUMS" | sha256sum -c
sha256sum -c --quiet "$dist/SHA256SUMS"
