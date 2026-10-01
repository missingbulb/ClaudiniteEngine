#!/bin/sh
# Writes a member repo's Claudinite wiring into DIR: the launcher, verbatim,
# as .claudinite/launch; .claudinite/settings.yaml pinning PACKAGE at
# VERSION and INTEGRITY; .claude/settings.json wiring the six hooks; and a
# .gitignore for .claudinite/bin/. The smoke legs, the rehearsal and the
# sandbox pin all write a member through this one script.
# usage: release/member-fixture.sh DIR VERSION INTEGRITY PACKAGE
set -eu
[ $# -eq 4 ] || { echo "usage: release/member-fixture.sh DIR VERSION INTEGRITY PACKAGE" >&2; exit 2; }
here=$(cd "$(dirname "$0")/.." && pwd)
dir=$1
mkdir -p "$dir/.claudinite" "$dir/.claude"
cp "$here/launcher/launch" "$dir/.claudinite/launch"
printf '.claudinite/bin/\n' > "$dir/.gitignore"
cat > "$dir/.claudinite/settings.yaml" <<YAML
engine:
  package: "$4"
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
