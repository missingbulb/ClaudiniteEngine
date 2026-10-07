#!/bin/sh
# Whether a promotion may go on, from the gate's verdict
# (dev/release/publish/promote gate): pass goes on; no-canaries goes on only
# when the dispatch's confirm repeats the version; anything else stops. Prints
# allowed=true|false, the step's output under Actions, and writes the verdict,
# the advisory hop and the answer to the step summary.
#
#   dev/release/publish/allowed.sh VERSION CONFIRM VERDICT HOP
set -eu
[ $# -eq 4 ] || { echo "usage: dev/release/publish/allowed.sh VERSION CONFIRM VERDICT HOP" >&2; exit 2; }
version=$1 confirm=$2 verdict=$3 hop=$4
allowed=false
case $verdict in
  pass) allowed=true; why="every gate passed" ;;
  no-canaries)
    if [ "$confirm" = "$version" ]; then
      allowed=true; why="no canary is registered, and the dispatch typed the version twice"
    else
      why="no canary is registered; dispatch again with confirm set to the version to promote anyway"
    fi ;;
  *) why="the gate answered $verdict" ;;
esac
{
  echo "### Promotion of @claudinite/cli $version"
  echo
  echo "- gate: \`$verdict\` ($why)"
  echo "- hop: \`$hop\` (advisory until a canary runs candidates; release.yml's hop job proved this candidate's updater before it was published)"
  echo "- promoting: $allowed"
} >> "${GITHUB_STEP_SUMMARY:-/dev/stderr}"
echo "::notice::gate $verdict, $hop: $why"
echo "allowed=$allowed"
[ -z "${GITHUB_OUTPUT:-}" ] || echo "allowed=$allowed" >> "$GITHUB_OUTPUT"
