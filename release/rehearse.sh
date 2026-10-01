#!/bin/sh
# The local Phase 1, 2 and 3 gates, against the built release in $DIST
# (default dist/) served by regstub, in five modes:
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
#   packs    a fresh repo adopts the hello pack through the npx bootstrap
#            from a local pack source (release/packs-fixture.sh, served by
#            release/cdnstub and read as the vendored branch), its session
#            loads the pack and its Go check blocks Stop, and cn update
#            packs proposes, refuses, skips and lands pack versions as the
#            fixture publishes and revokes them, then refuses an index whose
#            serial regressed or whose signature broke.
#
#   release/rehearse.sh [--mode fresh|current|stale|update|packs]   (default: all five)
#
# UPDATE_STEPS=4 stops the update mode after the landing and the session on
# the landed version (release/hop.sh); the default, 8, runs every step.
#
# Needs go, node, curl, git and a release from release/build.sh.
set -eu
cd "$(dirname "$0")/.."
root=$(pwd)
DIST=${DIST:-dist}
case $DIST in /*) ;; *) DIST=$root/$DIST ;; esac
# The modes move HOME; the Go builds keep the caller's build cache.
GOCACHE=${GOCACHE:-$(go env GOCACHE)}
export GOCACHE

fail() { echo "rehearse: FAIL: $*" >&2; exit 1; }
step() { echo "rehearse: $*"; }

usage="usage: release/rehearse.sh [--mode fresh|current|stale|update|packs]"
modes="fresh current stale update packs"
case $# in
  0) ;;
  2)
    [ "$1" = --mode ] || fail "$usage"
    case $2 in fresh|current|stale|update|packs) modes=$2 ;; *) fail "unknown mode $2" ;; esac ;;
  *) fail "$usage" ;;
esac

update_steps=${UPDATE_STEPS:-8}
case $update_steps in 4|8) ;; *) fail "UPDATE_STEPS must be 4 or 8, not $update_steps" ;; esac

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
cdn_pids=
cleanup() {
  [ -n "$stub_pid" ] && kill "$stub_pid" 2>/dev/null
  [ -n "$gh_pid" ] && kill "$gh_pid" 2>/dev/null
  for p in $cdn_pids; do kill "$p" 2>/dev/null; done
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
    if [ "$update_steps" -ge 5 ]; then
      step "building $third into dist3/ with verify broken (rehearsal_break)"
      DIST=$dist3build VERSION=$third PACKAGE=$package REHEARSAL=1 BUILD_TAGS=rehearsal_break sh release/build.sh > "$work/build3.out" \
        || fail "build of $third: $(cat "$work/build3.out")"
      DIST=$dist3build sh release/sign.sh > "$work/sign.out" 2>&1 || fail "signing $third: $(cat "$work/sign.out")"
    fi
    DIST=$dist2 sh release/sign.sh > "$work/sign.out" 2>&1 || fail "signing $dist2: $(cat "$work/sign.out")"
    ;;
esac
case " $modes " in
  *" update "*|*" packs "*)
    # The caller's dist stays as it was: sign a copy.
    dist1=$work/dist1
    cp -R "$DIST" "$dist1"
    DIST=$dist1 sh release/sign.sh > "$work/sign.out" 2>&1 || fail "signing $dist1: $(cat "$work/sign.out")"
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

# start_ghstub ORIGIN: the GitHub stub over a bare origin; sets gh.
start_ghstub() {
  [ -x "$work/ghstub" ] || go build -o "$work/ghstub" ./release/ghstub
  [ -n "$gh_pid" ] && kill "$gh_pid" 2>/dev/null
  rm -f "$work/gh-ready"
  "$work/ghstub" --origin "$1" --repo acme/member --token rehearsal-token --ready "$work/gh-ready" --ca-out "$work/gh-ca.pem" &
  gh_pid=$!
  tries=0
  until [ -f "$work/gh-ready" ]; do
    tries=$((tries + 1))
    [ "$tries" -le 100 ] || fail "ghstub did not start"
    sleep 0.1
  done
  gh=$(cat "$work/gh-ready")
}
# ctl PATH JSON: a control call on the stub.
ctl() { curl -sS --fail -X POST -H 'Content-Type: application/json' -d "$2" "$gh$1" > /dev/null || fail "ghstub $1 $2"; }
gh_state() { curl -sS --fail "$gh/_stub/state"; }
# gh_count EXPR: a number read from the stub's state with node.
gh_count() { gh_state | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>{const st=JSON.parse(s);process.stdout.write(String(eval(process.argv[1])))})' "$1"; }
# cn_member ARGS: the member's linked engine, run in the checkout as the
# workflow runs it.
cn_member() { (cd "$member" && GITHUB_TOKEN=rehearsal-token .claudinite/bin/cn "$@"); }
main_run() { ctl /_stub/run "{\"ref\":\"main\",\"conclusion\":\"$1\"}"; }

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
      start_ghstub "$origin"
      cat "$work/ca.pem" "$work/gh-ca.pem" > "$work/cas.pem"
      SSL_CERT_FILE=$work/cas.pem CURL_CA_BUNDLE=$work/cas.pem
      GITHUB_REPOSITORY=acme/member CLAUDINITE_GITHUB_API=$gh
      export SSL_CERT_FILE CURL_CA_BUNDLE GITHUB_REPOSITORY CLAUDINITE_GITHUB_API

      # update_engine: the update; its verdict is the last stdout line.
      update_engine() {
        cn_member update engine > "$work/update.out" 2> "$work/update.err" || fail "update engine: $(cat "$work/update.out" "$work/update.err")"
        verdict=$(sed -n '$p' "$work/update.out")
      }
      expect_verdict() { [ "$verdict" = "$1" ] || fail "update: verdict '$verdict', want '$1': $(cat "$work/update.out" "$work/update.err")"; }
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
      if [ "$update_steps" -eq 4 ]; then
        step "update: stopped after step 4 (UPDATE_STEPS=4)"
        continue
      fi

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
    packs)
      step "packs: a local pack source with hello 1.0, the CDN stub and the GitHub stub"
      src=$work/packsrc
      sh release/packs-fixture.sh "$src" --min-engine "$version" > "$work/fixture.out" 2>&1 || fail "packs: fixture: $(cat "$work/fixture.out")"
      fixture() { sh release/packs-fixture.sh "$src" "$@" > "$work/fixture.out" 2>&1 || fail "packs: fixture $*: $(cat "$work/fixture.out")"; }
      go build -o "$work/cdnstub" ./release/cdnstub
      # start_cdn NAME [--down]: a CDN stub over the fixture; prints its URL.
      start_cdn() {
        name=$1
        shift
        "$work/cdnstub" --repo "$src/cdn.git" --ready "$work/$name-ready" --ca-out "$work/$name-ca.pem" --log "$work/$name.log" "$@" &
        cdn_pids="$cdn_pids $!"
        tries=0
        until [ -f "$work/$name-ready" ]; do
          tries=$((tries + 1))
          [ "$tries" -le 100 ] || fail "cdnstub did not start"
          sleep 0.1
        done
      }
      start_cdn cdn
      start_cdn cdn-down --down
      cdn=$(cat "$work/cdn-ready")
      origin=$work/packs-origin.git
      git init -q --bare -b main "$origin"
      start_ghstub "$origin"
      cat "$work/ca.pem" "$work/gh-ca.pem" "$work/cdn-ca.pem" "$work/cdn-down-ca.pem" > "$work/cas.pem"
      SSL_CERT_FILE=$work/cas.pem CURL_CA_BUNDLE=$work/cas.pem
      GITHUB_REPOSITORY=acme/member CLAUDINITE_GITHUB_API=$gh
      CLAUDINITE_PACKS_CDN=$cdn CLAUDINITE_PACKS_REPO=$src/mirror.git
      export SSL_CERT_FILE CURL_CA_BUNDLE GITHUB_REPOSITORY CLAUDINITE_GITHUB_API CLAUDINITE_PACKS_CDN CLAUDINITE_PACKS_REPO

      # The npx layout: the channel package unpacked, its bin linked.
      name=${package#@claudinite/}
      npx=$work/npx/node_modules
      mkdir -p "$npx/@claudinite" "$npx/.bin"
      cp -R "$dist1/npm/$name/package" "$npx/$package"
      ln -s "../$package/launch" "$npx/.bin/cn"
      # adopt NAME: cn init through the npx bin into a new empty repo.
      adopt() {
        member=$work/$1
        mkdir -p "$member" "$member-home" "$member-cache"
        HOME=$member-home XDG_CACHE_HOME=$member-cache
        export HOME XDG_CACHE_HOME
        (cd "$member" && "$npx/.bin/cn" init --packs hello --channel canary --package "$package" --repo "$member") \
          > "$work/init.out" 2>&1 || fail "packs 1: init: $(cat "$work/init.out")"
      }
      CLAUDINITE_PACKS_CDN=$(cat "$work/cdn-down-ready")
      adopt packs-down
      CLAUDINITE_PACKS_CDN=$cdn
      grep -q "hello: index serial 1 from branch" "$work/init.out" || fail "packs 1: with the CDN down the log names no branch: $(cat "$work/init.out")"
      adopt packs-member
      grep -q "hello: index serial 1 from cdn" "$work/init.out" || fail "packs 1: the log names no CDN: $(cat "$work/init.out")"
      for f in .claudinite/launch .claudinite/settings.yaml .claudinite/.gitignore .claude/settings.json .claude/skills/.gitignore \
        .github/workflows/claudinite-update.yml .github/workflows/claudinite-ci.yml .claudinite/shared/packs/hello/pack.json; do
        [ -f "$member/$f" ] || fail "packs 1: init wrote no $f"
      done
      verify_out=$(cd "$member" && sh .claudinite/launch verify) || fail "packs 1: verify: $verify_out"
      [ -z "$verify_out" ] || fail "packs 1: verify reported: $verify_out"
      (cd "$member" && git init -q -b main && git add -A && git -c user.name=rehearse -c user.email=r@x -c commit.gpgsign=false commit -q -m adopt \
        && git remote add origin "$origin" && git -c push.negotiate=false push -q origin main) || fail "packs 1: git setup"
      step "packs 1: cn init through npx adopted hello 1.0 (CDN, and the branch with the CDN down)"

      out=$(session_start) || fail "packs 2: SessionStart exited non-zero"
      case $out in *"# hello 1.0"*) ;; *) fail "packs 2: no hello rule: $out" ;; esac
      case $out in *"[cn] packs 1/1 loaded (hello 1.0: rules 1 skills 1)"*) ;; *) fail "packs 2: self-check line: $out" ;; esac
      [ -f "$member/.claude/skills/hello/SKILL.md" ] || fail "packs 2: the skill is not mounted"
      cn_member check build --wait > "$work/build.out" 2>&1 || fail "packs 2: check build: $(cat "$work/build.out")"
      checks=$XDG_CACHE_HOME/claudinite/checks
      keys=$(find "$checks" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')
      [ "$keys" = 1 ] || fail "packs 2: $keys checks binaries"
      buildlog=$(find "$checks" -name build.log)
      [ -x "$(dirname "$buildlog")/checks" ] || fail "packs 2: no checks binary beside $buildlog"
      head -n 1 "$buildlog" | grep -q '^go version' || fail "packs 2: build.log: $(cat "$buildlog")"
      step "packs 2: SessionStart loads hello 1.0, mounts its skill; the checks binary is built"

      stop_hook() { (cd "$member" && printf '{"hook_event_name":"Stop","stop_hook_active":false}' | CLAUDE_PROJECT_DIR=$member sh -c "$(hook_command Stop)" 2>/dev/null); }
      touch "$member/HELLO_FINDING"
      out=$(stop_hook)
      case $out in *'"decision":"block"'*hello/hello-check*) ;; *) fail "packs 3: Stop did not block: $out" ;; esac
      if cn_member check --tag world > "$work/check.out" 2>&1; then fail "packs 3: check --tag world passed with HELLO_FINDING"; fi
      rm "$member/HELLO_FINDING"
      [ "$(stop_hook)" = "{}" ] || fail "packs 3: Stop blocked without the file"
      cn_member check --tag world > "$work/check.out" 2>&1 || fail "packs 3: check --tag world: $(cat "$work/check.out")"
      cn_member check --pack hello > "$work/check.out" 2>&1 || fail "packs 3: check --pack: $(cat "$work/check.out")"
      grep -q "hello/hello-check" "$work/check.out" || fail "packs 3: check --pack hello lists nothing: $(cat "$work/check.out")"
      step "packs 3: hello-check blocks Stop and fails check --tag world while HELLO_FINDING exists"

      touch "$work/built-marker"
      sleep 1
      session_start > /dev/null || fail "packs 4: SessionStart"
      sleep 1
      [ -z "$(find "$checks" -name build.log -newer "$work/built-marker")" ] || fail "packs 4: a second SessionStart rebuilt the checks binary"
      step "packs 4: a second SessionStart rebuilds nothing"

      update_packs() {
        cn_member update packs > "$work/update.out" 2> "$work/update.err" || fail "update packs: $(cat "$work/update.out" "$work/update.err")"
        verdict=$(sed -n '$p' "$work/update.out")
      }
      expect_verdict() { [ "$verdict" = "$1" ] || fail "packs: verdict '$verdict', want '$1': $(cat "$work/update.out" "$work/update.err")"; }
      pull() { (cd "$member" && git fetch -q origin && git reset -q --hard origin/main) || fail "packs: pull"; }
      day=$(cn_member version --day)
      branch=claudinite/packs-$day
      # land N: CI green on PR N's head, then cn update land.
      land() {
        head=$(git --git-dir "$origin" rev-parse "$branch")
        ctl /_stub/run "{\"sha\":\"$head\",\"event\":\"workflow_dispatch\",\"conclusion\":\"success\"}"
        cn_member update land --pr "$1" --sha "$head" > "$work/land.out" 2>&1 || fail "packs: land #$1: $(cat "$work/land.out")"
        verdict=$(sed -n '$p' "$work/land.out")
      }

      fixture --publish v2
      main_run success
      update_packs
      expect_verdict "opened #1 for packs hello 1.1"
      if git --git-dir "$origin" diff --name-only main "$branch" | grep -qv '^\.claudinite/shared/packs/hello/'; then fail "packs 5: the branch changes more than the hello pack"; fi
      [ "$(gh_count 'st.dispatches.filter(d=>d.ref==="'"$branch"'"&&d.inputs.pr==="1").length')" = 1 ] || fail "packs 5: dispatches $(gh_state)"
      head=$(git --git-dir "$origin" rev-parse "$branch")
      (cd "$member" && git fetch -q origin && git checkout -q "$head") || fail "packs 5: checkout"
      cn_member check world --pr-author 'github-actions[bot]' --base-ref origin/main > "$work/world.out" 2>&1 || fail "packs 5: check world on the branch: $(cat "$work/world.out")"
      (cd "$member" && git checkout -q main) || fail "packs 5: back to main"
      land 1
      expect_verdict "landed packs hello 1.1"
      pull
      grep -q '"version": "1.1"' "$member/.claudinite/shared/packs/hello/pack.json" || fail "packs 5: main does not hold hello 1.1"
      out=$(session_start) || fail "packs 5: SessionStart"
      case $out in *"# hello 1.1"*) ;; *) fail "packs 5: SessionStart on 1.1: $out" ;; esac
      cn_member check build --wait > "$work/build.out" 2>&1 || fail "packs 5: check build: $(cat "$work/build.out")"
      # The key covers the engine, the SDK and the check sources; 1.1
      # changes only a rule, so its checks binary is 1.0's.
      [ "$(find "$checks" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')" = 1 ] || fail "packs 5: a rule change rebuilt the checks binary"
      step "packs 5: opened, checked and landed hello 1.1; the session loads it on the same checks binary"

      fixture --publish v3
      main_run success
      update_packs
      expect_verdict "no PR: hello 1.2 fails this repo's checks"
      grep -q 'hello/always' "$work/update.out" || fail "packs 6: the finding was not printed: $(cat "$work/update.out")"
      [ -z "$(git --git-dir "$origin" branch --list 'claudinite/*')" ] || fail "packs 6: a branch was pushed"
      [ "$(gh_count 'st.pulls.length')" = 1 ] || fail "packs 6: a PR was opened"
      step "packs 6: $verdict"

      fixture --revoke v3 --publish v4
      update_packs
      expect_verdict "opened #2 for packs hello 1.3"
      grep -q '^hello 1.2 skipped: revoked$' "$work/update.out" || fail "packs 7: no revoked skip: $(cat "$work/update.out")"
      land 2
      expect_verdict "landed packs hello 1.3"
      pull
      main_run success
      step "packs 7: revoked 1.2 skipped; hello 1.3 landed"

      fixture --publish v5
      update_packs
      expect_verdict "up to date"
      grep -q '^hello 1.4 skipped: not for this engine$' "$work/update.out" || fail "packs 8: no engine skip: $(cat "$work/update.out")"
      step "packs 8: hello 1.4 skipped: not for this engine"

      # set_channel C: the member's packs channel, committed and pushed, main green.
      set_channel() {
        sed "s|^  channel: .*|  channel: \"$1\"|" "$member/.claudinite/settings.yaml" > "$work/settings.yaml"
        mv "$work/settings.yaml" "$member/.claudinite/settings.yaml"
        (cd "$member" && git -c user.name=rehearse -c user.email=r@x -c commit.gpgsign=false commit -q -am "packs channel $1" \
          && git -c push.negotiate=false push -q origin main) || fail "packs 9: channel $1"
        main_run success
      }
      set_channel stable
      update_packs
      expect_verdict "up to date"
      grep -q 'skipped: canary$' "$work/update.out" || fail "packs 9: no canary skip: $(cat "$work/update.out")"
      set_channel canary
      step "packs 9: on the stable channel a canary entry is skipped"

      (cd "$member" && git checkout -q -b claudinite/engine-9.9.9 && git -c push.negotiate=false push -q origin claudinite/engine-9.9.9 && git checkout -q main) \
        || fail "packs 10: engine branch"
      curl -sS --fail -X POST -H 'Authorization: Bearer rehearsal-token' -H 'Content-Type: application/json' \
        -d '{"title":"Claudinite engine 9.9.9","head":"claudinite/engine-9.9.9","base":"main","body":""}' "$gh/repos/acme/member/pulls" > /dev/null \
        || fail "packs 10: opening the engine PR"
      update_packs
      expect_verdict "skipped: engine PR #3 is open"
      curl -sS --fail -X PATCH -H 'Authorization: Bearer rehearsal-token' -H 'Content-Type: application/json' \
        -d '{"state":"closed"}' "$gh/repos/acme/member/pulls/3" > /dev/null || fail "packs 10: closing #3"
      main_run failure
      : > "$work/cdn.log"
      update_packs
      expect_verdict "skipped: main is not green (failure)"
      [ ! -s "$work/cdn.log" ] || fail "packs 10: the CDN was read: $(cat "$work/cdn.log")"
      main_run success
      step "packs 10: an open engine PR and a red main each skip the pack update before any pack read"

      fixture --serial 1 --mirror-only
      if cn_member update packs > "$work/update.out" 2>&1; then fail "packs 11: a regressed serial was read: $(cat "$work/update.out")"; fi
      grep -q 'serial 1 is older than serial' "$work/update.out" || fail "packs 11: the refusal names no serial: $(cat "$work/update.out")"
      fixture --flip-sig
      if cn_member update packs > "$work/update.out" 2>&1; then fail "packs 11: a flipped signature was read: $(cat "$work/update.out")"; fi
      grep -qi 'signature\|certificate' "$work/update.out" || fail "packs 11: the refusal names no signature check: $(cat "$work/update.out")"
      [ -z "$(cd "$member" && git status --porcelain)" ] || fail "packs 11: the checkout changed: $(cd "$member" && git status --porcelain)"
      [ -z "$(git --git-dir "$origin" branch --list 'claudinite/packs-*')" ] || fail "packs 11: a branch was pushed"
      step "packs 11: a regressed serial and a flipped signature are refused; nothing changed"
      ;;
  esac
done

step "ok ($package $version, $pin)"
