#!/bin/sh
# Runs go test over every package but the parity harness, which is off CI
# since the port it proved is done (design record 135), with this script's
# arguments as go test's flags.
#
#   dev/test/test.sh [go test flags]   the fast check passes -short -count=1 -p 8
set -eu
cd "$(dirname "$0")/../.."
# Every git the tests start, fixtures' and the code's under test alike, reads
# this config instead of the machine's: a developer's signing or push
# settings add processes and change outcomes, and git's automatic
# maintenance and gc add a process after nearly every commit, fetch and push.
conf=$(mktemp)
trap 'rm -f "$conf"' EXIT
printf '[maintenance]\n\tauto = false\n[gc]\n\tauto = 0\n[receive]\n\tautogc = false\n' > "$conf"
export GIT_CONFIG_GLOBAL="$conf" GIT_CONFIG_NOSYSTEM=1
# shellcheck disable=SC2046 # one word per package path
go test "$@" $(go list ./... | grep -v /parity)
