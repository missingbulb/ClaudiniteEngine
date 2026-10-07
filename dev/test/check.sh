#!/bin/sh
# The fast check every pull request and every push to main waits on (ci.yml):
# golangci-lint, shellcheck and actionlint in the background while the short
# tests take the cores. Each linter's output is printed whole after the tests;
# the exit is non-zero when any of the four failed.
#
# golangci-lint's standard linters include govet. actionlint v1.7.11, the last
# release that builds on Go 1.24, covers the engine's workflows and the member
# templates, running shellcheck over every run: body.
set -u
cd "$(dirname "$0")/../.." || exit 1
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT

golangci-lint run > "$out/golangci" 2>&1 &
gl=$!
# shellcheck disable=SC2046 # one word per script path, none with spaces
shellcheck -s sh cn/launcher/launch $(find dev rewrite-temp/probe -name '*.sh' -not -path '*/testdata/*' | sort) > "$out/shellcheck" 2>&1 &
sc=$!
go run github.com/rhysd/actionlint/cmd/actionlint@393031adb9afb225ee52ae2ccd7a5af5525e03e8 .github/workflows/*.yml cn/lifecycle/workflows/templates/*.yml > "$out/actionlint" 2>&1 &
al=$!

rc=0
# -count=1: a cached pass cannot see the scripts and binaries a test runs as
# subprocesses. -p 8, twice the cores: many packages spend their time waiting
# on subprocesses and timeouts.
sh dev/test/test.sh -short -count=1 -p 8 || rc=1
wait "$gl" || rc=1
cat "$out/golangci"
wait "$sc" || rc=1
cat "$out/shellcheck"
wait "$al" || rc=1
cat "$out/actionlint"
exit "$rc"
