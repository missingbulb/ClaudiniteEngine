#!/bin/sh
# Builds a release into $DIST (default dist/): the binaries, an UNSIGNED
# manifest.json, the npm package folders of @claudinite/cli (whose manifest
# package carries the launcher as its bin, for npx bootstrap; every package
# carries THIRD_PARTY_LICENSES) and their tarballs, and SHA256SUMS over all
# of it, then prints the manifest's integrity string, the value a member
# pins. dev/release/create/sign.sh signs it.
#
# VERSION (default 0.0.0). PLATFORMS (default all five) names the platforms
# built, separated by spaces; a staging build is linux-x64 alone, and its
# manifest lists that one.
set -eu
cd "$(dirname "$0")/../../.."
root=$(pwd)

VERSION=${VERSION:-0.0.0}
DIST=${DIST:-dist}
PACKAGE=@claudinite/cli
name=cli
all="linux-x64 linux-arm64 darwin-x64 darwin-arm64 windows-x64"
platforms=${PLATFORMS-$all}
[ -n "$(printf %s "$platforms" | tr -d " ")" ] || { echo "build: PLATFORMS names no platform" >&2; exit 2; }
seen=" "
for p in $platforms; do
  case " $all " in *" $p "*) ;; *) echo "build: PLATFORMS names $p, not one of $all" >&2; exit 2 ;; esac
  case $seen in *" $p "*) echo "build: PLATFORMS names $p twice" >&2; exit 2 ;; esac
  seen="$seen$p "
done
[ $# -eq 0 ] || { echo "usage: dev/release/create/build.sh" >&2; exit 2; }
COMMIT=$(git rev-parse --short=7 HEAD 2>/dev/null || echo unknown)
export VERSION COMMIT

case $DIST in /*) ;; *) DIST=$root/$DIST ;; esac
rm -rf "$DIST"
mkdir -p "$DIST/bin" "$DIST/npm" "$DIST/tarballs"

tools=$(mktemp -d)
trap 'rm -rf "$tools"' EXIT
go build -o "$tools/manifest" ./dev/release/create/manifest

for p in $platforms; do
  bin=cn
  [ "$p" = windows-x64 ] && bin=cn.exe
  mkdir -p "$DIST/bin/$p"
  sh dev/build/gobuild.sh "$p" "$DIST/bin/$p/$bin"
done

"$tools/manifest" write --dist "$DIST" --version "$VERSION" --commit "$COMMIT" --source "$root" --platforms "$platforms"

pkgjson() {
  # name, description, extra fields
  printf '{\n  "name": "%s",\n  "version": "%s",\n  "description": "Claudinite engine %s",\n  "license": "UNLICENSED",\n  "repository": {"type": "git", "url": "git+https://github.com/missingbulb/ClaudiniteEngine.git"}%s\n}\n' \
    "$1" "$VERSION" "$2" "$3"
}

cli=$DIST/npm/$name/package
mkdir -p "$cli"
cp "$DIST/manifest.json" "$cli/"
cp cn/packaging/launcher/launch "$cli/launch"
cp dev/build/THIRD_PARTY_LICENSES "$cli/"
chmod 0755 "$cli/launch"
pkgjson "$PACKAGE" "release manifest and launcher" ",
  \"bin\": {\"cn\": \"launch\"}" > "$cli/package.json"

for p in $platforms; do
  bin=cn
  [ "$p" = windows-x64 ] && bin=cn.exe
  os=${p%-*}
  [ "$os" = windows ] && os=win32
  cpu=${p#*-}
  dir=$DIST/npm/$name-$p/package
  mkdir -p "$dir/bin"
  cp "$DIST/bin/$p/$bin" "$dir/bin/$bin"
  cp dev/build/THIRD_PARTY_LICENSES "$dir/"
  pkgjson "$PACKAGE-$p" "binary for $p" ",
  \"os\": [\"$os\"],
  \"cpu\": [\"$cpu\"]" > "$dir/package.json"
done

for d in "$DIST"/npm/*/package; do
  sh dev/release/create/npmpack.sh "$d" "$DIST/tarballs"
done

"$tools/manifest" sums --dist "$DIST"
integrity=$("$tools/manifest" integrity "$cli/manifest.json")
printf '%s\n' "$integrity" > "$DIST/manifest.integrity"
echo "version $VERSION"
echo "manifest $integrity"
