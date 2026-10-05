#!/bin/sh
# The mirror: a public repository whose release v<version> holds a fresh
# release's tarballs, under the file names npm gives them, for the minutes
# after a publish when npm lists the version but answers 404 for its
# tarballs. The launcher and cn update read it then (npmreg.DefaultMirror);
# what they read there is checked exactly as npm's tarballs are.
#
#   release/mirror.sh put VERSION DIST   upload DIST/tarballs/*-VERSION.tgz as
#                                        release vVERSION, then drop every
#                                        release older than a day
#   release/mirror.sh drop VERSION       drop release vVERSION, once npm
#                                        serves it
#
# GH_TOKEN must be able to write the mirror's releases: the MIRROR_TOKEN
# secret.
set -eu

repo=missingbulb/ClaudiniteMirror
fail() { echo "mirror: $*" >&2; exit 1; }

[ -n "${GH_TOKEN:-}" ] || fail "GH_TOKEN is empty: the MIRROR_TOKEN secret, which writes $repo's releases, is missing"

drop() {
  out=$(gh release delete "v$1" --repo "$repo" --cleanup-tag --yes 2>&1) && return 0
  case $out in
    *"release not found"*) echo "mirror: v$1 is already gone" ;;
    *) fail "dropping v$1: $out" ;;
  esac
}

case ${1:-} in
  put)
    [ $# -eq 3 ] || fail "usage: release/mirror.sh put VERSION DIST"
    version=$2
    dist=$3
    set -- "$dist"/tarballs/*-"$version".tgz
    [ -f "$1" ] || fail "no tarballs of $version in $dist/tarballs"
    gh release create "v$version" --repo "$repo" --title "$version" \
      --notes "Kept until npm serves $version's tarballs. Checked as npm's are: the manifest against each member's pin, the binary against the manifest." \
      "$@"
    cutoff=$(date -u -d '1 day ago' +%Y-%m-%dT%H:%M:%SZ)
    for old in $(gh release list --repo "$repo" --limit 100 --json tagName,createdAt \
      --jq ".[] | select(.createdAt < \"$cutoff\") | .tagName"); do
      [ "$old" = "v$version" ] || drop "${old#v}"
    done
    ;;
  drop)
    [ $# -eq 2 ] || fail "usage: release/mirror.sh drop VERSION"
    drop "$2"
    ;;
  *) fail "usage: release/mirror.sh put VERSION DIST | drop VERSION" ;;
esac
