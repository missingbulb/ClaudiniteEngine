#!/bin/sh
# Moves @claudinite/cli's latest dist-tag onto VERSION and republishes
# nothing (promote.yml's promote job). npm's trusted publishing authorizes
# dist-tag for the promote environment's publisher of @claudinite/cli. Fails
# when npm refuses, or accepts while latest reads anything but VERSION after.
#
#   dev/release/publish/latest.sh VERSION
set -eu
[ $# -eq 1 ] || { echo "usage: dev/release/publish/latest.sh VERSION" >&2; exit 2; }
version=$1
before=$(npm view @claudinite/cli dist-tags.latest)
if ! npm_config_loglevel=verbose npm dist-tag add "@claudinite/cli@$version" latest; then
  echo "::error::npm refused to move latest onto $version; check the promote.yml trusted publisher of @claudinite/cli and its Allow npm dist-tag box on https://github.com/missingbulb/ClaudiniteEngine/issues/2"
  exit 1
fi
after=$(npm view @claudinite/cli dist-tags.latest)
echo "### latest: $before -> $after" >> "${GITHUB_STEP_SUMMARY:-/dev/stdout}"
if [ "$after" != "$version" ]; then
  echo "::error::npm accepted the dist-tag, but latest reads $after, not $version"
  exit 1
fi
