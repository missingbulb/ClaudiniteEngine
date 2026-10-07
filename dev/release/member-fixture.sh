#!/bin/sh
# Writes a member repo's Claudinite wiring into DIR: the launcher, verbatim,
# as .claudinite/launch; .claudinite/settings.yaml pinning @claudinite/cli
# at VERSION and INTEGRITY on the engine channel CHANNEL (stable, canary or
# staging, which take the latest, rc and staging dist-tags);
# .claude/settings.json wiring the six hooks;
# .claudinite/.gitignore for bin/; and the three member workflows init writes,
# in .github/workflows/, deleting the superseded update workflow. The smoke legs, the
# rehearsal and the sandbox pin all write a member through this one script.
# usage: dev/release/member-fixture.sh DIR VERSION INTEGRITY CHANNEL
set -eu
[ $# -eq 4 ] || { echo "usage: dev/release/member-fixture.sh DIR VERSION INTEGRITY CHANNEL" >&2; exit 2; }
case $4 in
  stable|canary|staging) ;;
  *) echo "member-fixture: CHANNEL must be stable, canary or staging, not $4" >&2; exit 2 ;;
esac
here=$(cd "$(dirname "$0")/../.." && pwd)
dir=$1
mkdir -p "$dir/.claudinite" "$dir/.claude" "$dir/.github/workflows"
cp "$here/cn/launcher/launch" "$dir/.claudinite/launch"
rm -f "$dir/.github/workflows/claudinite-update.yml"
for w in ci scheduler executor; do
  cp "$here/cn/lifecycle/workflows/templates/claudinite-$w.yml" "$dir/.github/workflows/"
done
printf 'bin/\n' > "$dir/.claudinite/.gitignore"
cat > "$dir/.claudinite/settings.yaml" <<YAML
engine:
  package: "@claudinite/cli"
  channel: "$4"
  version: "$2"
  manifest: "$3"
YAML
cat > "$dir/.claude/settings.json" <<'JSON'
{
  "hooks": {
    "SessionStart": [{"hooks": [{"type": "command", "command": "sh \"$CLAUDE_PROJECT_DIR/.claudinite/launch\" hook session-start"}]}],
    "PreToolUse": [{"matcher": "*", "hooks": [{"type": "command", "command": ".claudinite/bin/cn hook pre-tool-use"}]}],
    "PostToolUse": [{"matcher": "*", "hooks": [{"type": "command", "command": ".claudinite/bin/cn hook post-tool-use"}]}],
    "UserPromptSubmit": [{"hooks": [{"type": "command", "command": ".claudinite/bin/cn hook user-prompt-submit"}]}],
    "Stop": [{"hooks": [{"type": "command", "command": ".claudinite/bin/cn hook stop"}]}],
    "SessionEnd": [{"hooks": [{"type": "command", "command": ".claudinite/bin/cn hook session-end"}]}]
  }
}
JSON
