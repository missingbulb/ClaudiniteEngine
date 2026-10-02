#!/bin/sh
# Hook latency probe (#37 T6): times cn's per-call hooks over the payloads a
# session sends most, against a throwaway member declaring every canon pack
# of a frozen Claudinite checkout, with no transcript and with a 5 MB one,
# and times the per-call derivation from the tree alone; then writes
# results/<platform>-<date>.md and .json.
#
#   sh probe/hook-latency/run.sh --node DIR [--runs N] [--out DIR] [--budget]
#
# DIR is a Claudinite checkout whose packs/ the member vendors. --budget
# then fails the run when an unnamed PreToolUse call's median passes 50 ms
# or a held call's over the 5 MB transcript passes 250 ms.
set -eu
runs=50
out=
node=
budget=
while [ $# -gt 0 ]; do
  case $1 in
    --node) node=$2; shift 2 ;;
    --runs) runs=$2; shift 2 ;;
    --out) out=$2; shift 2 ;;
    --budget) budget=1; shift ;;
    *) echo "usage: run.sh --node DIR [--runs N] [--out DIR] [--budget]" >&2; exit 2 ;;
  esac
done
case $runs in ''|*[!0-9]*) echo "run.sh: --runs takes a number" >&2; exit 2 ;; esac
[ -d "$node/packs" ] || { echo "run.sh: --node names no Claudinite checkout with packs/" >&2; exit 2; }
node=$(cd "$node" && pwd)
cd "$(dirname "$0")/../.."
root=$(pwd)
[ -n "$out" ] || out=$root/probe/hook-latency/results
case $out in /*|[A-Za-z]:*) ;; *) out=$root/$out ;; esac

exe=
case $(uname -s) in MINGW*|MSYS*|CYGWIN*) exe=.exe ;; esac

work=$(mktemp -d)
cleanup() {
  chmod -R u+w "$work" 2>/dev/null
  rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
log=$work/timings.jsonl
say() { echo "run.sh: $*" >&2; }

go build -o "$work/timeit$exe" ./probe/desktop-timings/timeit
go build -o "$work/derive$exe" ./probe/hook-latency/derive
CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "-s -w" -o "$work/cn$exe" ./cmd/cn
timeit=$work/timeit$exe
cn=$work/cn$exe

member=$work/member
mkdir -p "$member/.claudinite/shared/packs"
{
  printf 'engine:\n  version: "0.0.0"\n  manifest: "sha512-%s=="\npacks:\n  declared:\n' \
    AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
  for dir in "$node"/packs/*/; do
    id=$(basename "$dir")
    [ -f "$dir/pack.json" ] || continue
    cp -R "$dir" "$member/.claudinite/shared/packs/$id"
    printf '    - %s\n' "$id"
  done
} > "$work/settings.yaml"
mv "$work/settings.yaml" "$member/.claudinite/settings.yaml"
packs=$(grep -c '^    - ' "$member/.claudinite/settings.yaml")
mkdir -p "$member/docs/x"
printf '# x\n' > "$member/docs/x/DESIGN.md"
printf '# member\n' > "$member/README.md"
(cd "$member" && git init -q -b main && git add -A \
  && git -c user.name=probe -c user.email=p@x -c commit.gpgsign=false commit -q -m base)

# A 5 MB transcript: owner turns and tool calls, a quarter of its lines a
# load of a skill no trigger here names.
big=$work/big.jsonl
line1='{"type":"user","message":{"role":"user","content":"please keep going with the change and say when it is done"}}'
line2='{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Comment class: other"},{"type":"tool_use","id":"t","name":"Bash","input":{"command":"ls -la && git status --short && go test ./... 2>&1 | tail -20"}}]}}'
line3='{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","is_error":false,"content":"ok  github.com/acme/member  0.42s\nok  github.com/acme/member/sub  0.13s"}]}}'
line4='{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"s","name":"Skill","input":{"skill":"probe-unrelated"}}]}}'
printf '%s\n%s\n%s\n%s\n' "$line1" "$line2" "$line3" "$line4" > "$work/chunk.jsonl"
for _ in 1 2 3 4 5 6 7 8 9 10 11; do cat "$work/chunk.jsonl" "$work/chunk.jsonl" > "$work/chunk2.jsonl"; mv "$work/chunk2.jsonl" "$work/chunk.jsonl"; done
: > "$big"
while [ "$(wc -c < "$big")" -lt 5242880 ]; do cat "$work/chunk.jsonl" >> "$big"; done
say "member: $packs packs; transcript $(wc -c < "$big" | tr -d ' ') bytes"

