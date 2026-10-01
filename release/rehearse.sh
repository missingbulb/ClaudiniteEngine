#!/bin/sh
# The local Phase 1 gate: a fixture member in a temp dir runs the real
# launcher against the built release in $DIST (default dist/), served by
# regstub, through the hook commands its .claude/settings.json wires, then
# proves a one-character change to the pin is refused and leaves nothing
# cached. Needs go, node, curl and a release from release/build.sh.
set -eu
cd "$(dirname "$0")/.."
root=$(pwd)
DIST=${DIST:-dist}
case $DIST in /*) ;; *) DIST=$root/$DIST ;; esac

fail() { echo "rehearse: FAIL: $*" >&2; exit 1; }
step() { echo "rehearse: $*"; }

[ -f "$DIST/manifest.integrity" ] || fail "no release in $DIST; run release/build.sh first"
DIST=$DIST sh release/smoke.sh
version=$(sed -n 's/^  "version": "\(.*\)",$/\1/p' "$DIST/manifest.json")
pin=$(cat "$DIST/manifest.integrity")

work=$(mktemp -d)
stub_pid=
cleanup() {
  [ -n "$stub_pid" ] && kill "$stub_pid" 2>/dev/null
  chmod -R u+w "$work" 2>/dev/null
  rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

go build -o "$work/regstub" ./release/regstub
"$work/regstub" --dist "$DIST" --ready "$work/ready" --ca-out "$work/ca.pem" --log "$work/requests.log" &
stub_pid=$!
tries=0
until [ -f "$work/ready" ]; do
  tries=$((tries + 1))
  [ "$tries" -le 100 ] || fail "regstub did not start"
  sleep 0.1
done

member=$work/member
mkdir -p "$member/.claudinite" "$member/.claude" "$work/home" "$work/cache"
cp launcher/launch "$member/.claudinite/launch"
printf '.claudinite/bin/\n' > "$member/.gitignore"
cat > "$member/.claudinite/settings.yaml" <<YAML
engine:
  version: "$version"
  manifest: "$pin"
YAML
cat > "$member/.claude/settings.json" <<'JSON'
{
  "hooks": {
    "SessionStart": [{"hooks": [{"type": "command", "command": "sh \"$CLAUDE_PROJECT_DIR/.claudinite/launch\" hook session-start"}]}],
    "PreToolUse": [{"matcher": "*", "hooks": [{"type": "command", "command": ".claudinite/bin/cn hook pre-tool-use"}]}],
    "PostToolUse": [{"matcher": "*", "hooks": [{"type": "command", "command": ".claudinite/bin/cn hook post-tool-use"}]}],
    "UserPromptSubmit": [{"hooks": [{"type": "command", "command": ".claudinite/bin/cn hook user-prompt-submit"}]}],
    "Stop": [{"hooks": [{"type": "command", "command": ".claudinite/bin/cn hook stop"}]}],
    "SessionEnd": [{"hooks": [{"type": "command", "command": ".claudinite/bin/cn hook session-end"}]}]
  }
}
JSON

# The environment of a Claude Code session on this fixture.
unset GITHUB_ACTIONS
HOME=$work/home
XDG_CACHE_HOME=$work/cache
CLAUDE_PROJECT_DIR=$member
CLAUDINITE_REGISTRY=$(cat "$work/ready")
CURL_CA_BUNDLE=$work/ca.pem
NO_PROXY=127.0.0.1,localhost
export HOME XDG_CACHE_HOME CLAUDE_PROJECT_DIR CLAUDINITE_REGISTRY CURL_CA_BUNDLE NO_PROXY

hook_command() {
  node -e 'const s = require(process.argv[1]); process.stdout.write(s.hooks[process.argv[2]][0].hooks[0].command)' \
    "$member/.claude/settings.json" "$1"
}

step "SessionStart through the wired launcher"
cmd=$(hook_command SessionStart)
out=$(cd "$member" && printf '%s' '{"session_id":"rehearse","cwd":"'"$member"'","hook_event_name":"SessionStart","source":"startup"}' | sh -c "$cmd") \
  || fail "SessionStart command exited non-zero"
printf '%s' "$out" | node -e '
  let s = ""; process.stdin.on("data", d => s += d).on("end", () => {
    const o = JSON.parse(s).hookSpecificOutput;
    if (o.hookEventName !== "SessionStart") throw new Error("hookEventName " + o.hookEventName);
    const lines = o.additionalContext.trimEnd().split("\n");
    if (!o.additionalContext.includes("# Claudinite engine " + process.argv[1])) throw new Error("no hello heading for " + process.argv[1]);
    if (!o.additionalContext.includes("Hello from cn: this rule proves the pinned engine loaded.")) throw new Error("no hello rule");
    if (!/^\[cn\] hooks session-start ok [0-9]+ms$/.test(lines[lines.length - 1])) throw new Error("last line " + lines[lines.length - 1]);
  });' "$version" || fail "SessionStart output: $out"
[ -L "$member/.claudinite/bin/cn" ] || [ -f "$member/.claudinite/bin/cn" ] || fail ".claudinite/bin/cn was not linked"

step "Stop through the linked binary"
cmd=$(hook_command Stop)
err=$(cd "$member" && printf '{}' | sh -c "$cmd" 2>&1 >"$work/stop.out") || fail "Stop command exited non-zero"
[ "$(cat "$work/stop.out")" = "{}" ] || fail "Stop stdout: $(cat "$work/stop.out")"
printf '%s\n' "$err" | grep -Eqx '\[cn\] hooks stop ok [0-9]+ms' || fail "Stop breadcrumb: $err"

step "cn selftest"
(cd "$member" && .claudinite/bin/cn selftest) > "$work/selftest.out" || fail "selftest: $(cat "$work/selftest.out")"
grep -qx "version $version" "$work/selftest.out" || fail "selftest version: $(cat "$work/selftest.out")"

step "a one-character change to the pin is refused"
bad=$(printf '%s' "$pin" | awk '{ c = substr($0, 20, 1); r = (c == "A") ? "B" : "A"; print substr($0, 1, 19) r substr($0, 21) }')
[ "$bad" != "$pin" ] || fail "could not alter the pin"
sed "s|$pin|$bad|" "$member/.claudinite/settings.yaml" > "$work/settings.yaml" && mv "$work/settings.yaml" "$member/.claudinite/settings.yaml"
cmd=$(hook_command SessionStart)
out=$(cd "$member" && printf '{}' | sh -c "$cmd" 2>"$work/refuse.err") || fail "refusal exited non-zero in SessionStart"
case $out in
  "Claudinite refused to run its engine: "*) ;;
  *) fail "flipped pin was not refused: $out" ;;
esac
grep -q "does not match the pin" "$work/refuse.err" || fail "refusal reason: $(cat "$work/refuse.err")"
left=$(ls -A "$XDG_CACHE_HOME/claudinite/$version")
[ -z "$left" ] || fail "cache for $version still holds: $left"

step "ok ($version, $pin)"
