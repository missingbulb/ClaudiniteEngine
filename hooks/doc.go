// Package hooks answers every Claude Code hook: cn hook <event>.
//
// SessionStart answers hookSpecificOutput.additionalContext, and Stop
// answers {"decision":"block","reason":…} to keep Claude working, as
// https://code.claude.com/docs/en/hooks describes; Claude Code sets
// stop_hook_active on a Stop that follows such a block.
//
// PreToolUse, PostToolUse and UserPromptSubmit answer one verdict each. A
// block on PreToolUse exits 2 with the block text first on stderr and
// nothing on stdout, the form Claude Code reads as a denial and records in
// the call's error result; a block on the other two cannot block and goes
// out as context. Context is one hookSpecificOutput.additionalContext;
// nothing to say is {}. A payload that is not JSON, a missing tool_name, a
// panic, or no verdict within HookDeadline answers {} and lets the call
// through. Every path ends with the breadcrumb, outcome ok, block, advise,
// nudge, deadline or error. The forced-loading holds and nudges run only
// when the license allows them; the guards always run. A transcript_path
// that is set but unreadable reads as a session that loaded nothing, so a
// hold cannot clear, as in the Node engine. Project skills are
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
//
// A per-call hook derives its context (packs, triggers, declared checks)
// from the tree on every call, with no cache on disk. Measured by
// probe/hook-latency on the 38 canon packs, p50 on the Claude Code web VM
// and on the Linux CI runner:
//
//	                                         web VM   CI runner
//	derivation alone                         20.5 ms  13.2 ms
//	PreToolUse, no declaration names it      31-38 ms 16 ms
//	PreToolUse held, no transcript           18 ms    8 ms
//	PreToolUse held, 5 MB transcript         144 ms   75 ms
//	PostToolUse, UserPromptSubmit            17-18 ms 8 ms
//
// The budgets are 50 ms for an unnamed call and 250 ms for a held call
// over a 5 MB transcript.
package hooks
