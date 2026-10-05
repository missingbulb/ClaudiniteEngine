#!/bin/sh
# The release version, <major>.<day>.<n>, from the v* tags of the repository
# in the working directory.
#
#   release/version.sh next [--taken FILE]
#                                 print the next version for today (UTC),
#                                 past any FILE names (npm view's versions
#                                 JSON), since npm can hold a version its
#                                 run failed to tag
#   release/version.sh check V    exit 0 if v<V> is free, 1 if it is taken;
#                                 a V below the engine floor is refused
#   release/version.sh day [DATE] print the <day> of DATE (YYYY-MM-DD,
#                                 default today, UTC)
#
# <major> is release/major, the one number a person raises by hand. <day>
# is (year - 2020)*10000 + month*100 + day, as shared/version's Today
# computes it; TestVersionDayIsToday holds the two equal. <n> counts today's
# builds of this major from 1. Nothing here runs Go: the floor is
# checksdk/engine_floor.txt, the file the SDK's EngineFloor is held equal to
# (`cn version --floor` prints the same).
set -eu
here=$(cd "$(dirname "$0")/.." && pwd)
fail() { echo "version: $*" >&2; exit 2; }

day_re='([1-9][0-9]*(0[1-9]|1[0-2])|[1-9]|1[0-2])(0[1-9]|[12][0-9]|3[01])'
version_re="(0|[1-9][0-9]*)\.$day_re\.[1-9][0-9]*"
shaped() { printf '%s\n' "$1" | grep -Eqx "$version_re"; }
part() { echo "$1" | cut -d. -f"$2"; }

# day DATE: the <day> of DATE, YYYY-MM-DD. Each part loses its leading
# zero first, since shell arithmetic reads 08 as octal.
day() {
  printf '%s\n' "$1" | grep -Eqx '20[2-9][0-9]-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])' || fail "$1 is not a YYYY-MM-DD date from 2020 on"
  y=$(echo "$1" | cut -d- -f1)
  m=$(echo "$1" | cut -d- -f2 | sed 's/^0//')
  d=$(echo "$1" | cut -d- -f3 | sed 's/^0//')
  echo $(((y - 2020) * 10000 + m * 100 + d))
}

if [ "${1:-}" = day ]; then
  [ $# -le 2 ] || fail "usage: release/version.sh day [YYYY-MM-DD]"
  day "${2:-$(date -u +%Y-%m-%d)}"
  exit 0
fi

if [ "$(git rev-parse --is-shallow-repository)" = true ]; then
  fail "this is a shallow clone, whose tags are incomplete; check out with fetch-depth: 0"
fi

case ${1:-} in
  next)
    taken=
    if [ $# -eq 3 ] && [ "$2" = --taken ]; then
      [ -f "$3" ] || fail "--taken $3 is not a file"
      taken=$3
    elif [ $# -ne 1 ]; then
      fail "usage: release/version.sh next [--taken FILE]"
    fi
    major=$(cat "$here/release/major")
    printf '%s\n' "$major" | grep -Eqx '0|[1-9][0-9]*' || fail "release/major holds $major, not one number"
    day=$(day "$(date -u +%Y-%m-%d)")
    last=$(git tag --list "v$major.$day.*" | sed -n "s/^v$major\.$day\.\([1-9][0-9]*\)\$/\1/p" | sort -n | tail -n 1)
    n=$((${last:-0} + 1))
    if [ -n "$taken" ]; then
      while grep -Fq "\"$major.$day.$n\"" "$taken"; do n=$((n + 1)); done
    fi
    echo "$major.$day.$n"
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
  *) fail "usage: release/version.sh next [--taken FILE] | check VERSION | day [YYYY-MM-DD]" ;;
esac
