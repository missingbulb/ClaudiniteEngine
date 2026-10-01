#!/bin/sh
# The local Phase 1 and 2 gates, against the built release in $DIST
# (default dist/) served by regstub, in four modes:
#
#   fresh    the host's smoke leg (release/smoke-platform.sh) on a new member
#   current  a member warm on this release moves its pin to the next ordinal
#            (rebuilt from the same source into dist2/): SessionStart fetches
#            it and the old version's cache stays untouched
#   stale    a member warm on this release moves its pin to a version the
#            registry does not serve: SessionStart halts, and the guards
#            still run the verified engine linked before
#   update   cn update engine, against regstub and release/ghstub, moves a
#            member with a bare origin from this release to dist2/ by a
#            merged update PR, then refuses a dist3/ whose verify breaks
#            the member (built with the rehearsal_break tag), holds back on
#            a red main, skips held and revoked versions and files the
#            revoked pin's issue. Every dist is signed with the development
#            keys, as the updater checks signatures.
#
#   release/rehearse.sh [--mode fresh|current|stale|update]   (default: all four)
#
# Needs go, node, curl, git and a release from release/build.sh.
set -eu
cd "$(dirname "$0")/.."
root=$(pwd)
DIST=${DIST:-dist}
case $DIST in /*) ;; *) DIST=$root/$DIST ;; esac

fail() { echo "rehearse: FAIL: $*" >&2; exit 1; }
step() { echo "rehearse: $*"; }

usage="usage: release/rehearse.sh [--mode fresh|current|stale|update]"
modes="fresh current stale update"
case $# in
  0) ;;
  2)
    [ "$1" = --mode ] || fail "$usage"
    case $2 in fresh|current|stale|update) modes=$2 ;; *) fail "unknown mode $2" ;; esac ;;
  *) fail "$usage" ;;
esac

[ -f "$DIST/manifest.integrity" ] || fail "no release in $DIST; run release/build.sh first"
DIST=$DIST sh release/smoke.sh
version=$(sed -n 's/^  "version": "\(.*\)",$/\1/p' "$DIST/manifest.json")
pin=$(cat "$DIST/manifest.integrity")
package=
for d in "$DIST"/npm/*/package; do
  [ -f "$d/manifest.json" ] && package=@claudinite/$(basename "$(dirname "$d")")
done
[ -n "$package" ] || fail "no npm package in $DIST carries manifest.json"

work=$(mktemp -d)
stub_pid=
gh_pid=
cleanup() {
  [ -n "$stub_pid" ] && kill "$stub_pid" 2>/dev/null
  [ -n "$gh_pid" ] && kill "$gh_pid" 2>/dev/null
  chmod -R u+w "$work" 2>/dev/null
  rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

next=$(printf '%s\n' "$version" | awk -F. '{ print $1 "." $2 + 1 "." $3 }')
third=$(printf '%s\n' "$version" | awk -F. '{ print $1 "." $2 + 2 "." $3 }')
dist1=$DIST
dist2=$work/dist2
dist3=$work/dist3
# dist3 is built aside and served only from update step 5 on.
dist3build=$work/dist3-build
case " $modes " in
  *" current "*|*" update "*)
    step "building $next into dist2/ from the same source"
    DIST=$dist2 VERSION=$next PACKAGE=$package sh release/build.sh > "$work/build2.out" || fail "build of $next: $(cat "$work/build2.out")"
    ;;
  *) mkdir -p "$dist2/tarballs" ;;
esac
mkdir -p "$dist3/tarballs"
case " $modes " in
  *" update "*)
    step "building $third into dist3/ with verify broken (rehearsal_break)"
    DIST=$dist3build VERSION=$third PACKAGE=$package REHEARSAL=1 BUILD_TAGS=rehearsal_break sh release/build.sh > "$work/build3.out" \
      || fail "build of $third: $(cat "$work/build3.out")"
    # The caller's dist stays as it was: sign a copy.
    dist1=$work/dist1
    cp -R "$DIST" "$dist1"
    for d in "$dist1" "$dist2" "$dist3build"; do
      DIST=$d sh release/sign.sh > "$work/sign.out" 2>&1 || fail "signing $d: $(cat "$work/sign.out")"
    done
    ;;
esac
printf '{}\n' > "$work/deprecations.json"

go build -o "$work/regstub" ./release/regstub
"$work/regstub" --dist "$dist1" --dist "$dist2" --dist "$dist3" --deprecations "$work/deprecations.json" \
  --ready "$work/ready" --ca-out "$work/ca.pem" --log "$work/requests.log" &
