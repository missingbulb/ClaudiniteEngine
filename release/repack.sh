#!/bin/sh
# @legacy-tolerance advisory:engine-package retire:#97
# Rewrites @claudinite/cli's tarballs in IN as @claudinite/cli-rc's in OUT,
# for members whose updater still reads the retired rc package:
# @claudinite/cli[-<platform>] becomes @claudinite/cli-rc[-<platform>].
# Only package.json's name changes; every other file, the binaries,
# manifest.json and manifest.sig.json, is checked byte for byte against
# the cli tarball, and any other difference fails. The manifest tarball
# must carry manifest.sig.json: only a signed candidate is bridged.
# usage: release/repack.sh IN OUT
set -eu
[ $# -eq 2 ] || { echo "usage: release/repack.sh IN OUT" >&2; exit 2; }
here=$(cd "$(dirname "$0")/.." && pwd)
in=$(cd "$1" && pwd)
mkdir -p "$2"
out=$(cd "$2" && pwd)
fail() { echo "repack: $*" >&2; exit 1; }

work=$(mktemp -d)
trap 'chmod -R u+w "$work" 2>/dev/null; rm -rf "$work"' EXIT

count=0
for tgz in "$in"/*; do
  base=$(basename "$tgz")
  case $base in
    cli-rc-*) fail "$base is already a cli-rc tarball" ;;
    cli-*.tgz) ;;
    *) fail "$base is not a @claudinite/cli tarball" ;;
  esac
  count=$((count + 1))
  src=$work/cli-$count
  mkdir -p "$src"
  tar -xzf "$tgz" -C "$src"
  [ -f "$src/package/package.json" ] || fail "$base holds no package/package.json"
  case $base in
    cli-[0-9]*.tgz)
      if [ ! -f "$src/package/manifest.json" ] || [ ! -f "$src/package/manifest.sig.json" ]; then
        fail "$base lacks manifest.json or manifest.sig.json; only a signed candidate is bridged"
      fi ;;
  esac

  rc=$work/rc-$count
  cp -R "$src" "$rc"
  sed 's|^  "name": "@claudinite/cli|  "name": "@claudinite/cli-rc|' "$src/package/package.json" > "$rc/package/package.json"
  sh "$here/release/npmpack.sh" "$rc/package" "$out"
  new=$out/cli-rc-${base#cli-}
  [ -f "$new" ] || fail "packing $base did not produce $(basename "$new")"

  check=$work/check-$count
  mkdir -p "$check"
  tar -xzf "$new" -C "$check"
  (cd "$src" && find . -type f | sort) > "$work/cli.list"
  (cd "$check" && find . -type f | sort) > "$work/rc.list"
  cmp -s "$work/cli.list" "$work/rc.list" || fail "$(basename "$new") holds other files than $base"
  while IFS= read -r f; do
    [ "$f" = ./package/package.json ] && continue
    cmp -s "$src/$f" "$check/$f" || fail "$f differs between $base and $(basename "$new")"
  done < "$work/cli.list"
  node -e '
    const fs = require("fs");
    const a = JSON.parse(fs.readFileSync(process.argv[1])), b = JSON.parse(fs.readFileSync(process.argv[2]));
    if (b.name !== a.name.replace(/^@claudinite\/cli/, "@claudinite/cli-rc")) throw new Error("name " + b.name);
    delete a.name; delete b.name;
    if (JSON.stringify(a) !== JSON.stringify(b)) throw new Error("package.json differs beyond its name");
  ' "$src/package/package.json" "$check/package/package.json" || fail "$(basename "$new"): package.json"
  echo "repack: $base -> $(basename "$new")"
done
[ "$count" -gt 0 ] || fail "no tarballs in $in"
