#!/bin/sh
# A local pack source for the rehearsal: the hello pack (release/testdata/
# hello) on the vendored branch of two bare repositories in DIR, cdn.git
# (which release/cdnstub serves at the CDN's paths) and mirror.git (which
# CLAUDINITE_PACKS_REPO names), laid out and signed as ClaudinitePacks'
# branch is. The first call issues a packs key certified by the
# development root (keys/dev/root.key) and publishes v1; later calls apply
# their operations in order, commit once and push.
#
#   release/packs-fixture.sh DIR --min-engine VERSION      first call: v1
#   release/packs-fixture.sh DIR --publish vN | --revoke vN ...
#   release/packs-fixture.sh DIR --serial N --mirror-only  rewrite the serial, mirror only
#   release/packs-fixture.sh DIR --flip-sig                flip one byte of index.sig.json
#
# release/packfixture names what each label publishes.
set -eu
here=$(cd "$(dirname "$0")/.." && pwd)
fail() { echo "packs-fixture: $*" >&2; exit 1; }
[ $# -ge 1 ] || fail "usage: release/packs-fixture.sh DIR [--min-engine V] [--publish vN] [--revoke vN] [--serial N] [--mirror-only] [--flip-sig]"
mkdir -p "$1"
dir=$(cd "$1" && pwd)
shift
tools=$dir/tools
tree=$dir/tree
g() { git -C "$tree" -c user.name=packs-fixture -c user.email=p@x -c commit.gpgsign=false -c push.negotiate=false "$@"; }

if [ ! -d "$dir/cdn.git" ]; then
  if [ "${1:-}" != --min-engine ] || [ -z "${2:-}" ]; then fail "the first call takes --min-engine VERSION"; fi
  mkdir -p "$tools" "$dir/keys"
  (cd "$here" && go build -o "$tools/cn-keys" ./cmd/cn-keys && go build -o "$tools/packfixture" ./release/packfixture) \
    || fail "building the tools"
  "$tools/cn-keys" key new --out "$dir/keys" --name packs > /dev/null
  "$tools/cn-keys" certify --root "$here/keys/dev/root.key" --subject "$dir/keys/packs.pub" --use packs --days 90 \
    --out "$dir/keys/packs.cert.json" > /dev/null
  printf '%s\n' "$2" > "$dir/min-engine"
  shift 2
  git init -q --bare -b vendored "$dir/cdn.git"
  git init -q --bare -b vendored "$dir/mirror.git"
  git init -q -b vendored "$tree"
  set -- --publish v1 "$@"
fi

key="--key $dir/keys/packs.key --cert $dir/keys/packs.cert.json"
remotes="$dir/cdn.git $dir/mirror.git"
msg=
while [ $# -gt 0 ]; do
  case $1 in
    --publish|--revoke)
      [ -n "${2:-}" ] || fail "$1 takes a label"
      # shellcheck disable=SC2086 # $key is two flag pairs
      "$tools/packfixture" --tree "$tree" --src "$here/release/testdata/hello" --min-engine "$(cat "$dir/min-engine")" $key "$1" "$2" \
        || fail "$1 $2"
      msg="$msg $1 $2"
      shift 2 ;;
    --serial)
      [ -n "${2:-}" ] || fail "--serial takes a number"
      # shellcheck disable=SC2086
      "$tools/packfixture" --tree "$tree" $key --serial "$2" || fail "--serial $2"
      msg="$msg serial $2"
      shift 2 ;;
    --flip-sig)
      "$tools/packfixture" --tree "$tree" --flip-sig || fail "--flip-sig"
      msg="$msg flip-sig"
      shift ;;
    --mirror-only)
      remotes=$dir/mirror.git
      shift ;;
    *) fail "unknown argument $1" ;;
  esac
done
g add -A
g commit -q -m "packs-fixture:$msg"
for r in $remotes; do
  g push -q --force "$r" vendored
done