stub_pid=$!
tries=0
until [ -f "$work/ready" ]; do
  tries=$((tries + 1))
  [ "$tries" -le 100 ] || fail "regstub did not start"
  sleep 0.1
done
CURL_CA_BUNDLE=$work/ca.pem
NO_PROXY=127.0.0.1,localhost
export CURL_CA_BUNDLE NO_PROXY

registry=$(cat "$work/ready")

# warm_member NAME: a member pinned to this release, its cache warmed and
# its engine linked by one SessionStart.
warm_member() {
  member=$work/$1
  mkdir -p "$member-home" "$member-cache"
  sh release/member-fixture.sh "$member" "$version" "$pin" "$package"
  HOME=$member-home XDG_CACHE_HOME=$member-cache
  export HOME XDG_CACHE_HOME
  out=$(session_start) || fail "$1: SessionStart exited non-zero"
  case $out in *"# Claudinite engine $version"*) ;; *) fail "$1: SessionStart on $version: $out" ;; esac
}

# repin VERSION INTEGRITY: moves the member's pin.
repin() {
  sed -e "s|^  version: .*|  version: \"$1\"|" -e "s|^  manifest: .*|  manifest: \"$2\"|" \
    "$member/.claudinite/settings.yaml" > "$work/settings.yaml"
  mv "$work/settings.yaml" "$member/.claudinite/settings.yaml"
}

hook_command() {
  node -e 'const s = require(process.argv[1]); process.stdout.write(s.hooks[process.argv[2]][0].hooks[0].command)' \
    "$member/.claude/settings.json" "$1"
}

session_start() {
  cmd=$(hook_command SessionStart)
  (cd "$member" && printf '%s' '{"session_id":"rehearse","hook_event_name":"SessionStart","source":"startup"}' \
    | CLAUDE_PROJECT_DIR=$member sh -c "$cmd")
}

# cache_state DIR: every file under DIR with its hash and mode.
cache_state() {
  find "$1" -type f | sort | while IFS= read -r f; do
    # shellcheck disable=SC2012 # ls -ln is the portable way to read a mode
    printf '%s %s %s\n' "$f" "$(sha256sum < "$f" | cut -d' ' -f1)" "$(ls -ln "$f" | cut -d' ' -f1)"
  done
}

unset GITHUB_ACTIONS
CLAUDINITE_REGISTRY=$registry
export CLAUDINITE_REGISTRY

