#!/bin/sh
# The release version, <day>.<n>.0, from the v* tags of the repository in
# the working directory.
#
#   release/version.sh next       print the next version for today (UTC)
#   release/version.sh check V    exit 0 if v<V> is free, 1 if it is taken;
#                                 a V below the engine floor is refused
#
# <day> comes from `cn version --day`, the one implementation of the format.
# check runs no Go: the floor is checksdk/engine_floor.txt, the file the
# SDK's EngineFloor is held equal to (`cn version --floor` prints the same).
set -eu
here=$(cd "$(dirname "$0")/.." && pwd)
fail() { echo "version: $*" >&2; exit 2; }

if [ "$(git rev-parse --is-shallow-repository)" = true ]; then
  fail "this is a shallow clone, whose tags are incomplete; check out with fetch-depth: 0"
fi

case ${1:-} in
  next)
    [ $# -eq 1 ] || fail "usage: release/version.sh next"
    day=$(cd "$here" && go run ./cmd/cn version --day)
    printf '%s\n' "$day" | grep -Eqx '[1-9][0-9]*' || fail "cn version --day printed $day"
    last=$(git tag --list "v$day.*.0" | sed -n "s/^v$day\.\([1-9][0-9]*\)\.0\$/\1/p" | sort -n | tail -n 1)
    echo "$day.$((${last:-0} + 1)).0"
    ;;
  check)
    [ $# -eq 2 ] || fail "usage: release/version.sh check VERSION"
    printf '%s\n' "$2" | grep -Eqx '[1-9][0-9]*\.[1-9][0-9]*\.0' || fail "$2 is not a <day>.<n>.0 version"
    floor=$(cat "$here/checksdk/engine_floor.txt")
    printf '%s\n' "$floor" | grep -Eqx '[1-9][0-9]*\.[1-9][0-9]*\.0' || fail "checksdk/engine_floor.txt holds $floor"
    if [ "${2%%.*}" -lt "${floor%%.*}" ] || { [ "${2%%.*}" -eq "${floor%%.*}" ] && [ "$(echo "$2" | cut -d. -f2)" -lt "$(echo "$floor" | cut -d. -f2)" ]; }; then
      fail "$2 is below the engine floor $floor, the first engine the SDK's checks may need"
    fi
    if [ -n "$(git tag --list "v$2")" ]; then
      echo "version: v$2 is already tagged" >&2
      exit 1
    fi
    ;;
  *) fail "usage: release/version.sh next | check VERSION" ;;
esac
