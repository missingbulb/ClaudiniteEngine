#!/bin/sh
# Keeps one issue per title for a scheduled check, opened by this
# repository's Actions token:
#
#   dev/test/issue.sh open TITLE BODY    comment BODY on the open issue so titled, or open it
#   dev/test/issue.sh close TITLE BODY   comment BODY on it and close it; nothing when none is open
#
# Needs GH_TOKEN, GITHUB_API_URL and GITHUB_REPOSITORY, as an Actions job has.
set -eu
[ $# -eq 3 ] || { echo "usage: dev/test/issue.sh open|close TITLE BODY" >&2; exit 2; }
action=$1 title=$2 body=$3
case $action in open|close) ;; *) echo "issue.sh: $action is neither open nor close" >&2; exit 2 ;; esac
api="$GITHUB_API_URL/repos/$GITHUB_REPOSITORY"
call() {
  curl -fsS -H "Authorization: Bearer $GH_TOKEN" "$@"
}
number=$(call "$api/issues?state=open&creator=github-actions%5Bbot%5D&per_page=100" \
  | jq -r --arg t "$title" 'map(select(.title == $t and (.pull_request | not))) | first | .number // empty')
if [ -n "$number" ]; then
  jq -n --arg b "$body" '{body: $b}' | call -X POST "$api/issues/$number/comments" -d @- > /dev/null
  [ "$action" = open ] || call -X PATCH "$api/issues/$number" -d '{"state":"closed","state_reason":"completed"}' > /dev/null
elif [ "$action" = open ]; then
  jq -n --arg t "$title" --arg b "$body" '{title: $t, body: $b}' | call -X POST "$api/issues" -d @- > /dev/null
fi
