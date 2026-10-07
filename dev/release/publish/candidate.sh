#!/bin/sh
# Checks the published bytes of a version before latest moves onto them
# (dev/release/publish/promote check): downloads every CLI package's tarball
# of VERSION from npm, then verifies them against the roots this checkout
# trusts and the candidate's own source at SOURCE, a checkout of its tag. A
# staging build has no tarball on four platform packages; the check names it
# as one from its manifest. Prints check=, reason= and stable_test=, the
# step's outputs under Actions; a refusal is written to the step summary and
# exits 1.
#
#   dev/release/publish/candidate.sh VERSION SOURCE
set -eu
[ $# -eq 2 ] || { echo "usage: dev/release/publish/candidate.sh VERSION SOURCE" >&2; exit 2; }
cd "$(dirname "$0")/../../.."
version=$1
source=$(cd "$2" && pwd)
tarballs=$(mktemp -d)
trap 'rm -rf "$tarballs"' EXIT

for name in $(go run ./dev/release/pipeline names); do
  npm pack "$name@$version" --pack-destination "$tarballs" --silent \
    || echo "::notice::npm has no $name@$version"
done
for f in "$tarballs"/claudinite-*.tgz; do
  [ -e "$f" ] || continue
  mv "$f" "$tarballs/$(basename "$f" | sed 's/^claudinite-//')"
done
ls -l "$tarballs"

out=$(go run ./dev/release/publish/promote check --version "$version" --tarballs "$tarballs" --roots cn/shared/trust/roots --source "$source")
printf '%s\n' "$out"
[ -z "${GITHUB_OUTPUT:-}" ] || printf '%s\n' "$out" >> "$GITHUB_OUTPUT"
if [ "$(printf '%s\n' "$out" | sed -n 's/^check=//p')" != pass ]; then
  reason=$(printf '%s\n' "$out" | sed -n 's/^reason=//p')
  echo "### $version not promoted: $reason" >> "${GITHUB_STEP_SUMMARY:-/dev/stderr}"
  echo "::error::promote check refused $version: $reason"
  exit 1
fi
