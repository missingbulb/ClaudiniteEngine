#!/bin/sh
# Desktop timing probe (#4): times a Go hook call, a Go-plus-Node hook call,
# a cold and a warm build of a small check program, and the launcher on a
# warm cache, then writes results/<platform>-<date>.md and .json.
#
#   sh rewrite-temp/probe/desktop-timings/run.sh [--runs N] [--out DIR]
#
# Needs Go 1.24+, Node 22+, npm, curl and tar; runs under Git Bash's sh on
# Windows.
set -eu
runs=50
out=
while [ $# -gt 0 ]; do
  case $1 in
    --runs) runs=$2; shift 2 ;;
    --out) out=$2; shift 2 ;;
    *) echo "usage: run.sh [--runs N] [--out DIR]" >&2; exit 2 ;;
  esac
done
case $runs in ''|*[!0-9]*) echo "run.sh: --runs takes a number" >&2; exit 2 ;; esac
cd "$(dirname "$0")/../../.."
root=$(pwd)
[ -n "$out" ] || out=$root/rewrite-temp/probe/desktop-timings/results
case $out in /*|[A-Za-z]:*) ;; *) out=$root/$out ;; esac

exe=
case $(uname -s) in MINGW*|MSYS*|CYGWIN*) exe=.exe ;; esac
cold_runs=$runs
[ "$cold_runs" -le 5 ] || cold_runs=5

work=$(mktemp -d)
stub_pid=
cleanup() {
  [ -n "$stub_pid" ] && kill "$stub_pid" 2>/dev/null
  chmod -R u+w "$work" 2>/dev/null
  rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
log=$work/timings.jsonl
say() { echo "run.sh: $*" >&2; }

go build -o "$work/timeit$exe" ./rewrite-temp/probe/desktop-timings/timeit
go build -o "$work/gonode$exe" ./rewrite-temp/probe/desktop-timings/gonode
CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "-s -w" -o "$work/cn$exe" ./cn
timeit=$work/timeit$exe

say "1/4 cn hook session-start"
(
  XDG_CACHE_HOME=$work/cn-cache
  export XDG_CACHE_HOME
  "$timeit" run --name "cn hook session-start (Go alone)" --runs "$runs" --log "$log" \
    --stdin '{"session_id":"probe","cwd":"/tmp","hook_event_name":"SessionStart","source":"startup"}' \
    -- "$work/cn$exe" hook session-start
)

say "2/4 Go calls Node once"
"$timeit" run --name "Go calls Node once (gonode)" --runs "$runs" --log "$log" -- "$work/gonode$exe"

say "3/4 check program builds"
mkdir -p "$work/checkprog"
cp rewrite-temp/probe/desktop-timings/checkprog/main.go "$work/checkprog/main.go"
printf 'module checkprog\n\ngo 1.24\n' > "$work/checkprog/go.mod"
"$timeit" run --name "checks cold build (fresh GOCACHE, GOPROXY=off)" --runs "$cold_runs" --log "$log" \
  --setup "chmod -R u+w '$work/gocache' 2>/dev/null; rm -rf '$work/gocache'" \
  -- env GOCACHE="$work/gocache" GOPROXY=off GOFLAGS= go -C "$work/checkprog" build -o "$work/checkprog.bin$exe" .
"$timeit" run --name "checks warm rebuild (one file changed)" --runs "$runs" --log "$log" \
  --setup "printf '// changed\n' >> '$work/checkprog/main.go'" \
  -- env GOCACHE="$work/gocache" GOPROXY=off GOFLAGS= go -C "$work/checkprog" build -o "$work/checkprog.bin$exe" .

say "4/4 launcher on a warm cache"
DIST=$work/dist VERSION=1.61001.1 sh dev/release/create/build.sh > "$work/build.out"
pin=$(cat "$work/dist/manifest.integrity")
go build -o "$work/regstub$exe" ./dev/release/verify/stubs/regstub
"$work/regstub$exe" --dist "$work/dist" --ready "$work/ready" --ca-out "$work/ca.pem" &
stub_pid=$!
tries=0
until [ -f "$work/ready" ]; do
  tries=$((tries + 1))
  [ "$tries" -le 100 ] || { say "regstub did not start"; exit 1; }
  sleep 1
done
say "regstub up at $(cat "$work/ready")"
if [ -n "$exe" ] && [ "${GITHUB_ACTIONS:-}" = true ]; then
  # Windows curl is Schannel, which ignores CURL_CA_BUNDLE; on a disposable runner the stub's
  # certificate goes into the machine's Root store, as in release.yml's smoke leg. A person's
  # own machine is left alone and needs the certificate trusted by hand.
  powershell.exe -NoProfile -NonInteractive -Command \
    "Import-Certificate -FilePath '$(cygpath -w "$work/ca.pem")' -CertStoreLocation Cert:\LocalMachine\Root | Out-Null"
  say "stub certificate trusted"
fi
member=$work/member
mkdir -p "$member/.claudinite" "$work/home"
cp cn/packaging/launcher/launch "$member/.claudinite/launch"
printf 'engine:\n  version: "1.61001.1"\n  manifest: "%s"\n' "$pin" > "$member/.claudinite/settings.yaml"
(
  unset GITHUB_ACTIONS
  HOME=$work/home
  XDG_CACHE_HOME=$work/launch-cache
  CLAUDINITE_REGISTRY=$(cat "$work/ready")
  CURL_CA_BUNDLE=$work/ca.pem
  NO_PROXY=127.0.0.1,localhost
  export HOME XDG_CACHE_HOME CLAUDINITE_REGISTRY CURL_CA_BUNDLE NO_PROXY
  sh "$member/.claudinite/launch" env install
  say "first launcher install done; timing the warm cache"
  "$timeit" run --name "launcher, warm cache (sh .claudinite/launch env install)" --runs "$runs" --log "$log" \
    -- sh "$member/.claudinite/launch" env install
)

"$timeit" report --log "$log" --out "$out" --runs "$runs"
say "results in $out"
