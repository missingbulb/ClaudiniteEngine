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
//
// The packs' rules do not travel in additionalContext: Claude Code previews
// a hook's stdout at about 2 KB (the Node engine's measurement in its #807),
// and the shelf's prose is about 160 KB. They reach the session as project
// memory, through the CLAUDE.md import of the generated rules index, which
// SessionStart rewrites when the declaration has moved; additionalContext
// carries the engine's own lines. The live measurement of both channels at
// full size on Claude Code (ClaudiniteEngine#33 T7 A) has not run yet, and
// can return the prose to additionalContext.
package hooks
