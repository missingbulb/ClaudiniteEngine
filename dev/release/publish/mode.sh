#!/bin/sh
# Whether this run's publish is real, a dry run, or refused
# (dev/release/publish/publishmode.go), from the dist-tag, the key that
# signed, the dispatch's dry_run input and the versions npm holds of
# @claudinite/cli. Prints mode=, the step's output under Actions; the reason
# goes to stderr as a workflow notice.
#
#   dev/release/publish/mode.sh --tag rc|staging --signing KEY --dry-run true|false
set -eu
cd "$(dirname "$0")/../../.."
versions=$(mktemp)
trap 'rm -f "$versions"' EXIT
npm view @claudinite/cli versions --json > "$versions" 2>/dev/null || true
out=$(go run ./dev/release/pipeline publish-mode "$@" --npm-versions "$versions")
printf '%s\n' "$out"
[ -z "${GITHUB_OUTPUT:-}" ] || printf '%s\n' "$out" >> "$GITHUB_OUTPUT"
