#!/bin/sh
# What later jobs check a built release in $DIST (default dist/) against:
# sums=, the SHA-256 of its SHA256SUMS (dev/release/verify/sums.sh), and
# integrity=, the manifest's integrity string, the pin a member writes.
# Printed, and under Actions also the step's outputs.
#
#   dev/release/create/dist-id.sh
set -eu
[ $# -eq 0 ] || { echo "usage: dev/release/create/dist-id.sh" >&2; exit 2; }
cd "$(dirname "$0")/../../.."
DIST=${DIST:-dist}
out="sums=$(sha256sum "$DIST/SHA256SUMS" | cut -d' ' -f1)
integrity=$(cat "$DIST/manifest.integrity")"
printf '%s\n' "$out"
[ -z "${GITHUB_OUTPUT:-}" ] || printf '%s\n' "$out" >> "$GITHUB_OUTPUT"
