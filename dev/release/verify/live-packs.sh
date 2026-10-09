#!/bin/sh
# The nightly live-packs check (live-packs.yml): cn built from this commit at
# today's version, so it meets every floor the shelf names, with the
# development roots beside the ceremony's, then the rehearsal's live-packs
# mode against the real pack CDN and ClaudinitePacks' vendored branch.
#
#   dev/release/verify/live-packs.sh
set -eu
[ $# -eq 0 ] || { echo "usage: dev/release/verify/live-packs.sh" >&2; exit 2; }
cd "$(dirname "$0")/../../.."
VERSION="$(cat dev/build/major).$(sh dev/build/version.sh day).1" REHEARSAL=1 BUILD_TAGS=devroots sh dev/release/create/build.sh
sh dev/release/verify/rehearse.sh --mode live-packs
