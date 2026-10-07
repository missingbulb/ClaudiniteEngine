#!/bin/sh
# Pins a checkout of ClaudiniteSandbox to a published @claudinite/cli
# version on the canary channel: the launcher verbatim, the settings with the pin, the six hook
# wirings, .claudinite/.gitignore and the three member workflows, exactly as
# the rehearsal's fixture member has them. The live gates (#8 T9, #13 T9)
# commit the result on a branch a person merges, since it adds workflows.
# usage: dev/release/publish/sandbox-pin.sh VERSION INTEGRITY [CHECKOUT]   (default: .)
set -eu
fail() { echo "sandbox-pin: $*" >&2; exit 2; }
[ $# -eq 2 ] || [ $# -eq 3 ] || fail "usage: dev/release/publish/sandbox-pin.sh VERSION INTEGRITY [CHECKOUT]"
printf '%s\n' "$1" | grep -Eqx '[0-9]+\.[0-9]+\.[0-9]+' || fail "$1 is not a version"
printf '%s\n' "$2" | grep -Eqx 'sha512-[A-Za-z0-9+/]{86}==' || fail "$2 is not a sha512 integrity string"
sh "$(dirname "$0")/../verify/fixtures/member-fixture.sh" "${3:-.}" "$1" "$2" canary
