#!/bin/sh
# Writes the rehearsal's fleet under WORK: the manager MANAGER (a member
# release/member-fixture.sh already wrote) declares claudinite-fleet-sheepdog
# over owner acme with acme/ignored excluded and the hello pack seeded
# with {greeting: hi}, on the canary channel, its packs needing engine
# VERSION, its fleet-roster and fleet-update tasks as the pack declares
# them, and claudinite-tasks; and
# one tree per member under WORK/fleet/, each served by release/ghstub as
# acme/<name>:
#
#   current      pinned to LATEST, with its scheduler, its mount carrying hello
#   behind       pinned to BEHIND, with its scheduler, on the canary
#                channel, a hello.json and a greeting.txt asking for hello-asks
#   noscheduler  pinned to LATEST, no scheduler workflow
#   dormant      pinned to LATEST, claudinite-tasks dormant
#   uncovered    no declaration
#   ignored      pinned to LATEST, on the exclude list
#   nodemember   a Node engine declaration, with its scheduler
#   forked       pinned to LATEST, served as a fork
#
# Every pin names manifest INTEGRITY. It prints the ghstub --repo flags, one per line.
# usage: release/fleet-fixture.sh WORK MANAGER VERSION LATEST BEHIND INTEGRITY PACKAGE
set -eu
[ $# -eq 7 ] || { echo "usage: release/fleet-fixture.sh WORK MANAGER VERSION LATEST BEHIND INTEGRITY PACKAGE" >&2; exit 2; }
work=$1 manager=$2 version=$3 latest=$4 behind=$5 integrity=$6 package=$7

for p in claudinite-fleet-sheepdog claudinite-tasks; do
  mkdir -p "$manager/.claudinite/shared/packs/$p"
  printf '{\n  "version": "1.0",\n  "minEngineVersion": "%s"\n}\n' "$version" > "$manager/.claudinite/shared/packs/$p/pack.json"
done
tasks=$manager/.claudinite/shared/packs/claudinite-fleet-sheepdog/tasks
mkdir -p "$tasks/fleet-roster" "$tasks/fleet-update"
cat > "$tasks/fleet-roster/task.json" <<'JSON'
{
  "id": "fleet-roster",
  "description": "The fleet manager's census and freshness sweep.",
  "trigger": "schedule",
  "preconditions": ["schedule:at-most-daily"],
  "expected_outcome": "no_code_changes",
  "code_work": "cn fleet roster",
  "code_work_timeout": 900,
  "code_work_required_secrets": ["FLEET_GITHUB_TOKEN"]
}
JSON
cat > "$tasks/fleet-update/task.json" <<'JSON'
{
  "id": "fleet-update",
  "description": "Dispatch every covered member's update and follow it.",
  "trigger": "request",
  "preconditions": [],
  "expected_outcome": "no_code_changes",
  "code_work": "cn fleet update",
  "code_work_timeout": 1800,
  "code_work_required_secrets": ["FLEET_GITHUB_TOKEN"]
}
JSON
cat >> "$manager/.claudinite/settings.yaml" <<'YAML'
packs:
  channel: "canary"
  declared:
    - id: claudinite-fleet-sheepdog
      config:
        owner: "acme"
        exclude:
          - "acme/ignored"
        packSeeds:
          - id: "hello"
            config:
              greeting: "hi"
    - id: claudinite-tasks
      config:
        disabledTasks:
          - engine/update
YAML

# member NAME PIN [FLAG]: a cn member pinned to PIN.
member() {
  d=$work/fleet/$1
  mkdir -p "$d/.claudinite"
  cat > "$d/.claudinite/settings.yaml" <<YAML
engine:
  package: "$package"
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
packs:
  declared:
    - id: claudinite-tasks
      config:
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
