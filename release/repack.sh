#!/bin/sh
# Rewrites the rc channel's tarballs in IN as the stable channel's in OUT:
# @claudinite/cli-rc[-<platform>] becomes @claudinite/cli[-<platform>].
# Only package.json's name changes; every other file, the binaries,
# manifest.json and manifest.sig.json, is checked byte for byte against
# the rc tarball, and any other difference fails. The channel tarball must
# carry manifest.sig.json: only a signed candidate is promoted.
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
    cli-rc-*.tgz) ;;
    *) fail "$base is not an rc channel tarball" ;;
  esac
  count=$((count + 1))
  rc=$work/rc-$count
  mkdir -p "$rc"
  tar -xzf "$tgz" -C "$rc"
  [ -f "$rc/package/package.json" ] || fail "$base holds no package/package.json"
  case $base in
    cli-rc-[0-9]*.tgz)
      if [ ! -f "$rc/package/manifest.json" ] || [ ! -f "$rc/package/manifest.sig.json" ]; then
        fail "$base lacks manifest.json or manifest.sig.json; only a signed candidate is promoted"
      fi ;;
  esac

  stable=$work/stable-$count
  cp -R "$rc" "$stable"
  sed 's|^  "name": "@claudinite/cli-rc|  "name": "@claudinite/cli|' "$rc/package/package.json" > "$stable/package/package.json"
  sh "$here/release/npmpack.sh" "$stable/package" "$out"
  new=$out/cli-${base#cli-rc-}
  [ -f "$new" ] || fail "packing $base did not produce $(basename "$new")"

  check=$work/check-$count
  mkdir -p "$check"
  tar -xzf "$new" -C "$check"
  (cd "$rc" && find . -type f | sort) > "$work/rc.list"
  (cd "$check" && find . -type f | sort) > "$work/stable.list"
  cmp -s "$work/rc.list" "$work/stable.list" || fail "$(basename "$new") holds other files than $base"
  while IFS= read -r f; do
    [ "$f" = ./package/package.json ] && continue
    cmp -s "$rc/$f" "$check/$f" || fail "$f differs between $base and $(basename "$new")"
  done < "$work/rc.list"
  node -e '
    const fs = require("fs");
    const a = JSON.parse(fs.readFileSync(process.argv[1])), b = JSON.parse(fs.readFileSync(process.argv[2]));
    if (b.name !== a.name.replace(/^@claudinite\/cli-rc/, "@claudinite/cli")) throw new Error("name " + b.name);
    delete a.name; delete b.name;
    if (JSON.stringify(a) !== JSON.stringify(b)) throw new Error("package.json differs beyond its name");
  ' "$rc/package/package.json" "$check/package/package.json" || fail "$(basename "$new"): package.json"
  echo "repack: $base -> $(basename "$new")"
done
[ "$count" -gt 0 ] || fail "no tarballs in $in"
