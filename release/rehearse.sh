#!/bin/sh
# The local Phase 1 to 4 gates, against the built release in $DIST
# (default dist/) served by regstub, in six modes:
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
#            serial regressed or whose signature broke; cn init writes the
#            plan its key names, and the update key's pack index serial and
#            key ids reach the pack reader; hello's declared checks fail the
#            world and block Stop, and the member's checks block turns them
#            off and accepts them; a local pack's Go check builds and
#            finds, and hello 1.4's checks read the change and the pack's
#            config through the SDK.
#   tasks    a member on hello 1.4 with the four workflows runs the task
#            queue against ghstub (its routine route and release/stub-
#            agent.sh) and licstub: tasks list and the declaration check,
#            the scheduler filing and gating, the executor running hello's
#            worker through the SDK and landing its PR, a missing secret,
#            the routine hand-off with its grant and nonce, a degraded key,
#            a lost claim and the leash, the suspend switch, the
#            continuation chain and the failure issue, verify, init and the
#            flat declarations.
#   license  a member's session keys against release/ghstub and
#            release/licstub: the web key, the cut and the late key, no App,
#            no push access, refusals and bindings, resume and renewal, the
#            desktop after cn login, and the Actions key in cn update engine
#            with its release states, refusals and the plan correction PR.
#
# The update, packs, tasks and license modes give every member a GitHub-shaped
# origin (url.<bare>.insteadOf), as the session's key request reads it.
#
#   release/rehearse.sh [--mode fresh|current|stale|update|packs|tasks|license]   (default: all seven)
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

usage="usage: release/rehearse.sh [--mode fresh|current|stale|update|packs|tasks|license]"
modes="fresh current stale update packs tasks license"
case $# in
  0) ;;
  2)
    [ "$1" = --mode ] || fail "$usage"
    case $2 in fresh|current|stale|update|packs|tasks|license) modes=$2 ;; *) fail "unknown mode $2" ;; esac ;;
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
lic_pid=
cdn_pids=
cleanup() {
  [ -z "$stub_pid" ] || kill "$stub_pid" 2>/dev/null || :
  [ -z "$gh_pid" ] || kill "$gh_pid" 2>/dev/null || :
  [ -z "$lic_pid" ] || kill "$lic_pid" 2>/dev/null || :
  for p in $cdn_pids; do kill "$p" 2>/dev/null || :; done
  # A hook starts the checks build detached, in its own session, so it is
  # no child to wait on: stop any still running for a member under $work.
  if command -v pkill >/dev/null 2>&1; then
    pkill -f "check build --repo $work" 2>/dev/null || :
    for _ in 1 2 3 4 5; do
      pgrep -f "check build --repo $work" >/dev/null 2>&1 || break
      sleep 1
    done
  fi
  chmod -R u+w "$work" 2>/dev/null || :
  for _ in 1 2 3 4 5; do
    rm -rf "$work" 2>/dev/null && return 0
    sleep 1
  done
  echo "rehearse: $work not removed" >&2
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
  *" current "*|*" update "*|*" license "*)
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
  *" license "*)
    DIST=$dist2 sh release/sign.sh > "$work/sign.out" 2>&1 || fail "signing $dist2: $(cat "$work/sign.out")"
    ;;
