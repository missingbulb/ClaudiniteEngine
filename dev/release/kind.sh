#!/bin/sh
# The settings a release kind carries (dev/release/packages.go): its dist-tag,
# engine channel and platforms, printed as tag=, channel= and platforms=
# lines. Under Actions they are also the step's outputs, and PLATFORMS joins
# the job's environment for the steps after it.
#
#   dev/release/kind.sh full|staging
set -eu
[ $# -eq 1 ] || { echo "usage: dev/release/kind.sh full|staging" >&2; exit 2; }
cd "$(dirname "$0")/../.."
out=$(go run ./dev/release/pipeline release-kind --kind "$1")
printf '%s\n' "$out"
[ -z "${GITHUB_OUTPUT:-}" ] || printf '%s\n' "$out" >> "$GITHUB_OUTPUT"
[ -z "${GITHUB_ENV:-}" ] || printf '%s\n' "$out" | sed -n 's/^platforms=/PLATFORMS=/p' >> "$GITHUB_ENV"
