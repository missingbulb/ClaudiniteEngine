#!/bin/sh
# Initial adoption on a clean repo, against the release in $DIST (default
# dist/) served by regstub on loopback: the linux-x64 leg of
# dev/release/verify/smoke-platform.sh, pinned to VERSION and the manifest's
# integrity on the engine channel CHANNEL. linux-x64 alone until there are
# paying customers (#47); a full release still publishes all five platforms.
#
#   dev/release/verify/adoption.sh --channel C --version V --pin SHA512
set -eu
cd "$(dirname "$0")/../../.."
usage() { echo "usage: dev/release/verify/adoption.sh --channel C --version V --pin SHA512" >&2; exit 2; }
channel='' version='' pin=''
while [ $# -gt 0 ]; do
  [ $# -ge 2 ] || usage
  case $1 in
    --channel) channel=$2 ;;
    --version) version=$2 ;;
    --pin) pin=$2 ;;
    *) usage ;;
  esac
  shift 2
done
if [ -z "$channel" ] || [ -z "$version" ] || [ -z "$pin" ]; then usage; fi
DIST=${DIST:-dist}

stub=$(mktemp -d)
go build -o "$stub/regstub" ./dev/release/verify/stubs/regstub
"$stub/regstub" --dist "$DIST" --ready "$stub/ready" --ca-out "$stub/ca.pem" &
pid=$!
trap 'kill "$pid" 2>/dev/null; rm -rf "$stub"' EXIT
for _ in $(seq 100); do [ -f "$stub/ready" ] && break; sleep 0.1; done
[ -f "$stub/ready" ] || { echo "::error::regstub did not start"; exit 1; }
CURL_CA_BUNDLE=$stub/ca.pem NO_PROXY=127.0.0.1,localhost \
  sh dev/release/verify/smoke-platform.sh --registry "$(cat "$stub/ready")" --channel "$channel" \
  --version "$version" --pin "$pin" --dist "$DIST" --platform linux-x64