esac
case " $modes " in
  *" update "*|*" packs "*|*" tasks "*|*" license "*)
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
  (cd "$member" && printf '%s' "{\"session_id\":\"${1:-rehearse}\",\"hook_event_name\":\"SessionStart\",\"source\":\"startup\"}" \
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

# start_ghstub ORIGIN [FLAG...]: the GitHub stub over a bare origin,
# forwarding key dispatches to licstub; sets gh.
start_ghstub() {
  [ -x "$work/ghstub" ] || go build -o "$work/ghstub" ./release/ghstub
  [ -n "$gh_pid" ] && kill "$gh_pid" 2>/dev/null
  rm -f "$work/gh-ready"
  gh_origin=$1
  shift
  "$work/ghstub" --origin "$gh_origin" --repo acme/member --token rehearsal-token --ready "$work/gh-ready" --ca-out "$work/gh-ca.pem" \
    --licstub-ready "$work/lic-ready" --licstub-ca "$work/lic-ca.pem" "$@" &
  gh_pid=$!
  tries=0
  until [ -f "$work/gh-ready" ]; do
    tries=$((tries + 1))
    [ "$tries" -le 100 ] || fail "ghstub did not start"
    sleep 0.1
  done
  gh=$(cat "$work/gh-ready")
}
# start_licstub: the license server stand-in, reading the user and repo
# from ghstub; sets lic and CLAUDINITE_LICENSE_API.
start_licstub() {
  [ -x "$work/licstub" ] || go build -o "$work/licstub" ./release/licstub
  [ -n "$lic_pid" ] && kill "$lic_pid" 2>/dev/null
  rm -f "$work/lic-ready"
  "$work/licstub" --root-key keys/dev/root.key --gh-ready "$work/gh-ready" --gh-ca "$work/gh-ca.pem" \
    --ready "$work/lic-ready" --ca-out "$work/lic-ca.pem" &
  lic_pid=$!
  tries=0
  until [ -f "$work/lic-ready" ]; do
    tries=$((tries + 1))
    [ "$tries" -le 100 ] || fail "licstub did not start"
    sleep 0.1
  done
  lic=$(cat "$work/lic-ready")
  CLAUDINITE_LICENSE_API=$lic
  export CLAUDINITE_LICENSE_API
}
# licctl JSON: replaces the licstub config fields JSON names.
licctl() { curl -sS --fail -X POST -H 'Content-Type: application/json' -d "$1" "$lic/_stub/config" > /dev/null || fail "licstub config $1"; }
# github_origin BARE: the member's origin as GitHub names it, its fetches
# and pushes going to BARE.
github_origin() {
  (cd "$member" && git remote add origin https://github.com/acme/member.git \
    && git config "url.$1.insteadOf" https://github.com/acme/member.git) || fail "github_origin"
}
# plan_public: the member's settings name the Public plan.
plan_public() { printf 'license:\n  plan: "public"\n' >> "$member/.claudinite/settings.yaml"; }
# actions_env: what an Actions job with id-token: write sees, its OIDC
# token from ghstub.
actions_env() {
  ACTIONS_ID_TOKEN_REQUEST_URL="$gh/_oidc/token?api-version=2.0" ACTIONS_ID_TOKEN_REQUEST_TOKEN=oidc-request-token
  GITHUB_REPOSITORY_ID=1001 GITHUB_REPOSITORY_OWNER_ID=3 GITHUB_REPOSITORY_OWNER=acme
  export ACTIONS_ID_TOKEN_REQUEST_URL ACTIONS_ID_TOKEN_REQUEST_TOKEN GITHUB_REPOSITORY_ID GITHUB_REPOSITORY_OWNER_ID GITHUB_REPOSITORY_OWNER
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
      plan_public
      origin=$work/origin.git
      git init -q --bare -b main "$origin"
      (cd "$member" && git init -q -b main && git add -A && git -c user.name=rehearse -c user.email=r@x commit -q -m adopt) || fail "update: git setup"
      github_origin "$origin"
      (cd "$member" && git push -q origin main) || fail "update: push"
      start_ghstub "$origin"
      start_licstub
      cat "$work/ca.pem" "$work/gh-ca.pem" "$work/lic-ca.pem" > "$work/cas.pem"
      SSL_CERT_FILE=$work/cas.pem CURL_CA_BUNDLE=$work/cas.pem
      GITHUB_REPOSITORY=acme/member CLAUDINITE_GITHUB_API=$gh
      export SSL_CERT_FILE CURL_CA_BUNDLE GITHUB_REPOSITORY CLAUDINITE_GITHUB_API
      actions_env

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
      # The candidate's verify ran in this checkout: the token reached the
      # push child alone, never the checkout's config.
      [ -z "$(git -C "$member" config --get http.https://github.com/.extraheader)" ] || fail "update 1: the checkout holds an extraheader"
      if grep -qi 'authorization' "$member/.git/config"; then fail "update 1: .git/config holds a credential"; fi
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
      start_licstub
      cat "$work/ca.pem" "$work/gh-ca.pem" "$work/lic-ca.pem" "$work/cdn-ca.pem" "$work/cdn-down-ca.pem" > "$work/cas.pem"
      SSL_CERT_FILE=$work/cas.pem CURL_CA_BUNDLE=$work/cas.pem
      GITHUB_REPOSITORY=acme/member CLAUDINITE_GITHUB_API=$gh GH_TOKEN=rehearsal-token
      CLAUDINITE_PACKS_CDN=$cdn CLAUDINITE_PACKS_REPO=$src/mirror.git
      export SSL_CERT_FILE CURL_CA_BUNDLE GITHUB_REPOSITORY CLAUDINITE_GITHUB_API GH_TOKEN CLAUDINITE_PACKS_CDN CLAUDINITE_PACKS_REPO
      actions_env

      # The npx layout: the channel package unpacked, its bin linked.
      name=${package#@claudinite/}
      npx=$work/npx/node_modules
      mkdir -p "$npx/@claudinite" "$npx/.bin"
      cp -R "$dist1/npm/$name/package" "$npx/$package"
      # npm marks a package's bin executable on install; a dist that came through an
      # Actions artifact has lost its modes.
      chmod 0755 "$npx/$package/launch"
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
        .github/workflows/claudinite-update.yml .github/workflows/claudinite-ci.yml .github/workflows/claudinite-scheduler.yml \
        .github/workflows/claudinite-executor.yml .claudinite/shared/packs/hello/pack.json; do
        [ -f "$member/$f" ] || fail "packs 1: init wrote no $f"
      done
      verify_out=$(cd "$member" && sh .claudinite/launch verify) || fail "packs 1: verify: $verify_out"
      [ -z "$verify_out" ] || fail "packs 1: verify reported: $verify_out"
      grep -q "installations/new" "$work/init.out" || fail "packs 1: init with no origin ends on no install link: $(cat "$work/init.out")"
      (cd "$member" && git init -q -b main && git add -A && git -c user.name=rehearse -c user.email=r@x -c commit.gpgsign=false commit -q -m adopt) \
        || fail "packs 1: git setup"
      github_origin "$origin"
      (cd "$member" && git -c push.negotiate=false push -q origin main) || fail "packs 1: push"
      step "packs 1: cn init through npx adopted hello 1.0 (CDN, and the branch with the CDN down)"

      # init_with_origin NAME: cn init in a new repo whose origin is the
      # member's on GitHub, so its key request reaches the stubs.
      init_with_origin() {
        dir=$work/$1
        mkdir -p "$dir"
        (cd "$dir" && git init -q -b main && git remote add origin https://github.com/acme/member.git \
          && git config "url.$origin.insteadOf" https://github.com/acme/member.git) || fail "packs init: git setup"
        (cd "$dir" && "$npx/.bin/cn" init --packs hello --channel canary --package "$package" --repo "$dir") > "$work/init.out" 2>&1 \
          || fail "packs init: init exited non-zero: $(cat "$work/init.out")"
      }
      init_with_origin packs-init
      [ "$(sed -n '$p' "$work/init.out")" = "plan: public" ] || fail "packs init: the checklist does not end on the plan: $(cat "$work/init.out")"
      grep -q '^  plan: "public"$' "$work/packs-init/.claudinite/settings.yaml" || fail "packs init: no plan block: $(cat "$work/packs-init/.claudinite/settings.yaml")"
      ctl /_stub/session '{"no_app":true}'
      init_with_origin packs-init-no-app
      ctl /_stub/session '{"no_app":false}'
      sed -n '$p' "$work/init.out" | grep -q "installations/new" || fail "packs init: with no App the checklist does not end on the install link: $(cat "$work/init.out")"
      if grep -q '^license:' "$work/packs-init-no-app/.claudinite/settings.yaml"; then fail "packs init: a plan block with no key"; fi
      step "packs init: cn init writes the plan its key names, and ends on the install link with no App"

      out=$(session_start) || fail "packs 2: SessionStart exited non-zero"
      index=$member/.claudinite/flat/claudinite-rules.GENERATED.md
      [ "$(cat "$index" 2>/dev/null)" = "@../shared/packs/hello/RULES.md" ] || fail "packs 2: the rules index does not import hello's rules: $(cat "$index" 2>&1)"
      grep -q '^# hello 1.0$' "$member/.claudinite/shared/packs/hello/RULES.md" || fail "packs 2: hello's rules are not 1.0's"
      case $out in *"# hello 1.0"*) fail "packs 2: the pack's rules reached additionalContext: $out" ;; esac
      case $out in *"[cn] packs 1/1 loaded (hello 1.0: rules 1 skills 1)"*) ;; *) fail "packs 2: self-check line: $out" ;; esac
      [ -f "$member/.claude/skills/hello/SKILL.md" ] || fail "packs 2: the skill is not mounted"
      cn_member check build --wait > "$work/build.out" 2>&1 || fail "packs 2: check build: $(cat "$work/build.out")"
      checks=$XDG_CACHE_HOME/claudinite/checks
      keys=$(find "$checks" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')
      [ "$keys" = 1 ] || fail "packs 2: $keys checks binaries"
      buildlog=$(find "$checks" -name build.log)
      [ -x "$(dirname "$buildlog")/checks" ] || fail "packs 2: no checks binary beside $buildlog"
      head -n 1 "$buildlog" | grep -q '^go version' || fail "packs 2: build.log: $(cat "$buildlog")"
      step "packs 2: hello 1.0 reaches the session through the rules index, its skill is mounted; the checks binary is built"

      stop_hook() { (cd "$member" && printf '{"session_id":"rehearse","hook_event_name":"Stop","stop_hook_active":false}' | CLAUDE_PROJECT_DIR=$member sh -c "$(hook_command Stop)" 2>/dev/null); }
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

      # A member from before the rules channel: CLAUDE.md without the import.
      printf '# Member\n' > "$member/CLAUDE.md"
      (cd "$member" && git add CLAUDE.md && git -c user.name=rehearse -c user.email=r@x -c commit.gpgsign=false commit -q -m "no import" \
        && git -c push.negotiate=false push -q origin main) || fail "packs 5: drop the import"
      out=$(session_start) || fail "packs 5: SessionStart without the import"
      case $out in *"[cn] rules not loaded: CLAUDE.md does not import"*) ;; *) fail "packs 5: no missing-import line: $out" ;; esac
      fixture --publish v2
      main_run success
      update_packs
      expect_verdict "opened #1 for packs hello 1.4"
      if git --git-dir "$origin" diff --name-only main "$branch" | grep -v '^CLAUDE\.md$' | grep -qv '^\.claudinite/shared/packs/hello/'; then fail "packs 5: the branch changes more than the hello pack and CLAUDE.md"; fi
      [ "$(git --git-dir "$origin" show "$branch:CLAUDE.md")" = "$(printf '# Member\n@.claudinite/flat/claudinite-rules.GENERATED.md')" ] || fail "packs 5: the branch's CLAUDE.md: $(git --git-dir "$origin" show "$branch:CLAUDE.md")"
      [ "$(gh_count 'st.dispatches.filter(d=>d.ref==="'"$branch"'"&&d.inputs.pr==="1").length')" = 1 ] || fail "packs 5: dispatches $(gh_state)"
      head=$(git --git-dir "$origin" rev-parse "$branch")
      (cd "$member" && git fetch -q origin && git checkout -q "$head") || fail "packs 5: checkout"
      cn_member check world --pr-author 'github-actions[bot]' --base-ref origin/main > "$work/world.out" 2>&1 || fail "packs 5: check world on the branch: $(cat "$work/world.out")"
      (cd "$member" && git checkout -q main) || fail "packs 5: back to main"
      land 1
      expect_verdict "landed packs hello 1.4"
      pull
      grep -q '"version": "1.4"' "$member/.claudinite/shared/packs/hello/pack.json" || fail "packs 5: main does not hold hello 1.4"
      out=$(session_start) || fail "packs 5: SessionStart"
      case $out in *"[cn] packs 1/1 loaded (hello 1.4: rules 5 skills 2)"*) ;; *) fail "packs 5: SessionStart on 1.4: $out" ;; esac
      case $out in *"rules not loaded"*) fail "packs 5: the import is still missing after the pack PR: $out" ;; esac
      grep -q '^# hello 1.4$' "$member/.claudinite/shared/packs/hello/RULES.md" || fail "packs 5: hello's rules are not 1.4's"
      cn_member check build --wait > "$work/build.out" 2>&1 || fail "packs 5: check build: $(cat "$work/build.out")"
      # The key covers the engine, the SDK and the check sources; 1.4 adds
      # Go checks, so its checks binary is a second one beside 1.0's.
      [ "$(find "$checks" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')" = 2 ] || fail "packs 5: hello 1.4's checks did not build a second checks binary"
      step "packs 5: opened, checked and landed hello 1.4 with the CLAUDE.md import restored; its Go checks build a second checks binary"

      fixture --publish v3
      main_run success
      update_packs
      expect_verdict "no PR: hello 1.5 fails this repo's checks"
      grep -q 'hello/always' "$work/update.out" || fail "packs 6: the finding was not printed: $(cat "$work/update.out")"
      [ -z "$(git --git-dir "$origin" branch --list 'claudinite/*')" ] || fail "packs 6: a branch was pushed"
      [ "$(gh_count 'st.pulls.length')" = 1 ] || fail "packs 6: a PR was opened"
      step "packs 6: $verdict"

      fixture --revoke v3 --publish v4
      update_packs
      expect_verdict "opened #2 for packs hello 1.6"
      grep -q '^hello 1.5 skipped: revoked$' "$work/update.out" || fail "packs 7: no revoked skip: $(cat "$work/update.out")"
      land 2
      expect_verdict "landed packs hello 1.6"
      pull
      main_run success
      step "packs 7: revoked 1.5 skipped; hello 1.6 landed"

      fixture --publish v5
      update_packs
      expect_verdict "up to date"
      grep -q '^hello 1.7 skipped: not for this engine$' "$work/update.out" || fail "packs 8: no engine skip: $(cat "$work/update.out")"
      step "packs 8: hello 1.7 skipped: not for this engine"

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

      licctl '{"release":{"pack_index_serial":99}}'
      if cn_member update packs > "$work/update.out" 2>&1; then fail "packs keys: an index below the key's serial was read: $(cat "$work/update.out")"; fi
      grep -q "below the license key's pack index serial 99" "$work/update.out" || fail "packs keys: the refusal names no serial: $(cat "$work/update.out")"
      licctl '{"release":{"pack_keys":["0123456789abcdef"]}}'
      if cn_member update packs > "$work/update.out" 2>&1; then fail "packs keys: an index under a key the license key does not list was read: $(cat "$work/update.out")"; fi
      grep -q "signed by key [0-9a-f]\{16\}, which the license key does not list" "$work/update.out" || fail "packs keys: the refusal names no key id: $(cat "$work/update.out")"
      licctl '{"release":{}}'
      step "packs keys: the update key's pack index serial and key ids refuse an index below or outside them"

      fixture --serial 1 --mirror-only
      update_packs
      case $verdict in "skipped: pack index sources disagree (cdn serial "*", branch serial 1)") ;; *) fail "packs 11: a branch behind the CDN: verdict '$verdict'" ;; esac
      fixture --flip-sig
      if cn_member update packs > "$work/update.out" 2>&1; then fail "packs 11: a flipped signature was read: $(cat "$work/update.out")"; fi
      grep -qi 'signature\|certificate' "$work/update.out" || fail "packs 11: the refusal names no signature check: $(cat "$work/update.out")"
      [ -z "$(cd "$member" && git status --porcelain)" ] || fail "packs 11: the checkout changed: $(cd "$member" && git status --porcelain)"
      [ -z "$(git --git-dir "$origin" branch --list 'claudinite/packs-*')" ] || fail "packs 11: a branch was pushed"
      step "packs 11: a branch behind the CDN is a skip naming both serials; a flipped signature is refused; nothing changed"

      echo hello > "$member/HELLO_DECLARED"
      if cn_member check --tag world > "$work/check.out" 2>&1; then fail "packs 12: check --tag world passed with HELLO_DECLARED"; fi
      grep -q '^finding hello/hello-declared HELLO_DECLARED:1: ' "$work/check.out" || fail "packs 12: no declared finding: $(cat "$work/check.out")"
      grep -q '^  why: ' "$work/check.out" || fail "packs 12: no why line: $(cat "$work/check.out")"
      grep -q '^  fix: delete HELLO_DECLARED$' "$work/check.out" || fail "packs 12: no fix line: $(cat "$work/check.out")"
      if cn_member check world > "$work/world.out" 2>&1; then fail "packs 12: check world passed with HELLO_DECLARED"; fi
      grep -q 'hello/hello-declared' "$work/world.out" || fail "packs 12: check world names no declared finding: $(cat "$work/world.out")"
      rm "$member/HELLO_DECLARED"
      cn_member check --tag world > "$work/check.out" 2>&1 || fail "packs 12: check --tag world without the file: $(cat "$work/check.out")"
      cn_member check world > "$work/world.out" 2>&1 || fail "packs 12: check world without the file: $(cat "$work/world.out")"
      step "packs 12: hello-declared fails check --tag world and check world while HELLO_DECLARED holds a line"

      touch "$member/HELLO_UNTRACKED"
      out=$(stop_hook)
      case $out in *'"decision":"block"'*hello/hello-declared-work*) ;; *) fail "packs 13: Stop did not block on HELLO_UNTRACKED: $out" ;; esac
      (cd "$member" && git add HELLO_UNTRACKED) || fail "packs 13: git add"
      out=$(stop_hook)
      [ "$out" = "{}" ] || fail "packs 13: Stop blocked with HELLO_UNTRACKED tracked: $out"
      (cd "$member" && git rm -q --cached HELLO_UNTRACKED && rm HELLO_UNTRACKED) || fail "packs 13: cleanup"
      step "packs 13: hello-declared-work blocks Stop while HELLO_UNTRACKED is untracked"

      settings=$member/.claudinite/settings.yaml
      cp "$settings" "$work/settings.orig"
      # with_checks BLOCK: the member's settings with BLOCK as their checks block.
      with_checks() { { cat "$work/settings.orig"; printf 'checks:\n%s\n' "$1"; } > "$settings"; }
      echo hello > "$member/HELLO_DECLARED"
      with_checks '  rules:
    hello-declared: "off"'
      cn_member check --tag world > "$work/check.out" 2>&1 || fail "packs 14: an off rule still found: $(cat "$work/check.out")"
      if grep -q '^\(finding\|advisory\) hello/hello-declared ' "$work/check.out"; then fail "packs 14: an off rule printed a finding: $(cat "$work/check.out")"; fi
      with_checks '  accept:
    - rule: hello-declared
      path: HELLO_DECLARED'
      if cn_member check --tag world > "$work/check.out" 2>&1; then fail "packs 14: an acceptance with no reason passed"; fi
      [ "$(grep -c '^finding config .claudinite/settings.yaml: ' "$work/check.out")" = 1 ] || fail "packs 14: no one config finding: $(cat "$work/check.out")"
      with_checks '  accept:
    - rule: hello-declared
      path: HELLO_DECLARED
      reason: "the rehearsal holds it on purpose"'
      cn_member check --tag world > "$work/check.out" 2>&1 || fail "packs 14: an accepted finding still failed: $(cat "$work/check.out")"
      if grep -q '^\(finding\|advisory\) hello/hello-declared ' "$work/check.out"; then fail "packs 14: an accepted finding printed: $(cat "$work/check.out")"; fi
      rm "$member/HELLO_DECLARED"
      touch "$member/HELLO_FINDING"
      with_checks '  rules:
    hello-check: "off"'
      out=$(stop_hook)
      [ "$out" = "{}" ] || fail "packs 14: Stop blocked on a coded check set off: $out"
      rm "$member/HELLO_FINDING"
      cp "$work/settings.orig" "$settings"
      cn_member check list > "$work/list.out" 2>&1 || fail "packs 14: check list: $(cat "$work/list.out")"
      for want in '^hello/hello-declared declared (world, declared, hello) block$' \
        '^hello/hello-declared-work declared (work, declared, hello) block$' '^hello/hello-check coded ('; do
        grep -q "$want" "$work/list.out" || fail "packs 14: check list has no $want: $(cat "$work/list.out")"
      done
      [ -z "$(cd "$member" && git status --porcelain)" ] || fail "packs 14: the checkout changed: $(cd "$member" && git status --porcelain)"
      step "packs 14: an off rule silences hello-declared and the coded hello-check; an acceptance needs a reason; check list names both kinds"

      # hook EVENT JSON [TRANSCRIPT]: the member's per-call hook on a payload;
      # stdout to hook.out, stderr to hook.err, the exit code in $code.
      hook() {
        tp=
        [ -n "${3:-}" ] && tp=",\"transcript_path\":\"$3\""
        cmd=$(hook_command "$1")
        set +e
        (cd "$member" && printf '%s' "{\"session_id\":\"rehearse\",\"hook_event_name\":\"$1\"$tp,$2}" \
          | CLAUDE_PROJECT_DIR=$member sh -c "$cmd") > "$work/hook.out" 2> "$work/hook.err"
        code=$?
        set -e
      }
      bash_call() { printf '"tool_name":"Bash","tool_input":{"command":"%s"}' "$1"; }
      hook PreToolUse "$(bash_call 'echo HELLO_GUARD')"
      [ "$code" = 2 ] || fail "packs 15: HELLO_GUARD exited $code: $(cat "$work/hook.out" "$work/hook.err")"
      head -n 1 "$work/hook.err" | grep -q '^Blocked by hello-guard: ' || fail "packs 15: HELLO_GUARD block: $(cat "$work/hook.err")"
      [ ! -s "$work/hook.out" ] || fail "packs 15: a block wrote stdout: $(cat "$work/hook.out")"
      hook PreToolUse "$(bash_call 'echo HELLO_JUDGE')"
      [ "$code" = 2 ] || fail "packs 15: HELLO_JUDGE exited $code: $(cat "$work/hook.out" "$work/hook.err")"
      head -n 1 "$work/hook.err" | grep -q '^Blocked by hello-judge: ' || fail "packs 15: HELLO_JUDGE block: $(cat "$work/hook.err")"
      hook PreToolUse "$(bash_call ls)"
      [ "$code:$(cat "$work/hook.out")" = "0:{}" ] || fail "packs 15: ls exited $code: $(cat "$work/hook.out" "$work/hook.err")"
      step "packs 15: PreToolUse blocks the declared hello-guard and the coded hello-judge, and lets ls through"

      transcripts=$work/transcripts
      mkdir -p "$transcripts"
      loaded=$transcripts/loaded.jsonl
      printf '%s\n' '{"type":"user","message":{"role":"user","content":"go"}}' \
        '{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"s1","name":"Skill","input":{"skill":"hello-guide"}}]}}' > "$loaded"
      edit='"tool_name":"Edit","tool_input":{"file_path":"HELLO_SCOPED/x","old_string":"a","new_string":"b"}'
      hook PreToolUse "$edit"
      [ "$code" = 2 ] || fail "packs 16: the scoped edit exited $code: $(cat "$work/hook.out" "$work/hook.err")"
      head -n 1 "$work/hook.err" | grep -q '^Blocked: HELLO_SCOPED/x is edited only with the .hello-guide. skill loaded .*Skill tool, skill: "hello-guide"' \
        || fail "packs 16: the hold: $(cat "$work/hook.err")"
      hook PreToolUse "$edit" "$loaded"
      [ "$code" = 0 ] || fail "packs 16: the scoped edit with hello-guide loaded exited $code: $(cat "$work/hook.err")"
      hook UserPromptSubmit '"prompt":"say HELLO PROMPT"'
      grep -q 'this prompt matches the .hello-guide. skill' "$work/hook.out" || fail "packs 16: no prompt nudge: $(cat "$work/hook.out" "$work/hook.err")"
      hook UserPromptSubmit '"prompt":"say HELLO PROMPT"' "$loaded"
      if grep -q 'hello-guide' "$work/hook.out"; then fail "packs 16: a loaded skill was nudged again: $(cat "$work/hook.out")"; fi
      hook PostToolUse "$(bash_call ls),\"tool_response\":{\"stdout\":\"HELLO_RESULT\"}"
      grep -q 'this Bash result matches the .hello-guide. skill' "$work/hook.out" || fail "packs 16: no result nudge: $(cat "$work/hook.out" "$work/hook.err")"
      step "packs 16: the hello-guide triggers hold a scoped edit until it is loaded, and nudge a prompt and a result"

      session=$transcripts/stop.jsonl
      printf '%s\n' '{"type":"user","message":{"role":"user","content":"go"}}' \
        '{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"b1","name":"Bash","input":{"command":"echo HELLO_GUARD"}}]}}' \
        '{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"b1","is_error":true,"content":"Blocked by hello-guard: the command names HELLO_GUARD."}]}}' \
        '{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"b2","name":"Bash","input":{"command":"sed -i s/a/b/ HELLO_SCOPED/x"}}]}}' > "$session"
      mkdir -p "$member/HELLO_SCOPED"
      echo b > "$member/HELLO_SCOPED/x"
      out=$(cd "$member" && printf '%s' "{\"session_id\":\"rehearse\",\"hook_event_name\":\"Stop\",\"stop_hook_active\":false,\"transcript_path\":\"$session\"}" \
        | CLAUDE_PROJECT_DIR=$member sh -c "$(hook_command Stop)" 2>/dev/null)
      case $out in *'"decision":"block"'*skill-loaded-before-editing*) ;; *) fail "packs 17: Stop did not block on the unloaded skill: $out" ;; esac
      case $out in *'hello-guard'*'(denied at the hook)'*) ;; *) fail "packs 17: Stop names no denied hello-guard call: $out" ;; esac
      rm -r "$member/HELLO_SCOPED"
      step "packs 17: Stop blocks on skill-loaded-before-editing and records the denied hello-guard call"

      with_checks '  rules:
    hello-guard: "off"
    remote-branch-delete: "off"'
      hook PreToolUse "$(bash_call 'echo HELLO_GUARD')"
      [ "$code" = 0 ] || fail "packs 18: hello-guard off still exited $code: $(cat "$work/hook.err")"
      hook PreToolUse "$(bash_call 'git push origin --delete x')"
      [ "$code" = 0 ] || fail "packs 18: remote-branch-delete off still exited $code: $(cat "$work/hook.err")"
      cp "$work/settings.orig" "$settings"
      hook PreToolUse "$(bash_call 'git push origin --delete x')"
      [ "$code" = 2 ] || fail "packs 18: remote-branch-delete on exited $code: $(cat "$work/hook.err")"
      cn_member check list > "$work/list.out" 2>&1 || fail "packs 18: check list: $(cat "$work/list.out")"
      for want in '^hello/hello-guard declared (action, work, pre-tool-use[,)]' '^hello/hello-judge judge (pre-tool-use)'; do
        grep -q "$want" "$work/list.out" || fail "packs 18: check list has no $want: $(cat "$work/list.out")"
      done
      cn_member check --tag pre-tool-use > "$work/check.out" 2>&1 || fail "packs 18: check --tag pre-tool-use: $(cat "$work/check.out")"
      grep -q '^judge hello/hello-judge (pre-tool-use) runs only in its hook' "$work/check.out" || fail "packs 18: check --tag pre-tool-use: $(cat "$work/check.out")"
      [ -z "$(cd "$member" && git status --porcelain)" ] || fail "packs 18: the checkout changed: $(cd "$member" && git status --porcelain)"
      step "packs 18: rules off silence hello-guard and remote-branch-delete; check list names the guard's three tags and the judge"

      probe=$member/.claudinite/local/packs/probe
      mkdir -p "$probe/checks"
      printf '{}\n' > "$probe/pack.json"
      cat > "$probe/checks/local.go" <<'GO'
