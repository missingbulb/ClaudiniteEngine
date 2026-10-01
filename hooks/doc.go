// Package hooks answers every Claude Code hook: cn hook <event>.
//
// SessionStart answers hookSpecificOutput.additionalContext, as
// PreToolUse, PostToolUse and UserPromptSubmit do to pass a license notice
// on, and Stop
// answers {"decision":"block","reason":…} to keep Claude working, as
// https://code.claude.com/docs/en/hooks describes; Claude Code sets
// stop_hook_active on a Stop that follows such a block. Project skills are
// read from .claude/skills/<name>/SKILL.md, as
// https://code.claude.com/docs/en/skills describes.
package hooks
