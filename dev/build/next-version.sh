#!/bin/sh
# The next release version for today (dev/build/version.sh next), past every
# version npm already holds of @claudinite/cli as well as every v* tag, since
# npm can hold a version its run failed to tag. Prints it; under Actions it is
# also the step's version output and VERSION in the job's environment.
#
#   dev/build/next-version.sh   (needs the repository's tags: fetch-depth 0)
set -eu
[ $# -eq 0 ] || { echo "usage: dev/build/next-version.sh" >&2; exit 2; }
cd "$(dirname "$0")/../.."
taken=$(mktemp)
trap 'rm -f "$taken"' EXIT
npm view @claudinite/cli versions --json > "$taken" 2>/dev/null || true
version=$(sh dev/build/version.sh next --taken "$taken")
echo "$version"
[ -z "${GITHUB_OUTPUT:-}" ] || echo "version=$version" >> "$GITHUB_OUTPUT"
[ -z "${GITHUB_ENV:-}" ] || echo "VERSION=$version" >> "$GITHUB_ENV"
