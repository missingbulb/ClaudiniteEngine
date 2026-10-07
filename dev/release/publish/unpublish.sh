#!/bin/sh
# Removes a version from @claudinite/cli and its platform packages with npm
# unpublish (promote.yml's unpublish job), on NPM_DEPRECATE_TOKEN as
# dev/release/publish/deprecate.sh is. CONFIRM must repeat the version, since
# an unpublish cannot be undone. It refuses, failing before any unpublish,
# when npm cannot be read, the version is the one latest points at, or it is
# the only one any of those packages holds, since npm deletes a package whose
# last version is unpublished (dev/release/publish/unpublish.go). A version
# npm does not have is a notice and a success. The commands stop at the first
# that fails.
#
#   dev/release/publish/unpublish.sh VERSION CONFIRM
set -eu
[ $# -eq 2 ] || { echo "usage: dev/release/publish/unpublish.sh VERSION CONFIRM" >&2; exit 2; }
cd "$(dirname "$0")/../../.."
version=$1 confirm=$2
summary=${GITHUB_STEP_SUMMARY:-/dev/stdout}
if [ "$confirm" != "$version" ]; then
  echo "::error::unpublish cannot be undone; dispatch again with confirm set to the version, $version"
  echo "### unpublish $version: refused, confirm does not repeat the version; nothing unpublished" >> "$summary"
  exit 1
fi
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

if ! npm view @claudinite/cli dist-tags --json > "$work/dist-tags.json" 2> "$work/view.err"; then
  echo "::error::npm view @claudinite/cli dist-tags failed, so whether $version is latest is unknown; nothing unpublished: $(cat "$work/dist-tags.json" "$work/view.err" | tr '\n' ' ')"
  exit 1
fi
if ! sh dev/release/publish/npm-versions.sh "$work/versions" 2> "$work/view.err"; then
  echo "::error::$(cat "$work/view.err"), so whether $version is its only version is unknown; nothing unpublished"
  exit 1
fi
if ! go run ./dev/release/pipeline unpublish-commands --version "$version" --versions-dir "$work/versions" \
  --dist-tags "$work/dist-tags.json" > "$work/commands.sh"; then
  echo "### unpublish $version: refused; nothing unpublished (the error is in this job's log)" >> "$summary"
  exit 1
fi
if [ ! -s "$work/commands.sh" ]; then
  echo "### unpublish $version: npm has no such version; nothing done" >> "$summary"
  exit 0
fi
{
  echo "### unpublish $version"
  echo
  echo '```'
  cat "$work/commands.sh"
  echo '```'
} >> "$summary"
if [ -z "${NPM_DEPRECATE_TOKEN:-}" ]; then
  echo "::error::the promote environment has no NPM_DEPRECATE_TOKEN secret, an npm token with write access to @claudinite; add it and dispatch again (the commands are in this job's summary)"
  exit 1
fi
NODE_AUTH_TOKEN=$NPM_DEPRECATE_TOKEN sh -e "$work/commands.sh"
echo "versions now: $(npm view @claudinite/cli versions --json | tr -d '\n ')" >> "$summary"
