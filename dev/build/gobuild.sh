#!/bin/sh
# Builds one platform's cn exactly as a release does: static, stripped,
# reproducible (no VCS stamp, trimmed paths, no timestamps).
# usage: dev/build/gobuild.sh <platform> <out-file>   (VERSION, COMMIT from the environment)
# BUILD_TAGS may name devroots and rehearsal_break, comma-separated, and
# only with REHEARSAL=1 (dev/release/verify/rehearse.sh): a build that trusts the
# development roots, or whose verify is deliberately broken, never reaches
# a release.
set -eu
tags=${BUILD_TAGS:-}
if [ -n "$tags" ]; then
  refused=
  [ "${REHEARSAL:-}" = 1 ] || refused=1
  for tag in $(printf '%s' "$tags" | tr ',' ' '); do
    case $tag in devroots|rehearsal_break) ;; *) refused=1 ;; esac
  done
  if [ -n "$refused" ]; then
    echo "gobuild: BUILD_TAGS=$tags refused: only devroots and rehearsal_break, and only with REHEARSAL=1 (dev/release/verify/rehearse.sh)" >&2
    exit 2
  fi
fi
platform=$1
out=$2
case $platform in
  linux-x64) goos=linux goarch=amd64 ;;
  linux-arm64) goos=linux goarch=arm64 ;;
  darwin-x64) goos=darwin goarch=amd64 ;;
  darwin-arm64) goos=darwin goarch=arm64 ;;
  windows-x64) goos=windows goarch=amd64 ;;
  *) echo "gobuild: unknown platform $platform" >&2; exit 2 ;;
esac
pkg=github.com/missingbulb/ClaudiniteEngine/cn/shared/version
CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch GOFLAGS=-trimpath \
  go build -buildvcs=false ${tags:+-tags "$tags"} \
  -ldflags "-s -w -X $pkg.version=${VERSION:-0.0.0} -X $pkg.commit=${COMMIT:-unknown}${EXTRA_LDFLAGS:+ $EXTRA_LDFLAGS}" \
  -o "$out" ./cn
