#!/bin/sh
# Writes the rehearsal's dashboard wiring.
#
#   dev/release/verify/fixtures/dashboard-fixture.sh member DIR VERSION
#     the member DIR (dev/release/verify/fixtures/member-fixture.sh already wrote it) declares
#     claudinite-single-repo-dashboard with a defaultRepo in its YAML
#     settings, the pack vendored at 1.0 needing engine VERSION, the local
#     pack acme, and the flat dashboard file an earlier engine wrote
set -eu
[ $# -eq 3 ] || { echo "usage: dev/release/verify/fixtures/dashboard-fixture.sh member DIR VERSION" >&2; exit 2; }
kind=$1 dir=$2 version=$3

mount=$dir/.claudinite/shared/packs/claudinite-single-repo-dashboard
mkdir -p "$mount"
printf '{\n  "version": "1.0",\n  "minEngineVersion": "%s"\n}\n' "$version" > "$mount/pack.json"

case $kind in
  member)
    cat >> "$dir/.claudinite/settings.yaml" <<'YAML'
packs:
  declared:
    - id: claudinite-single-repo-dashboard
      config:
        defaultRepo: "acme/member"
    - local/acme
YAML
    local=$dir/.claudinite/local/packs/acme
    mkdir -p "$local"
    printf '# acme\n\n- **Shipping an acme widget** — say what it counts. (shipping-acme-widget)\n' > "$local/RULES.md"
    mkdir -p "$dir/.claudinite/cache"
    printf '{\n  "version": 1,\n  "dashboards": {}\n}\n' > "$dir/.claudinite/cache/dashboard.GENERATED.json"
    ;;
  *) echo "usage: dev/release/verify/fixtures/dashboard-fixture.sh member DIR VERSION" >&2; exit 2 ;;
esac