XDG_CACHE_HOME=$work/cache CLAUDE_PROJECT_DIR=$member CLAUDINITE_CHECKS_NO_FETCH=1
export XDG_CACHE_HOME CLAUDE_PROJECT_DIR CLAUDINITE_CHECKS_NO_FETCH
(cd "$member" && printf '{"session_id":"probe","hook_event_name":"SessionStart","source":"startup"}' | "$cn" hook session-start > /dev/null 2>&1) || true

# The session's license state is a key request in flight, in which every
# surface runs, forced skill loading included; it is written afresh,
# untimed, before each call, as the state's own clock would otherwise let
# it lapse.
sessions=$XDG_CACHE_HOME/claudinite/sessions
mkdir -p "$sessions"
chmod 0700 "$XDG_CACHE_HOME/claudinite" "$sessions"
pending="printf '{\"v\":1,\"session_id\":\"probe\",\"repo\":\"\",\"mode\":\"web\",\"nonce\":\"probe\",\"requested_at\":\"%s\",\"state\":\"pending\"}' \"\$(date -u +%Y-%m-%dT%H:%M:%SZ)\" > '$sessions/probe.json'"

# time NAME EVENT PAYLOAD TRANSCRIPT WANT: cn hook EVENT over PAYLOAD, each
# call exiting WANT (2 for a block).
time_hook() {
  stdin="{\"session_id\":\"probe\",\"cwd\":\"$member\",\"transcript_path\":\"$4\",$3}"
  "$timeit" run --name "$1" --runs "$runs" --log "$log" --exit "$5" --setup "$pending" --stdin "$stdin" -- "$cn" hook "$2"
}

say "1/3 the derivation alone"
"$work/derive$exe" --repo "$member" --runs "$runs" | "$timeit" samples --name "derivation from the tree (in process)" --log "$log"

n=0
for t in none big; do
  n=$((n + 1))
  tp=$work/none.jsonl
  label="no transcript"
  if [ "$t" = big ]; then
    tp=$big
    label="5 MB transcript"
  fi
  say "$((n + 1))/3 the hooks, $label"
  time_hook "pre-tool-use Read, no declaration ($label)" pre-tool-use '"tool_name":"Read","tool_input":{"file_path":"README.md"}' "$tp" 0
  time_hook "pre-tool-use Bash plain ($label)" pre-tool-use '"tool_name":"Bash","tool_input":{"command":"ls -la"}' "$tp" 0
  time_hook "pre-tool-use Bash held, git commit ($label)" pre-tool-use '"tool_name":"Bash","tool_input":{"command":"git commit -m x"}' "$tp" 2
  time_hook "pre-tool-use Edit under a scoped path ($label)" pre-tool-use '"tool_name":"Edit","tool_input":{"file_path":"docs/x/DESIGN.md","old_string":"x","new_string":"y"}' "$tp" 2
  time_hook "pre-tool-use mcp tool ($label)" pre-tool-use '"tool_name":"mcp__github__issue_read","tool_input":{"method":"get"}' "$tp" 0
  time_hook "post-tool-use Bash result ($label)" post-tool-use '"tool_name":"Bash","tool_input":{"command":"ls"},"tool_response":{"stdout":"ok","stderr":""}' "$tp" 0
  time_hook "user-prompt-submit prompt ($label)" user-prompt-submit '"prompt":"please explain the design"' "$tp" 0
done

"$timeit" report --log "$log" --out "$out" --runs "$runs" --title "Hook latency ($packs packs)" --command "sh probe/hook-latency/run.sh --node <Claudinite checkout> --runs $runs"
say "results in $out"
if [ -n "$budget" ]; then
  "$timeit" budget --log "$log" \
    --max "pre-tool-use Read, no declaration (no transcript)=50" \
    --max "pre-tool-use Bash plain (no transcript)=50" \
    --max "pre-tool-use mcp tool (no transcript)=50" \
    --max "pre-tool-use Bash held, git commit (5 MB transcript)=250" \
    --max "pre-tool-use Edit under a scoped path (5 MB transcript)=250"
  say "within the hook budgets"
fi
