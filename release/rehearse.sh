#!/bin/sh
# The local Phase 1 gate: the host's smoke leg (release/smoke-platform.sh)
# against the built release in $DIST (default dist/), served by regstub.
# Needs go, node, curl and a release from release/build.sh.
set -eu
cd "$(dirname "$0")/.."
root=$(pwd)
DIST=${DIST:-dist}
case $DIST in /*) ;; *) DIST=$root/$DIST ;; esac

fail() { echo "rehearse: FAIL: $*" >&2; exit 1; }
step() { echo "rehearse: $*"; }

[ -f "$DIST/manifest.integrity" ] || fail "no release in $DIST; run release/build.sh first"
DIST=$DIST sh release/smoke.sh
version=$(sed -n 's/^  "version": "\(.*\)",$/\1/p' "$DIST/manifest.json")
pin=$(cat "$DIST/manifest.integrity")
package=
for d in "$DIST"/npm/*/package; do
  [ -f "$d/manifest.json" ] && package=@claudinite/$(basename "$(dirname "$d")")
done
[ -n "$package" ] || fail "no npm package in $DIST carries manifest.json"

work=$(mktemp -d)
stub_pid=
cleanup() {
  [ -n "$stub_pid" ] && kill "$stub_pid" 2>/dev/null
  chmod -R u+w "$work" 2>/dev/null
  rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

go build -o "$work/regstub" ./release/regstub
"$work/regstub" --dist "$DIST" --ready "$work/ready" --ca-out "$work/ca.pem" --log "$work/requests.log" &
stub_pid=$!
tries=0
until [ -f "$work/ready" ]; do
  tries=$((tries + 1))
  [ "$tries" -le 100 ] || fail "regstub did not start"
  sleep 0.1
done
CURL_CA_BUNDLE=$work/ca.pem
NO_PROXY=127.0.0.1,localhost
export CURL_CA_BUNDLE NO_PROXY

step "fresh: the host's smoke leg"
sh release/smoke-platform.sh --registry "$(cat "$work/ready")" --package "$package" --version "$version" --pin "$pin" --dist "$DIST" \
  || fail "the smoke leg failed"

step "ok ($package $version, $pin)"
