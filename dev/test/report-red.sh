#!/bin/sh
# A red nightly full check opens, or comments on, one issue
# (dev/test/issue.sh).
#
#   dev/test/report-red.sh RUN_URL
set -eu
[ $# -eq 1 ] || { echo "usage: dev/test/report-red.sh RUN_URL" >&2; exit 2; }
cd "$(dirname "$0")/../.."
sh dev/test/issue.sh open "The nightly full check is red" \
  "$(date -u +%F): $1 failed. After a fix, run full from the Actions tab to confirm."
