#!/bin/sh
# Checks a built release in $DIST (default dist/): manifest.json lists
# exactly $PLATFORMS (default all five; a staging build names linux-x64),
# each binary exists and is the right executable type, static where the
# platform allows (Linux; macOS binaries always link dyld, Windows has no
# such notion), and the npm manifest.json hashes to the integrity string
# build.sh printed.
set -eu
cd "$(dirname "$0")/.."
DIST=${DIST:-dist}
PLATFORMS=${PLATFORMS:-linux-x64 linux-arm64 darwin-x64 darwin-arm64 windows-x64}
fail() { echo "smoke: $*" >&2; exit 1; }
version=$(sed -n 's/^  "version": "\(.*\)",$/\1/p' "$DIST/manifest.json")
name=cli
[ -f "$DIST/npm/$name/package/manifest.json" ] || fail "the @claudinite/cli package carries no manifest.json"
[ -f "$DIST/tarballs/$name-$version.tgz" ] || fail "no tarball for @claudinite/$name"
listed=$(sed -n 's/^    "\([a-z0-9-]*\)": {"file".*/\1/p' "$DIST/manifest.json" | tr '\n' ' ')
want=$(printf '%s\n' "$PLATFORMS" | tr -s ' ' '\n' | sed '/^$/d' | tr '\n' ' ')
[ "$listed" = "$want" ] || fail "manifest.json lists $listed; want $want"

for p in $PLATFORMS; do
  bin=cn
  [ "$p" = windows-x64 ] && bin=cn.exe
  f=$DIST/bin/$p/$bin
  [ -f "$f" ] || fail "missing $f"
  desc=$(file -b "$f")
  case $p in
    linux-x64) want="ELF 64-bit LSB executable, x86-64*statically linked*" ;;
    linux-arm64) want="ELF 64-bit LSB executable, ARM aarch64*statically linked*" ;;
    darwin-x64) want="Mach-O 64-bit x86_64 executable*" ;;
    darwin-arm64) want="Mach-O 64-bit arm64 executable*" ;;
    windows-x64) want="PE32+ executable (console) x86-64*" ;;
    *) fail "unknown platform $p" ;;
  esac
  # shellcheck disable=SC2254 # $want is a glob pattern on purpose
  case $desc in $want) ;; *) fail "$f: $desc" ;; esac
  [ -f "$DIST/npm/$name-$p/package/bin/$bin" ] || fail "npm package for $p lacks bin/$bin"
  cmp -s release/THIRD_PARTY_LICENSES "$DIST/npm/$name-$p/package/THIRD_PARTY_LICENSES" || fail "npm package for $p lacks THIRD_PARTY_LICENSES"
  [ -f "$DIST/tarballs/$name-$p-$version.tgz" ] || fail "no tarball for $p"
done

cmp -s release/THIRD_PARTY_LICENSES "$DIST/npm/$name/package/THIRD_PARTY_LICENSES" || fail "the cli package lacks THIRD_PARTY_LICENSES"

printed=$(cat "$DIST/manifest.integrity")
actual="sha512-$(openssl dgst -sha512 -binary < "$DIST/npm/$name/package/manifest.json" | openssl base64 -A)"
[ "$printed" = "$actual" ] || fail "npm manifest.json hashes to $actual, build.sh printed $printed"
echo "smoke: ok ($printed)"