package checks

import "claudinite.com/checksdk"

func init() {
	checksdk.Register(checksdk.Check{
		ID:   "local-check",
		Tags: []string{"work", "world"},
		Run: func(repo checksdk.Repo) []checksdk.Finding {
			if !repo.Exists("HELLO_LOCAL") {
				return nil
			}
			return []checksdk.Finding{{Path: "HELLO_LOCAL", Sentence: "the local probe file is present; delete HELLO_LOCAL"}}
		},
	})
}
GO
      awk '{ print } /^    - hello$/ { print "    - local/probe" }' "$work/settings.orig" > "$settings"
      touch "$member/HELLO_LOCAL"
      if cn_member check --pack local/probe > "$work/check.out" 2>&1; then fail "packs 19: check --pack local/probe passed with HELLO_LOCAL"; fi
      grep -q '^finding local/probe/local-check HELLO_LOCAL: ' "$work/check.out" || fail "packs 19: no local finding: $(cat "$work/check.out")"
      cn_member check list > "$work/list.out" 2>&1 || fail "packs 19: check list: $(cat "$work/list.out")"
      grep -q '^local/probe/local-check coded (' "$work/list.out" || fail "packs 19: check list has no local check: $(cat "$work/list.out")"
      rm -r "$member/HELLO_LOCAL" "$member/.claudinite/local"
      cp "$work/settings.orig" "$settings"
      step "packs 19: a local pack's Go check builds as local/probe, finds through check --pack and is listed"

      (cd "$member" && git checkout -q -b probe-change && mkdir HELLO_CHANGED && echo changed > HELLO_CHANGED/x && git add HELLO_CHANGED \
        && git -c user.name=rehearse -c user.email=r@x -c commit.gpgsign=false commit -q -m "probe change") || fail "packs 20: the change"
      out=$(cd "$member" && printf '{"session_id":"rehearse","hook_event_name":"Stop","stop_hook_active":false}' \
        | CLAUDE_PROJECT_DIR=$member sh -c "$(hook_command Stop)" 2> "$work/stop.err")
      [ "$out" = "{}" ] || fail "packs 20: an advisory alone blocked Stop: $out"
      grep -q '^advisory hello/hello-change HELLO_CHANGED/x:1: ' "$work/stop.err" || fail "packs 20: Stop printed no hello-change advisory: $(cat "$work/stop.err")"
      touch "$member/HELLO_FINDING"
      out=$(stop_hook)
      case $out in *'"decision":"block"'*hello/hello-check*hello/hello-change*|*'"decision":"block"'*hello/hello-change*hello/hello-check*) ;;
        *) fail "packs 20: the block form does not list hello-change beside hello-check: $out" ;; esac
      rm "$member/HELLO_FINDING"
      (cd "$member" && git checkout -q main && git branch -q -D probe-change) || fail "packs 20: back to main"
      [ "$(stop_hook)" = "{}" ] || fail "packs 20: Stop on main still reports"
      # with_probe [CHECKS]: the settings with the hello entry's config probe set, and CHECKS as their checks block.
      with_probe() {
        awk '/^    - hello$/ { print "    - id: hello"; print "      config:"; print "        probe: true"; next } { print }' "$work/settings.orig" > "$settings"
        [ -z "${1:-}" ] || printf 'checks:\n%s\n' "$1" >> "$settings"
      }
      with_probe
      if cn_member check world > "$work/world.out" 2> "$work/world.err"; then fail "packs 20: check world passed with probe: true"; fi
      grep -q 'hello/hello-config' "$work/world.out" || fail "packs 20: check world names no hello-config finding: $(cat "$work/world.out" "$work/world.err")"
      [ "$(grep -c '^\[cn\] sdk ' "$work/world.err")" = 1 ] || fail "packs 20: want one sdk breadcrumb: $(cat "$work/world.err")"
      with_probe '  rules:
    hello-config: "off"'
      cn_member check world > "$work/world.out" 2>&1 || fail "packs 20: hello-config off still failed: $(cat "$work/world.out")"
      cp "$work/settings.orig" "$settings"
      [ -z "$(cd "$member" && git status --porcelain)" ] || fail "packs 20: the checkout changed: $(cd "$member" && git status --porcelain)"
      step "packs 20: hello-change advises at Stop on a change under HELLO_CHANGED/, alone and in the block form; hello-config reads the entry's config; one sdk breadcrumb"

      mkdir -p "$probe/checks"
      printf '{}\n' > "$probe/pack.json"
      printf 'package checks\n\nfunc init() { panic("probe init boom") }\n' > "$probe/checks/boom.go"
      awk '{ print } /^    - hello$/ { print "    - local/probe" }' "$work/settings.orig" > "$settings"
      if cn_member check --pack local/probe > "$work/check.out" 2>&1; then fail "packs 21: a check panicking in init passed"; fi
      grep 'checks-run' "$work/check.out" | grep 'exited before answering' | grep -q 'panic: probe init boom' \
        || fail "packs 21: the break does not carry the panic: $(cat "$work/check.out")"
      rm "$probe/checks/boom.go"
      cat > "$probe/checks/clock.go" <<'GO'
