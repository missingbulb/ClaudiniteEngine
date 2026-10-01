#!/bin/sh
# Builds a release into $DIST (default dist/): the five binaries, a signed
# manifest.json, the npm package folders and their tarballs, then prints the
# manifest's integrity string, the value a member pins.
#
# VERSION (default 0.0.0). RELEASE_KEY and RELEASE_CERT name the release key
# and its certificate; unset, they fall back to the development keys in
# $DEV_KEYS (default keys/dev/), and once #5 removes those both are required.
set -eu
cd "$(dirname "$0")/.."
root=$(pwd)

VERSION=${VERSION:-0.0.0}
DIST=${DIST:-dist}
DEV_KEYS=${DEV_KEYS:-keys/dev}
if [ -z "${RELEASE_KEY:-}" ] || [ -z "${RELEASE_CERT:-}" ]; then
  if [ ! -f "$DEV_KEYS/release.key" ] || [ ! -f "$DEV_KEYS/release.cert.json" ]; then
    echo "build: set RELEASE_KEY and RELEASE_CERT to the release key and its certificate; there are no development keys in $DEV_KEYS" >&2
    exit 1
  fi
  RELEASE_KEY=$DEV_KEYS/release.key
  RELEASE_CERT=$DEV_KEYS/release.cert.json
fi
COMMIT=$(git rev-parse --short=7 HEAD 2>/dev/null || echo unknown)
export VERSION COMMIT
platforms="linux-x64 linux-arm64 darwin-x64 darwin-arm64 windows-x64"

case $DIST in /*) ;; *) DIST=$root/$DIST ;; esac
rm -rf "$DIST"
mkdir -p "$DIST/bin" "$DIST/npm" "$DIST/tarballs"

tools=$(mktemp -d)
trap 'rm -rf "$tools"' EXIT
go build -o "$tools/manifest" ./release/manifest

for p in $platforms; do
  bin=cn
  [ "$p" = windows-x64 ] && bin=cn.exe
  mkdir -p "$DIST/bin/$p"
  sh release/gobuild.sh "$p" "$DIST/bin/$p/$bin"
done

"$tools/manifest" write --dist "$DIST" --version "$VERSION" --commit "$COMMIT"
"$tools/manifest" sign --dist "$DIST" --key "$RELEASE_KEY" --cert "$RELEASE_CERT"
"$tools/manifest" verify --dist "$DIST" --roots license/roots >&2

pkgjson() {
  # name, extra fields
  printf '{\n  "name": "%s",\n  "version": "%s",\n  "description": "Claudinite engine %s",\n  "license": "UNLICENSED",\n  "repository": "github:missingbulb/ClaudiniteEngine"%s\n}\n' \
    "$1" "$VERSION" "$2" "$3"
}

cli=$DIST/npm/cli/package
mkdir -p "$cli"
cp "$DIST/manifest.json" "$DIST/manifest.sig.json" "$cli/"
pkgjson "@claudinite/cli" "release manifest" "" > "$cli/package.json"

for p in $platforms; do
  bin=cn
  [ "$p" = windows-x64 ] && bin=cn.exe
  os=${p%-*}
  [ "$os" = windows ] && os=win32
  cpu=${p#*-}
  dir=$DIST/npm/cli-$p/package
  mkdir -p "$dir/bin"
  cp "$DIST/bin/$p/$bin" "$dir/bin/$bin"
  pkgjson "@claudinite/cli-$p" "binary for $p" ",
  \"os\": [\"$os\"],
  \"cpu\": [\"$cpu\"]" > "$dir/package.json"
done

for d in "$DIST"/npm/*/package; do
  name=$(basename "$(dirname "$d")")
  (cd "$d" && npm pack --silent --pack-destination "$DIST/tarballs" >/dev/null)
  mv "$DIST/tarballs/claudinite-$name-$VERSION.tgz" "$DIST/tarballs/$name-$VERSION.tgz"
done

integrity=$("$tools/manifest" integrity "$cli/manifest.json")
printf '%s\n' "$integrity" > "$DIST/manifest.integrity"
echo "version $VERSION"
echo "manifest $integrity"
