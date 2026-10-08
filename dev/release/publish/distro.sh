#!/bin/sh
# Uploads the staging build in $DIST (default dist/) to the GitHub
# repository dev/release/publish/staging-distro names, as release v<VERSION>:
# the manifest tarball and the platform tarball exactly as npm takes them,
# and release.json naming the version, its pin and the commit, marked the
# latest release. A member whose engine.releases names that repository
# fetches the build as soon as the upload ends, minutes before npm serves
# it. The publish job runs it after the tag and before npm, with GH_TOKEN
# holding contents write on that repository.
#
#   dev/release/publish/distro.sh --version V --integrity PIN --commit SHA [--dry-run]
#
# --dry-run prints the upload line and release.json, and uploads nothing.
set -eu
cd "$(dirname "$0")/../../.."
DIST=${DIST:-dist}
fail() { echo "distro: $*" >&2; exit 1; }

version='' integrity='' commit='' dry=false
while [ $# -gt 0 ]; do
  case $1 in
    --version|--integrity|--commit)
      [ $# -ge 2 ] || fail "$1 needs a value"
      case $1 in
        --version) version=$2 ;;
        --integrity) integrity=$2 ;;
        --commit) commit=$2 ;;
      esac
      shift 2 ;;
    --dry-run) dry=true; shift ;;
    *) fail "usage: dev/release/publish/distro.sh --version V --integrity PIN --commit SHA [--dry-run]" ;;
  esac
done
if [ -z "$version" ] || [ -z "$integrity" ] || [ -z "$commit" ]; then
  fail "--version, --integrity and --commit are required"
fi

repo=$(tr -d ' \r\n' < dev/release/publish/staging-distro)
printf '%s\n' "$repo" | grep -Eqx '[A-Za-z0-9-]+/[A-Za-z0-9_-][A-Za-z0-9._-]*' \
  || fail "dev/release/publish/staging-distro must hold one owner/name, not \"$repo\""

files=''
for f in "cli-$version.tgz" "cli-linux-x64-$version.tgz"; do
  [ -f "$DIST/tarballs/$f" ] || fail "$DIST/tarballs holds no $f"
  files="$files $DIST/tarballs/$f"
done
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
go run ./dev/release/pipeline release-json --version "$version" --manifest "$integrity" --commit "$commit" > "$work/release.json" \
  || fail "the pipeline refused release.json for --version $version --manifest $integrity --commit $commit"

if [ "$dry" = true ]; then
  # shellcheck disable=SC2086 # $files is paths without spaces, one argument each
  echo "dry-run: gh release create v$version --repo $repo --latest" $files release.json
  cat "$work/release.json"
  exit 0
fi
[ -n "${GH_TOKEN:-}" ] || fail "the release environment must hold STAGING_DISTRO_TOKEN, a token with contents write on $repo"
# shellcheck disable=SC2086 # $files is paths without spaces, one argument each
gh release create "v$version" --repo "$repo" --title "$version" --latest \
  --notes "Staging build $version of the Claudinite engine, from commit $commit. Its pin: $integrity" \
  $files "$work/release.json"
echo "distro: https://github.com/$repo/releases/tag/v$version"