package checks

import (
	"time"

	"claudinite.com/checksdk"
)

func init() {
	checksdk.Register(checksdk.Check{ID: "slow", Tags: []string{"work"}, Run: func(checksdk.Repo) []checksdk.Finding {
		time.Sleep(300 * time.Millisecond)
		return nil
	}})
	checksdk.Register(checksdk.Check{ID: "sibling", Tags: []string{"work"}, Run: func(checksdk.Repo) []checksdk.Finding {
		return []checksdk.Finding{{Path: "pack.json", Sentence: "the sibling still lands"}}
	}})
}
GO
      if (CLAUDINITE_CHECK_DEADLINE_MS=100 && export CLAUDINITE_CHECK_DEADLINE_MS && cn_member check --pack local/probe) > "$work/check.out" 2>&1; then fail "packs 21: a check past its deadline passed"; fi
      grep -q 'local/probe/slow: deadline (100ms) passed in run' "$work/check.out" || fail "packs 21: no deadline for slow: $(cat "$work/check.out")"
      grep -q '^finding local/probe/sibling pack.json: the sibling still lands' "$work/check.out" || fail "packs 21: the sibling's finding did not land: $(cat "$work/check.out")"
      rm -rf "$checks"
      cn_member verify > "$work/verify.out" 2> "$work/verify.err" || fail "packs 21: verify failed: $(cat "$work/verify.out" "$work/verify.err")"
      [ ! -e "$checks" ] || [ -z "$(ls -A "$checks")" ] || fail "packs 21: verify built into $checks: $(ls -A "$checks")"
      [ "$(grep -c 'coded checks are not built' "$work/verify.err")" = 1 ] || fail "packs 21: verify did not name the unlisted coded checks once: $(cat "$work/verify.err")"
      cn_member check world > "$work/world.out" 2> "$work/world.err" || true
      [ -n "$(ls -A "$checks" 2>/dev/null)" ] || fail "packs 21: check world built nothing: $(cat "$work/world.err")"
      cn_member verify > "$work/verify.out" 2> "$work/verify.err" || fail "packs 21: verify failed after the build: $(cat "$work/verify.out" "$work/verify.err")"
      ! grep -q 'coded checks are not built' "$work/verify.err" || fail "packs 21: verify still says the coded checks are unbuilt after check world"
      rm -r "$member/.claudinite/local"
      cp "$work/settings.orig" "$settings"
      [ -z "$(cd "$member" && git status --porcelain)" ] || fail "packs 21: the checkout changed: $(cd "$member" && git status --porcelain)"
      step "packs 21: a panic in init names itself; a check past its deadline leaves its sibling's finding; verify never builds, and check world builds what it then lists"
      ;;
    tasks)
      step "tasks: a member on hello 1.4 with the four workflows, against ghstub's routine route and licstub"
      src=$work/tasksrc
      sh release/packs-fixture.sh "$src" --min-engine "$version" > "$work/fixture.out" 2>&1 || fail "tasks: fixture: $(cat "$work/fixture.out")"
      sh release/packs-fixture.sh "$src" --publish v2 > "$work/fixture.out" 2>&1 || fail "tasks: fixture v2: $(cat "$work/fixture.out")"
      [ -x "$work/cdnstub" ] || go build -o "$work/cdnstub" ./release/cdnstub
      "$work/cdnstub" --repo "$src/cdn.git" --ready "$work/tasks-cdn-ready" --ca-out "$work/tasks-cdn-ca.pem" --log "$work/tasks-cdn.log" &
      cdn_pids="$cdn_pids $!"
      tries=0
      until [ -f "$work/tasks-cdn-ready" ]; do
        tries=$((tries + 1))
        [ "$tries" -le 100 ] || fail "cdnstub did not start"
        sleep 0.1
      done
      origin=$work/tasks-origin.git
      git init -q --bare -b main "$origin"
      git --git-dir "$origin" config uploadpack.allowAnySHA1InWant true
      member=$work/tasks-member
      STUB_AGENT_CN=$member/.claudinite/bin/cn STUB_AGENT_MEMBER=$member STUB_AGENT_CA=$work/gh-ca.pem
      export STUB_AGENT_CN STUB_AGENT_MEMBER STUB_AGENT_CA
      start_ghstub "$origin" --agent "$root/release/stub-agent.sh"
      start_licstub
      mkdir -p "$member" "$member-home" "$member-cache"
      HOME=$member-home XDG_CACHE_HOME=$member-cache
      export HOME XDG_CACHE_HOME
      cat "$work/ca.pem" "$work/gh-ca.pem" "$work/lic-ca.pem" "$work/tasks-cdn-ca.pem" > "$work/cas.pem"
      SSL_CERT_FILE=$work/cas.pem CURL_CA_BUNDLE=$work/cas.pem
      GITHUB_REPOSITORY=acme/member CLAUDINITE_GITHUB_API=$gh GH_TOKEN=rehearsal-token
      CLAUDINITE_PACKS_CDN=$(cat "$work/tasks-cdn-ready") CLAUDINITE_PACKS_REPO=$src/mirror.git
      CCR_ROUTINE_TOKEN=routine-token
      # The member's queue workflows run on its main; in CI the job's own
      # ref (a pull request's merge ref) would otherwise be the queue's branch.
      GITHUB_REF_NAME=main
      export SSL_CERT_FILE CURL_CA_BUNDLE GITHUB_REPOSITORY CLAUDINITE_GITHUB_API GH_TOKEN CLAUDINITE_PACKS_CDN CLAUDINITE_PACKS_REPO CCR_ROUTINE_TOKEN GITHUB_REF_NAME
      actions_env
      name=${package#@claudinite/}
      npx=$work/tasks-npx/node_modules
      mkdir -p "$npx/@claudinite" "$npx/.bin"
      cp -R "$dist1/npm/$name/package" "$npx/$package"
      chmod 0755 "$npx/$package/launch"
      ln -s "../$package/launch" "$npx/.bin/cn"
      (cd "$member" && "$npx/.bin/cn" init --packs hello --channel canary --package "$package" --repo "$member") > "$work/init.out" 2>&1 \
        || fail "tasks: init: $(cat "$work/init.out")"
      grep -q '"version": "1.4"' "$member/.claudinite/shared/packs/hello/pack.json" || fail "tasks: init did not vendor hello 1.4"
      # The two engine packs whose checks the steps run, claudinite-tasks
      # carrying the routine the hand-off fires; each stands in as its
      # manifest alone, as nothing here publishes them.
      for p in claudinite-tasks claudinite-lifecycle; do
        mkdir -p "$member/.claudinite/shared/packs/$p"
        printf '{\n  "version": "1.0",\n  "minEngineVersion": "%s"\n}\n' "$version" > "$member/.claudinite/shared/packs/$p/pack.json"
      done
      awk -v url="$gh/routines/trig_hello" '{ print } /^    - hello$/ {
        print "    - claudinite-lifecycle"; print "    - id: claudinite-tasks"; print "      config:"
        print "        agenticTaskInvocationEndpoints:"; print "          default:"; print "            url: \"" url "\"" }' \
        "$member/.claudinite/settings.yaml" > "$work/settings.yaml"
      mv "$work/settings.yaml" "$member/.claudinite/settings.yaml"
      # The adoption is three days old, so the repo starts quiet.
      old=$(date -u -d '3 days ago' +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -v-3d +%Y-%m-%dT%H:%M:%SZ)
      gitc() { (cd "$member" && git -c user.name=rehearse -c user.email=r@x -c commit.gpgsign=false "$@"); }
      (cd "$member" && git init -q -b main && git add -A) || fail "tasks: git setup"
      GIT_AUTHOR_DATE=$old GIT_COMMITTER_DATE=$old gitc commit -q -m adopt || fail "tasks: adopt commit"
      github_origin "$origin"
      (cd "$member" && git -c push.negotiate=false push -q origin main) || fail "tasks: push"
      out=$(session_start) || fail "tasks: SessionStart exited non-zero"
      # sched ARGS: a scheduler step as the workflow runs it, its outputs in $work/gh-output.
      sched() { : > "$work/gh-output"; (cd "$member" && GITHUB_OUTPUT=$work/gh-output GITHUB_TOKEN=rehearsal-token .claudinite/bin/cn schedule "$@"); }
      gate() { sed -n 's/^pickable=//p' "$work/gh-output"; }
      execute() { cn_member execute loop > "$work/exec.out" 2>&1; }
      issues_titled() { gh_count 'st.issues.filter(i=>i.title==="'"$1"'").length'; }
      # item TITLE: the newest issue with that title, as a number.
      item() { gh_count 'Math.max(0,...st.issues.filter(i=>i.title==="'"$1"'").map(i=>i.number))'; }
      labels_of() { gh_count 'st.issues.find(i=>i.number==='"$1"').labels.join(" ")'; }
      state_of() { gh_count 'st.issues.find(i=>i.number==='"$1"').state'; }
      comments_of() { gh_count 'st.issues.find(i=>i.number==='"$1"').comments.join("\n----\n")'; }
      pull_after() { (cd "$member" && git -c push.negotiate=false pull -q --ff-only origin main) || fail "tasks: pull"; }

      cn_member tasks list > "$work/list.out" 2>&1 || fail "tasks 1: tasks list: $(cat "$work/list.out")"
      for t in hello/hello-fold hello/hello-agent engine/implement-request; do
        grep -q "^$t " "$work/list.out" || fail "tasks 1: tasks list does not name $t: $(cat "$work/list.out")"
      done
      cn_member check world > "$work/world.out" 2>&1 || fail "tasks 1: check world: $(cat "$work/world.out")"
      # The check reads the repo's own declarations, never the vendored mount.
      decl=$member/.claudinite/shared/packs/hello/tasks/hello-fold/task.json
      cp "$decl" "$work/task.json"
      mkdir -p "$member/.claudinite/local/packs/probe/tasks/probe"
      sed 's/"expected_outcome"/"expected_outcom"/' "$decl" > "$member/.claudinite/local/packs/probe/tasks/probe/task.json"
      if cn_member check world > "$work/world.out" 2>&1; then fail "tasks 1: check world passed a misspelt expected_outcome"; fi
      grep -q "task-declaration-shape .claudinite/local/packs/probe/tasks/probe/task.json" "$work/world.out" \
        || fail "tasks 1: task-declaration-shape did not fail it: $(cat "$work/world.out")"
      rm -r "$member/.claudinite/local"
      step "tasks 1: tasks list names the three tasks; check world passes, and task-declaration-shape fails a misspelt field"

      sched run > "$work/sched.out" 2>&1 || fail "tasks 2: schedule run: $(cat "$work/sched.out")"
      [ "$(gh_count 'st.issues.length')" = 0 ] || fail "tasks 2: a quiet repo filed an item: $(gh_state)"
      [ "$(gate)" = false ] || fail "tasks 2: the gate on a quiet repo is $(gate): $(cat "$work/sched.out")"
      echo fold > "$member/FOLD_ME"
      { gitc add FOLD_ME && gitc commit -q -m "a commit to fold"; } || fail "tasks 2: commit"
      (cd "$member" && git -c push.negotiate=false push -q origin main) || fail "tasks 2: push"
      sched run > "$work/sched.out" 2>&1 || fail "tasks 2: schedule run after a commit: $(cat "$work/sched.out")"
      fold="[claudinite-work] hello/hello-fold"
      [ "$(issues_titled "$fold")" = 1 ] || fail "tasks 2: no hello-fold item: $(cat "$work/sched.out") $(gh_state)"
      n=$(item "$fold")
      case " $(labels_of "$n") " in *" task:status:waiting-for-executor "*) ;; *) fail "tasks 2: #$n is not ready: $(labels_of "$n")" ;; esac
      [ "$(gate)" = true ] || fail "tasks 2: the gate after filing is $(gate)"
      sched run > "$work/sched.out" 2>&1 || fail "tasks 2: second schedule run: $(cat "$work/sched.out")"
      [ "$(issues_titled "$fold")" = 1 ] || fail "tasks 2: a second run filed another item: $(gh_state)"
      step "tasks 2: a quiet repo files nothing; a commit files #$n ready and opens the gate; a second run files nothing"

      ctl /_stub/dispatch '{"conclusion":"success"}'
      HELLO_FOLD_SECRET=rehearsal-secret
      export HELLO_FOLD_SECRET
      execute || fail "tasks 3: execute loop: $(cat "$work/exec.out")"
      grep -q "HELLO_FOLD_SECRET handed over" "$work/exec.out" || fail "tasks 3: the worker's sdk line is missing: $(cat "$work/exec.out")"
      grep -q "CCR_ROUTINE_TOKEN withheld" "$work/exec.out" || fail "tasks 3: the routine token reached the worker: $(cat "$work/exec.out")"
      grep -q "sdk hello/hello-fold: github.openPr #" "$work/exec.out" || fail "tasks 3: no openPr breadcrumb: $(cat "$work/exec.out")"
      pr=$(gh_count 'st.pulls.length ? st.pulls[st.pulls.length-1].number : 0')
      head_ref=$(gh_count 'st.pulls.find(p=>p.number==='"$pr"').head')
      case $head_ref in claudinite/hello/hello-fold/*-*) ;; *) fail "tasks 3: PR #$pr is from $head_ref" ;; esac
      [ "$(gh_count 'st.pulls.find(p=>p.number==='"$pr"').state')" = closed ] || fail "tasks 3: PR #$pr did not land: $(cat "$work/exec.out")"
      git --git-dir "$origin" show main:HELLO_FOLD.json > /dev/null 2>&1 || fail "tasks 3: main holds no HELLO_FOLD.json"
      head_sha=$(gh_count 'st.pulls.find(p=>p.number==='"$pr"').head_sha')
      msg=$(git --git-dir "$origin" log -1 --format=%B "$head_sha")
      case $msg in *"Claudinite-Task: hello/hello-fold"*"Claudinite-Automerge-Policy: hello-generated"*) ;; *) fail "tasks 3: the PR head's trailers: $msg" ;; esac
      msg=$(git --git-dir "$origin" log -1 --format=%B main)
      case $msg in *"Claudinite-Task: hello/hello-fold"*) ;; *) fail "tasks 3: the landed commit's trailer: $msg" ;; esac
      [ "$(state_of "$n")" = closed ] || fail "tasks 3: #$n is still open: $(cat "$work/exec.out")"
      case " $(labels_of "$n") " in *" task:status:done "*) ;; *) fail "tasks 3: #$n closed as $(labels_of "$n")" ;; esac
      grep -q "^- #$n: " "$work/exec.out" || fail "tasks 3: no settled line for #$n: $(cat "$work/exec.out")"
      comments_of "$n" | grep -q "claudinite-task-exec v1 hello/hello-fold \[#$n\] success" || fail "tasks 3: no execution record on #$n: $(comments_of "$n")"
      (cd "$member" && git fetch -q origin main "$head_sha" && git checkout -q -b fold-probe "$head_sha") || fail "tasks 3: checking out the PR head"
      cn_member check --tag work > "$work/work.out" 2>&1 || fail "tasks 3: check --tag work on the branch: $(cat "$work/work.out")"
      echo planted >> "$member/README.md"
      { gitc add README.md && gitc commit -q -m "plant a README edit"; } || fail "tasks 3: plant"
      if cn_member check --tag work > "$work/work.out" 2>&1; then fail "tasks 3: check --tag work passed a README edit under hello-generated"; fi
      grep -q "automerge-policy-scope" "$work/work.out" || fail "tasks 3: automerge-policy-scope did not fail it: $(cat "$work/work.out")"
      (cd "$member" && git checkout -q main && git branch -q -D fold-probe) || fail "tasks 3: back to main"
      pull_after
      step "tasks 3: #$n ran the worker, landed PR #$pr from $head_ref with both trailers and converged done; automerge-policy-scope holds the branch to the policy"

      unset HELLO_FOLD_SECRET
      cn_member work create hello/hello-fold --qualifier secret > "$work/create.out" 2>&1 || fail "tasks 4: work create: $(cat "$work/create.out")"
      n=$(item "[claudinite-work] hello/hello-fold secret")
      [ "$n" -gt 0 ] || fail "tasks 4: work create filed nothing: $(cat "$work/create.out") $(gh_state)"
      execute || fail "tasks 4: execute loop: $(cat "$work/exec.out")"
      case " $(labels_of "$n") " in *" task:status:needs-human-action "*) ;; *) fail "tasks 4: #$n is $(labels_of "$n"): $(cat "$work/exec.out")" ;; esac
      comments_of "$n" | grep -q HELLO_FOLD_SECRET || fail "tasks 4: the park does not name the secret: $(comments_of "$n")"
      step "tasks 4: an unset declared secret parks #$n action, naming it"

      lic_before=$(curl -sS --fail "$lic/_stub/log" | grep -c item-grant || :)
      cn_member work create hello/hello-agent --qualifier probe > "$work/create.out" 2>&1 || fail "tasks 5: work create: $(cat "$work/create.out")"
      n=$(item "[claudinite-work] hello/hello-agent probe")
      [ "$n" -gt 0 ] || fail "tasks 5: work create filed nothing: $(cat "$work/create.out")"
      execute || fail "tasks 5: execute loop: $(cat "$work/exec.out")"
      [ "$(curl -sS --fail "$lic/_stub/log" | grep -c item-grant)" -gt "$lic_before" ] || fail "tasks 5: no grant was requested: $(cat "$work/exec.out")"
      comments_of "$n" | grep -q "claudinite-grant" || fail "tasks 5: no grant comment on #$n: $(comments_of "$n")"
      gh_count 'st.fires.map(f=>f.trigger+" "+f.text).join("\n")' | grep -q "^trig_hello Claudinite work item: acme/member#$n. Invocation nonce: $n-" \
        || fail "tasks 5: the fire: $(gh_count 'JSON.stringify(st.fires)') $(cat "$work/exec.out")"
      tries=0
      until [ "$(gh_count 'st.agent.length')" -ge 1 ]; do
        tries=$((tries + 1))
        [ "$tries" -le 300 ] || fail "tasks 5: the stub agent never finished"
        sleep 0.1
      done
      [ "$(gh_count 'st.agent[0].exit')" = 0 ] || fail "tasks 5: the stub agent: $(gh_count 'st.agent[0].output')"
      gh_count 'st.agent[0].output' | grep -q "is this session's" || fail "tasks 5: validate: $(gh_count 'st.agent[0].output')"
      gh_count 'st.agent[0].output' | grep -q "the transition below is yours to execute" || fail "tasks 5: converge: $(gh_count 'st.agent[0].output')"
      [ "$(state_of "$n")" = closed ] || fail "tasks 5: #$n is still open: $(labels_of "$n")"
      case " $(labels_of "$n") " in *" task:status:done "*) ;; *) fail "tasks 5: #$n closed as $(labels_of "$n")" ;; esac
      ctl /_stub/routine '{"down":true}'
      cn_member work create hello/hello-agent --qualifier down > "$work/create.out" 2>&1 || fail "tasks 5: work create: $(cat "$work/create.out")"
      n=$(item "[claudinite-work] hello/hello-agent down")
      execute || fail "tasks 5: execute loop with the routine down: $(cat "$work/exec.out")"
      ctl /_stub/routine '{"down":false}'
      case " $(labels_of "$n") " in *" task:status:needs-human-action "*) ;; *) fail "tasks 5: with the routine down #$n is $(labels_of "$n"): $(cat "$work/exec.out")" ;; esac
      step "tasks 5: the hand-off granted, fired and validated; the stub agent converged the item; a routine down parks action"

      licctl '{"state":"degraded"}'
      HELLO_FOLD_SECRET=rehearsal-secret
      export HELLO_FOLD_SECRET
      cn_member work create hello/hello-agent --qualifier degraded > "$work/create.out" 2>&1 || fail "tasks 6: work create: $(cat "$work/create.out")"
      agentic=$(item "[claudinite-work] hello/hello-agent degraded")
      cn_member work create hello/hello-fold --qualifier degraded > "$work/create.out" 2>&1 || fail "tasks 6: work create: $(cat "$work/create.out")"
      folded=$(item "[claudinite-work] hello/hello-fold degraded")
      execute || fail "tasks 6: execute loop: $(cat "$work/exec.out")"
      licctl '{"state":"ok"}'
      case " $(labels_of "$agentic") " in *" task:status:needs-human-action "*) ;; *) fail "tasks 6: degraded, #$agentic is $(labels_of "$agentic"): $(cat "$work/exec.out")" ;; esac
      comments_of "$agentic" | grep -qi "license" || fail "tasks 6: #$agentic's park carries no license notice: $(comments_of "$agentic")"
      [ "$(state_of "$folded")" = closed ] || fail "tasks 6: degraded, hello-fold #$folded did not run: $(labels_of "$folded") $(cat "$work/exec.out")"
      pull_after
      step "tasks 6: a degraded key parks the agentic #$agentic with the notice; hello-fold #$folded still ran"

      cn_member work create hello/hello-fold --qualifier race > "$work/create.out" 2>&1 || fail "tasks 7: work create: $(cat "$work/create.out")"
      n=$(item "[claudinite-work] hello/hello-fold race")
      at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
      curl -sS --fail -X POST -H "Authorization: Bearer rehearsal-token" -H 'Content-Type: application/json' \
        -d "{\"body\":\"<!-- claudinite-claim -->\\nClaimed by executor \`exec-a\` at $at.\"}" "$gh/repos/acme/member/issues/$n/comments" > /dev/null \
        || fail "tasks 7: exec-a's claim"
      CLAUDINITE_EXECUTOR_ID=exec-b execute || fail "tasks 7: execute loop as exec-b: $(cat "$work/exec.out")"
      [ "$(gh_count 'st.issues.find(i=>i.number==='"$n"').comments.filter(c=>c.includes("executor \u0060exec-b\u0060")&&c.includes("claudinite-episode")).length')" = 1 ] || fail "tasks 7: exec-b did not strike its claim: $(comments_of "$n")"
      [ "$(state_of "$n")" = open ] || fail "tasks 7: exec-b ran #$n past exec-a's earlier claim"
      # exec-b left the item running, which is exec-a's to finish; exec-a never heartbeats.
      case " $(labels_of "$n") " in *" task:status:running-executor "*) ;; *) fail "tasks 7: #$n is $(labels_of "$n") after the lost claim" ;; esac
      later=$(date -u -d '+61 minutes' +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -v+61M +%Y-%m-%dT%H:%M:%SZ)
      CLAUDINITE_NOW=$later sched run > "$work/sched.out" 2>&1 || fail "tasks 7: schedule run past the leash: $(cat "$work/sched.out")"
      case " $(labels_of "$n") " in *" task:status:waiting-for-executor "*) ;; *) fail "tasks 7: the leash left #$n $(labels_of "$n"): $(cat "$work/sched.out")" ;; esac
      comments_of "$n" | grep -q "Reclaimed" || fail "tasks 7: no reclaim comment on #$n: $(comments_of "$n")"
      step "tasks 7: exec-b lost #$n to exec-a's earlier claim and struck itself; the leash reclaimed the silent claim"

      calls=$(gh_count 'st.calls.length')
      CLAUDINITE_TASKS_SUSPEND_ALL=1 sched run > "$work/sched.out" 2>&1 || fail "tasks 8: suspended schedule run: $(cat "$work/sched.out")"
      CLAUDINITE_TASKS_SUSPEND_ALL=1 execute || fail "tasks 8: suspended execute loop: $(cat "$work/exec.out")"
      [ "$(gh_count 'st.calls.length')" = "$calls" ] || fail "tasks 8: a suspended run reached GitHub: $(cat "$work/sched.out")"
      grep -q "the queue is held" "$work/sched.out" || fail "tasks 8: no held notice: $(cat "$work/sched.out")"
      step "tasks 8: CLAUDINITE_TASKS_SUSPEND_ALL stops both runs before any call"

      chain="Claudinite executor chain failed repeatedly"
      if (cd "$member" && GITHUB_TOKEN=rehearsal-token CLAUDINITE_CONTINUATION_DEPTH=3 .claudinite/bin/cn execute continue) > "$work/cont.out" 2>&1; then
        fail "tasks 9: execute continue at depth 3 exited zero: $(cat "$work/cont.out")"
      fi
      [ "$(issues_titled "$chain")" = 1 ] || fail "tasks 9: no chain-failure issue: $(cat "$work/cont.out")"
      failure="Claudinite scheduler run failed"
      sched report-failure > "$work/fail.out" 2>&1 || fail "tasks 9: report-failure: $(cat "$work/fail.out")"
      sched report-failure > "$work/fail.out" 2>&1 || fail "tasks 9: report-failure again: $(cat "$work/fail.out")"
      grep -q "^- commented on #" "$work/fail.out" || fail "tasks 9: the second report did not comment: $(cat "$work/fail.out")"
      [ "$(issues_titled "$failure")" = 1 ] || fail "tasks 9: report-failure filed $(issues_titled "$failure") issues: $(gh_state)"
      fn=$(item "$failure")
      [ "$(gh_count 'st.issues.find(i=>i.number==='"$fn"').comments.length')" = 1 ] || fail "tasks 9: #$fn carries $(gh_count 'st.issues.find(i=>i.number==='"$fn"').comments.length') comments"
      step "tasks 9: the chain stops at depth 3 on one issue; report-failure files once and comments after"

      mv "$member/.github/workflows/claudinite-executor.yml" "$work/executor.yml"
      if verify_out=$(cd "$member" && sh .claudinite/launch verify 2>&1); then fail "tasks 10: verify passed without the executor workflow"; fi
      case $verify_out in *"break member-workflows .github/workflows/claudinite-executor.yml"*) ;; *) fail "tasks 10: verify: $verify_out" ;; esac
      mv "$work/executor.yml" "$member/.github/workflows/claudinite-executor.yml"
      fresh=$work/tasks-init
      mkdir -p "$fresh"
      (cd "$fresh" && "$npx/.bin/cn" init --packs hello --channel canary --package "$package" --repo "$fresh") > "$work/init.out" 2>&1 \
        || fail "tasks 10: init: $(cat "$work/init.out")"
      for f in claudinite-update claudinite-ci claudinite-scheduler claudinite-executor; do
        [ -f "$fresh/.github/workflows/$f.yml" ] || fail "tasks 10: init wrote no $f.yml"
      done
      for f in tasks.GENERATED.json dashboard.GENERATED.json; do
        [ -f "$fresh/.claudinite/flat/$f" ] || fail "tasks 10: init wrote no $f"
      done
      sed 's/once a day after any commit/once a day after a commit/' "$work/task.json" > "$decl"
      if cn_member check world > "$work/world.out" 2>&1; then fail "tasks 10: check world passed a stale flat file"; fi
      grep -q "flat-declarations-current" "$work/world.out" || fail "tasks 10: flat-declarations-current did not fail it: $(cat "$work/world.out")"
      cn_member tasks flat --write > "$work/flat.out" 2>&1 || fail "tasks 10: tasks flat --write: $(cat "$work/flat.out")"
      cn_member check world > "$work/world.out" 2>&1 || fail "tasks 10: check world after tasks flat --write: $(cat "$work/world.out")"
      step "tasks 10: verify breaks without the executor; init writes four workflows and both flat files; flat-declarations-current tracks a task edit"
      ;;
    license)
      step "license: a public member on $version with a GitHub origin, ghstub and licstub"
      # The registry serves this release and $next only, none deprecated,
      # whatever the update mode left behind.
      rm -f "$dist3"/tarballs/*
      printf '{}\n' > "$work/deprecations.json"
      warm_member license
      plan_public
      origin=$work/license-origin.git
      git init -q --bare -b main "$origin"
      (cd "$member" && git init -q -b main && git add -A && git -c user.name=rehearse -c user.email=r@x -c commit.gpgsign=false commit -q -m adopt) \
        || fail "license: git setup"
      github_origin "$origin"
      (cd "$member" && git -c push.negotiate=false push -q origin main) || fail "license: push"
      start_ghstub "$origin"
      start_licstub
      cat "$work/ca.pem" "$work/gh-ca.pem" "$work/lic-ca.pem" > "$work/cas.pem"
      SSL_CERT_FILE=$work/cas.pem CURL_CA_BUNDLE=$work/cas.pem
      GITHUB_REPOSITORY=acme/member CLAUDINITE_GITHUB_API=$gh GH_TOKEN=rehearsal-token
      # The tail is 8 s here, long enough for step 2's late key.
      CLAUDINITE_LICENSE_TAIL_MS=8000
      export SSL_CERT_FILE CURL_CA_BUNDLE GITHUB_REPOSITORY CLAUDINITE_GITHUB_API GH_TOKEN CLAUDINITE_LICENSE_TAIL_MS
      sessions=$XDG_CACHE_HOME/claudinite/sessions

      # field ID KEY: one field of session ID's state file, "" when absent.
      field() {
        node -e 'try { const f = JSON.parse(require("fs").readFileSync(process.argv[1], "utf8")); const v = f[process.argv[2]]; process.stdout.write(v === undefined || v === null ? "" : String(v)) } catch (e) {}' \
          "$sessions/$1.json" "$2"
      }
      # wait_field ID KEY VALUE SECONDS: until the field reads VALUE.
      wait_field() {
        tries=0
        until [ "$(field "$1" "$2")" = "$3" ]; do
          tries=$((tries + 1))
          [ "$tries" -le $(($4 * 10)) ] || fail "license: session $1's $2 is '$(field "$1" "$2")', not '$3', after $4 s: $(cat "$sessions/$1.json" "$sessions/$1.log" 2>&1)"
          sleep 0.1
        done
      }
      # hook EVENT ID: one hook of session ID; stdout to $work/hook.out,
      # stderr to $work/hook.err.
      hook() {
        (cd "$member" && printf '{"session_id":"%s","hook_event_name":"x","tool_name":"Read","tool_input":{},"stop_hook_active":false}' "$2" \
          | CLAUDE_PROJECT_DIR=$member .claudinite/bin/cn hook "$1" > "$work/hook.out" 2> "$work/hook.err") || fail "license: hook $1 exited non-zero"
      }
      status() { cn_member license status --session "$1" > "$work/status.out" 2>&1 || fail "license: status $1: $(cat "$work/status.out")"; }
      now_ms() { node -e 'process.stdout.write(String(Date.now()))'; }
      # sleep_until T0 MS: until MS milliseconds after T0.
      sleep_until() { node -e 'const t = Number(process.argv[1]) + Number(process.argv[2]) - Date.now(); setTimeout(() => {}, Math.max(0, t))' "$1" "$2"; }
      work_checks_on() { hook stop "$1"; [ "$(cat "$work/hook.out")" = "{}" ] && ! grep -q 'work checks off' "$work/hook.err"; }

      out=$(session_start l1) || fail "license 1: SessionStart exited non-zero"
      case $out in *"[cn] license pending"*) ;; *) fail "license 1: no pending line: $out" ;; esac
      ms=$(printf '%s\n' "$out" | sed -n 's/.*\[cn\] hooks session-start ok \([0-9]*\)ms.*/\1/p')
      if [ -z "$ms" ] || [ "$ms" -ge 300 ]; then fail "license 1: SessionStart took ${ms:-?} ms: $out"; fi
      wait_field l1 state landed 5
      [ "$(field l1 path)" = web ] || fail "license 1: landed by $(field l1 path)"
      status l1
      grep -q '^key: public plan, ok' "$work/status.out" || fail "license 1: status: $(cat "$work/status.out")"
      work_checks_on l1 || fail "license 1: work checks are off with a key: $(cat "$work/hook.out" "$work/hook.err")"
      grep -q 'check run [0-9]* (Claudinite key, app claudinite)' "$sessions/l1.log" || fail "license 1: the log names no check run: $(cat "$sessions/l1.log")"
      step "license 1: SessionStart answered in ${ms} ms; the web key landed; status says public ok"

      licctl '{"delay_ms":15000}'
      t0=$(now_ms)
      out=$(session_start l2) || fail "license 2: SessionStart exited non-zero"
      case $out in *"[cn] license pending"*) ;; *) fail "license 2: no pending line: $out" ;; esac
      sleep_until "$t0" 11000
      hook pre-tool-use l2
      if ! grep -q 'App is not installed' "$work/hook.out" || ! grep -q 'installations/new' "$work/hook.out"; then
        fail "license 2: the cut's notice: $(cat "$work/hook.out" "$work/hook.err")"
      fi
      if work_checks_on l2; then fail "license 2: work checks on past the cut"; fi
      wait_field l2 state landed 9
      licctl '{"delay_ms":0}'
      landed_by=$(field l2 path)
      case $landed_by in web|hook-poll) ;; *) fail "license 2: the late key landed by '$landed_by'" ;; esac
      work_checks_on l2 || fail "license 2: work checks off after the late key: $(cat "$work/hook.err")"
      step "license 2: degraded at the cut (app-not-installed), the late key landed by $landed_by, work checks back on"

      ctl /_stub/session '{"no_app":true}'
      t0=$(now_ms)
      session_start l3 > /dev/null || fail "license 3: SessionStart exited non-zero"
      sleep_until "$t0" 11000
      hook pre-tool-use l3
      grep -q 'installations/new' "$work/hook.out" || fail "license 3: no install link: $(cat "$work/hook.out")"
      sleep_until "$t0" 19500
      grep -q '\[cn\] license request-web timeout' "$sessions/l3.log" || fail "license 3: the request did not end at the tail: $(cat "$sessions/l3.log")"
      polls=$(gh_count 'st.calls.filter(c=>c.startsWith("check-runs")).length')
      sleep 1
      [ "$(gh_count 'st.calls.filter(c=>c.startsWith("check-runs")).length')" = "$polls" ] || fail "license 3: still polling after the tail"
      ctl /_stub/session '{"no_app":false}'
      step "license 3: no App: degraded with the install link; polling stopped at the tail"

      ctl /_stub/session '{"no_push":true}'
      session_start l4 > /dev/null || fail "license 4: SessionStart exited non-zero"
      wait_field l4 cause no-push-access 3
      [ "$(field l4 dispatched)" = "" ] || fail "license 4: the dispatch counts as sent"
      if grep -q 'check run' "$sessions/l4.log"; then fail "license 4: check runs read after a refused dispatch"; fi
      ctl /_stub/session '{"no_push":false}'
      step "license 4: no push access: degraded at once, no check runs read"

      licctl '{"refuse":{"web":"no-plan"}}'
      session_start l5a > /dev/null || fail "license 5: SessionStart"
      wait_field l5a cause no-plan 5
      licctl '{"refuse":{"web":""},"plan":"public"}'
      ctl /_stub/session '{"private":true}'
      session_start l5b > /dev/null || fail "license 5: SessionStart"
      wait_field l5b cause bind-plan 5
      hook pre-tool-use l5b
      grep -q 'does not fit' "$work/hook.out" || fail "license 5: the bind-plan notice: $(cat "$work/hook.out")"
      licctl '{"state":"unverified"}'
      session_start l5c > /dev/null || fail "license 5: SessionStart"
      wait_field l5c state landed 5
      status l5c
      grep -q '^key: public plan, unverified' "$work/status.out" || fail "license 5: unverified: $(cat "$work/status.out")"
      ctl /_stub/session '{"private":false}'
      licctl '{"state":"ok","plan":"","wrong_nonce":true}'
      session_start l5d > /dev/null || fail "license 5: SessionStart"
      wait_field l5d cause bind-nonce 5
      licctl '{"wrong_nonce":false}'
      step "license 5: no-plan refused; a public key on a private repo is bind-plan, unverified applies; a foreign nonce is bind-nonce"

      before=$(cat "$sessions/l1.json")
      dispatches=$(gh_count 'st.calls.filter(c=>c.startsWith("repository-dispatch")).length')
      session_start l1 > /dev/null || fail "license 6: SessionStart"
      [ "$(cat "$sessions/l1.json")" = "$before" ] || fail "license 6: the resume rewrote the state file"
      [ "$(gh_count 'st.calls.filter(c=>c.startsWith("repository-dispatch")).length')" = "$dispatches" ] || fail "license 6: the resume asked again"
      oldnonce=$(field l1 key_nonce)
      node -e 'const fs = require("fs"), p = process.argv[1], f = JSON.parse(fs.readFileSync(p, "utf8")), day = 86400000;
        f.requested_at = new Date(Date.parse(f.requested_at) - day).toISOString(); f.landed_at = new Date(Date.parse(f.landed_at) - day).toISOString();
        fs.writeFileSync(p, JSON.stringify(f))' "$sessions/l1.json"
      hook pre-tool-use l1
      grep -q '\[cn\] license renew ok' "$work/hook.err" || fail "license 6: no renewal: $(cat "$work/hook.err")"
      status l1
      grep -q '^key: public plan, ok' "$work/status.out" || fail "license 6: the old key stopped applying: $(cat "$work/status.out")"
      tries=0
      until [ "$(field l1 key_nonce)" != "$oldnonce" ] && [ "$(field l1 key_nonce)" = "$(field l1 nonce)" ]; do
        tries=$((tries + 1))
        [ "$tries" -le 50 ] || fail "license 6: the renewed key did not land: $(cat "$sessions/l1.json")"
        sleep 0.1
      done
      step "license 6: a resume asks nothing; a day-old key renews while it still applies"

      unset GH_TOKEN
      store=$XDG_CACHE_HOME/claudinite/license
      (cd "$member" && .claudinite/bin/cn login) > "$work/login.out" 2>&1 || fail "license 7: cn login: $(cat "$work/login.out")"
      if ! grep -q 'STUB-1234' "$work/login.out" || ! grep -q '^logged in as acme-dev$' "$work/login.out"; then fail "license 7: cn login: $(cat "$work/login.out")"; fi
      # shellcheck disable=SC2012 # ls -ln is the portable way to read a mode
      case $(ls -ln "$store/login.json" | cut -d' ' -f1) in -rw-------*) ;; *) fail "license 7: login.json is not 0600" ;; esac
      session_start d1 > /dev/null || fail "license 7: SessionStart"
      wait_field d1 state landed 5
      [ "$(field d1 path)" = desktop ] || fail "license 7: landed by $(field d1 path)"
      [ -n "$(find "$store/keys" -name '*.json')" ] || fail "license 7: no key cached"
      licctl '{"down":true}'
      session_start d2 > /dev/null || fail "license 7: SessionStart"
      wait_field d2 state landed 5
      [ "$(field d2 path)" = cache ] || fail "license 7: with the server down, landed by $(field d2 path)"
      rm -rf "$store/keys"
      session_start d3 > /dev/null || fail "license 7: SessionStart"
      wait_field d3 cause server-unreachable 5
      licctl '{"down":false,"reject_token":"ghu_login-1"}'
      session_start d4 > /dev/null || fail "license 7: SessionStart"
      wait_field d4 state landed 5
      grep -q '"access_token": "ghu_refreshed-1"' "$store/login.json" || fail "license 7: no refresh stored: $(cat "$store/login.json")"
      node -e 'const fs = require("fs"), p = process.argv[1], l = JSON.parse(fs.readFileSync(p, "utf8")); l.refresh_token = "expired"; fs.writeFileSync(p, JSON.stringify(l))' "$store/login.json"
      licctl '{"reject_token":"ghu_refreshed-1"}'
      session_start d5 > /dev/null || fail "license 7: SessionStart"
      wait_field d5 cause login-expired 5
      status d5
      grep -q 'cn login' "$work/status.out" || fail "license 7: the login-expired notice names no cn login: $(cat "$work/status.out")"
      licctl '{"reject_token":""}'
      (cd "$member" && .claudinite/bin/cn login --logout) > "$work/login.out" 2>&1 || fail "license 7: logout"
      [ ! -f "$store/login.json" ] || fail "license 7: logout left login.json"
      GH_TOKEN=rehearsal-token
      export GH_TOKEN
      step "license 7: cn login; the desktop key, the cached key with the server down, a refresh on 401, login-expired, logout"

      actions_env
      update_engine() {
        cn_member update engine > "$work/update.out" 2> "$work/update.err" || fail "update engine: $(cat "$work/update.out" "$work/update.err")"
        verdict=$(sed -n '$p' "$work/update.out")
      }
      expect_verdict() { [ "$verdict" = "$1" ] || fail "license: verdict '$verdict', want '$1': $(cat "$work/update.out" "$work/update.err")"; }
      main_run success
      licctl "{\"release\":{\"held\":[\"$next\"]}}"
      update_engine
      expect_verdict "up to date"
      grep -q "^$next skipped: held (license key)$" "$work/update.out" || fail "license 8: no held skip: $(cat "$work/update.out")"
      grep -q '\[cn\] license request-actions ok' "$work/update.out" || fail "license 8: no Actions key: $(cat "$work/update.out")"
      licctl '{"release":{}}'
      update_engine
      expect_verdict "opened #1 for $next"
      step "license 8: the Actions key holds $next, then lets it through: $verdict"

      licctl '{"refuse":{"actions":"app-not-installed"}}'
      update_engine
      expect_verdict "skipped: the Claudinite App is not installed (#2)"
      update_engine
      expect_verdict "skipped: the Claudinite App is not installed (#2)"
      [ "$(gh_count 'st.issues.filter(i=>i.title==="Claudinite needs its GitHub App installed").length')" = 1 ] || fail "license 9: issues $(gh_state)"
      licctl '{"refuse":{"actions":""},"state":"degraded"}'
      : > "$work/requests.log"
      update_engine
      case $verdict in "skipped: license degraded ("*")") ;; *) fail "license 9: degraded verdict '$verdict'" ;; esac
      [ ! -s "$work/requests.log" ] || fail "license 9: npm was read under a degraded key: $(cat "$work/requests.log")"
      licctl '{"state":"ok"}'
      saved=$ACTIONS_ID_TOKEN_REQUEST_URL
      unset ACTIONS_ID_TOKEN_REQUEST_URL
      update_engine
      expect_verdict "skipped: no OIDC token (id-token: write is missing)"
      ACTIONS_ID_TOKEN_REQUEST_URL=$saved
      export ACTIONS_ID_TOKEN_REQUEST_URL
      ctl /_stub/session '{"event_name":"pull_request"}'
      update_engine
      expect_verdict "skipped: license refused (pull-request-trigger)"
      ctl /_stub/session '{"event_name":""}'
      step "license 9: no App files one install issue; degraded, no OIDC token and a pull_request trigger each skip"

      licctl '{"plan":"personal"}'
      update_engine
      expect_verdict "opened #3 for plan personal"
      day=$(cn_member version --day)
      pbranch=claudinite/plan-$day
      [ "$(git --git-dir "$origin" diff --name-only main "$pbranch")" = .claudinite/settings.yaml ] || fail "license 10: the plan branch changes more than the settings file"
      [ "$(git --git-dir "$origin" diff --numstat main "$pbranch" | cut -f1,2)" = "$(printf '1\t1')" ] || fail "license 10: not a one-line change: $(git --git-dir "$origin" diff main "$pbranch")"
      phead=$(git --git-dir "$origin" rev-parse "$pbranch")
      (cd "$member" && git fetch -q origin && git checkout -q "$phead") || fail "license 10: checkout"
      cn_member check world --pr-author 'github-actions[bot]' --base-ref origin/main > "$work/world.out" 2>&1 || fail "license 10: check world refused the bot's plan PR: $(cat "$work/world.out")"
      if cn_member check world --pr-author someone --base-ref origin/main > "$work/world.out" 2>&1; then fail "license 10: check world passed a person's plan change"; fi
      (cd "$member" && git checkout -q main) || fail "license 10: back to main"
      ctl /_stub/run "{\"sha\":\"$phead\",\"event\":\"workflow_dispatch\",\"conclusion\":\"success\"}"
      cn_member update land --pr 3 --sha "$phead" > "$work/land.out" 2>&1 || fail "license 10: land: $(cat "$work/land.out")"
      verdict=$(sed -n '$p' "$work/land.out")
      expect_verdict "landed plan personal"
      (cd "$member" && git fetch -q origin && git reset -q --hard origin/main) || fail "license 10: pull"
      grep -q '^  plan: "personal"$' "$member/.claudinite/settings.yaml" || fail "license 10: main's plan: $(cat "$member/.claudinite/settings.yaml")"
      verify_out=$(cd "$member" && .claudinite/bin/cn verify) || fail "license 10: verify: $verify_out"
      [ -z "$verify_out" ] || fail "license 10: verify reported: $verify_out"
      licctl '{"plan":""}'
      step "license 10: the key's plan opened, passed check world and landed the plan PR; verify is clean"
      ;;
  esac
done

step "ok ($package $version, $pin)"
