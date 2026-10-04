#!/bin/sh
# The release version, <major>.<day>.<n>, from the v* tags of the repository
# in the working directory.
#
#   release/version.sh next       print the next version for today (UTC)
#   release/version.sh check V    exit 0 if v<V> is free, 1 if it is taken;
#                                 a V below the engine floor is refused
#
# <major> is release/major, the one number a person raises by hand. <day>
# comes from `cn version --day`, the one implementation of the format, and
# <n> counts today's builds of this major from 1. check runs no Go: the
# floor is checksdk/engine_floor.txt, the file the SDK's EngineFloor is held
# equal to (`cn version --floor` prints the same).
set -eu
here=$(cd "$(dirname "$0")/.." && pwd)
fail() { echo "version: $*" >&2; exit 2; }

day_re='([1-9][0-9]*(0[1-9]|1[0-2])|[1-9]|1[0-2])(0[1-9]|[12][0-9]|3[01])'
version_re="(0|[1-9][0-9]*)\.$day_re\.[1-9][0-9]*"
shaped() { printf '%s\n' "$1" | grep -Eqx "$version_re"; }
part() { echo "$1" | cut -d. -f"$2"; }

if [ "$(git rev-parse --is-shallow-repository)" = true ]; then
  fail "this is a shallow clone, whose tags are incomplete; check out with fetch-depth: 0"
fi

case ${1:-} in
  next)
    [ $# -eq 1 ] || fail "usage: release/version.sh next"
    major=$(cat "$here/release/major")
    printf '%s\n' "$major" | grep -Eqx '0|[1-9][0-9]*' || fail "release/major holds $major, not one number"
    day=$(cd "$here" && go run ./cmd/cn version --day)
    printf '%s\n' "$day" | grep -Eqx '[1-9][0-9]*' || fail "cn version --day printed $day"
    last=$(git tag --list "v$major.$day.*" | sed -n "s/^v$major\.$day\.\([1-9][0-9]*\)\$/\1/p" | sort -n | tail -n 1)
    echo "$major.$day.$((${last:-0} + 1))"
    ;;
  check)
    [ $# -eq 2 ] || fail "usage: release/version.sh check VERSION"
    shaped "$2" || fail "$2 is not a <major>.<day>.<n> version"
    floor=$(cat "$here/checksdk/engine_floor.txt")
    shaped "$floor" || fail "checksdk/engine_floor.txt holds $floor"
    below=false
    for i in 1 2 3; do
      if [ "$(part "$2" $i)" -ne "$(part "$floor" $i)" ]; then
        [ "$(part "$2" $i)" -lt "$(part "$floor" $i)" ] && below=true
        break
      fi
    done
    if [ "$below" = true ]; then
      fail "$2 is below the engine floor $floor, the first engine the SDK's checks may need"
    fi
    if [ -n "$(git tag --list "v$2")" ]; then
      echo "version: v$2 is already tagged" >&2
      exit 1
    fi
    ;;
  *) fail "usage: release/version.sh next | check VERSION" ;;
esac
