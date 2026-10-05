#!/bin/sh
# The local Phase 1 to 4 gates, against the built release in $DIST
# (default dist/) served by regstub, in ten modes:
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
#            merged update PR, then refuses a dist3/ whose selftest fails a
#            probe over the member or whose verify breaks it (built with the
#            rehearsal_break tag), holds back on a red main, skips held and
#            revoked versions, files the revoked pin's issue, and runs as the
#            engine/update task the scheduler files and the executor drains. Every dist is signed with the development
#            keys (testkeys/), as the updater checks signatures.
#   packs    a fresh repo adopts the hello pack through the npx bootstrap
#            from a local pack source (release/packs-fixture.sh, served by
#            release/cdnstub and read as the vendored branch), its session
#            loads the pack and its Go check blocks Stop, and cn update
#            packs proposes, refuses, skips and lands pack versions as the
#            fixture publishes and revokes them, then refuses an index whose
#            serial regressed or whose signature broke; cn init asks for no
#            license; hello's declared checks fail the
#            world and block Stop, and the member's checks block turns them
#            off and accepts them; a local pack's Go check builds and
#            finds, and hello 1.4's checks read the change and the pack's
#            config through the SDK.
#   adopt    the adoption flow over hello and the hello-asks probe: cn init
#            ends on QUESTIONS and HANDOVER with the seed written and the
#            task secret stamped, adoption-answers-pending blocks Stop until
#            cn settings answer, cn adopt does the same for a pack added
#            later, and cn init --from-node moves a Node-shaped member.
#   tasks    a member on hello 1.4 with the three workflows runs the task
#            queue against ghstub (its routine route and release/stub-
#            agent.sh): tasks list and the declaration check,
#            the scheduler filing and gating, the executor running hello's
#            worker through the SDK and landing its PR, a missing secret,
#            the routine hand-off with its nonce, a lost claim and the leash, the suspend switch, the
#            continuation chain and the failure issue, verify, init and the
#            flat declarations.
#   growth   a member declaring claudinite-growth with a GitHub origin: cn
#            growth capture writes the branch and a disjoint delta,
#            SessionEnd captures under CLAUDINITE_SESSION_ISSUE, a logs-prune item runs cn growth
#            prune as its code-work only once the captures are past
#            retention, and cn pack new leaves verify and cn provenance
#            check clean.
#   fleet    a manager declaring claudinite-fleet-sheepdog over eight acme
#            repositories (release/fleet-fixture.sh) served by ghstub and
#            licstub: cn
#            fleet roster opens one adoption issue and counts the census and
#            freshness, a second run writes nothing, a 403 is unknown with the
#            action marker, cn fleet update's dry run and live run follow each
#            member to its outcome, an owner without a fleet plan reaches no
#            member, a paid owner's sweep skips a repo another account owns,
#            and a fleet-roster item parks without FLEET_GITHUB_TOKEN and runs
#            to done with it; then, over a local pack source's signed
#            catalog, cn fleet add-packs files a suspected list, writes
#            nothing on a second run, refuses an unanswered force and files
#            the requested list, cn fleet pack-seeds splices a seed into one
#            member, and cn check world blocks on a seed the manager runs
#            differently.
#   dashboard a member declaring claudinite-dashboard in YAML with a local
#            descriptor (release/dashboard-fixture.sh): the flat files and
#            the member file, tasks flat --check, descriptor-usable on a
#            local descriptor and silent on the mount's, cn dashboard
#            descriptor; then the fleet fixture's manager declaring the
#            dashboard: cn fleet roster's artifact, a fleet-roster item
#            landing it under its policy and writing nothing again, cn
#            fleet add-packs measuring a canary member on a stable manager,
#            and publish-pages building the real pack from
#            CLAUDINITE_PACKS_TREE (a ClaudinitePacks checkout, which this
#            mode requires) and following its deploy to success.
#
# One more mode reads the real shelf and is never in the default set:
#
#   live-packs  a member declaring basics, git-github, claudinite-lifecycle,
#            claudinite-tasks, claudinite-growth, node, python and aws-sam on
#            the canary channel adopts them through the npx bootstrap from
#            the real pack CDN and ClaudinitePacks' vendored branch, each
#            index verified against the roots this release embeds (a
#            devroots build trusts the ceremony's and the development roots); its session loads them, check world runs their declared
#            and Go checks silent and a planted aws-sam violation fires; cn
#            update packs is up to date from both sources, from the branch
#            alone with the CDN unreachable, and skips naming both serials
#            when a local mirror of the branch is one release behind; every
#            relevanceDetector the live catalog carries is accepted. Built
#            at today's version when the release is older than the shelf's
#            floor. CLAUDINITE_OFFLINE=1 skips it with a notice.
#
# The update, packs, adopt, tasks and growth modes give every member a GitHub-shaped
# origin (url.<bare>.insteadOf).
#
#   release/rehearse.sh [--mode fresh|current|stale|update|packs|adopt|tasks|growth|fleet|dashboard|live-packs]   (default: the ten offline modes)
#
# UPDATE_STEPS=4 stops the update mode after the landing and the session on
# the landed version (release/hop.sh); the default, 11, runs every step.
#
# Needs go, node, curl, git and a release from release/build.sh, ideally
# built with REHEARSAL=1 BUILD_TAGS=devroots: every key, certificate and
# signature here chains to the development root (testkeys/), which only such
# a build trusts, beside the ceremony's roots the real pack shelf is signed
# under. Any other release is rebuilt so first.
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

usage="usage: release/rehearse.sh [--mode fresh|current|stale|update|packs|adopt|tasks|growth|fleet|dashboard|live-packs]"
modes="fresh current stale update packs adopt tasks growth fleet dashboard"
case $# in
  0) ;;
  2)
    [ "$1" = --mode ] || fail "$usage"
    case $2 in fresh|current|stale|update|packs|adopt|tasks|growth|fleet|dashboard|live-packs) modes=$2 ;; *) fail "unknown mode $2" ;; esac ;;
  *) fail "$usage" ;;
esac
if [ "$modes" = live-packs ] && [ "${CLAUDINITE_OFFLINE:-}" = 1 ]; then
  step "live-packs: skipped: CLAUDINITE_OFFLINE=1, and this mode reads the real pack CDN and ClaudinitePacks' vendored branch"
  exit 0
fi
# The roots a real host is checked against, before any mode trusts a stub's.
ca_base=${SSL_CERT_FILE:-/etc/ssl/certs/ca-certificates.crt}

update_steps=${UPDATE_STEPS:-11}
case $update_steps in 4|11) ;; *) fail "UPDATE_STEPS must be 4 or 11, not $update_steps" ;; esac

[ -f "$DIST/manifest.integrity" ] || fail "no release in $DIST; run release/build.sh first"
# rehearsal_sign DIST: signs DIST with the development release key.
rehearsal_sign() {
  DIST=$1 RELEASE_KEY=$root/testkeys/release.key RELEASE_CERT=$root/testkeys/release.cert.json ROOTS=$root/shared/trust/devroots \
    sh release/sign.sh > "$work/sign.out" 2>&1 || fail "signing $1: $(cat "$work/sign.out")"
}
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

