#!/bin/sh
# Signs the release in $DIST (default dist/) with the real release key
# (dev/release/create/sign.sh), then verifies it against the roots a released
# cn trusts and the platforms $PLATFORMS names. The key and its certificate
# arrive in CN_RELEASE_KEY and CN_RELEASE_CERT, the release environment's
# secrets (dev/release/keys/cn-keys/README.md); they are written only to a private
# temporary folder, which is gone before the verify runs. Under Actions the
# step's output signing= names the key that signed.
#
#   dev/release/create/sign-release.sh
set -eu
[ $# -eq 0 ] || { echo "usage: dev/release/create/sign-release.sh" >&2; exit 2; }
cd "$(dirname "$0")/../../.."
if [ -z "${CN_RELEASE_KEY:-}" ] || [ -z "${CN_RELEASE_CERT:-}" ]; then
  echo "::error::the release environment must hold both CN_RELEASE_KEY and CN_RELEASE_CERT (dev/release/keys/cn-keys/README.md)"
  exit 1
fi
keys=$(mktemp -d)
trap 'rm -rf "$keys"' EXIT
( umask 077
  printf '%s\n' "$CN_RELEASE_KEY" > "$keys/release.key"
  printf '%s\n' "$CN_RELEASE_CERT" > "$keys/release.cert.json" )
RELEASE_KEY=$keys/release.key RELEASE_CERT=$keys/release.cert.json sh dev/release/create/sign.sh
rm -rf "$keys"
go run ./dev/release/create/manifest verify --dist "${DIST:-dist}" --roots cn/shared/trust/roots --platforms "${PLATFORMS:?PLATFORMS names the platforms the manifest must list}"
[ -z "${GITHUB_OUTPUT:-}" ] || echo "signing=release" >> "$GITHUB_OUTPUT"
