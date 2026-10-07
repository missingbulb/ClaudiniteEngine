#!/bin/sh
# Writes the rehearsal's dashboard wiring.
#
#   dev/release/dashboard-fixture.sh member DIR VERSION
#     the member DIR (dev/release/member-fixture.sh already wrote it) declares
#     claudinite-dashboard with mode repo in its YAML settings, the pack
#     vendored at 1.0 needing engine VERSION with a descriptor of its own,
#     and the local pack acme carrying a dashboard.json
#
#   dev/release/dashboard-fixture.sh manager DIR VERSION
#     the manager DIR (dev/release/fleet-fixture.sh already wrote it) on the
#     stable channel, declaring claudinite-dashboard with mode fleet over
#     the owner acme, its
#     fleet-roster task landing the roster artifact under the sheepdog's
#     fleet-roster-artifact policy as the pack declares them
set -eu
[ $# -eq 3 ] || { echo "usage: dev/release/dashboard-fixture.sh member|manager DIR VERSION" >&2; exit 2; }
kind=$1 dir=$2 version=$3

mount=$dir/.claudinite/shared/packs/claudinite-dashboard
mkdir -p "$mount"
printf '{\n  "version": "1.0",\n  "minEngineVersion": "%s"\n}\n' "$version" > "$mount/pack.json"
cat > "$mount/dashboard.json" <<'JSON'
{
  "widgets": [
    {"id": "members", "kind": "stat", "label": "Members", "noun": "members"}
  ],
  "repo": ["members"],
  "fleet": {"member": "members"}
}
JSON

case $kind in
  member)
    cat >> "$dir/.claudinite/settings.yaml" <<'YAML'
packs:
  declared:
    - id: claudinite-dashboard
      config:
        mode: "repo"
    - local/acme
YAML
    local=$dir/.claudinite/local/packs/acme
    mkdir -p "$local"
    printf '# acme\n\n- **Shipping an acme widget** — say what it counts. (shipping-acme-widget)\n' > "$local/RULES.md"
    cat > "$local/dashboard.json" <<'JSON'
{
  "widgets": [
    {"id": "widgets", "kind": "stat", "label": "Widgets", "noun": "widgets"},
    {"id": "shipped", "kind": "event", "label": "Last shipped"}
  ],
  "repo": ["widgets", "shipped"],
  "fleet": {"member": "widgets"}
}
JSON
    ;;
  manager)
    settings=$dir/.claudinite/settings.yaml
    # The packs channel only: the engine block has a channel of its own.
    awk '/^[a-z]/ { block = $0 } !(block == "packs:" && $0 == "  channel: \"canary\"")' "$settings" > "$settings.tmp"
    mv "$settings.tmp" "$settings"
    printf '    - id: claudinite-dashboard\n      config:\n        mode: "fleet"\n        owner: "acme"\n' >> "$settings"
    sheepdog=$dir/.claudinite/shared/packs/claudinite-fleet-sheepdog
    cat > "$sheepdog/tasks/fleet-roster/task.json" <<'JSON'
{
  "id": "fleet-roster",
  "description": "The fleet manager's census and freshness sweep, and the roster artifact.",
  "trigger": "schedule",
  "preconditions": ["schedule:at-most-daily"],
  "expected_outcome": "amend_existing_or_create_new_pr",
  "automerge": ["fleet-roster-artifact"],
  "code_work": "cn fleet roster",
  "code_work_timeout": 900,
  "code_work_required_secrets": ["FLEET_GITHUB_TOKEN"]
}
JSON
    cat > "$sheepdog/merge-rules.json" <<'JSON'
[
  {
    "name": "fleet-roster-artifact",
    "pathMatching": "/^\\.claudinite\\/fleet\\/roster\\.GENERATED\\.json$/",
    "changeKinds": ["added", "modified"],
    "editShape": "any"
  }
]
JSON
    ;;
  *) echo "usage: dev/release/dashboard-fixture.sh member|manager DIR VERSION" >&2; exit 2 ;;
esac
