#!/bin/sh
# Opens the release-blocker issue for a version a verification failed
# (dev/release/verify/blocker.go): the live-packs gate, or one platform leg of
# the install from npm. The promotion gate holds back any version an open
# release-blocker issue names. The issue carries the tail of LOG and this
# Actions run's URL.
#
#   dev/release/verify/blocker-issue.sh --gate live-packs --version V --log LOG
#   dev/release/verify/blocker-issue.sh --leg PLATFORM --version V --log LOG
#
# Needs GH_TOKEN, GITHUB_REPOSITORY, GITHUB_SERVER_URL and GITHUB_RUN_ID, as
# an Actions job has.
set -eu
cd "$(dirname "$0")/../../.."
[ $# -eq 6 ] || { echo "usage: dev/release/verify/blocker-issue.sh --gate live-packs|--leg PLATFORM --version V --log LOG" >&2; exit 2; }
issue=$(mktemp)
trap 'rm -f "$issue"' EXIT
go run ./dev/release/pipeline blocker-issue "$@" \
  --run-url "$GITHUB_SERVER_URL/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID" > "$issue"
gh label create release-blocker --repo "$GITHUB_REPOSITORY" --color B60205 \
  --description "Holds the named version back from promotion" 2>/dev/null || true
gh issue create --repo "$GITHUB_REPOSITORY" --label release-blocker \
  --title "$(head -n 1 "$issue")" --body "$(tail -n +3 "$issue")"
