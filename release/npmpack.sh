#!/bin/sh
# Packs one npm package folder into OUT as <name>-<version>.tgz, the name
# without the @claudinite/ scope, which is how the registry names it.
# usage: release/npmpack.sh PACKAGE_DIR OUT
set -eu
[ $# -eq 2 ] || { echo "usage: release/npmpack.sh PACKAGE_DIR OUT" >&2; exit 2; }
packed=$(cd "$1" && npm pack --silent --pack-destination "$2")
case $packed in
  claudinite-*.tgz) mv "$2/$packed" "$2/${packed#claudinite-}" ;;
  *) echo "npmpack: $1 packed as $packed, not an @claudinite package" >&2; exit 1 ;;
esac
