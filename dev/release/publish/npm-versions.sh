#!/bin/sh
# Writes each CLI package's `npm view <pkg> versions --json` to
# DIR/<package name>.json, an empty file for a package npm has never held
# (E404). Any other failure exits 1 naming the package, since then which
# packages hold a version is unknown.
#
#   dev/release/publish/npm-versions.sh DIR
set -eu
[ $# -eq 1 ] || { echo "usage: dev/release/publish/npm-versions.sh DIR" >&2; exit 2; }
cd "$(dirname "$0")/../../.."
dir=$1
err=$(mktemp)
trap 'rm -f "$err"' EXIT
mkdir -p "$dir/@claudinite"
for name in $(go run ./dev/release/pipeline names); do
  out=$dir/$name.json
  if ! npm view "$name" versions --json > "$out" 2> "$err"; then
    if grep -q E404 "$out" "$err"; then
      : > "$out"
    else
      echo "npm view $name failed: $(cat "$out" "$err" | tr '\n' ' ')" >&2
      exit 1
    fi
  fi
done
