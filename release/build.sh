#!/bin/sh
# Builds a release into $DIST (default dist/): the five binaries, an
# UNSIGNED manifest.json, the npm package folders of $PACKAGE and their
# tarballs, and SHA256SUMS over all of it, then prints the manifest's
# integrity string, the value a member pins. release/sign.sh signs it.
#
# VERSION (default 0.0.0); PACKAGE @claudinite/cli (default) or
# @claudinite/cli-rc. --placeholder-sdk adds the @claudinite/sdk placeholder
# package, at 0.0.0 only, for npm-bootstrap.yml.
set -eu
cd "$(dirname "$0")/.."
root=$(pwd)

VERSION=${VERSION:-0.0.0}
DIST=${DIST:-dist}
PACKAGE=${PACKAGE:-@claudinite/cli}
case $PACKAGE in
  @claudinite/cli|@claudinite/cli-rc) ;;
  *) echo "build: PACKAGE must be @claudinite/cli or @claudinite/cli-rc, not $PACKAGE" >&2; exit 2 ;;
esac
name=${PACKAGE#@claudinite/}
sdk=false
case $# in
  0) ;;
  1) [ "$1" = --placeholder-sdk ] || { echo "usage: release/build.sh [--placeholder-sdk]" >&2; exit 2; }; sdk=true ;;
  *) echo "usage: release/build.sh [--placeholder-sdk]" >&2; exit 2 ;;
esac
if [ "$sdk" = true ] && [ "$VERSION" != 0.0.0 ]; then
  echo "build: --placeholder-sdk reserves @claudinite/sdk at 0.0.0 only, not $VERSION" >&2
  exit 2
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

pkgjson() {
  # name, description, extra fields
  printf '{\n  "name": "%s",\n  "version": "%s",\n  "description": "Claudinite engine %s",\n  "license": "UNLICENSED",\n  "repository": "github:missingbulb/ClaudiniteEngine"%s\n}\n' \
    "$1" "$VERSION" "$2" "$3"
}

cli=$DIST/npm/$name/package
mkdir -p "$cli"
cp "$DIST/manifest.json" "$cli/"
pkgjson "$PACKAGE" "release manifest" "" > "$cli/package.json"

for p in $platforms; do
  bin=cn
  [ "$p" = windows-x64 ] && bin=cn.exe
  os=${p%-*}
  [ "$os" = windows ] && os=win32
  cpu=${p#*-}
  dir=$DIST/npm/$name-$p/package
  mkdir -p "$dir/bin"
  cp "$DIST/bin/$p/$bin" "$dir/bin/$bin"
  pkgjson "$PACKAGE-$p" "binary for $p" ",
  \"os\": [\"$os\"],
  \"cpu\": [\"$cpu\"]" > "$dir/package.json"
done

if [ "$sdk" = true ]; then
  dir=$DIST/npm/sdk/package
  mkdir -p "$dir"
  pkgjson "@claudinite/sdk" "SDK" ",
  \"type\": \"module\",
  \"main\": \"index.mjs\"" > "$dir/package.json"
  echo 'export {};' > "$dir/index.mjs"
  echo 'placeholder, see ClaudiniteEngine' > "$dir/README.md"
fi

for d in "$DIST"/npm/*/package; do
  sh release/npmpack.sh "$d" "$DIST/tarballs"
done

"$tools/manifest" sums --dist "$DIST"
integrity=$("$tools/manifest" integrity "$cli/manifest.json")
printf '%s\n' "$integrity" > "$DIST/manifest.integrity"
echo "version $VERSION"
echo "manifest $integrity"
