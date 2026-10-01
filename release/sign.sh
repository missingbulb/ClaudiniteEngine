#!/bin/sh
# Signs the release in $DIST (default dist/) that release/build.sh wrote:
# writes manifest.sig.json, copies it into the channel's npm package beside
# manifest.json, re-packs that one tarball, rewrites SHA256SUMS, verifies
# the signature against license/roots and prints the integrity string,
# which signing leaves unchanged since the signature travels beside the
# manifest, never inside it.
#
# RELEASE_KEY and RELEASE_CERT name the release key and its certificate.
# Unset, they fall back to the development keys in $DEV_KEYS (default
# keys/dev/) with a warning; once #5 removes those, both are required.
set -eu
cd "$(dirname "$0")/.."
root=$(pwd)
DIST=${DIST:-dist}
case $DIST in /*) ;; *) DIST=$root/$DIST ;; esac
DEV_KEYS=${DEV_KEYS:-keys/dev}

if [ -z "${RELEASE_KEY:-}" ] || [ -z "${RELEASE_CERT:-}" ]; then
  if [ ! -f "$DEV_KEYS/release.key" ] || [ ! -f "$DEV_KEYS/release.cert.json" ]; then
    echo "sign: set RELEASE_KEY and RELEASE_CERT to the release key and its certificate; there are no development keys in $DEV_KEYS" >&2
    exit 1
  fi
  RELEASE_KEY=$DEV_KEYS/release.key
  RELEASE_CERT=$DEV_KEYS/release.cert.json
  echo "sign: warning: signing with the development release key in $DEV_KEYS, which every launcher accepts until #5 replaces the embedded roots" >&2
fi

[ -f "$DIST/manifest.json" ] || { echo "sign: no manifest.json in $DIST; run release/build.sh first" >&2; exit 1; }
pkg=
for d in "$DIST"/npm/*/package; do
  if [ -f "$d/manifest.json" ]; then
    [ -z "$pkg" ] || { echo "sign: more than one npm package in $DIST carries manifest.json" >&2; exit 1; }
    pkg=$d
  fi
done
[ -n "$pkg" ] || { echo "sign: no npm package in $DIST carries manifest.json" >&2; exit 1; }
cmp -s "$DIST/manifest.json" "$pkg/manifest.json" || { echo "sign: $pkg/manifest.json differs from $DIST/manifest.json" >&2; exit 1; }

tools=$(mktemp -d)
trap 'rm -rf "$tools"' EXIT
go build -o "$tools/manifest" ./release/manifest

"$tools/manifest" sign --dist "$DIST" --key "$RELEASE_KEY" --cert "$RELEASE_CERT"
cp "$DIST/manifest.sig.json" "$pkg/manifest.sig.json"
version=$(sed -n 's/^  "version": "\(.*\)",$/\1/p' "$DIST/manifest.json")
name=$(basename "$(dirname "$pkg")")
rm -f "$DIST/tarballs/$name-$version.tgz"
sh release/npmpack.sh "$pkg" "$DIST/tarballs"
"$tools/manifest" verify --dist "$DIST" --roots license/roots >&2
"$tools/manifest" sums --dist "$DIST"

integrity=$("$tools/manifest" integrity "$pkg/manifest.json")
echo "version $version"
echo "manifest $integrity"
