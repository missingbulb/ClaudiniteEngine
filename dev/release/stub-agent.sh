#!/bin/sh
# The stub agent dev/release/ghstub runs on a routine fire (--agent): what a
# routine session does with the item it was handed, minus the model. It
# validates the dispatch with the nonce it was given, proves a wrong nonce
# is refused, asks cn work converge for the transition and has ghstub
# apply it, as the session's GitHub tools would.
#
# ghstub hands it CLAUDINITE_AGENT_{ISSUE,NONCE,ITEM,COMMENTS,STUB,REPO};
# the rehearsal sets STUB_AGENT_CN (the member's cn), STUB_AGENT_MEMBER
# (its checkout) and STUB_AGENT_CA (ghstub's certificate).
set -eu
fail() { echo "stub-agent: $*" >&2; exit 1; }
n=$CLAUDINITE_AGENT_ISSUE
cd "$STUB_AGENT_MEMBER"
validate() {
  "$STUB_AGENT_CN" work validate --issue "$n" --nonce "$1" --item-file "$CLAUDINITE_AGENT_ITEM" --comments-file "$CLAUDINITE_AGENT_COMMENTS"
}
validate "$CLAUDINITE_AGENT_NONCE" || fail "the item's own nonce did not validate"
if wrong=$(validate "not-$CLAUDINITE_AGENT_NONCE" 2>&1); then fail "a wrong nonce validated: $wrong"; fi
case $wrong in *"not this item's session"*) ;; *) fail "a wrong nonce was refused for another reason: $wrong" ;; esac
echo "stub-agent: a wrong nonce is not this item's session"
"$STUB_AGENT_CN" work converge --issue "$n" --outcome "done" --summary "the hello agent ran" --repo "$CLAUDINITE_AGENT_REPO" \
  --item-file "$CLAUDINITE_AGENT_ITEM" || fail "converge refused"
curl -sS --fail --cacert "$STUB_AGENT_CA" -X POST -H 'Content-Type: application/json' \
  -d "{\"issue\":$n,\"outcome\":\"done\",\"summary\":\"the hello agent ran\"}" "$CLAUDINITE_AGENT_STUB/_stub/converge" > /dev/null \
  || fail "ghstub refused the transition"
