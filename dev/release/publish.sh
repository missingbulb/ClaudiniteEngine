#!/bin/sh
# Publishes every tarball under $DIST/tarballs (default dist/) to npm under
# the dist-tag --tag, the platform packages first so the manifest package
# never names a binary the registry lacks. Every tarball is checked before
# anything is published: its package.json name must be @claudinite/cli or
# one of its platform packages and its version must be $VERSION (default:
# $DIST/manifest.json's), so a stale dist/ from another run can never be
# published.
#
#   dev/release/publish.sh --tag rc|staging --auth oidc
#   dev/release/publish.sh --tag rc|staging --dry-run
#
# latest is never published: promote.yml moves it onto a verified rc version.
# --dry-run prints each npm publish line and runs nothing. --auth oidc is
# trusted publishing from the release workflows, the only way npm accepts
# these packages; a refusal names the trusted publisher to attach.
set -eu
cd "$(dirname "$0")/../.."
DIST=${DIST:-dist}
fail() { echo "publish: $*" >&2; exit 1; }

tag=
auth=
dry=false
while [ $# -gt 0 ]; do
  case $1 in
    --tag) [ $# -ge 2 ] || fail "--tag needs rc or staging"; tag=$2; shift 2 ;;
    --auth) [ $# -ge 2 ] || fail "--auth needs oidc"; auth=$2; shift 2 ;;
    --dry-run) dry=true; shift ;;
    *) fail "usage: dev/release/publish.sh --tag rc|staging --auth oidc | --dry-run" ;;
  esac
done
case $auth in
  oidc) ;;
  '') [ "$dry" = true ] || fail "--auth must name oidc for a real publish" ;;
  *) fail "--auth must be oidc, not $auth" ;;
esac
case $tag in
  rc|staging) ;;
  latest) fail "nothing publishes under latest: promote.yml moves it onto an rc version it verified" ;;
  *) fail "--tag must be rc or staging" ;;
esac
if [ -z "${VERSION:-}" ]; then
  [ -f "$DIST/manifest.json" ] || fail "set VERSION; there is no $DIST/manifest.json to read it from"
  VERSION=$(sed -n 's/^  "version": "\(.*\)",$/\1/p' "$DIST/manifest.json")
fi
allowed='@claudinite/cli(-(linux|darwin|windows)-x64|-(linux|darwin)-arm64)?'

list=$(mktemp)
trap 'rm -f "$list"' EXIT
for tgz in "$DIST"/tarballs/*.tgz; do
  [ -f "$tgz" ] || fail "no tarballs in $DIST/tarballs"
  pj=$(tar -xzOf "$tgz" package/package.json) || fail "$tgz holds no package/package.json"
  name=$(printf '%s\n' "$pj" | sed -n 's/^  "name": "\(.*\)",$/\1/p')
  version=$(printf '%s\n' "$pj" | sed -n 's/^  "version": "\(.*\)",\{0,1\}$/\1/p')
  printf '%s\n' "$name" | grep -Eqx "$allowed" || fail "$tgz is $name, which this publish may not carry (@claudinite/cli and its platform packages)"
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
  # The tag is explicit, since npm otherwise moves latest onto every publish.
  line="npm publish $tgz --access public --provenance false --tag $tag"
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
