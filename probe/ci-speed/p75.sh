#!/bin/sh
# CI speed probe: the 75th-percentile wall time of a workflow's successful runs, read from a
# list-workflow-runs response on stdin, against a budget in seconds.
#
#   sh probe/ci-speed/p75.sh --budget SECONDS < runs.json
#
# Prints a Markdown report and, when GITHUB_OUTPUT is set, writes verdict (over, under or
# too-few, under five successful runs), p75 and runs there. Every verdict exits 0.
set -eu
budget=
while [ $# -gt 0 ]; do
  case $1 in
    --budget) budget=$2; shift 2 ;;
    *) echo "usage: p75.sh --budget SECONDS < runs.json" >&2; exit 2 ;;
  esac
done
case $budget in ''|*[!0-9]*) echo "p75.sh: --budget takes a number of seconds" >&2; exit 2 ;; esac

jq -r --argjson budget "$budget" '
  [.workflow_runs[]
   | select(.conclusion == "success")
   | {url: .html_url, secs: ((.updated_at | fromdateiso8601) - (.run_started_at | fromdateiso8601))}]
  | sort_by(.secs) as $rs
  | ($rs | length) as $n
  | if $n < 5 then
      "verdict=too-few", "p75=", "runs=\($n)",
      "# CI speed", "", "\($n) successful runs in the window; the p75 needs five."
    else
      ($rs[(($n * 3 + 3) / 4 | floor) - 1].secs) as $p75
      | "verdict=\(if $p75 > $budget then "over" else "under" end)", "p75=\($p75)", "runs=\($n)",
        "# CI speed", "",
        "p75 \($p75)s over \($n) successful runs (budget \($budget)s); median \($rs[($n - 1) / 2 | floor].secs)s, slowest \($rs[-1].secs)s.",
        "", "Slowest runs:", "",
        ($rs | reverse | .[:3][] | "- \(.secs)s \(.url)")
    end
' | while IFS= read -r line; do
  case $line in
    verdict=*|p75=*|runs=*) [ -z "${GITHUB_OUTPUT:-}" ] || echo "$line" >> "$GITHUB_OUTPUT" ;;
    *) echo "$line" ;;
  esac
done
