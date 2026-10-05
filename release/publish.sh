#!/bin/sh
# Publishes every tarball under $DIST/tarballs (default dist/) to npm, the
# platform packages first so the channel package never names a binary the
# registry lacks. Every tarball is checked before anything is published:
# its package.json name must belong to --channel and its version must be
# $VERSION (default: $DIST/manifest.json's), so a stale dist/ from another
# run can never be published.
#
#   release/publish.sh --channel rc|stable --auth oidc
#   release/publish.sh --channel rc|stable --dry-run
#
# --dry-run prints each npm publish line and runs nothing. --auth oidc is
# trusted publishing from the release workflows, the only way npm accepts
# these packages; a refusal names the trusted publisher to attach.
set -eu
cd "$(dirname "$0")/.."
DIST=${DIST:-dist}
fail() { echo "publish: $*" >&2; exit 1; }

channel=
auth=
dry=false
while [ $# -gt 0 ]; do
  case $1 in
    --channel) [ $# -ge 2 ] || fail "--channel needs rc or stable"; channel=$2; shift 2 ;;
    --auth) [ $# -ge 2 ] || fail "--auth needs oidc"; auth=$2; shift 2 ;;
    --dry-run) dry=true; shift ;;
    *) fail "usage: release/publish.sh --channel rc|stable --auth oidc | --dry-run" ;;
  esac
done
case $auth in
  oidc) ;;
  '') [ "$dry" = true ] || fail "--auth must name oidc for a real publish" ;;
  *) fail "--auth must be oidc, not $auth" ;;
esac
platform='(-(linux|darwin|windows)-(x64|arm64))?'
case $channel in
  rc) allowed="@claudinite/cli-rc$platform" ;;
  stable) allowed="(@claudinite/cli$platform|@claudinite/sdk)" ;;
  *) fail "--channel must be rc or stable" ;;
esac
if [ -z "${VERSION:-}" ]; then
  [ -f "$DIST/manifest.json" ] || fail "set VERSION; there is no $DIST/manifest.json to read it from"
  VERSION=$(sed -n 's/^  "version": "\(.*\)",$/\1/p' "$DIST/manifest.json")
fi

list=$(mktemp)
trap 'rm -f "$list"' EXIT
for tgz in "$DIST"/tarballs/*.tgz; do
  [ -f "$tgz" ] || fail "no tarballs in $DIST/tarballs"
  pj=$(tar -xzOf "$tgz" package/package.json) || fail "$tgz holds no package/package.json"
  name=$(printf '%s\n' "$pj" | sed -n 's/^  "name": "\(.*\)",$/\1/p')
  version=$(printf '%s\n' "$pj" | sed -n 's/^  "version": "\(.*\)",\{0,1\}$/\1/p')
  printf '%s\n' "$name" | grep -Eqx "$allowed" || fail "$tgz is $name, which is not a package of the $channel channel"
  [ "$version" = "$VERSION" ] || fail "$tgz is $name $version, not $VERSION"
  [ "$(basename "$tgz")" = "${name#@claudinite/}-$version.tgz" ] || fail "$tgz holds $name $version"
  case $name in
    *-linux-*|*-darwin-*|*-windows-*) order=1 ;;
    *) order=2 ;;
  esac
  printf '%s %s %s\n' "$order" "$name" "$tgz" >> "$list"
done

sort "$list" | while read -r _ name tgz; do
  # No provenance: the repository goes private, and npm attests only public
  # sources; the release key's manifest signature is what members verify.
  # The tag is explicit: npm refuses to move latest implicitly onto a
  # version semver sorts below the current one, and each channel is its own
  # package, so latest is always this release.
  line="npm publish $tgz --access public --provenance false --tag latest"
  if [ "$dry" = true ]; then
    echo "dry-run: $line"
    continue
  fi
  echo "$line"
  # Verbose so a refusal shows whether npm's token exchange ran and what the
  # registry answered it, which the error itself never says.
  # $line is word-split on purpose: the tarball path has no spaces.
  # shellcheck disable=SC2086
  if ! npm_config_loglevel=verbose $line; then
    echo "publish: npm refused $name $VERSION; attach its trusted publisher, the checkbox for $name on https://github.com/missingbulb/ClaudiniteEngine/issues/2" >&2
    exit 1
  fi
done