for mode in $modes; do
  case $mode in
    fresh)
      step "fresh: the host's smoke leg"
      sh release/smoke-platform.sh --registry "$registry" --package "$package" --version "$version" --pin "$pin" --dist "$DIST" \
        || fail "the smoke leg failed"
      ;;
    current)
      step "current: a member warm on $version moves its pin to $next"
      warm_member current
      before=$(cache_state "$XDG_CACHE_HOME/claudinite/$version")
      [ -n "$before" ] || fail "current: nothing cached for $version"
      : > "$work/requests.log"
      repin "$next" "$(cat "$dist2/manifest.integrity")"
      out=$(session_start) || fail "current: SessionStart on $next exited non-zero"
      case $out in *"# Claudinite engine $next"*) ;; *) fail "current: SessionStart on $next: $out" ;; esac
      grep -q "/-/${package#@claudinite/}-$next.tgz" "$work/requests.log" || fail "current: $next was not fetched: $(cat "$work/requests.log")"
      step "current: SessionStart fetched $next"
      [ "$(cache_state "$XDG_CACHE_HOME/claudinite/$version")" = "$before" ] || fail "current: the $version cache changed"
      case $(ls -l "$member/.claudinite/bin/cn") in *"/claudinite/$next/cn") ;; *) fail "current: .claudinite/bin/cn is not linked to $next" ;; esac
      step "current: the old cache is untouched"
      ;;
    stale)
      step "stale: a member warm on $version moves its pin to a version nobody serves"
      warm_member stale
      missing=$(printf '%s\n' "$version" | awk -F. '{ print $1 "." $2 + 1000 "." $3 }')
      repin "$missing" "$pin"
      out=$(session_start) || fail "stale: SessionStart exited non-zero"
      case $out in
        "Claudinite could not fetch its engine from "*": stop and ask the person before continuing.") ;;
        *) fail "stale: SessionStart did not halt: $out" ;;
      esac
      step "stale: SessionStart halted ($out)"
      cmd=$(hook_command Stop)
      err=$(cd "$member" && printf '{}' | sh -c "$cmd" 2>&1 >"$work/stop.out") || fail "stale: Stop exited non-zero"
      [ "$(cat "$work/stop.out")" = "{}" ] || fail "stale: Stop stdout: $(cat "$work/stop.out")"
      printf '%s\n' "$err" | grep -Eqx '\[cn\] hooks stop ok [0-9]+ms' || fail "stale: Stop breadcrumb: $err"
      case $(ls -l "$member/.claudinite/bin/cn") in *"/claudinite/$version/cn") ;; *) fail "stale: .claudinite/bin/cn moved off $version" ;; esac
      step "stale: Stop ran the cached $version engine"
      ;;
    update)
      step "update: a member on $version with a bare origin, and the GitHub stub"
      warm_member update
      origin=$work/origin.git
      git init -q --bare -b main "$origin"
      (cd "$member" && git init -q -b main && git add -A && git -c user.name=rehearse -c user.email=r@x commit -q -m adopt \
        && git remote add origin "$origin" && git push -q origin main) || fail "update: git setup"
      go build -o "$work/ghstub" ./release/ghstub
      "$work/ghstub" --origin "$origin" --repo acme/member --token rehearsal-token --ready "$work/gh-ready" --ca-out "$work/gh-ca.pem" &
      gh_pid=$!
      tries=0
      until [ -f "$work/gh-ready" ]; do
        tries=$((tries + 1))
        [ "$tries" -le 100 ] || fail "ghstub did not start"
        sleep 0.1
      done
      gh=$(cat "$work/gh-ready")
      cat "$work/ca.pem" "$work/gh-ca.pem" > "$work/cas.pem"
      SSL_CERT_FILE=$work/cas.pem CURL_CA_BUNDLE=$work/cas.pem
      GITHUB_REPOSITORY=acme/member CLAUDINITE_GITHUB_API=$gh
      export SSL_CERT_FILE CURL_CA_BUNDLE GITHUB_REPOSITORY CLAUDINITE_GITHUB_API

      # ctl PATH JSON: a control call on the stub.
      ctl() { curl -sS --fail -X POST -H 'Content-Type: application/json' -d "$2" "$gh$1" > /dev/null || fail "ghstub $1 $2"; }
      gh_state() { curl -sS --fail "$gh/_stub/state"; }
      # gh_count EXPR: a number read from the stub's state with node.
      gh_count() { gh_state | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>{const st=JSON.parse(s);process.stdout.write(String(eval(process.argv[1])))})' "$1"; }
      # cn_member ARGS: the member's linked engine, run in the checkout as
      # the workflow runs it.
      cn_member() { (cd "$member" && GITHUB_TOKEN=rehearsal-token .claudinite/bin/cn "$@"); }
      # update_engine: the update; its verdict is the last stdout line.
      update_engine() {
        cn_member update engine > "$work/update.out" 2> "$work/update.err" || fail "update engine: $(cat "$work/update.out" "$work/update.err")"
        verdict=$(sed -n '$p' "$work/update.out")
      }
      expect_verdict() { [ "$verdict" = "$1" ] || fail "update: verdict '$verdict', want '$1': $(cat "$work/update.out" "$work/update.err")"; }
      main_run() { ctl /_stub/run "{\"ref\":\"main\",\"conclusion\":\"$1\"}"; }
      deprecate() { printf '%s\n' "$1" > "$work/deprecations.json"; }
      branch=claudinite/engine-$next

      main_run success
      update_engine
      expect_verdict "opened #1 for $next"
      [ "$(git --git-dir "$origin" rev-list --count "main..$branch")" = 1 ] || fail "update 1: $branch is not one commit on main"
      [ "$(git --git-dir "$origin" diff --name-only main "$branch")" = .claudinite/settings.yaml ] || fail "update 1: the branch changes more than the pin"
      [ "$(gh_count 'st.dispatches.filter(d=>d.ref==="'"$branch"'"&&d.inputs.pr==="1").length')" = 1 ] || fail "update 1: dispatches $(gh_state)"
      step "update 1: $verdict"

      head=$(git --git-dir "$origin" rev-parse "$branch")
      (cd "$member" && git fetch -q origin && git checkout -q "$head") || fail "update 2: checkout"
      (cd "$member" && sh .claudinite/launch version > /dev/null) || fail "update 2: the launcher did not link $next"
      cn_member check world --pr-author 'github-actions[bot]' --base-ref origin/main > "$work/world.out" 2>&1 || fail "update 2: check world refused the bot: $(cat "$work/world.out")"
      if cn_member check world --pr-author someone --base-ref origin/main > "$work/world.out" 2>&1; then fail "update 2: check world passed a person's pin move"; fi
      grep -q pin-guard "$work/world.out" || fail "update 2: the refusal names no rule: $(cat "$work/world.out")"
      (cd "$member" && git checkout -q main && sh .claudinite/launch version > /dev/null) || fail "update 2: back to main"
      step "update 2: check world passes the bot's pin and refuses a person's"

      ctl /_stub/run "{\"sha\":\"$head\",\"event\":\"workflow_dispatch\",\"conclusion\":\"success\"}"
      cn_member update land --pr 1 --sha "$head" > "$work/land.out" 2>&1 || fail "update 3: land: $(cat "$work/land.out")"
      verdict=$(sed -n '$p' "$work/land.out")
      expect_verdict "landed $next"
      [ -z "$(git --git-dir "$origin" branch --list "$branch")" ] || fail "update 3: $branch was not deleted"
      [ "$(gh_count 'st.dispatches.filter(d=>d.ref==="main").length')" = 1 ] || fail "update 3: no dispatch on main: $(gh_state)"
      (cd "$member" && git fetch -q origin && git reset -q --hard origin/main) || fail "update 3: pull"
      grep -q "version: \"$next\"" "$member/.claudinite/settings.yaml" || fail "update 3: main does not pin $next"
      step "update 3: $verdict"

      : > "$work/requests.log"
      out=$(session_start) || fail "update 4: SessionStart exited non-zero"
      case $out in *"# Claudinite engine $next"*) ;; *) fail "update 4: SessionStart on $next: $out" ;; esac
      [ ! -s "$work/requests.log" ] || fail "update 4: SessionStart downloaded: $(cat "$work/requests.log")"
      step "update 4: SessionStart runs $next with no download"

      cp "$dist3build"/tarballs/* "$dist3/tarballs/"
      main_run success
      update_engine
      expect_verdict "no PR: $third would break this repo"
      grep -q '^break rehearsal ' "$work/update.out" || fail "update 5: the finding was not printed: $(cat "$work/update.out")"
      [ -z "$(git --git-dir "$origin" branch --list 'claudinite/*')" ] || fail "update 5: a branch was pushed"
      [ "$(gh_count 'st.pulls.length')" = 1 ] || fail "update 5: a PR was opened"
      step "update 5: $verdict"

      main_run failure
      : > "$work/requests.log"
      update_engine
      expect_verdict "skipped: main is not green (failure)"
      [ ! -s "$work/requests.log" ] || fail "update 6: npm was read: $(cat "$work/requests.log")"
      step "update 6: $verdict"

      main_run success
      deprecate "{\"$third\": \"held: rehearsal\"}"
      update_engine
      expect_verdict "up to date"
      grep -q "^$third skipped: held" "$work/update.out" || fail "update 7: no held skip: $(cat "$work/update.out")"
      step "update 7: held $third skipped"
      deprecate "{\"$third\": \"revoked: rehearsal\"}"
      update_engine
      expect_verdict "up to date"
      grep -q "^$third skipped: revoked" "$work/update.out" || fail "update 7: no revoked skip: $(cat "$work/update.out")"
      [ "$(gh_count 'st.issues ? st.issues.length : 0')" = 0 ] || fail "update 7: an issue for a version this repo does not pin"
      step "update 7: revoked $third skipped"
      deprecate "{\"$third\": \"revoked: rehearsal\", \"$next\": \"revoked: rehearsal\"}"
      update_engine
      update_engine
      expect_verdict "up to date"
      [ "$(gh_count 'st.issues.filter(i=>i.title==="Claudinite engine '"$next"' is revoked").length')" = 1 ] || fail "update 7: issues $(gh_state)"
      [ "$(gh_count 'st.issues.length')" = 1 ] || fail "update 7: issues $(gh_state)"
      step "update 7: one issue for the revoked pin $next, kept by two runs"

      old=$work/old-shape
      sh release/member-fixture.sh "$old" "$next" "$(cat "$dist2/manifest.integrity")" "$package"
      rm "$old/.claudinite/.gitignore"
      printf '.claudinite/bin/\n' > "$old/.gitignore"
      cn_member verify --repo "$old" > "$work/verify.out" 2>&1 || fail "update 8: verify: $(cat "$work/verify.out")"
      if [ "$(grep -c '^deprecation bin-ignore ' "$work/verify.out")" != 1 ] || [ "$(wc -l < "$work/verify.out" | tr -d ' ')" != 1 ]; then
        fail "update 8: want one bin-ignore deprecation: $(cat "$work/verify.out")"
      fi
      step "update 8: the old shape is one deprecation"
      ;;
  esac
done

step "ok ($package $version, $pin)"
