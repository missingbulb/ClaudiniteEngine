#!/bin/sh
# The fast check's speed budget (ci-speed.yml): the p75 wall time of ci.yml's
# successful runs over the last seven days against 60 seconds. Over it, one
# issue is opened or commented on, for the routine that works it; back under
# it, that issue closes; a quiet week (too few runs) says nothing about speed.
# The report also lands in the step summary when GITHUB_STEP_SUMMARY is set.
#
#   dev/test/ci-speed.sh RUN_URL
#
# Needs GH_TOKEN, GITHUB_API_URL and GITHUB_REPOSITORY, as an Actions job has.
set -eu
[ $# -eq 1 ] || { echo "usage: dev/test/ci-speed.sh RUN_URL" >&2; exit 2; }
cd "$(dirname "$0")/../.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
title="Engine CI p75 is over 60s"

since=$(date -u -d '7 days ago' +%Y-%m-%d)
# Runs before the fast check's first full day timed a different workflow.
[ "$(echo "$since" | tr -d -)" -gt 20261005 ] || since=2026-10-06
curl -fsS -H "Authorization: Bearer $GH_TOKEN" \
  "$GITHUB_API_URL/repos/$GITHUB_REPOSITORY/actions/workflows/ci.yml/runs?status=completed&created=%3E%3D$since&per_page=100" > "$work/runs.json"
GITHUB_OUTPUT=$work/out sh rewrite-temp/probe/ci-speed/p75.sh --budget 60 < "$work/runs.json" > "$work/report.md"
cat "$work/report.md"
[ -z "${GITHUB_STEP_SUMMARY:-}" ] || cat "$work/report.md" >> "$GITHUB_STEP_SUMMARY"
body="$(cat "$work/report.md")

$1"
case $(sed -n 's/^verdict=//p' "$work/out") in
  over) sh dev/test/issue.sh open "$title" "$body" ;;
  under) sh dev/test/issue.sh close "$title" "Back under budget. $body" ;;
esac
