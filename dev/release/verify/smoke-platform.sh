#!/bin/sh
# One platform leg of the smoke matrix: a fixture member in a temp dir runs
# the real launcher against a registry, through the hook commands its
# .claude/settings.json wires (SessionStart, one guarded PreToolUse call,
# Stop), then cn selftest, then proves a one-character change to the pin is
# refused and leaves nothing cached. Needs node and curl.
#
#   dev/release/verify/smoke-platform.sh --registry URL --channel C --version V --pin SHA512
#                             [--dist DIR] [--platform P] [--keep]
#
# The member is pinned to @claudinite/cli V on the engine channel C
# (stable, canary or staging). --dist is the release the registry serves, checked for this platform's
# binary first; --platform names the leg (default: this host's); --keep
# leaves the temp dir for debugging. A registry with a self-signed
# certificate needs the caller to make curl trust it: CURL_CA_BUNDLE, or on
# Windows, whose Schannel curl ignores that variable, the system Root store.
set -eu
cd "$(dirname "$0")/../../.."

fail() { echo "smoke-platform: FAIL: $*" >&2; exit 1; }
step() { echo "smoke-platform: $*"; }
usage() { fail "usage: dev/release/verify/smoke-platform.sh --registry URL --channel C --version V --pin SHA512 [--dist DIR] [--platform P] [--keep]"; }

registry='' channel='' version='' pin='' dist='' platform='' keep=false
while [ $# -gt 0 ]; do
  case $1 in
    --registry|--channel|--version|--pin|--dist|--platform)
      [ $# -ge 2 ] || usage
      case $1 in
        --registry) registry=$2 ;;
        --channel) channel=$2 ;;
        --version) version=$2 ;;
        --pin) pin=$2 ;;
        --dist) dist=$2 ;;
        --platform) platform=$2 ;;
      esac
      shift 2 ;;
    --keep) keep=true; shift ;;
    *) usage ;;
  esac
done
for v in "$registry" "$channel" "$version" "$pin"; do
  [ -n "$v" ] || usage
done

step "uname -s: $(uname -s), uname -m: $(uname -m)"
case $(uname -s) in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  MINGW*|MSYS*|CYGWIN*) os=windows ;;
  *) os=unknown ;;
esac
case $(uname -m) in
  x86_64|amd64) arch=x64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) arch=unknown ;;
esac
host=$os-$arch
[ -n "$platform" ] || platform=$host
bin=cn
case $platform in windows-*) bin=cn.exe ;; esac
if [ -n "$dist" ]; then
  [ -f "$dist/bin/$platform/$bin" ] || fail "no $platform binary in $dist/bin/$platform"
fi
[ "$platform" = "$host" ] || fail "the $platform leg runs on a $platform host; this is $host"
step "leg $platform ($(uname -s) $(uname -m)), @claudinite/cli $version ($channel) from $registry"

work=$(mktemp -d)
cleanup() {
  if [ "$keep" = true ]; then
    echo "smoke-platform: kept $work"
  else
    chmod -R u+w "$work" 2>/dev/null
    rm -rf "$work"
  fi
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

member=$work/member
mkdir -p "$work/home" "$work/cache"
sh dev/release/verify/fixtures/member-fixture.sh "$member" "$version" "$pin" "$channel"

# The environment of a Claude Code session on this fixture.
unset GITHUB_ACTIONS
HOME=$work/home
XDG_CACHE_HOME=$work/cache
CLAUDE_PROJECT_DIR=$member
CLAUDINITE_REGISTRY=$registry
NO_PROXY=${NO_PROXY:-127.0.0.1,localhost}
export HOME XDG_CACHE_HOME CLAUDE_PROJECT_DIR CLAUDINITE_REGISTRY NO_PROXY

hook_command() {
  node -e 'const s = require(process.argv[1]); process.stdout.write(s.hooks[process.argv[2]][0].hooks[0].command)' \
    "$member/.claude/settings.json" "$1"
}

# guarded EVENT STDIN: runs a wired guard hook; stdout must be {} and the
# last stderr line its ok breadcrumb.
guarded() {
  cmd=$(hook_command "$1")
  err=$(cd "$member" && printf '%s' "$3" | sh -c "$cmd" 2>&1 >"$work/hook.out") || fail "$1 command exited non-zero: $err"
  [ "$(cat "$work/hook.out")" = "{}" ] || fail "$1 stdout: $(cat "$work/hook.out")"
  printf '%s\n' "$err" | grep -Eqx "\[cn\] hooks $2 ok [0-9]+ms" || fail "$1 breadcrumb: $err"
}

step "SessionStart through the wired launcher"
cmd=$(hook_command SessionStart)
out=$(cd "$member" && printf '%s' '{"session_id":"smoke","cwd":"'"$member"'","hook_event_name":"SessionStart","source":"startup"}' | sh -c "$cmd") \
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
[ -L "$member/.claudinite/bin/cn" ] || [ -f "$member/.claudinite/bin/$bin" ] || fail ".claudinite/bin/$bin was not linked"

step "pre-tool-use for a Bash call through the linked binary"
guarded PreToolUse pre-tool-use '{"session_id":"smoke","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls"}}'

step "Stop through the linked binary"
guarded Stop stop '{"session_id":"smoke","hook_event_name":"Stop"}'

step "cn selftest"
(cd "$member" && .claudinite/bin/cn selftest --repo .) > "$work/selftest.out" || fail "selftest: $(cat "$work/selftest.out")"
cat "$work/selftest.out"
grep -qx "version $version" "$work/selftest.out" || fail "selftest version: $(cat "$work/selftest.out")"
grep -qx "ok binary: $platform" "$work/selftest.out" || fail "selftest platform: $(cat "$work/selftest.out")"
grep -qx "ok hooks: .*" "$work/selftest.out" || fail "selftest did not probe the member's hooks: $(cat "$work/selftest.out")"

step "a one-character change to the pin"
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
step "pin change refused"

step "ok ($platform, @claudinite/cli $version, $pin)"