# A release candidate trusts only the ceremony's roots, to which no key here
# chains: rehearse its source rebuilt at its version and package with the
# development roots. The host's binary must first be byte for byte this
# source's plain build, so the rebuild differs from the candidate only in
# what the devroots tag changes, the embedded roots (shared/trust/roots_dev.go;
# TestOnlyTheRootsFilesReadTheDevrootsTag holds it to that).
devroot=$(cat shared/trust/devroots/root.pub)
for b in "$DIST"/bin/*/*; do
  grep -qF "$devroot" "$b" && continue
  case $(uname -s)-$(uname -m) in
    Linux-x86_64) host=linux-x64 ;;
    Linux-aarch64|Linux-arm64) host=linux-arm64 ;;
    Darwin-x86_64) host=darwin-x64 ;;
    Darwin-arm64) host=darwin-arm64 ;;
    *) fail "no candidate check on $(uname -s)-$(uname -m)" ;;
  esac
  COMMIT=$(git rev-parse --short=7 HEAD 2>/dev/null || echo unknown) VERSION=$version BUILD_TAGS='' \
    sh release/gobuild.sh "$host" "$work/candidate-check" || fail "the plain build of $version for $host"
  cmp -s "$work/candidate-check" "$DIST/bin/$host/cn" \
    || fail "$DIST/bin/$host/cn is not this source's plain build of $version, so a development-roots rebuild would not stand for it"
  step "rebuilding $version with the development roots: $DIST trusts only the ceremony's, and its $host binary is this source's plain build"
  DIST=$work/devroots-dist VERSION=$version PACKAGE=$package REHEARSAL=1 BUILD_TAGS=devroots sh release/build.sh > "$work/build-devroots.out" \
    || fail "build of $version with the development roots: $(cat "$work/build-devroots.out")"
  DIST=$work/devroots-dist
  pin=$(cat "$DIST/manifest.integrity")
  break
done

next=$(printf '%s\n' "$version" | awk -F. '{ print $1 "." $2 "." $3 + 1 }')
third=$(printf '%s\n' "$version" | awk -F. '{ print $1 "." $2 "." $3 + 2 }')
dist1=$DIST
dist2=$work/dist2
dist3=$work/dist3
# dist3 is built aside and served only from update step 5 on.
dist3build=$work/dist3-build
case " $modes " in
  *" current "*|*" update "*)
    step "building $next into dist2/ from the same source"
    DIST=$dist2 VERSION=$next PACKAGE=$package REHEARSAL=1 BUILD_TAGS=devroots sh release/build.sh > "$work/build2.out" || fail "build of $next: $(cat "$work/build2.out")"
    ;;
  *) mkdir -p "$dist2/tarballs" ;;
esac
mkdir -p "$dist3/tarballs"
case " $modes " in
  *" update "*)
    if [ "$update_steps" -ge 5 ]; then
      step "building $third into dist3/ with verify broken (rehearsal_break)"
      DIST=$dist3build VERSION=$third PACKAGE=$package REHEARSAL=1 BUILD_TAGS=devroots,rehearsal_break sh release/build.sh > "$work/build3.out" \
        || fail "build of $third: $(cat "$work/build3.out")"
      rehearsal_sign "$dist3build"
    fi
    rehearsal_sign "$dist2"
    ;;
esac
case " $modes " in
  *" update "*|*" packs "*|*" adopt "*|*" tasks "*|*" growth "*|*" fleet "*|*" live-packs "*)
    # The caller's dist stays as it was: sign a copy.
    dist1=$work/dist1
    cp -R "$DIST" "$dist1"
    rehearsal_sign "$dist1"
    ;;
esac
# live-packs runs alone, so dist2's slot is free for the build it needs
# when this release predates the shelf's floor: a release of today's major
# and day is never older than the packs it ships beside.
live_version=$version live_dist=$dist1
case " $modes " in
  *" live-packs "*)
    day=$(go run ./cmd/cn version --day)
    major=$(cat release/major)
    vmajor=${version%%.*} vday=$(echo "$version" | cut -d. -f2)
    if [ "$vmajor" -lt "$major" ] || { [ "$vmajor" -eq "$major" ] && [ "$vday" -lt "$day" ]; }; then
      live_version=$major.$day.1 live_dist=$dist2
      step "live-packs: building $live_version into dist2/ from the same source, the shelf's floor being above $version"
      DIST=$dist2 VERSION=$live_version PACKAGE=$package REHEARSAL=1 BUILD_TAGS=devroots sh release/build.sh > "$work/build-live.out" || fail "build of $live_version: $(cat "$work/build-live.out")"
      rehearsal_sign "$dist2"
    fi
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

# start_ghstub ORIGIN [FLAG...]: the GitHub stub over a bare origin; sets
# gh.
start_ghstub() {
  [ -x "$work/ghstub" ] || go build -o "$work/ghstub" ./release/ghstub
  [ -n "$gh_pid" ] && kill "$gh_pid" 2>/dev/null
  rm -f "$work/gh-ready"
  gh_origin=$1
  shift
  "$work/ghstub" --origin "$gh_origin" --repo acme/member --token rehearsal-token --ready "$work/gh-ready" --ca-out "$work/gh-ca.pem" "$@" &
  gh_pid=$!
  tries=0
  until [ -f "$work/gh-ready" ]; do
    tries=$((tries + 1))
    [ "$tries" -le 100 ] || fail "ghstub did not start"
    sleep 0.1
  done
  gh=$(cat "$work/gh-ready")
}
# start_licstub: the license server stand-in a fleet run asks for its
# owner's plan; sets lic and CLAUDINITE_LICENSE_API.
start_licstub() {
  [ -x "$work/licstub" ] || go build -o "$work/licstub" ./release/licstub
  [ -n "$lic_pid" ] && kill "$lic_pid" 2>/dev/null
  rm -f "$work/lic-ready"
  "$work/licstub" --root-key testkeys/root.key --ready "$work/lic-ready" --ca-out "$work/lic-ca.pem" &
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

# fleet_rig NAME ROOT: the manager NAME declaring claudinite-fleet-sheepdog
# over release/fleet-fixture.sh's eight acme repositories under
# ROOT/fleet/, served by ghstub and licstub, the sweep environment a
# desktop runs it in, and fleet_cn.
fleet_rig() {
  warm_member "$1"
  # The newest release the registry serves: earlier modes may have
  # published $next.
  latest=$version latest_pin=$pin
  if [ -f "$dist2/manifest.integrity" ]; then latest=$next latest_pin=$(cat "$dist2/manifest.integrity"); fi
  behind=$(printf '%s\n' "$version" | awk -F. '{ print $1 - 1 "." $2 "." $3 }')
  flags=$(sh release/fleet-fixture.sh "$2" "$member" "$version" "$latest" "$behind" "$latest_pin" "$package") || fail "fleet: fixture"
  origin=$work/$1-origin.git
  git init -q --bare -b main "$origin"
  (cd "$member" && git init -q -b main && git add -A && git -c user.name=rehearse -c user.email=r@x -c commit.gpgsign=false commit -q -m adopt) \
    || fail "fleet: git setup"
  oldifs=$IFS
  IFS='
'
  # shellcheck disable=SC2086 # the fixture prints one flag per line
  set -- $flags
  IFS=$oldifs
  start_ghstub "$origin" "$@" --repo acme/manager
  start_licstub
  # Steps 1 to 5 sweep from a desktop; an earlier mode's Actions
  # identity would name a stub that is gone.
  unset ACTIONS_ID_TOKEN_REQUEST_URL ACTIONS_ID_TOKEN_REQUEST_TOKEN GITHUB_REPOSITORY_ID GITHUB_REPOSITORY_OWNER_ID GITHUB_REPOSITORY_OWNER
  cat "$work/ca.pem" "$work/gh-ca.pem" "$work/lic-ca.pem" > "$work/cas.pem"
  SSL_CERT_FILE=$work/cas.pem CURL_CA_BUNDLE=$work/cas.pem
  GITHUB_REPOSITORY=acme/manager CLAUDINITE_GITHUB_API=$gh FLEET_GITHUB_TOKEN=rehearsal-token CLAUDINITE_FLEET_POLL_MS=50
  export SSL_CERT_FILE CURL_CA_BUNDLE GITHUB_REPOSITORY CLAUDINITE_GITHUB_API FLEET_GITHUB_TOKEN CLAUDINITE_FLEET_POLL_MS
  fleet_cn() { (cd "$member" && .claudinite/bin/cn fleet "$@") > "$work/fleet.out" 2> "$work/fleet.err"; }
  adoption() { gh_count 'st.issues.filter(i=>i.labels.includes("fleet-adoption")&&i.state==="open").map(i=>i.title).join(",")'; }
  member_calls() { gh_count 'st.calls.filter(c=>c.startsWith("member ")||c==="user-repos").length'; }
  expect_line() { grep -qF -- "$1" "$work/fleet.out" || fail "fleet $2: no line '$1': $(cat "$work/fleet.out" "$work/fleet.err")"; }
}

# fleet_catalog NAME: the shelf's catalog from a local pack source, hello
# and hello-asks, each with a fingerprint, on the canary channel, served
# by cdnstub with a floor of two packs; member_issues and member_writes
# read what the sweeps leave in the members.
fleet_catalog() {
  src=$work/$1-src
  sh release/packs-fixture.sh "$src" --min-engine "$version" > "$work/fixture.out" 2>&1 || fail "$1: packs fixture: $(cat "$work/fixture.out")"
  sh release/packs-fixture.sh "$src" --publish-pack hello-asks > "$work/fixture.out" 2>&1 || fail "$1: packs fixture hello-asks: $(cat "$work/fixture.out")"
  [ -x "$work/cdnstub" ] || go build -o "$work/cdnstub" ./release/cdnstub
  "$work/cdnstub" --repo "$src/cdn.git" --ready "$work/$1-cdn-ready" --ca-out "$work/$1-cdn-ca.pem" --log "$work/$1-cdn.log" &
  cdn_pids="$cdn_pids $!"
  tries=0
  until [ -f "$work/$1-cdn-ready" ]; do
    tries=$((tries + 1))
    [ "$tries" -le 100 ] || fail "cdnstub did not start"
    sleep 0.1
  done
  cat "$work/$1-cdn-ca.pem" >> "$work/cas.pem"
  CLAUDINITE_PACKS_CDN=$(cat "$work/$1-cdn-ready") CLAUDINITE_PACKS_REPO=$src/mirror.git CLAUDINITE_FLEET_MIN_PACKS=2
  export CLAUDINITE_PACKS_CDN CLAUDINITE_PACKS_REPO CLAUDINITE_FLEET_MIN_PACKS
  member_issues() { gh_count 'st.fleet["acme/'"$1"'"].filter(i=>i.state==="open").map(i=>i.number+" "+i.title+" ["+i.labels.join(" ")+"]").join("\n")'; }
  member_writes() { gh_count 'st.calls.filter(c=>/^member \S+ (POST|PATCH|PUT|DELETE) \/(issues|contents)/.test(c)).length'; }
  suspected="Add packs: suspected"
}

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
      missing=$(printf '%s\n' "$version" | awk -F. '{ print $1 "." $2 "." $3 + 1000 }')
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
      # The member still carries the retired license block; its update PR
      # drops it.
      printf 'license:\n  plan: "public"\n' >> "$member/.claudinite/settings.yaml"
      origin=$work/origin.git
      git init -q --bare -b main "$origin"
      # The member declares a pack, so its update PR restates the member file.
      mkdir -p "$member/.claudinite/shared/packs/hello"
      printf '{\n  "version": "1.0",\n  "minEngineVersion": "%s"\n}\n' "$version" > "$member/.claudinite/shared/packs/hello/pack.json"
      printf 'packs:\n  declared:\n    - hello\n' >> "$member/.claudinite/settings.yaml"
      (cd "$member" && git init -q -b main) || fail "update: git init"
      (cd "$member" && sh .claudinite/launch rules-index) > "$work/index.out" 2>&1 || fail "update: rules-index: $(cat "$work/index.out")"
      grep -q '"hello": "1.0"' "$member/.claudinite/flat/member.GENERATED.json" || fail "update: rules-index wrote no member file holding hello"
      (cd "$member" && git add -A && git -c user.name=rehearse -c user.email=r@x commit -q -m adopt) || fail "update: git setup"
      github_origin "$origin"
      (cd "$member" && git push -q origin main) || fail "update: push"
      start_ghstub "$origin"
      cat "$work/ca.pem" "$work/gh-ca.pem" > "$work/cas.pem"
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
      [ "$(git --git-dir "$origin" diff --name-only main "$branch" | tr '\n' ' ')" = ".claudinite/flat/member.GENERATED.json .claudinite/settings.yaml " ] \
        || fail "update 1: the branch changes more than the pin and the member file: $(git --git-dir "$origin" diff --name-only main "$branch")"
      git --git-dir "$origin" show "$branch:.claudinite/flat/member.GENERATED.json" | grep -q "\"version\": \"$next\"" || fail "update 1: the member file does not state $next"
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
      grep -q "\"version\": \"$next\"" "$member/.claudinite/flat/member.GENERATED.json" || fail "update 3: main's member file does not state $next"
      if grep -q '^license:' "$member/.claudinite/settings.yaml"; then fail "update 3: main kept the retired license block"; fi
      grep -q '^  declared:$' "$member/.claudinite/settings.yaml" || fail "update 3: dropping the license block took the packs block: $(cat "$member/.claudinite/settings.yaml")"
      step "update 3: $verdict, the member file beside the pin, the retired license block dropped"

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
      hooks=$member/.claude/settings.json
      cp "$hooks" "$work/hooks.json"
      sed 's/cn hook pre-tool-use/cn hook pre-tool-uses/' "$work/hooks.json" > "$hooks"
      update_engine
      expect_verdict "skipped: selftest failed (hooks)"
      grep -q '^fail hooks: PreToolUse → cn hook pre-tool-uses' "$work/update.out" || fail "update 5: the probe's report was not printed: $(cat "$work/update.out")"
      [ -z "$(git --git-dir "$origin" branch --list 'claudinite/*')" ] || fail "update 5: a branch was pushed"
      [ "$(gh_count 'st.pulls.length')" = 1 ] || fail "update 5: a PR was opened"
      cp "$work/hooks.json" "$hooks"
      step "update 5: a hook naming an event $third does not answer: $verdict"

      update_engine
      expect_verdict "no PR: $third would break this repo"
      grep -q '^break rehearsal ' "$work/update.out" || fail "update 6: the finding was not printed: $(cat "$work/update.out")"
      [ -z "$(git --git-dir "$origin" branch --list 'claudinite/*')" ] || fail "update 6: a branch was pushed"
      [ "$(gh_count 'st.pulls.length')" = 1 ] || fail "update 6: a PR was opened"
      step "update 6: $verdict"

      main_run failure
      : > "$work/requests.log"
      update_engine
      expect_verdict "skipped: main is not green (failure)"
      [ ! -s "$work/requests.log" ] || fail "update 7: npm was read: $(cat "$work/requests.log")"
      step "update 7: $verdict"

      main_run success
      deprecate "{\"$third\": \"held: rehearsal\"}"
      update_engine
      expect_verdict "up to date"
      grep -q "^$third skipped: held" "$work/update.out" || fail "update 8: no held skip: $(cat "$work/update.out")"
      step "update 8: held $third skipped"
      deprecate "{\"$third\": \"revoked: rehearsal\"}"
      update_engine
      expect_verdict "up to date"
      grep -q "^$third skipped: revoked" "$work/update.out" || fail "update 8: no revoked skip: $(cat "$work/update.out")"
      [ "$(gh_count 'st.issues ? st.issues.length : 0')" = 0 ] || fail "update 8: an issue for a version this repo does not pin"
      step "update 8: revoked $third skipped"
      deprecate "{\"$third\": \"revoked: rehearsal\", \"$next\": \"revoked: rehearsal\"}"
      update_engine
      update_engine
      expect_verdict "up to date"
      [ "$(gh_count 'st.issues.filter(i=>i.title==="Claudinite engine '"$next"' is revoked").length')" = 1 ] || fail "update 8: issues $(gh_state)"
      [ "$(gh_count 'st.issues.length')" = 1 ] || fail "update 8: issues $(gh_state)"
      step "update 8: one issue for the revoked pin $next, kept by two runs"

      old=$work/old-shape
      sh release/member-fixture.sh "$old" "$next" "$(cat "$dist2/manifest.integrity")" "$package"
      rm "$old/.claudinite/.gitignore"
      printf '.claudinite/bin/\n' > "$old/.gitignore"
      cn_member verify --repo "$old" > "$work/verify.out" 2>&1 || fail "update 9: verify: $(cat "$work/verify.out")"
      if [ "$(grep -c '^deprecation bin-ignore ' "$work/verify.out")" != 1 ] || [ "$(wc -l < "$work/verify.out" | tr -d ' ')" != 1 ]; then
        fail "update 9: want one bin-ignore deprecation: $(cat "$work/verify.out")"
      fi
      step "update 9: the old shape is one deprecation"

      # The queue runs the same update: the scheduler files engine/update,
      # the executor runs it and closes the item on its verdicts.
      sched() { : > "$work/gh-output"; (cd "$member" && GITHUB_OUTPUT=$work/gh-output GITHUB_TOKEN=rehearsal-token .claudinite/bin/cn schedule "$@"); }
      upd="[claudinite-work] engine/update"
      main_run success
      sched run > "$work/sched.out" 2>&1 || fail "update 10: schedule run: $(cat "$work/sched.out")"
      [ "$(gh_count 'st.issues.filter(i=>i.title==="'"$upd"'").length')" = 1 ] || fail "update 10: no engine/update item: $(cat "$work/sched.out") $(gh_state)"
      n=$(gh_count 'st.issues.find(i=>i.title==="'"$upd"'").number')
      [ "$(sed -n 's/^pickable=//p' "$work/gh-output")" = true ] || fail "update 10: the gate after filing is closed: $(cat "$work/sched.out")"
      GH_TOKEN=rehearsal-token GITHUB_REF_NAME=main cn_member execute loop > "$work/exec.out" 2>&1 || fail "update 10: execute loop: $(cat "$work/exec.out")"
      [ "$(gh_count 'st.issues.find(i=>i.number==='"$n"').state')" = closed ] || fail "update 10: #$n is still open: $(cat "$work/exec.out")"
      case " $(gh_count 'st.issues.find(i=>i.number==='"$n"').labels.join(" ")') " in *" task:status:done "*) ;; *) fail "update 10: #$n did not close done: $(gh_state)" ;; esac
      gh_count 'st.issues.find(i=>i.number==='"$n"').comments.join("\n")' | grep -q -- "- cn update engine: up to date" \
        || fail "update 10: #$n does not carry the engine verdict: $(gh_count 'st.issues.find(i=>i.number==='"$n"').comments.join("\n")')"
      step "update 10: the scheduler filed #$n for engine/update; the executor ran it and closed it on its verdicts"

      sched run > "$work/sched.out" 2>&1 || fail "update 11: schedule run: $(cat "$work/sched.out")"
      [ "$(gh_count 'st.issues.filter(i=>i.title==="'"$upd"'").length')" = 1 ] || fail "update 11: a second run the same day filed another: $(gh_state)"
      cp "$root/lifecycle/workflows/templates/claudinite-update.yml" "$member/.github/workflows/"
      cn_member tasks list > "$work/list.out" 2>&1 || fail "update 11: tasks list: $(cat "$work/list.out")"
      if grep -q "^engine/update " "$work/list.out"; then fail "update 11: engine/update stands beside the update workflow: $(cat "$work/list.out")"; fi
      cn_member verify > "$work/verify.out" 2>&1 || fail "update 11: verify: $(cat "$work/verify.out")"
      grep -q "^deprecation member-workflows .github/workflows/claudinite-update.yml" "$work/verify.out" || fail "update 11: no deprecation for the update workflow: $(cat "$work/verify.out")"
      rm "$member/.github/workflows/claudinite-update.yml"
      step "update 11: a second run the same day files nothing; beside the update workflow the task stands aside and verify deprecates the workflow"
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
        .github/workflows/claudinite-ci.yml .github/workflows/claudinite-scheduler.yml \
        .github/workflows/claudinite-executor.yml .claudinite/shared/packs/hello/pack.json; do
        [ -f "$member/$f" ] || fail "packs 1: init wrote no $f"
      done
      verify_out=$(cd "$member" && sh .claudinite/launch verify) || fail "packs 1: verify: $verify_out"
      [ -z "$verify_out" ] || fail "packs 1: verify reported: $verify_out"
      if grep -q "installations/new" "$work/init.out"; then fail "packs 1: init still hands over the App install: $(cat "$work/init.out")"; fi
      (cd "$member" && git init -q -b main && git add -A && git -c user.name=rehearse -c user.email=r@x -c commit.gpgsign=false commit -q -m adopt) \
        || fail "packs 1: git setup"
      github_origin "$origin"
      (cd "$member" && git -c push.negotiate=false push -q origin main) || fail "packs 1: push"
      step "packs 1: cn init through npx adopted hello 1.0 (CDN, and the branch with the CDN down)"

      # init_with_origin NAME: cn init in a new repo whose origin is the
      # member's on GitHub.
      init_with_origin() {
        dir=$work/$1
        mkdir -p "$dir"
        (cd "$dir" && git init -q -b main && git remote add origin https://github.com/acme/member.git \
          && git config "url.$origin.insteadOf" https://github.com/acme/member.git) || fail "packs init: git setup"
        (cd "$dir" && "$npx/.bin/cn" init --packs hello --channel canary --package "$package" --repo "$dir") > "$work/init.out" 2>&1 \
          || fail "packs init: init exited non-zero: $(cat "$work/init.out")"
      }
      init_with_origin packs-init
      if grep -Eqi "(^|[^a-z])(plan|license)([^a-z]|$)|GitHub App" "$work/init.out"; then fail "packs init: init speaks of a plan, a license or the App: $(cat "$work/init.out")"; fi
      sed -n '$p' "$work/init.out" | grep -q "^NEXT: " || fail "packs init: init does not end on NEXT: $(cat "$work/init.out")"
      if grep -q '^license:' "$work/packs-init/.claudinite/settings.yaml"; then fail "packs init: init wrote a license block"; fi
      step "packs init: cn init asks for no license and writes no license block"

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
      if git --git-dir "$origin" diff --name-only main "$branch" | grep -v '^CLAUDE\.md$' | grep -Ev '^\.claudinite/flat/(claudinite-skills\.GENERATED\.md|(tasks|dashboard|member)\.GENERATED\.json)$' | grep -qv '^\.claudinite/shared/packs/hello/'; then fail "packs 5: the branch changes more than the hello pack, the skills index, the flat files and CLAUDE.md"; fi
      git --git-dir "$origin" show "$branch:.claudinite/flat/member.GENERATED.json" | grep -q '"hello": "1.4"' || fail "packs 5: the member file does not hold hello 1.4: $(git --git-dir "$origin" show "$branch:.claudinite/flat/member.GENERATED.json" 2>&1)"
      git --git-dir "$origin" show "$branch:.claudinite/flat/claudinite-skills.GENERATED.md" | grep -q hello-guide || fail "packs 5: the branch's skills index does not name hello-guide"
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

      fixture --publish v6
      update_packs
      expect_verdict "up to date"
      grep -q '^hello 1.8 skipped: names a Node engine version$' "$work/update.out" || fail "packs 8b: no Node-floor skip: $(cat "$work/update.out")"
      step "packs 8b: hello 1.8, whose floor names a Node engine version, skipped and never an error"

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
      step "tasks: a member on hello 1.4 with the three workflows, against ghstub's routine route"
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
      mkdir -p "$member" "$member-home" "$member-cache"
      HOME=$member-home XDG_CACHE_HOME=$member-cache
      export HOME XDG_CACHE_HOME
      cat "$work/ca.pem" "$work/gh-ca.pem" "$work/tasks-cdn-ca.pem" > "$work/cas.pem"
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
        print "        disabledTasks:"; print "          - engine/update"
        print "        agenticTaskInvocationEndpoints:"; print "          default:"; print "            url: \"" url "\"" }' \
        "$member/.claudinite/settings.yaml" > "$work/settings.yaml"
      mv "$work/settings.yaml" "$member/.claudinite/settings.yaml"
      # The adoption is three days old, so the repo starts quiet.
      old=$(date -u -d '3 days ago' +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -v-3d +%Y-%m-%dT%H:%M:%SZ)
      gitc() { (cd "$member" && git -c user.name=rehearse -c user.email=r@x -c commit.gpgsign=false "$@"); }
      (cd "$member" && git init -q -b main) || fail "tasks: git init"
      # The adopt commit holds the flat files that declaration produces.
      (cd "$member" && sh .claudinite/launch rules-index) > "$work/index.out" 2>&1 || fail "tasks: rules-index: $(cat "$work/index.out")"
      (cd "$member" && git add -A) || fail "tasks: git add"
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

      cn_member work create hello/hello-agent --qualifier probe > "$work/create.out" 2>&1 || fail "tasks 5: work create: $(cat "$work/create.out")"
      n=$(item "[claudinite-work] hello/hello-agent probe")
      [ "$n" -gt 0 ] || fail "tasks 5: work create filed nothing: $(cat "$work/create.out")"
      execute || fail "tasks 5: execute loop: $(cat "$work/exec.out")"
      if comments_of "$n" | grep -q "claudinite-grant"; then fail "tasks 5: a grant comment on #$n: $(comments_of "$n")"; fi
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
      step "tasks 5: the hand-off fired and validated with no license server; the stub agent converged the item; a routine down parks action"

      HELLO_FOLD_SECRET=rehearsal-secret
      export HELLO_FOLD_SECRET
      cn_member work create hello/hello-fold --qualifier keyless > "$work/create.out" 2>&1 || fail "tasks 6: work create: $(cat "$work/create.out")"
      folded=$(item "[claudinite-work] hello/hello-fold keyless")
      execute || fail "tasks 6: execute loop: $(cat "$work/exec.out")"
      [ "$(state_of "$folded")" = closed ] || fail "tasks 6: hello-fold #$folded did not run: $(labels_of "$folded") $(cat "$work/exec.out")"
      if grep -qi "license" "$work/exec.out"; then fail "tasks 6: the executor spoke of a license: $(cat "$work/exec.out")"; fi
      pull_after
      step "tasks 6: hello-fold #$folded ran with no license server and the executor said nothing of a license"

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
      for f in claudinite-ci claudinite-scheduler claudinite-executor; do
        [ -f "$fresh/.github/workflows/$f.yml" ] || fail "tasks 10: init wrote no $f.yml"
      done
      [ ! -e "$fresh/.github/workflows/claudinite-update.yml" ] || fail "tasks 10: init wrote the superseded update workflow"
      for f in tasks.GENERATED.json dashboard.GENERATED.json member.GENERATED.json; do
        [ -f "$fresh/.claudinite/flat/$f" ] || fail "tasks 10: init wrote no $f"
      done
      sed 's/once a day after any commit/once a day after a commit/' "$work/task.json" > "$decl"
      if cn_member check world > "$work/world.out" 2>&1; then fail "tasks 10: check world passed a stale flat file"; fi
      grep -q "flat-declarations-current" "$work/world.out" || fail "tasks 10: flat-declarations-current did not fail it: $(cat "$work/world.out")"
      cn_member tasks flat --write > "$work/flat.out" 2>&1 || fail "tasks 10: tasks flat --write: $(cat "$work/flat.out")"
      cn_member check world > "$work/world.out" 2>&1 || fail "tasks 10: check world after tasks flat --write: $(cat "$work/world.out")"
      step "tasks 10: verify breaks without the executor; init writes three workflows and the three flat files; flat-declarations-current tracks a task edit"
      ;;
    adopt)
      step "adopt: a local pack source with hello 1.0 and hello-asks 1.0, the CDN stub and ghstub"
      src=$work/adoptsrc
      sh release/packs-fixture.sh "$src" --min-engine "$version" > "$work/fixture.out" 2>&1 || fail "adopt: fixture: $(cat "$work/fixture.out")"
      sh release/packs-fixture.sh "$src" --publish-pack hello-asks > "$work/fixture.out" 2>&1 || fail "adopt: fixture hello-asks: $(cat "$work/fixture.out")"
      [ -x "$work/cdnstub" ] || go build -o "$work/cdnstub" ./release/cdnstub
      "$work/cdnstub" --repo "$src/cdn.git" --ready "$work/adopt-cdn-ready" --ca-out "$work/adopt-cdn-ca.pem" --log "$work/adopt-cdn.log" &
      cdn_pids="$cdn_pids $!"
      tries=0
      until [ -f "$work/adopt-cdn-ready" ]; do
        tries=$((tries + 1))
        [ "$tries" -le 100 ] || fail "cdnstub did not start"
        sleep 0.1
      done
      origin=$work/adopt-origin.git
      git init -q --bare -b main "$origin"
      start_ghstub "$origin"
      cat "$work/ca.pem" "$work/gh-ca.pem" "$work/adopt-cdn-ca.pem" > "$work/cas.pem"
      SSL_CERT_FILE=$work/cas.pem CURL_CA_BUNDLE=$work/cas.pem
      GITHUB_REPOSITORY=acme/member CLAUDINITE_GITHUB_API=$gh GH_TOKEN=rehearsal-token
      CLAUDINITE_PACKS_CDN=$(cat "$work/adopt-cdn-ready") CLAUDINITE_PACKS_REPO=$src/mirror.git
      export SSL_CERT_FILE CURL_CA_BUNDLE GITHUB_REPOSITORY CLAUDINITE_GITHUB_API GH_TOKEN CLAUDINITE_PACKS_CDN CLAUDINITE_PACKS_REPO
      name=${package#@claudinite/}
      npx=$work/adopt-npx/node_modules
      mkdir -p "$npx/@claudinite" "$npx/.bin"
      cp -R "$dist1/npm/$name/package" "$npx/$package"
      chmod 0755 "$npx/$package/launch"
      ln -s "../$package/launch" "$npx/.bin/cn"
      HOME=$work/adopt-home XDG_CACHE_HOME=$work/adopt-cache
      export HOME XDG_CACHE_HOME
      mkdir -p "$HOME" "$XDG_CACHE_HOME"
      # adopt_repo NAME: a new repo with one commit on main and a branch the
      # adoption lands on, as a session's would be; sets member.
      adopt_repo() {
        member=$work/$1
        mkdir -p "$member"
        (cd "$member" && git init -q -b main && printf '# member\n' > README.md && git add README.md \
          && git -c user.name=rehearse -c user.email=r@x -c commit.gpgsign=false commit -q -m base && git checkout -q -b adopt) \
          || fail "adopt: git setup for $1"
      }
      # launch ARGS: the member's launcher, as a session runs cn.
      launch() { (cd "$member" && sh .claudinite/launch "$@"); }
      # lifecycle_standin: claudinite-lifecycle, whose built-ins the steps run,
      # declared and standing in as its manifest alone, as nothing here
      # publishes it.
      lifecycle_standin() {
        mkdir -p "$member/.claudinite/shared/packs/claudinite-lifecycle"
        printf '{\n  "version": "1.0",\n  "minEngineVersion": "%s"\n}\n' "$version" > "$member/.claudinite/shared/packs/claudinite-lifecycle/pack.json"
        awk '{ print } /^  declared:$/ { print "    - claudinite-lifecycle" }' "$member/.claudinite/settings.yaml" > "$work/settings.yaml" \
          || fail "adopt: declaring claudinite-lifecycle"
        mv "$work/settings.yaml" "$member/.claudinite/settings.yaml"
        launch rules-index > /dev/null || fail "adopt: rules-index"
      }
      stop_hook() { (cd "$member" && printf '{"session_id":"rehearse","hook_event_name":"Stop","stop_hook_active":false}' | CLAUDE_PROJECT_DIR=$member sh -c "$(hook_command Stop)" 2>/dev/null); }
      selftest_member() { launch selftest --repo "$member" > "$work/selftest.out" 2>&1 || fail "adopt $1: selftest --repo: $(cat "$work/selftest.out")"; }
      # expect_blocks STEP FILE: the QUESTIONS, HANDOVER and NEXT blocks for
      # hello-asks in an init or adopt output.
      expect_blocks() {
        grep -q '^QUESTIONS — 1 adoption question(s) unanswered' "$2" || fail "adopt $1: no QUESTIONS block: $(cat "$2")"
        grep -q '^  hello-asks/goals: What should the hello probe prove on this repo?$' "$2" || fail "adopt $1: the question is not listed: $(cat "$2")"
        grep -q '^seeded docs/hello-asks.md$' "$2" || fail "adopt $1: nothing seeded: $(cat "$2")"
        grep -q '^stamped HELLO_TOKEN into .github/workflows/claudinite-executor.yml$' "$2" || fail "adopt $1: HELLO_TOKEN not stamped: $(cat "$2")"
        grep -q '^  \[ \] (hello-asks) Add the Actions secret HELLO_TOKEN (any value)$' "$2" || fail "adopt $1: no handover row: $(cat "$2")"
        sed -n '$p' "$2" | grep -q '^NEXT: ' || fail "adopt $1: does not end on NEXT: $(cat "$2")"
        cmp -s "$member/docs/hello-asks.md" release/testdata/hello-asks/templates/hello-asks.md || fail "adopt $1: the seed is not the template"
        grep -qF "          HELLO_TOKEN: \${{ secrets.HELLO_TOKEN }}" "$member/.github/workflows/claudinite-executor.yml" \
          || fail "adopt $1: the executor carries no HELLO_TOKEN line: $(cat "$member/.github/workflows/claudinite-executor.yml")"
      }

      adopt_repo adopt-init
      (cd "$member" && "$npx/.bin/cn" init --packs hello,hello-asks --channel canary --package "$package" --repo "$member") > "$work/init.out" 2>&1 \
        || fail "adopt 1: init exited non-zero with questions pending: $(cat "$work/init.out")"
      expect_blocks 1 "$work/init.out"
      grep -q '^  \[ \] (cn) In the repository.s Settings > Actions > General' "$work/init.out" || fail "adopt 1: no Actions setting row: $(cat "$work/init.out")"
      verify_out=$(launch verify) || fail "adopt 1: verify: $verify_out"
      [ -z "$verify_out" ] || fail "adopt 1: verify reported: $verify_out"
      selftest_member 1
      step "adopt 1: cn init --packs hello,hello-asks exits 0 with QUESTIONS and HANDOVER, the seed written, HELLO_TOKEN stamped"

      lifecycle_standin
      # Work checks run under a key: the session's request needs the origin.
      github_origin "$origin"
      (cd "$member" && git -c push.negotiate=false push -q origin main) || fail "adopt 2: push"
      session_start > /dev/null || fail "adopt 2: SessionStart"
      out=$(stop_hook)
      case $out in *'"decision":"block"'*adoption-answers-pending*) ;; *) fail "adopt 2: Stop did not block on the pending answer: $out" ;; esac
      launch settings answer hello-asks/goals "n/a — none wanted" > "$work/answer.out" 2>&1 || fail "adopt 2: settings answer: $(cat "$work/answer.out")"
      grep -q '^  declared:$' "$member/.claudinite/settings.yaml" || fail "adopt 2: the answer rewrote the settings: $(cat "$member/.claudinite/settings.yaml")"
      grep -q 'n/a — none wanted' "$member/.claudinite/settings.yaml" || fail "adopt 2: no answer recorded: $(cat "$member/.claudinite/settings.yaml")"
      out=$(stop_hook)
      case $out in *adoption-answers-pending*) fail "adopt 2: Stop still blocks after the answer: $out" ;; esac
      launch check world > "$work/world.out" 2>&1 || fail "adopt 2: check world: $(cat "$work/world.out")"
      selftest_member 2
      step "adopt 2: adoption-answers-pending blocks Stop until cn settings answer records it; check world is clean"

      adopt_repo adopt-later
      (cd "$member" && "$npx/.bin/cn" init --packs hello --channel canary --package "$package" --repo "$member") > "$work/init.out" 2>&1 \
        || fail "adopt 3: init: $(cat "$work/init.out")"
      if grep -q '^QUESTIONS' "$work/init.out"; then fail "adopt 3: hello asks nothing, yet: $(cat "$work/init.out")"; fi
      launch adopt hello-asks > "$work/adopt.out" 2>&1 || fail "adopt 3: cn adopt: $(cat "$work/adopt.out")"
      grep -q '^pack: hello-asks 1.0$' "$work/adopt.out" || fail "adopt 3: hello-asks not vendored: $(cat "$work/adopt.out")"
      expect_blocks 3 "$work/adopt.out"
      if launch adopt hello-asks > "$work/adopt.out" 2>&1; then fail "adopt 3: a second cn adopt hello-asks passed"; fi
      verify_out=$(launch verify) || fail "adopt 3: verify: $verify_out"
      [ -z "$verify_out" ] || fail "adopt 3: verify reported: $verify_out"
      selftest_member 3
      step "adopt 3: cn adopt hello-asks on a member holding hello seeds, stamps and prints the same blocks"

      unset GH_TOKEN
      if (cd "$member" && sh .claudinite/launch login) > "$work/login.out" 2>&1; then fail "adopt 4: cn login still runs: $(cat "$work/login.out")"; fi
      dir=$work/adopt-no-plan
      mkdir -p "$dir"
      (cd "$dir" && git init -q -b main && git remote add origin https://github.com/acme/member.git \
        && git config "url.$origin.insteadOf" https://github.com/acme/member.git) || fail "adopt 4: git setup"
      (cd "$dir" && "$npx/.bin/cn" init --packs hello --channel canary --package "$package" --repo "$dir") > "$work/init.out" 2>&1 \
        || fail "adopt 4: init: $(cat "$work/init.out")"
      if grep -Eqi "(^|[^a-z])(plan|license)([^a-z]|$)|GitHub App" "$work/init.out"; then fail "adopt 4: init speaks of a plan, a license or the App: $(cat "$work/init.out")"; fi
      if grep -q '^license:' "$dir/.claudinite/settings.yaml"; then fail "adopt 4: init wrote a license block"; fi
      GH_TOKEN=rehearsal-token
      export GH_TOKEN
      member=$dir
      selftest_member 4
      step "adopt 4: with no credential and no license server, cn init asks for no plan; cn login is gone"

      member=$work/adopt-node
      cp -R lifecycle/adopt/testdata/node-member "$member"
      printf '{\n  "packs": [\n    "hello",\n    "local/mine"\n  ]\n}\n' > "$member/.claudinite-settings.json"
      printf '{}\n' > "$member/.claudinite/local/packs/mine/pack.json"
      printf 'node_modules/\n' > "$member/.gitignore"
      rm "$member/.github/workflows/ci.yml"
      (cd "$member" && "$npx/.bin/cn" init --from-node --channel canary --package "$package" --repo "$member") > "$work/init.out" 2>&1 \
        || fail "adopt 5: init --from-node: $(cat "$work/init.out")"
      grep -q '^pack: hello 1.0$' "$work/init.out" || fail "adopt 5: hello not vendored: $(cat "$work/init.out")"
      grep -q '^NEXT: git rm .claudinite-settings.json' "$work/init.out" || fail "adopt 5: NEXT does not start with the declaration's removal: $(cat "$work/init.out")"
      [ ! -e "$member/.claudinite/shared/engine" ] || fail "adopt 5: the Node engine survived the move"
      verify_out=$(launch verify) || fail "adopt 5: verify: $verify_out"
      [ "$verify_out" = "deprecation node-leftovers .claudinite-settings.json: the Node engine's declaration, which cn no longer reads; the move pull request deletes it" ] \
        || fail "adopt 5: verify reported: $verify_out"
      selftest_member 5
      step "adopt 5: cn init --from-node moves the Node-shaped fixture; verify names the declaration alone"
      ;;
    growth)
      step "growth: a member declaring claudinite-growth with a GitHub origin and ghstub"
      rm -f "$dist3"/tarballs/*
      printf '{}\n' > "$work/deprecations.json"
      warm_member growth
      # The packs the steps read, each standing in as its manifest, and
      # claudinite-growth's logs-prune task as the pack declares it.
      for p in claudinite-growth claudinite-tasks claudinite-lifecycle; do
        mkdir -p "$member/.claudinite/shared/packs/$p"
        printf '{\n  "version": "1.0",\n  "minEngineVersion": "%s"\n}\n' "$version" > "$member/.claudinite/shared/packs/$p/pack.json"
      done
      mkdir -p "$member/.claudinite/shared/packs/claudinite-growth/tasks/logs-prune"
      cat > "$member/.claudinite/shared/packs/claudinite-growth/tasks/logs-prune/task.json" <<'JSON'
{
  "id": "logs-prune",
  "description": "Retention on the conversation-logs branch.",
  "trigger": "request",
  "preconditions": ["log-past-retention"],
  "expected_outcome": "no_code_changes",
  "code_work": "cn growth prune",
  "code_work_timeout": 60
}
JSON
      cat >> "$member/.claudinite/settings.yaml" <<'YAML'
packs:
  declared:
    - claudinite-growth
    - claudinite-lifecycle
    - id: claudinite-tasks
      config:
        disabledTasks:
          - engine/update
YAML
      # The rules index and its import, as init writes them.
      (cd "$member" && .claudinite/bin/cn rules-index) > "$work/index.out" 2>&1 || fail "growth: rules-index: $(cat "$work/index.out")"
      printf '@.claudinite/flat/claudinite-rules.GENERATED.md\n' > "$member/CLAUDE.md"
      origin=$work/growth-origin.git
      git init -q --bare -b main "$origin"
      gitc() { (cd "$member" && git -c user.name=rehearse -c user.email=r@x -c commit.gpgsign=false "$@"); }
      (cd "$member" && git init -q -b main && git add -A) || fail "growth: git setup"
      gitc commit -q -m adopt || fail "growth: adopt commit"
      github_origin "$origin"
      (cd "$member" && git -c push.negotiate=false push -q origin main) || fail "growth: push"
      start_ghstub "$origin"
      cat "$work/ca.pem" "$work/gh-ca.pem" > "$work/cas.pem"
      SSL_CERT_FILE=$work/cas.pem CURL_CA_BUNDLE=$work/cas.pem
      GITHUB_REPOSITORY=acme/member CLAUDINITE_GITHUB_API=$gh GH_TOKEN=rehearsal-token GITHUB_REF_NAME=main
      export SSL_CERT_FILE CURL_CA_BUNDLE GITHUB_REPOSITORY CLAUDINITE_GITHUB_API GH_TOKEN GITHUB_REF_NAME
      captures() { git --git-dir "$origin" ls-tree --name-only conversation-logs 2>/dev/null | grep -c '\.jsonl$' || :; }
      # transcript NAME PART...: a transcript of the growth-capture fixture's parts.
      fixture=$root/parity/testdata/scenarios/growth-capture/second-merge-delta
      transcript() { t=$work/$1.jsonl; shift; : > "$t"; for p in "$@"; do cat "$fixture/$p" >> "$t"; done; }
      session_end() {
        (cd "$member" && printf '{"hook_event_name":"SessionEnd","session_id":"%s","transcript_path":"%s","cwd":"%s"}' "$1" "$work/$1.jsonl" "$member" \
          | CLAUDE_PROJECT_DIR=$member CLAUDINITE_SESSION_ISSUE=$2 sh -c "$(hook_command SessionEnd)" > "$work/end.out" 2> "$work/end.err") \
          || fail "growth: SessionEnd $1 exited non-zero: $(cat "$work/end.err")"
        [ "$(cat "$work/end.out")" = "{}" ] || fail "growth: SessionEnd $1 answered $(cat "$work/end.out")"
      }

      transcript g1 part1.jsonl
      cn_member growth capture --pr 1 --transcript "$work/g1.jsonl" > "$work/cap.out" 2> "$work/cap.err" || fail "growth 1: capture: $(cat "$work/cap.out" "$work/cap.err")"
      names=$(git --git-dir "$origin" ls-tree --name-only conversation-logs) || fail "growth 1: no conversation-logs branch: $(cat "$work/cap.err")"
      case $names in *--pr-1--g1.jsonl*README.md) ;; *) fail "growth 1: the branch holds $names" ;; esac
      grep -q '^\[cn\] growth capture ok ' "$work/cap.err" || fail "growth 1: no breadcrumb: $(cat "$work/cap.err")"
      step "growth 1: the first capture created the branch with its README and one file"

      transcript g1 part1.jsonl part2.jsonl
      cn_member growth capture --pr 2 --transcript "$work/g1.jsonl" > "$work/cap.out" 2> "$work/cap.err" || fail "growth 2: capture: $(cat "$work/cap.out" "$work/cap.err")"
      grep -q '(delta since ' "$work/cap.out" || fail "growth 2: not a delta: $(cat "$work/cap.out")"
      second=$(git --git-dir "$origin" ls-tree --name-only conversation-logs | grep -- '--pr-2--g1.jsonl') || fail "growth 2: no second file"
      git --git-dir "$origin" show "conversation-logs:$second" | grep -q 'second merge' || fail "growth 2: the delta lacks the new lines"
      if git --git-dir "$origin" show "conversation-logs:$second" | grep -q 'first merge'; then fail "growth 2: the delta repeats the first capture"; fi
      step "growth 2: a second capture wrote only the delta, in a second file"

      session_start g3 > /dev/null || fail "growth 3: SessionStart exited non-zero"
      transcript g3 part1.jsonl
      session_end g3 5
      git --git-dir "$origin" ls-tree --name-only conversation-logs | grep -q -- '--issue-5--g3.jsonl$' || fail "growth 3: no issue-5 file: $(cat "$work/end.err")"
      grep -q '^\[cn\] growth capture ok ' "$work/end.err" || fail "growth 3: no capture breadcrumb: $(cat "$work/end.err")"
      step "growth 3: SessionEnd under CLAUDINITE_SESSION_ISSUE=5 captured --issue-5--, answered {} and exit 0"

      actions_env
      execute() { (cd "$member" && GITHUB_TOKEN=rehearsal-token CLAUDINITE_NOW=${1:-} .claudinite/bin/cn execute loop) > "$work/exec.out" 2>&1; }
      item() { gh_count 'Math.max(0,...st.issues.filter(i=>i.title==="'"$1"'").map(i=>i.number))'; }
      labels_of() { gh_count 'st.issues.find(i=>i.number==='"$1"').labels.join(" ")'; }
      prune="[claudinite-work] claudinite-growth/logs-prune"
      cn_member work create claudinite-growth/logs-prune --qualifier now > "$work/create.out" 2>&1 || fail "growth 5: work create: $(cat "$work/create.out")"
      (cd "$member" && GITHUB_OUTPUT=$work/gh-output GITHUB_TOKEN=rehearsal-token .claudinite/bin/cn schedule run) > "$work/sched.out" 2>&1 || fail "growth 5: schedule run: $(cat "$work/sched.out")"
      n=$(item "$prune now")
      execute || fail "growth 5: execute: $(cat "$work/exec.out")"
      case " $(labels_of "$n") " in *" task:status:done "*) fail "growth 5: #$n ran inside retention: $(cat "$work/exec.out")" ;; esac
      [ "$(captures)" = 3 ] || fail "growth 5: a prune inside retention removed files"
      later=$(date -u -d '11 days' +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -v+11d +%Y-%m-%dT%H:%M:%SZ)
      cn_member work create claudinite-growth/logs-prune --qualifier later > "$work/create.out" 2>&1 || fail "growth 5: work create: $(cat "$work/create.out")"
      n=$(item "$prune later")
      execute "$later" || fail "growth 5: execute 11 days on: $(cat "$work/exec.out")"
      case " $(labels_of "$n") " in *" task:status:done "*) ;; *) fail "growth 5: 11 days on, #$n is $(labels_of "$n"): $(cat "$work/exec.out")" ;; esac
      [ "$(captures)" = 0 ] || fail "growth 5: the prune left $(git --git-dir "$origin" ls-tree --name-only conversation-logs)"
      git --git-dir "$origin" ls-tree --name-only conversation-logs | grep -qx README.md || fail "growth 5: the prune removed the README"
      step "growth 5: logs-prune declined inside retention; 11 days on cn growth prune removed the three captures and kept the README"

      cn_member pack new acme --belongs "acme's own conventions" > "$work/pack.out" 2>&1 || fail "growth 6: pack new: $(cat "$work/pack.out")"
      grep -q "^declared local/acme in .claudinite/settings.yaml$" "$work/pack.out" || fail "growth 6: pack new said $(cat "$work/pack.out")"
      grep -q "^wrote .claudinite/flat/claudinite-rules.GENERATED.md$" "$work/pack.out" || fail "growth 6: pack new did not converge the rules index: $(cat "$work/pack.out")"
      verify_out=$(cd "$member" && .claudinite/bin/cn verify) || fail "growth 6: verify: $verify_out"
      [ -z "$verify_out" ] || fail "growth 6: verify reported: $verify_out"
      cn_member provenance check acme > "$work/prov.out" 2>&1 || fail "growth 6: provenance check: $(cat "$work/prov.out")"
      printf '## %s · scope-changed · acme owns its widgets\n- **Reason:** the first lesson landed.\n- **Actor:** @rehearse (owner).\n- **Mechanism:** the pack manifest.\n' "$(date -u +%Y-%m-%d)" \
        | cn_member provenance append acme _pack > "$work/prov.out" 2>&1 || fail "growth 6: provenance append: $(cat "$work/prov.out")"
      grep -q 'scope-changed · acme owns its widgets' "$member/.claudinite/local/packs/acme/provenance/_pack.md" || fail "growth 6: the entry is not in the file"
      step "growth 6: cn pack new declared local/acme and wrote the rules index; verify and cn provenance check are clean; append wrote the entry"
      ;;
    fleet)
      step "fleet: a manager declaring claudinite-fleet-sheepdog over eight acme repositories, ghstub and licstub"
      fleet_rig fleet "$work"

      fleet_cn roster || fail "fleet 1: roster: $(cat "$work/fleet.out" "$work/fleet.err")"
      expect_line '| 4 | 1 | 1 | 1 | 1 | 0 |' 1
      expect_line '| 1 | 1 | 1 | 0 | 1 | 1 | 1 | 2 | 0 |' 1
      [ "$(adoption)" = "Adopt acme/uncovered into the Claudinite fleet" ] || fail "fleet 1: adoption issues $(adoption)"
      grep -q '^\[cn\] fleet roster ok 5/9 ' "$work/fleet.err" || fail "fleet 1: breadcrumb: $(cat "$work/fleet.err")"
      step "fleet 1: the roster opened one adoption issue, for acme/uncovered; covered 4, dormant 1, ignored 1, skipped 1; fresh, behind, no scheduler and node 1 each"

      before=$(gh_count 'st.calls.filter(c=>/^(POST|PATCH) /.test(c)||c.startsWith("issue")).length')
      issues=$(gh_count 'st.issues.length')
      fleet_cn roster || fail "fleet 2: roster: $(cat "$work/fleet.out" "$work/fleet.err")"
      [ "$(gh_count 'st.issues.length')" = "$issues" ] || fail "fleet 2: a second roster filed an issue: $(gh_state)"
      [ "$(adoption)" = "Adopt acme/uncovered into the Claudinite fleet" ] || fail "fleet 2: adoption issues $(adoption)"
      [ "$before" = "$(gh_count 'st.calls.filter(c=>/^(POST|PATCH) /.test(c)||c.startsWith("issue")).length')" ] || fail "fleet 2: a second roster wrote"
      step "fleet 2: a second roster wrote nothing"

      ctl /_stub/deny '{"repo":"acme/behind","path":".claudinite"}'
      if fleet_cn roster; then fail "fleet 3: a denied member read as known: $(cat "$work/fleet.out")"; fi
      grep -q 'Contents' "$work/fleet.err" || fail "fleet 3: no Contents hint: $(cat "$work/fleet.err")"
      grep -q '^claudinite-needs-human: action — ' "$work/fleet.err" || fail "fleet 3: no action marker: $(cat "$work/fleet.err")"
      grep -q '^\[cn\] fleet roster unknown ' "$work/fleet.err" || fail "fleet 3: breadcrumb: $(cat "$work/fleet.err")"
      ctl /_stub/deny '{"repo":"acme/behind","deny":false}'
      step "fleet 3: a 403 on acme/behind's contents is unknown, exit 1, with the Contents hint and the action marker"

      CLAUDINITE_CONTEXT='DRY_RUN=true' fleet_cn update || fail "fleet 4: dry run: $(cat "$work/fleet.out" "$work/fleet.err")"
      for m in current behind noscheduler nodemember; do expect_line "acme/$m" 4; done
      bt='`'
      expect_line "${bt}acme/dormant${bt} — **dormant**" 4
      expect_line "${bt}acme/ignored${bt} — **excluded**" 4
      [ "$(gh_count 'st.dispatches.filter(d=>d.repo).length')" = 0 ] || fail "fleet 4: a dry run dispatched"
      step "fleet 4: a dry run would fire current, behind, noscheduler and nodemember, skipped dormant and ignored by name, and dispatched nothing"

      ctl /_stub/advance "{\"repo\":\"acme/behind\",\"files\":{\".claudinite/settings.yaml\":\"engine:\\n  package: \\\"$package\\\"\\n  version: \\\"$latest\\\"\\n  manifest: \\\"$latest_pin\\\"\\npacks:\\n  channel: \\\"canary\\\"\\n\"}}"
      ctl /_stub/advance '{"repo":"acme/nodemember","files":{".claudinite-settings.json":"{\"engineVersion\": \"61002.1\", \"packs\": [{\"id\": \"basics\", \"version\": \"3.0\"}]}\n"}}'
      if CLAUDINITE_CONTEXT='FOLLOW_MINUTES=0.05' fleet_cn update; then fail "fleet 5: a failed dispatch passed: $(cat "$work/fleet.out")"; fi
      expect_line "[${bt}acme/behind${bt}]" 5
      grep -A1 -F '**Updated during this run**' "$work/fleet.out" | grep -qF 'acme/behind' || fail "fleet 5: behind not updated: $(cat "$work/fleet.out")"
      grep -A1 -F '**Already current**' "$work/fleet.out" | grep -qF 'acme/current' || fail "fleet 5: current not already current: $(cat "$work/fleet.out")"
      grep -A1 -F '**Moved during this run**' "$work/fleet.out" | grep -qF 'acme/nodemember' || fail "fleet 5: nodemember not moved: $(cat "$work/fleet.out")"
      expect_line "${bt}acme/noscheduler${bt} — **no-scheduler**" 5
      grep -q '^\[cn\] fleet update error 3/3 ' "$work/fleet.err" || fail "fleet 5: breadcrumb: $(cat "$work/fleet.out" "$work/fleet.err")"
      [ "$(gh_count 'st.dispatches.filter(d=>d.repo&&d.inputs.wake==="update").length')" = 3 ] || fail "fleet 5: dispatches $(gh_state)"
      step "fleet 5: live, behind updated, current already current, nodemember moved, noscheduler's dispatch failed; exit 1"

      actions_env
      member_reads() { gh_count 'st.calls.filter(c=>c.startsWith("member ")).length'; }
      calls=$(member_calls)
      if fleet_cn roster; then fail "fleet 6: a public plan swept"; fi
      grep -q "^claudinite-needs-human: action — \[cn\] fleet: refused: acme is on the public plan, which does not include a fleet" "$work/fleet.err" || fail "fleet 6: no plan notice: $(cat "$work/fleet.err")"
      [ "$(member_calls)" = "$calls" ] || fail "fleet 6: a public plan reached a member"
      licctl '{"plan":"personal","owner_login":"someone-else"}'
      calls=$(member_reads)
      if fleet_cn roster; then fail "fleet 6: a sweep over another account's repos passed"; fi
      grep -q '^\[cn\] fleet: acme/current is not reached: the fleet.s personal plan covers the repos someone-else owns' "$work/fleet.err" || fail "fleet 6: no per-repo notice: $(cat "$work/fleet.err")"
      [ "$(member_reads)" = "$calls" ] || fail "fleet 6: another account's repo was read"
      licctl '{"owner_login":""}'
      fleet_cn roster || fail "fleet 6: the paid owner's roster: $(cat "$work/fleet.out" "$work/fleet.err")"
      if grep -q 'unverified\|not reached' "$work/fleet.err"; then fail "fleet 6: the paid owner's roster: $(cat "$work/fleet.err")"; fi
      step "fleet 6: a public plan parks action and reaches no member; a key naming another account reads none of acme's repos; acme's Personal plan sweeps"

      execute() { (cd "$member" && GITHUB_TOKEN=rehearsal-token .claudinite/bin/cn execute loop) > "$work/exec.out" 2>&1; }
      item() { gh_count 'Math.max(0,...st.issues.filter(i=>i.title==="'"$1"'").map(i=>i.number))'; }
      labels_of() { gh_count 'st.issues.find(i=>i.number==='"$1"').labels.join(" ")'; }
      roster_item="[claudinite-work] claudinite-fleet-sheepdog/fleet-roster"
      saved=$FLEET_GITHUB_TOKEN
      unset FLEET_GITHUB_TOKEN
      cn_member work create claudinite-fleet-sheepdog/fleet-roster --qualifier nosecret > "$work/create.out" 2>&1 || fail "fleet 7: work create: $(cat "$work/create.out")"
      n=$(item "$roster_item nosecret")
      execute || fail "fleet 7: execute: $(cat "$work/exec.out")"
      case " $(labels_of "$n") " in *" task:status:needs-human-action "*) ;; *) fail "fleet 7: #$n without the secret is $(labels_of "$n"): $(cat "$work/exec.out")" ;; esac
      gh_count 'st.issues.find(i=>i.number==='"$n"').comments.join("\n")' | grep -q FLEET_GITHUB_TOKEN || fail "fleet 7: the park names no secret: $(gh_state)"
      cn_member work create claudinite-fleet-sheepdog/fleet-roster --qualifier secret > "$work/create.out" 2>&1 || fail "fleet 7: work create: $(cat "$work/create.out")"
      n=$(item "$roster_item secret")
      CLAUDINITE_SECRETS="{\"FLEET_GITHUB_TOKEN\":\"$saved\"}" execute || fail "fleet 7: execute: $(cat "$work/exec.out")"
      case " $(labels_of "$n") " in *" task:status:done "*) ;; *) fail "fleet 7: #$n with the secret is $(labels_of "$n"): $(cat "$work/exec.out")" ;; esac
      step "fleet 7: a fleet-roster item parked needs-human-action naming FLEET_GITHUB_TOKEN without it, and ran cn fleet roster to done with it in CLAUDINITE_SECRETS"

      # Steps 8 to 13 read the shelf's catalog from a local pack source:
      # hello and hello-asks, each with a fingerprint, on the canary channel
      # acme/behind declares.
      fleet_catalog fleet
      FLEET_GITHUB_TOKEN=$saved
      export FLEET_GITHUB_TOKEN

      fleet_cn add-packs --scan-for-needed-packs=true --repos=all-covered-members || fail "fleet 8: add-packs: $(cat "$work/fleet.out" "$work/fleet.err")"
      expect_line 'catalog: 2 pack(s), 0 on stable and 2 on canary' 8
      member_issues behind | grep -qF "$suspected" || fail "fleet 8: no suspected issue in acme/behind: $(member_issues behind) $(cat "$work/fleet.out")"
      member_issues behind | grep -qF 'task:origin:ad-hoc' || fail "fleet 8: the suspected issue carries no mark: $(member_issues behind)"
      gh_count 'st.fleet["acme/behind"][0].body' | grep -qF "${bt}hello${bt}" || fail "fleet 8: the suspected list names no hello: $(gh_count 'st.fleet["acme/behind"][0].body')"
      gh_count 'st.fleet["acme/behind"][0].body' | grep -qF "${bt}hello-asks${bt}" || fail "fleet 8: the suspected list names no hello-asks: $(gh_count 'st.fleet["acme/behind"][0].body')"
      grep -F '**Fitted:**' "$work/fleet.out" | grep -qF 'acme/current' || fail "fleet 8: acme/current not fitted: $(cat "$work/fleet.out")"
      [ "$(gh_count 'st.calls.filter(c=>c.startsWith("member acme/dormant ")&&c.includes("/git/trees/")).length')" = 0 ] || fail "fleet 8: the dormant member was swept"
      grep -q '^\[cn\] fleet add-packs ok 1/' "$work/fleet.err" || fail "fleet 8: breadcrumb: $(cat "$work/fleet.err")"
      step "fleet 8: a scheduled add-packs filed the marked suspected list in acme/behind (hello, hello-asks), fitted acme/current, read 2 packs from the catalog and left the dormant member alone"

      writes=$(member_writes)
      fleet_cn add-packs --scan-for-needed-packs=true --repos=all-covered-members || fail "fleet 9: add-packs: $(cat "$work/fleet.out" "$work/fleet.err")"
      [ "$(member_writes)" = "$writes" ] || fail "fleet 9: an unchanged list was written again ($writes before): $(gh_count 'st.calls.filter(c=>c.startsWith("member ")&&!c.includes(" GET ")).join("; ")')"
      [ "$(member_issues behind | grep -cF "$suspected")" = 1 ] || fail "fleet 9: $(member_issues behind)"
      step "fleet 9: the same scan again wrote no issue or file into any member (its label ensure answers already-exists)"

      calls=$(member_calls)
      if CLAUDINITE_CONTEXT='SCAN_FOR_NEEDED_PACKS=false,REPOS=behind,ADD_PACKS=hello-asks' fleet_cn add-packs; then fail "fleet 10: an unanswered force passed: $(cat "$work/fleet.out")"; fi
      grep -qF 'hello-asks.goals' "$work/fleet.err" || fail "fleet 10: the refusal names no question: $(cat "$work/fleet.err")"
      grep -q '^\[cn\] fleet add-packs refused ' "$work/fleet.err" || fail "fleet 10: breadcrumb: $(cat "$work/fleet.err")"
      [ "$(member_calls)" = "$calls" ] || fail "fleet 10: a refused force reached a member"
      step "fleet 10: a force of hello-asks with its question unanswered was refused before any call to a member"

      suspect=$(gh_count 'st.fleet["acme/behind"].find(i=>i.title.startsWith("'"$suspected"'")).number')
      CLAUDINITE_CONTEXT='SCAN_FOR_NEEDED_PACKS=false,REPOS=behind,ADD_PACKS=hello-asks,PACK_ANSWER_1=hello-asks.goals=that adoption reaches a member' fleet_cn add-packs \
        || fail "fleet 11: add-packs: $(cat "$work/fleet.out" "$work/fleet.err")"
      requested=$(gh_count 'st.fleet["acme/behind"].find(i=>i.state==="open"&&i.title.startsWith("Add packs: requested")).body')
      printf '%s\n' "$requested" | grep -qF "Blocked-by: #$suspect" || fail "fleet 11: the requested list is not Blocked-by #$suspect: $requested"
      printf '%s\n' "$requested" | grep -qF '"hello-asks"' || fail "fleet 11: the requested list names no hello-asks: $requested"
      printf '%s\n' "$requested" | grep -qF 'that adoption reaches a member' || fail "fleet 11: the requested list carries no answer: $requested"
      step "fleet 11: with the answer, the force filed the requested list in acme/behind, Blocked-by the suspected one"

      cp "$work/fleet/current/.claudinite/settings.yaml" "$work/current-settings.before"
      fleet_cn pack-seeds || fail "fleet 12: pack-seeds: $(cat "$work/fleet.out" "$work/fleet.err")"
      grep -qF 'greeting: "hi"' "$work/fleet/current/.claudinite/settings.yaml" || fail "fleet 12: acme/current not seeded: $(cat "$work/fleet/current/.claudinite/settings.yaml")"
      if diff "$work/current-settings.before" "$work/fleet/current/.claudinite/settings.yaml" | grep -q '^<'; then
        fail "fleet 12: the seed rewrote acme/current outside its packs block: $(diff "$work/current-settings.before" "$work/fleet/current/.claudinite/settings.yaml")"
      fi
      grep -A2 -F '**Waiting on their next update' "$work/fleet.out" | grep -qF 'acme/behind' || fail "fleet 12: acme/behind not waiting on its mount: $(cat "$work/fleet.out")"
      grep -F 'acme/nodemember' "$work/fleet.out" | grep -qi 'move' || fail "fleet 12: acme/nodemember not waiting on its move: $(cat "$work/fleet.out")"
      [ "$(gh_count 'st.calls.filter(c=>c.startsWith("put ")).length')" = 1 ] || fail "fleet 12: puts $(gh_count 'st.calls.filter(c=>c.startsWith("put ")).join(";")')"
      grep -q '^\[cn\] fleet pack-seeds ok 1/' "$work/fleet.err" || fail "fleet 12: breadcrumb: $(cat "$work/fleet.err")"
      step "fleet 12: pack-seeds wrote hello {greeting: hi} into acme/current's packs block; acme/behind waits on its mount, acme/nodemember on its move"

      mkdir -p "$member/.claudinite/shared/packs/hello"
      cp "$work/fleet/current/.claudinite/shared/packs/hello/pack.json" "$member/.claudinite/shared/packs/hello/pack.json"
      printf '    - id: hello\n      config:\n        greeting: "hello"\n' >> "$member/.claudinite/settings.yaml"
      if cn_member check world > "$work/world.out" 2>&1; then fail "fleet 13: a disagreeing seed passed: $(cat "$work/world.out")"; fi
      grep -q 'fleet-pack-seed-agrees' "$work/world.out" || fail "fleet 13: no fleet-pack-seed-agrees finding: $(cat "$work/world.out")"
      step "fleet 13: cn check world in the manager, its own hello entry disagreeing with the seed, blocks on fleet-pack-seed-agrees"
      ;;
    dashboard)
      step "dashboard: a member declaring claudinite-dashboard in YAML with a local descriptor, then a fleet manager publishing its roster"
      warm_member dashboard
      sh release/dashboard-fixture.sh member "$member" "$version" || fail "dashboard: member fixture"
      (cd "$member" && git init -q -b main && git add -A && git -c user.name=rehearse -c user.email=r@x -c commit.gpgsign=false commit -q -m adopt) \
        || fail "dashboard: git setup"
      flat=$member/.claudinite/flat
      # json FILE EXPR: a value read from a JSON file with node.
      json() { node -e 'const st=JSON.parse(require("fs").readFileSync(process.argv[1],"utf8"));process.stdout.write(String(eval(process.argv[2])))' "$1" "$2"; }

      cn_member rules-index > "$work/index.out" 2>&1 || fail "dashboard 1: rules-index: $(cat "$work/index.out")"
      for f in tasks dashboard member; do
        [ -f "$flat/$f.GENERATED.json" ] || fail "dashboard 1: no $f.GENERATED.json: $(cat "$work/index.out")"
      done
      [ "$(json "$flat/member.GENERATED.json" 'st.settings.path+" "+st.settings.format')" = ".claudinite/settings.yaml yaml" ] \
        || fail "dashboard 1: the member file names $(json "$flat/member.GENERATED.json" 'JSON.stringify(st.settings)')"
      [ "$(json "$flat/member.GENERATED.json" 'st.engine.package+" "+st.engine.version+" "+st.engine.channel')" = "$package $version $(case $package in *-rc) echo canary ;; *) echo stable ;; esac)" ] \
        || fail "dashboard 1: the member file's pin is $(json "$flat/member.GENERATED.json" 'JSON.stringify(st.engine)')"
      [ "$(json "$flat/member.GENERATED.json" 'JSON.stringify(st.held)+" "+st.packs.declared.map(e=>e.id).join(",")+" "+st.dormant')" = '{"claudinite-dashboard":"1.0"} claudinite-dashboard,local/acme false' ] \
        || fail "dashboard 1: the member file: $(cat "$flat/member.GENERATED.json")"
      step "dashboard 1: rules-index wrote the three flat files; member.GENERATED.json names the YAML path, the pin and the held versions"

      cn_member tasks flat --check > "$work/flat.out" 2>&1 || fail "dashboard 2: a fresh tree is stale: $(cat "$work/flat.out")"
      sed 's/        mode: "repo"/        mode: "fleet"/' "$member/.claudinite/settings.yaml" > "$work/settings.yaml" && mv "$work/settings.yaml" "$member/.claudinite/settings.yaml"
      if cn_member tasks flat --check > "$work/flat.out" 2>&1; then fail "dashboard 2: --check passed a moved declaration"; fi
      grep -q 'member.GENERATED.json' "$work/flat.out" || fail "dashboard 2: --check names no member file: $(cat "$work/flat.out")"
      cn_member tasks flat --write > "$work/flat.out" 2>&1 || fail "dashboard 2: --write: $(cat "$work/flat.out")"
      cn_member tasks flat --check > "$work/flat.out" 2>&1 || fail "dashboard 2: stale after --write: $(cat "$work/flat.out")"
      [ "$(json "$flat/member.GENERATED.json" 'st.packs.declared[0].config.mode')" = fleet ] || fail "dashboard 2: the member file kept the old config"
      step "dashboard 2: tasks flat --check exits 1 once the declaration moves and 0 after --write"

      cn_member check world > "$work/world.out" 2>&1 || fail "dashboard 3: check world: $(cat "$work/world.out")"
      local_desc=$member/.claudinite/local/packs/acme/dashboard.json
      cp "$local_desc" "$work/dashboard.json"
      sed 's/"repo": \["widgets", "shipped"\]/"repo": ["widgets", "ghost"]/' "$work/dashboard.json" > "$local_desc"
      if cn_member check world > "$work/world.out" 2>&1; then fail "dashboard 3: an undeclared id passed: $(cat "$work/world.out")"; fi
      grep -F 'descriptor-usable' "$work/world.out" | grep -F '.claudinite/local/packs/acme/dashboard.json' | grep -qF 'selects widget id(s) it does not declare: ghost' \
        || fail "dashboard 3: no descriptor-usable finding: $(cat "$work/world.out")"
      cp "$local_desc" "$member/.claudinite/shared/packs/claudinite-dashboard/dashboard.json"
      cp "$work/dashboard.json" "$local_desc"
      cn_member check world > "$work/world.out" 2>&1 || fail "dashboard 3: a broken mount descriptor fired: $(cat "$work/world.out")"
      step "dashboard 3: check world passes; an undeclared id in the local descriptor blocks on descriptor-usable; the same file in the mount is silent"

      cn_member dashboard descriptor .claudinite/local/packs/acme/dashboard.json > "$work/desc.out" 2>&1 || fail "dashboard 4: a good descriptor: $(cat "$work/desc.out")"
      grep -qx '.claudinite/local/packs/acme/dashboard.json: ok' "$work/desc.out" || fail "dashboard 4: $(cat "$work/desc.out")"
      if cn_member dashboard descriptor --json .claudinite/shared/packs/claudinite-dashboard/dashboard.json > "$work/desc.out" 2>&1; then fail "dashboard 4: a broken descriptor exited 0"; fi
      [ "$(json "$work/desc.out" 'st[0].pack+" "+st[0].repo.join(",")+" "+st[0].problems.map(p=>p.what).join("|")')" = 'claudinite-dashboard widgets selects widget id(s) it does not declare: ghost' ] \
        || fail "dashboard 4: the verdict: $(cat "$work/desc.out")"
      step "dashboard 4: cn dashboard descriptor says ok over a good descriptor and exits 1 with the JSON verdict over the broken one"

      fleet_rig dashboard-manager "$work/dashboard-fleet"
      sh release/dashboard-fixture.sh manager "$member" "$version" || fail "dashboard: manager fixture"
      (cd "$member" && git add -A && git -c user.name=rehearse -c user.email=r@x -c commit.gpgsign=false commit -q -m dashboard \
        && git remote add origin "$origin" && git -c push.negotiate=false push -q origin main) || fail "dashboard: manager git setup"
      roster=$member/.claudinite/fleet/roster.GENERATED.json
      fleet_cn roster || fail "dashboard 5: roster: $(cat "$work/fleet.out" "$work/fleet.err")"
      [ "$(json "$roster" 'st.owner+" "+st.members.length+" "+st.members.find(m=>m.repo==="acme/manager").scope')" = "acme 9 home" ] \
        || fail "dashboard 5: the roster file: $(cat "$roster" 2>&1)"
      [ "$(json "$roster" 'st.members.find(m=>m.repo==="acme/current").freshness.state+" "+st.members.find(m=>m.repo==="acme/nodemember").shape')" = "fresh node" ] \
        || fail "dashboard 5: the roster's verdicts: $(cat "$roster")"
      rm "$roster"
      actions_env
      licctl '{"plan":"personal"}'
      execute() { (cd "$member" && GITHUB_TOKEN=rehearsal-token CLAUDINITE_SECRETS="{\"FLEET_GITHUB_TOKEN\":\"rehearsal-token\"}" .claudinite/bin/cn execute loop) > "$work/exec.out" 2>&1; }
      item() { gh_count 'Math.max(0,...st.issues.filter(i=>i.title==="'"$1"'").map(i=>i.number))'; }
      labels_of() { gh_count 'st.issues.find(i=>i.number==='"$1"').labels.join(" ")'; }
      roster_item="[claudinite-work] claudinite-fleet-sheepdog/fleet-roster"
      cn_member work create claudinite-fleet-sheepdog/fleet-roster --qualifier artifact > "$work/create.out" 2>&1 || fail "dashboard 5: work create: $(cat "$work/create.out")"
      n=$(item "$roster_item artifact")
      ctl /_stub/dispatch '{"conclusion":"success"}'
      execute || fail "dashboard 5: execute: $(cat "$work/exec.out")"
      case " $(labels_of "$n") " in *" task:status:done "*|*" task:status:needs-human-approval "*) ;; *) fail "dashboard 5: #$n is $(labels_of "$n"): $(cat "$work/exec.out")" ;; esac
      [ "$(gh_count 'st.pulls.length')" = 1 ] || fail "dashboard 5: $(gh_count 'st.pulls.length') pull requests: $(cat "$work/exec.out")"
      head_ref=$(gh_count 'st.pulls[0].head')
      case $head_ref in claudinite/claudinite-fleet-sheepdog/fleet-roster/*) ;; *) fail "dashboard 5: the PR is from $head_ref" ;; esac
      [ "$(gh_count 'st.pulls[0].state')" = closed ] || fail "dashboard 5: PR #$(gh_count 'st.pulls[0].number') did not land: $(cat "$work/exec.out")"
      head_sha=$(gh_count 'st.pulls[0].head_sha')
      msg=$(git --git-dir "$origin" log -1 --format=%B "$head_sha")
      case $msg in *"Claudinite-Task: claudinite-fleet-sheepdog/fleet-roster"*"Claudinite-Automerge-Policy: fleet-roster-artifact"*) ;; *) fail "dashboard 5: the PR head's trailers: $msg" ;; esac
      [ "$(git --git-dir "$origin" diff --name-only "$head_sha^" "$head_sha")" = .claudinite/fleet/roster.GENERATED.json ] \
        || fail "dashboard 5: the PR touches $(git --git-dir "$origin" diff --name-only "$head_sha^" "$head_sha")"
      git --git-dir "$origin" show main:.claudinite/fleet/roster.GENERATED.json > /dev/null 2>&1 || fail "dashboard 5: main holds no roster"
      [ -z "$(cd "$member" && git status --porcelain)" ] || fail "dashboard 5: the executor left the checkout changed: $(cd "$member" && git status --porcelain)"
      main_sha=$(git --git-dir "$origin" rev-parse main)
      cn_member work create claudinite-fleet-sheepdog/fleet-roster --qualifier again > "$work/create.out" 2>&1 || fail "dashboard 5: work create: $(cat "$work/create.out")"
      n=$(item "$roster_item again")
      execute || fail "dashboard 5: second execute: $(cat "$work/exec.out")"
      case " $(labels_of "$n") " in *" task:status:done "*|*" task:status:needs-human-approval "*) ;; *) fail "dashboard 5: the second #$n is $(labels_of "$n"): $(cat "$work/exec.out")" ;; esac
      [ "$(gh_count 'st.pulls.length')" = 1 ] || fail "dashboard 5: a second run opened another pull request: $(cat "$work/exec.out")"
      [ "$(git --git-dir "$origin" rev-parse main)" = "$main_sha" ] || fail "dashboard 5: a second run moved main"
      [ -z "$(cd "$member" && git status --porcelain)" ] || fail "dashboard 5: the second run left the checkout changed: $(cd "$member" && git status --porcelain)"
      step "dashboard 5: cn fleet roster wrote one verdict per repository, the manager's as home; a fleet-roster item landed it on one PR under fleet-roster-artifact; the second run wrote nothing"

      fleet_catalog dashboard
      bt='`'
      fleet_cn add-packs --scan-for-needed-packs=true --repos=all-covered-members || fail "dashboard 6: add-packs: $(cat "$work/fleet.out" "$work/fleet.err")"
      expect_line 'catalog: 2 pack(s), 0 on stable and 2 on canary' "dashboard 6"
      gh_count 'st.fleet["acme/behind"].find(i=>i.title.startsWith("'"$suspected"'")).body' | grep -qF "${bt}hello-asks${bt}" \
        || fail "dashboard 6: the canary member was not measured on canary: $(member_issues behind) $(cat "$work/fleet.out")"
      CLAUDINITE_FLEET_MIN_PACKS=3
      if fleet_cn add-packs --scan-for-needed-packs=true --repos=all-covered-members; then fail "dashboard 6: a catalog under the floor swept"; fi
      grep -qF "only 2 pack(s) in the shelf's catalog across both channels" "$work/fleet.err" || fail "dashboard 6: the floor's refusal: $(cat "$work/fleet.err")"
      CLAUDINITE_FLEET_MIN_PACKS=2
      step "dashboard 6: on a stable manager, add-packs measured canary acme/behind against the canary catalog, counted both channels, and the floor counts both"

      # The real pack from the ClaudinitePacks checkout, its tests left behind as an
      # archive leaves them, under the fixture's manifest granted the dispatch the
      # real one grants: publish-pages builds the site on this cn manager and
      # follows the deploy it dispatches to success.
      if [ -z "${CLAUDINITE_PACKS_TREE:-}" ] || [ ! -d "$CLAUDINITE_PACKS_TREE/packs/claudinite-dashboard/src" ]; then
        fail "dashboard 7: CLAUDINITE_PACKS_TREE names no ClaudinitePacks checkout carrying claudinite-dashboard"
      fi
      (cd "$member" && git -c push.negotiate=false pull -q --ff-only origin main) || fail "dashboard 7: catching up with the roster step 5 landed"
      dash=$member/.claudinite/shared/packs/claudinite-dashboard
      cp "$dash/pack.json" "$dash/dashboard.json" "$work/"
      rm -rf "$dash"
      mkdir -p "$dash"
      (cd "$CLAUDINITE_PACKS_TREE/packs/claudinite-dashboard" && tar cf - --exclude=./test .) | (cd "$dash" && tar xf -) || fail "dashboard 7: copying the pack"
      node -e 'const fs=require("fs");const m=JSON.parse(fs.readFileSync(process.argv[1],"utf8"));m.githubActions=["dispatchWorkflow"];fs.writeFileSync(process.argv[2],JSON.stringify(m,null,2)+"\n")' \
        "$work/pack.json" "$dash/pack.json" || fail "dashboard 7: the manifest"
      cp "$work/dashboard.json" "$dash/dashboard.json"
      cn_member tasks flat --write > "$work/flat.out" 2>&1 || fail "dashboard 7: tasks flat --write: $(cat "$work/flat.out")"
      [ "$(json "$member/.claudinite/flat/member.GENERATED.json" 'JSON.stringify(st.packs.declared.find(e=>e.id==="claudinite-dashboard").config)')" = '{"mode":"fleet","owner":"acme"}' ] \
        || fail "dashboard 7: the member file: $(cat "$member/.claudinite/flat/member.GENERATED.json")"
      (cd "$member" && git add -A && git -c user.name=rehearse -c user.email=r@x -c commit.gpgsign=false commit -q -m "the dashboard pack" \
        && git -c push.negotiate=false push -q origin main) || fail "dashboard 7: git"
      GITHUB_API_URL=$gh NODE_EXTRA_CA_CERTS=$work/cas.pem
      export GITHUB_API_URL NODE_EXTRA_CA_CERTS
      ctl /_stub/dispatch '{"conclusion":"success"}'
      cn_member work create claudinite-dashboard/publish-pages --qualifier rehearsal > "$work/create.out" 2>&1 || fail "dashboard 7: work create: $(cat "$work/create.out")"
      n=$(item "[claudinite-work] claudinite-dashboard/publish-pages rehearsal")
      execute || fail "dashboard 7: execute: $(cat "$work/exec.out")"
      case " $(labels_of "$n") " in *" task:status:done "*) ;; *) fail "dashboard 7: #$n is $(labels_of "$n"): $(cat "$work/exec.out")" ;; esac
      [ "$(gh_count 'st.dispatches.filter(d=>d.workflow==="claudinite-dashboard-pages.yml"&&d.ref==="main").length')" = 1 ] \
        || fail "dashboard 7: the deploy dispatches: $(gh_state)"
      [ "$(git --git-dir "$origin" ls-tree --name-only gh-pages | tr '\n' ' ')" = ".nojekyll deployed.json index.html packs " ] \
        || fail "dashboard 7: the site's root: $(git --git-dir "$origin" ls-tree --name-only gh-pages 2>&1)"
      [ "$(git --git-dir "$origin" ls-tree --name-only gh-pages:packs)" = claudinite-dashboard ] \
        || fail "dashboard 7: the site carries $(git --git-dir "$origin" ls-tree --name-only gh-pages:packs | tr '\n' ' ')"
      git --git-dir "$origin" cat-file -e gh-pages:packs/claudinite-dashboard/index.html || fail "dashboard 7: no page"
      git --git-dir "$origin" cat-file -e gh-pages:packs/claudinite-dashboard/src/app.mjs || fail "dashboard 7: no modules"
      git --git-dir "$origin" show gh-pages:packs/claudinite-dashboard/dashboard.config.json > "$work/site-config.json" || fail "dashboard 7: no config"
      [ "$(json "$work/site-config.json" 'st.mode+" "+st.owner+" "+st.deploymentRepo')" = "fleet acme acme/manager" ] \
        || fail "dashboard 7: the site's config: $(cat "$work/site-config.json")"
      if git --git-dir "$origin" ls-tree -r --name-only gh-pages | grep -q -e '^engine/' -e '/test/'; then fail "dashboard 7: the site carries an engine or tests"; fi
      [ -z "$(cd "$member" && git status --porcelain)" ] || fail "dashboard 7: the run left the checkout changed: $(cd "$member" && git status --porcelain)"
      unset GITHUB_API_URL NODE_EXTRA_CA_CERTS
      step "dashboard 7: publish-pages built the real pack on a cn manager, pushed index.html, the page's own modules and nothing else to gh-pages, and followed its deploy to success"
      ;;
    live-packs)
      live_packs="basics git-github claudinite-lifecycle claudinite-tasks claudinite-growth node python aws-sam"
      step "live-packs: cn $live_version, signed with the development keys, against the real pack CDN and ClaudinitePacks' vendored branch"
      # The real shelf: both sources at the engine's defaults.
      unset CLAUDINITE_PACKS_CDN CLAUDINITE_PACKS_REPO
      origin=$work/live-origin.git
      git init -q --bare -b main "$origin"
      start_ghstub "$origin"
      cat "$ca_base" "$work/ca.pem" "$work/gh-ca.pem" > "$work/cas.pem"
      SSL_CERT_FILE=$work/cas.pem CURL_CA_BUNDLE=$work/cas.pem
      GITHUB_REPOSITORY=acme/member CLAUDINITE_GITHUB_API=$gh GH_TOKEN=rehearsal-token
      export SSL_CERT_FILE CURL_CA_BUNDLE GITHUB_REPOSITORY CLAUDINITE_GITHUB_API GH_TOKEN
      actions_env
      livejson() { node -e 'const st=JSON.parse(require("fs").readFileSync(process.argv[1],"utf8"));process.stdout.write(String(eval(process.argv[2])))' "$1" "$2"; }

      # The shelf as the branch holds it now: the catalog read in step 4,
      # and the mirror step 3 rewinds.
      mirror=$work/live-vendored.git
      git clone -q --bare --single-branch -b vendored https://github.com/missingbulb/ClaudinitePacks "$mirror" || fail "live-packs: clone the vendored branch"
      git --git-dir "$mirror" show vendored:catalog.json > "$work/live-catalog.json" || fail "live-packs: the branch holds no catalog.json"

      name=${package#@claudinite/}
      npx=$work/live-npx/node_modules
      mkdir -p "$npx/@claudinite" "$npx/.bin"
      cp -R "$live_dist/npm/$name/package" "$npx/$package"
      chmod 0755 "$npx/$package/launch"
      ln -s "../$package/launch" "$npx/.bin/cn"
      member=$work/live-member
      mkdir -p "$member" "$member-home" "$member-cache"
      HOME=$member-home XDG_CACHE_HOME=$member-cache
      export HOME XDG_CACHE_HOME
      (cd "$member" && "$npx/.bin/cn" init --packs "$(echo "$live_packs" | tr ' ' ,)" --channel canary --package "$package" --repo "$member") \
        > "$work/init.out" 2>&1 || fail "live-packs 1: init: $(cat "$work/init.out")"
      for p in $live_packs; do
        grep -q "$p: index serial [0-9]* from cdn" "$work/init.out" || fail "live-packs 1: $p was not read from the CDN: $(cat "$work/init.out")"
        v=$(livejson "$member/.claudinite/shared/packs/$p/pack.json" st.version) || fail "live-packs 1: init vendored no $p"
        [ "$(livejson "$work/live-catalog.json" 'st.packs.filter(e=>e.id==="'"$p"'"&&e.version==="'"$v"'").length')" -ge 1 ] \
          || fail "live-packs 1: $p $v is not in the live catalog"
      done
      grep -qx '@.claudinite/flat/claudinite-rules.GENERATED.md' "$member/CLAUDE.md" || fail "live-packs 1: CLAUDE.md imports no rules index: $(cat "$member/CLAUDE.md")"
      grep -q '^@../shared/packs/basics/RULES.md$' "$member/.claudinite/flat/claudinite-rules.GENERATED.md" || fail "live-packs 1: the rules index: $(cat "$member/.claudinite/flat/claudinite-rules.GENERATED.md")"
      verify_out=$(cd "$member" && sh .claudinite/launch verify) || fail "live-packs 1: verify: $verify_out"
      [ -z "$verify_out" ] || fail "live-packs 1: verify reported: $verify_out"
      (cd "$member" && git init -q -b main && git add -A && git -c user.name=rehearse -c user.email=r@x -c commit.gpgsign=false commit -q -m adopt) \
        || fail "live-packs 1: git setup"
      github_origin "$origin"
      (cd "$member" && git -c push.negotiate=false push -q origin main) || fail "live-packs 1: push"
      n=$(find "$member/.claudinite/shared/packs" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')
      step "live-packs 1: cn init through npx vendored $n packs from the CDN, each against its signed index, every version in the live catalog"

      out=$(session_start) || fail "live-packs 2: SessionStart exited non-zero"
      case $out in *"[cn] packs $n/$n loaded"*) ;; *) fail "live-packs 2: self-check line: $out" ;; esac
      cn_member check build --wait > "$work/build.out" 2>&1 || fail "live-packs 2: check build: $(cat "$work/build.out")"
      cn_member check world > "$work/world.out" 2>&1 || fail "live-packs 2: check world on the adopted tree: $(cat "$work/world.out")"
      cn_member check list > "$work/list.out" 2>&1 || fail "live-packs 2: check list: $(cat "$work/list.out")"
      grep -q 'handler-path' "$work/list.out" || fail "live-packs 2: check list names no aws-sam Go check: $(cat "$work/list.out")"
      cat > "$member/template.yaml" <<'YAML'
Resources:
  Api:
    Type: AWS::Serverless::Function
    Properties:
      Handler: src/handler.handler
    Metadata:
      BuildMethod: esbuild
      BuildProperties:
        EntryPoints:
          - src/handler.ts
YAML
      (cd "$member" && git add template.yaml) || fail "live-packs 2: git add"
      if cn_member check world > "$work/world.out" 2>&1; then fail "live-packs 2: check world passed a planted handler-path violation"; fi
      grep -q 'handler-path' "$work/world.out" || fail "live-packs 2: check world failed without naming handler-path: $(cat "$work/world.out")"
      (cd "$member" && git rm -q -f template.yaml) || fail "live-packs 2: git rm"
      cn_member check world > "$work/world.out" 2>&1 || fail "live-packs 2: check world after the plant is gone: $(cat "$work/world.out")"
      step "live-packs 2: SessionStart loads the $n packs; check world builds their Go checks, is silent on the tree and fires aws-sam's handler-path on a plant"

      main_run success
      update_live() {
        cn_member update packs > "$work/update.out" 2> "$work/update.err" || fail "live-packs 3: update packs: $(cat "$work/update.out" "$work/update.err")"
        verdict=$(sed -n '$p' "$work/update.out")
        [ "$verdict" = "$1" ] || fail "live-packs 3: verdict '$verdict', want '$1': $(cat "$work/update.out" "$work/update.err")"
      }
      update_live "up to date"
      grep -q 'basics: index serial [0-9]* from cdn (read: cdn serial [0-9]*, branch serial [0-9]*)' "$work/update.out" "$work/update.err" \
        || fail "live-packs 3: the read names both sources: $(cat "$work/update.out" "$work/update.err")"
      CLAUDINITE_PACKS_CDN=https://127.0.0.1:9
      export CLAUDINITE_PACKS_CDN
      update_live "up to date"
      grep -q 'basics: index serial [0-9]* from branch' "$work/update.out" "$work/update.err" \
        || fail "live-packs 3: with the CDN unreachable the branch did not answer: $(cat "$work/update.out" "$work/update.err")"
      unset CLAUDINITE_PACKS_CDN
      # The newest release commit that moved a declared pack's index,
      # undone in the mirror: the branch is then one release behind the CDN
      # for that pack.
      paths=
      for p in $live_packs; do paths="$paths $p/index.json"; done
      # shellcheck disable=SC2086 # one pathspec per declared pack
      moved=$(git --git-dir "$mirror" log -1 --format=%H vendored -- $paths)
      [ -n "$moved" ] || fail "live-packs 3: no release on the branch moved a declared pack"
      git --git-dir "$mirror" update-ref refs/heads/vendored "$moved^" || fail "live-packs 3: rewind the mirror"
      CLAUDINITE_PACKS_REPO=$mirror
      export CLAUDINITE_PACKS_REPO
      cn_member update packs > "$work/update.out" 2> "$work/update.err" || fail "live-packs 3: update packs: $(cat "$work/update.out" "$work/update.err")"
      verdict=$(sed -n '$p' "$work/update.out")
      case $verdict in "skipped: pack index sources disagree (cdn serial "*", branch serial "*")") ;; *) fail "live-packs 3: a branch one release behind: '$verdict': $(cat "$work/update.err")" ;; esac
      unset CLAUDINITE_PACKS_REPO
      [ -z "$(cd "$member" && git status --porcelain)" ] || fail "live-packs 3: update packs left the checkout changed: $(cd "$member" && git status --porcelain)"
      [ "$(gh_count 'st.pulls.length')" = 0 ] || fail "live-packs 3: a pull request was opened: $(gh_state)"
      step "live-packs 3: cn update packs is up to date from both sources and from the branch alone; a branch one release behind is $verdict"

      ids=$(livejson "$work/live-catalog.json" 'st.packs.map(e=>e.id+"@"+e.version).join(" ")')
      checked=0
      for e in $ids; do
        livejson "$work/live-catalog.json" 'JSON.stringify({detector:st.packs.find(p=>p.id+"@"+p.version==="'"$e"'").relevanceDetector})' > "$work/detector.json"
        cn_member fleet decide detector --world "$work/detector.json" > "$work/decide.out" 2>&1 || fail "live-packs 4: decide detector $e: $(cat "$work/decide.out")"
        [ "$(tr -d ' \n' < "$work/decide.out")" = "[]" ] || fail "live-packs 4: $e's relevanceDetector is refused: $(cat "$work/decide.out")"
        checked=$((checked + 1))
      done
      [ "$checked" -gt 0 ] || fail "live-packs 4: the live catalog lists no pack"
      step "live-packs 4: every relevanceDetector in the live catalog ($checked rows) is accepted"
      ;;
  esac
done

case " $modes " in
  *" live-packs "*) step "ok ($package $live_version, $(cat "$live_dist/manifest.integrity"))" ;;
  *) step "ok ($package $version, $pin)" ;;
esac
