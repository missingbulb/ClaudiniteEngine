#!/bin/sh
# Tags the commit being released v<VERSION> and pushes the tag. The publish
# job runs it before npm, so a run that fails once npm took the version
# leaves the next run a fresh one.
#
#   dev/release/publish/tag.sh VERSION   (the commit: GITHUB_SHA, else HEAD)
set -eu
[ $# -eq 1 ] || { echo "usage: dev/release/publish/tag.sh VERSION" >&2; exit 2; }
cd "$(dirname "$0")/../../.."
git tag "v$1" "${GITHUB_SHA:-HEAD}"
git push origin "v$1"
