#!/bin/sh
# Holds, revokes or releases a version on npm (promote.yml's deprecate job):
# marks it with a "held:" or "revoked:" deprecation, which the updater and
# cn check world read, or clears it, on every CLI package holding it
# (dev/release/publish/deprecate.go). npm's trusted publishing covers publish
# and dist-tag only, not deprecate (https://docs.npmjs.com/trusted-publishers),
# so the commands run on NPM_DEPRECATE_TOKEN, a token the promote environment
# holds; without it the run fails naming the secret, the commands kept in the
# step summary. A version npm does not have is a notice and a success. The
# commands stop at the first that fails.
#
#   dev/release/publish/deprecate.sh hold|revoke|release VERSION [REASON]
set -eu
[ $# -ge 2 ] && [ $# -le 3 ] || { echo "usage: dev/release/publish/deprecate.sh hold|revoke|release VERSION [REASON]" >&2; exit 2; }
cd "$(dirname "$0")/../../.."
action=$1 version=$2 reason=${3:-}
summary=${GITHUB_STEP_SUMMARY:-/dev/stdout}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

if ! sh dev/release/publish/npm-versions.sh "$work/versions" 2> "$work/view.err"; then
  echo "::error::$(cat "$work/view.err"), so which packages hold $version is unknown; nothing deprecated"
  exit 1
fi
go run ./dev/release/pipeline deprecate-commands --action "$action" --version "$version" --reason "$reason" \
  --versions-dir "$work/versions" > "$work/commands.sh"
if [ ! -s "$work/commands.sh" ]; then
  echo "### $action $version: npm has no such version; nothing done" >> "$summary"
  exit 0
fi
{
  echo "### $action $version"
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
echo "deprecated now: $(npm view "@claudinite/cli@$version" deprecated)" >> "$summary"
