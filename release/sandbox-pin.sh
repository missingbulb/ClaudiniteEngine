#!/bin/sh
# Pins a checkout of ClaudiniteSandbox to a published @claudinite/cli-rc
# version: the launcher verbatim, the settings with the pin, the six hook
# wirings and the .gitignore, exactly as the rehearsal's fixture member
# has them. The live Phase 1 gate (#8 T9) commits the result on a branch.
# usage: release/sandbox-pin.sh VERSION INTEGRITY [CHECKOUT]   (default: .)
set -eu
fail() { echo "sandbox-pin: $*" >&2; exit 2; }
[ $# -eq 2 ] || [ $# -eq 3 ] || fail "usage: release/sandbox-pin.sh VERSION INTEGRITY [CHECKOUT]"
printf '%s\n' "$1" | grep -Eqx '[0-9]+\.[0-9]+\.[0-9]+' || fail "$1 is not a version"
printf '%s\n' "$2" | grep -Eqx 'sha512-[A-Za-z0-9+/]{86}==' || fail "$2 is not a sha512 integrity string"
sh "$(dirname "$0")/member-fixture.sh" "${3:-.}" "$1" "$2" "@claudinite/cli-rc"
