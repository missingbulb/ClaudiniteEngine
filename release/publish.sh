#!/bin/sh
# Publishes every tarball under $DIST/tarballs (default dist/) to npm, the
# platform packages first so the channel package never names a binary the
# registry lacks. Every tarball is checked before anything is published:
# its package.json name must belong to --channel and its version must be
# $VERSION (default: $DIST/manifest.json's), so a stale dist/ from another
# run can never be published.
#
#   release/publish.sh --channel rc|stable --auth oidc|token [--skip-existing]
#   release/publish.sh --channel rc|stable --dry-run
#
# --dry-run prints each npm publish line and runs nothing. --skip-existing
# leaves out a version npm view already reports, so a rerun is clean.
# --auth names how npm authenticates: oidc is trusted publishing from the
# release workflows, token is the npm bootstrap's NPM_BOOTSTRAP_TOKEN (#2),
# checked with npm whoami before anything is published. A refusal names the
# cause that fits the mode: a trusted publisher not yet attached, or what the
# token's account and scope permissions lack.
set -eu
cd "$(dirname "$0")/.."
DIST=${DIST:-dist}
fail() { echo "publish: $*" >&2; exit 1; }

channel=
auth=
dry=false
skip=false
while [ $# -gt 0 ]; do
  case $1 in
    --channel) [ $# -ge 2 ] || fail "--channel needs rc or stable"; channel=$2; shift 2 ;;
    --auth) [ $# -ge 2 ] || fail "--auth needs oidc or token"; auth=$2; shift 2 ;;
    --dry-run) dry=true; shift ;;
    --skip-existing) skip=true; shift ;;
    *) fail "usage: release/publish.sh --channel rc|stable --auth oidc|token [--skip-existing] | --dry-run" ;;
  esac
done
case $auth in
  oidc|token) ;;
  '') [ "$dry" = true ] || fail "--auth must name oidc or token for a real publish" ;;
  *) fail "--auth must be oidc or token, not $auth" ;;
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
npmerr=$(mktemp)
trap 'rm -f "$list" "$npmerr"' EXIT
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

scope=@claudinite
org=${scope#@}
user=
if [ "$dry" = false ] && [ "$auth" = token ]; then
  user=$(npm whoami 2>"$npmerr") || fail "the npm token does not authenticate (npm whoami: $(tail -n 1 "$npmerr")); NODE_AUTH_TOKEN is unset, mistyped, expired or revoked: generate a new one and save it as NPM_BOOTSTRAP_TOKEN, per https://github.com/missingbulb/ClaudiniteEngine/issues/2"
  echo "publish: npm authenticates as $user"
fi

# token_refusal NAME: why npm refused a token publish of NAME, from what npm
# reports about the account the token belongs to.
token_refusal() {
  roles=$(npm org ls "$org" --json 2>"$npmerr") || {
    echo "publish: npm refused $1 $VERSION for $user. npm org ls $org failed ($(tail -n 1 "$npmerr")), so the npm org $org does not exist or $user is not a member of it, or the token has no read access to the org: create the org as $user, or add $user to it as an owner"
    return
  }
  role=$(printf '%s' "$roles" | sed -n "s/.*\"$user\": *\"\([a-z]*\)\".*/\1/p")
  if [ -z "$role" ]; then
    echo "publish: npm refused $1 $VERSION: $user is not a member of the npm org $org (its members: $(printf '%s' "$roles" | tr -d '{}\n')); add $user to it as an owner, or generate the token as one of its owners"
    return
  fi
  echo "publish: npm refused $1 $VERSION, though $user is $role of the npm org $org: the granular token lacks write access to the $scope scope. Generate a new granular token whose Packages and scopes permission is read and write on $scope (and which may bypass two-factor authentication), and save it as NPM_BOOTSTRAP_TOKEN"
}

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
  if [ "$skip" = true ] && [ "$(npm view "$name@$VERSION" version 2>/dev/null)" = "$VERSION" ]; then
    echo "publish: $name $VERSION is already on npm; skipped"
    continue
  fi
  echo "$line"
  # Verbose under OIDC so a refusal shows whether npm's token exchange ran
  # and what the registry answered it, which the error itself never says.
  level=notice
  [ "$auth" = oidc ] && level=verbose
  # $line is word-split on purpose: the tarball path has no spaces.
  # shellcheck disable=SC2086
  if ! npm_config_loglevel=$level $line; then
    if [ "$auth" = token ]; then
      token_refusal "$name" >&2
    else
      echo "publish: npm refused $name $VERSION; attach its trusted publisher, the checkbox for $name on https://github.com/missingbulb/ClaudiniteEngine/issues/2" >&2
    fi
    exit 1
  fi
done
