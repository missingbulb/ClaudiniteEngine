#!/bin/sh
# The ported packs against this cn, and the per-call hooks' latency budgets
# (full.yml's packs job): ClaudinitePacks' Go tests and cn check --pack
# fixtures (tools/checks/test.sh), then the hook-latency probe over the
# same checkout's packs, held to its budgets, its table printed.
#
#   dev/test/packs.sh PACKS_CHECKOUT
#
# PACKS_CHECKOUT is ClaudinitePacks at dev/test/claudinitepacks.ref.
set -eu
[ $# -eq 1 ] || { echo "usage: dev/test/packs.sh PACKS_CHECKOUT" >&2; exit 2; }
cd "$(dirname "$0")/../.."
packs=$(cd "$1" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

go build -o "$work/cn" ./cn/cli
(cd "$packs" && CLAUDINITE_CN=$work/cn sh tools/checks/test.sh)

rc=0
sh rewrite-temp/probe/hook-latency/run.sh --shelf "$packs" --out "$work/hook-latency" --budget || rc=$?
cat "$work"/hook-latency/*.md
exit "$rc"
