#!/bin/sh
# Builds one platform's cn exactly as a release does: static, stripped,
# reproducible (no VCS stamp, trimmed paths, no timestamps).
# usage: release/gobuild.sh <platform> <out-file>   (VERSION, COMMIT from the environment)
set -eu
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
pkg=github.com/missingbulb/ClaudiniteEngine/shared/version
CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch GOFLAGS=-trimpath \
  go build -buildvcs=false \
  -ldflags "-s -w -X $pkg.version=${VERSION:-0.0.0} -X $pkg.commit=${COMMIT:-unknown}${EXTRA_LDFLAGS:+ $EXTRA_LDFLAGS}" \
  -o "$out" ./cmd/cn
