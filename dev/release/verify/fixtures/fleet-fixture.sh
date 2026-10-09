#!/bin/sh
# Writes the rehearsal's fleet under WORK: the manager MANAGER (a member
# dev/release/member-fixture.sh already wrote) carries a fleet block over
# owner acme with acme/ignored excluded and the hello pack seeded with
# {greeting: hi}, which turns on the engine's own fleet pack and its
# fleet-roster and fleet-update tasks, on the canary channel, with
# engine/update disabled in its tasks block; and
# one tree per member under WORK/fleet/, each served by dev/release/verify/stubs/ghstub as
# acme/<name>:
#
#   current      pinned to LATEST, with its scheduler, its mount carrying hello
#   behind       pinned to BEHIND, with its scheduler, on the canary
#                channel, a hello.json and a greeting.txt asking for hello-asks
#   noscheduler  pinned to LATEST, no scheduler workflow
#   dormant      pinned to LATEST, its tasks block dormant
#   uncovered    no declaration
#   ignored      pinned to LATEST, on the exclude list
#   nodemember   a Node engine declaration, with its scheduler
#   forked       pinned to LATEST, served as a fork
#
# Every pin names @claudinite/cli, manifest INTEGRITY and the engine
# channel CHANNEL; VERSION is the hello pack's minimum engine. It prints
# the ghstub --repo flags, one per line.
# usage: dev/release/verify/fixtures/fleet-fixture.sh WORK MANAGER VERSION LATEST BEHIND INTEGRITY CHANNEL
set -eu
[ $# -eq 7 ] || { echo "usage: dev/release/verify/fixtures/fleet-fixture.sh WORK MANAGER VERSION LATEST BEHIND INTEGRITY CHANNEL" >&2; exit 2; }
work=$1 manager=$2 version=$3 latest=$4 behind=$5 integrity=$6 channel=$7

cat >> "$manager/.claudinite/settings.yaml" <<'YAML'
packs:
  channel: "canary"
fleet:
  owner: "acme"
  exclude:
    - "acme/ignored"
  packSeeds:
    - id: "hello"
      config:
        greeting: "hi"
tasks:
  disabled:
    - engine/update
YAML

# member NAME PIN [FLAG]: a cn member pinned to PIN.
member() {
  d=$work/fleet/$1
  mkdir -p "$d/.claudinite"
  cat > "$d/.claudinite/settings.yaml" <<YAML
engine:
  package: "@claudinite/cli"
  channel: "$channel"
  version: "$2"
  manifest: "$integrity"
YAML
}
scheduler() {
  mkdir -p "$work/fleet/$1/.github/workflows"
  printf 'name: claudinite-scheduler\n' > "$work/fleet/$1/.github/workflows/claudinite-scheduler.yml"
}

member current "$latest"; scheduler current
mkdir -p "$work/fleet/current/.claudinite/shared/packs/hello"
printf '{\n  "version": "1.0",\n  "minEngineVersion": "%s"\n}\n' "$version" > "$work/fleet/current/.claudinite/shared/packs/hello/pack.json"
member behind "$behind"; scheduler behind
printf 'packs:\n  channel: "canary"\n' >> "$work/fleet/behind/.claudinite/settings.yaml"
printf '{}\n' > "$work/fleet/behind/hello.json"
printf 'please add hello-asks\n' > "$work/fleet/behind/greeting.txt"
member noscheduler "$latest"
member dormant "$latest"; scheduler dormant
cat >> "$work/fleet/dormant/.claudinite/settings.yaml" <<'YAML'
tasks:
  dormant: true
YAML
mkdir -p "$work/fleet/uncovered"
printf '# uncovered\n' > "$work/fleet/uncovered/README.md"
member ignored "$latest"; scheduler ignored
mkdir -p "$work/fleet/nodemember"
printf '{\n  "engineVersion": "61001.1",\n  "packs": [{"id": "basics", "version": "3.0"}]\n}\n' > "$work/fleet/nodemember/.claudinite-settings.json"
scheduler nodemember
member forked "$latest"; scheduler forked

for m in current behind noscheduler dormant uncovered ignored nodemember; do
  printf -- '--repo\nacme/%s=%s\n' "$m" "$work/fleet/$m"
done
printf -- '--repo\nacme/forked=%s;fork\n' "$work/fleet/forked"
