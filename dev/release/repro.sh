#!/bin/sh
# Builds linux-x64 twice from this source, the second time with an empty Go
# build cache, and fails unless the two binaries are byte-identical: the
# rehearsal and the hop stand a rebuild of the candidate in for it, which
# proves the candidate only while a build is reproducible.
set -eu
cd "$(dirname "$0")/../.."
VERSION="${VERSION:-1.61001.1}"
export VERSION
COMMIT=$(git rev-parse --short=7 HEAD 2>/dev/null || echo unknown)
export COMMIT
tmp=$(mktemp -d)
trap 'chmod -R u+w "$tmp" 2>/dev/null; rm -rf "$tmp"' EXIT
sh dev/release/gobuild.sh linux-x64 "$tmp/a"
GOCACHE=$tmp/gocache sh dev/release/gobuild.sh linux-x64 "$tmp/b"
a=$(openssl dgst -sha256 -r < "$tmp/a" | cut -d' ' -f1)
b=$(openssl dgst -sha256 -r < "$tmp/b" | cut -d' ' -f1)
[ "$a" = "$b" ] || { echo "repro: linux-x64 builds differ: $a vs $b" >&2; exit 1; }
echo "repro: ok ($a)"
