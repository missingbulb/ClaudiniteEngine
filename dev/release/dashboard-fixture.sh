#!/bin/sh
# Writes the rehearsal's dashboard wiring.
#
#   dev/release/dashboard-fixture.sh member DIR VERSION
#     the member DIR (dev/release/member-fixture.sh already wrote it) declares
#     claudinite-dashboard with mode repo in its YAML settings, the pack
#     vendored at 1.0 needing engine VERSION with a descriptor of its own,
#     and the local pack acme carrying a dashboard.json
set -eu
[ $# -eq 3 ] || { echo "usage: dev/release/dashboard-fixture.sh member DIR VERSION" >&2; exit 2; }
kind=$1 dir=$2 version=$3

mount=$dir/.claudinite/shared/packs/claudinite-dashboard
mkdir -p "$mount"
printf '{\n  "version": "1.0",\n  "minEngineVersion": "%s"\n}\n' "$version" > "$mount/pack.json"
cat > "$mount/dashboard.json" <<'JSON'
{
  "widgets": [
    {"id": "members", "kind": "stat", "label": "Members", "noun": "members"}
  ],
  "repo": ["members"]
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
  "repo": ["widgets", "shipped"]
}
JSON
    ;;
  *) echo "usage: dev/release/dashboard-fixture.sh member DIR VERSION" >&2; exit 2 ;;
esac
