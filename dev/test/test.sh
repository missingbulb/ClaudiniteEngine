#!/bin/sh
# Runs go test over every package but the parity harness, which is off CI
# since the port it proved is done (design record 135), with this script's
# arguments as go test's flags.
#
#   dev/test/test.sh [go test flags]   the fast check passes -short -count=1 -p 8
set -eu
cd "$(dirname "$0")/../.."
# shellcheck disable=SC2046 # one word per package path
go test "$@" $(go list ./... | grep -v /parity)
