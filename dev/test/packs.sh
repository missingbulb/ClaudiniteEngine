#!/bin/sh
# The ported packs against this cn, and the per-call hooks' latency budgets
# (full.yml's packs job): ClaudinitePacks' Go tests and cn check --pack
# fixtures (tools/checks/test.sh), the dashboard pack's suite against this cn,
# then the hook-latency probe
# over the frozen Node shelf's packs, held to its budgets, its table printed.
#
#   dev/test/packs.sh PACKS_CHECKOUT NODE_SHELF_CHECKOUT
#
# PACKS_CHECKOUT is ClaudinitePacks at dev/test/claudinitepacks.ref.
set -eu
[ $# -eq 2 ] || { echo "usage: dev/test/packs.sh PACKS_CHECKOUT NODE_SHELF_CHECKOUT" >&2; exit 2; }
cd "$(dirname "$0")/../.."
packs=$(cd "$1" && pwd)
shelf=$(cd "$2" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

go build -o "$work/cn" ./cn
(cd "$packs" && CLAUDINITE_CN=$work/cn sh tools/checks/test.sh)
(cd "$packs" && CLAUDINITE_CN=$work/cn node --test 'packs/claudinite-single-repo-dashboard/test/**/*.test.mjs')

rc=0
sh rewrite-temp/probe/hook-latency/run.sh --node "$shelf" --out "$work/hook-latency" --budget || rc=$?
cat "$work"/hook-latency/*.md
exit "$rc"
