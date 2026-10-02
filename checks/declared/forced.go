package declared

import (
	"fmt"

	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/shared/skilltriggers"
	"github.com/missingbulb/ClaudiniteEngine/shared/transcript"
)

// BuiltinSkillLoaded is the Stop-time half of forced skill loading: a file
// a skill forces itself for changed, or a call one forces itself for ran,
// in a session that never loaded that skill. The PreToolUse hold stops a
// file tool or a call before it runs; this catches what the hold never
// sees (an edit through Bash, a session whose hook did not fire).
const BuiltinSkillLoaded = "skill-loaded-before-editing"

var builtinSkillLoaded = Builtin{ID: BuiltinSkillLoaded, Pack: EnginePack, OnFail: "block", Tags: []string{"work", "builtin"}, Why: skillLoadedWhy, Run: skillLoadedFindings}

const skillLoadedWhy = "the pack scoped those files to a skill because editing them without it produces work the skill would have prevented; a load after the fact is the review the skill was meant to spare"

func skillLoadedFindings(ctx *Ctx, session *transcript.Session) []findings.Finding {
	if !session.Present() || len(transcript.Entries(session.Path)) == 0 {
		return nil
	}
	mk := func(path, what, fix string) findings.Finding {
		return findings.Finding{Class: findings.Coded, ID: BuiltinSkillLoaded, Pack: EnginePack, Path: path, Sentence: what, Why: skillLoadedWhy, Fix: fix}
	}
	var paths, calls []skilltriggers.Trigger
	for _, t := range ctx.Triggers {
		switch t.Kind {
		case skilltriggers.Path:
			paths = append(paths, t)
		case skilltriggers.ToolCall:
			calls = append(calls, t)
		}
	}
	var out []findings.Finding
	loaded := session.Loaded
	if len(paths) > 0 {
		for _, f := range ctx.ChangedFiles() {
			for _, t := range skilltriggers.Missing(paths, loaded, func(t skilltriggers.Trigger) bool { return t.HitsPath(f) }) {
				out = append(out, mk(f,
					fmt.Sprintf("changed under %s, which the %s pack's `%s` skill forces itself for, and this session never loaded that skill", t.Source, t.Pack, t.Skill),
					fmt.Sprintf(`load it now — Skill tool, skill: "%s", or Read its SKILL.md — and re-read the change against what it says before stopping`, t.Skill)))
			}
		}
	}
	if len(calls) > 0 {
		reported := map[string]bool{}
		for _, c := range session.Calls() {
			call := skilltriggers.Call{Tool: c.Name, Input: c.Input, HasInput: true}
			for _, t := range skilltriggers.Missing(calls, loaded, func(t skilltriggers.Trigger) bool { return t.HitsCall(call) }) {
				if reported[t.Skill] {
					continue
				}
				reported[t.Skill] = true
				out = append(out, mk("(session) "+c.Name+" call",
					fmt.Sprintf("a %s call the %s pack's `%s` skill forces itself for (%s) ran, and this session never loaded that skill", c.Name, t.Pack, t.Skill, t.Source),
					fmt.Sprintf(`load it now — Skill tool, skill: "%s", or Read its SKILL.md — and re-read what the call did against what it says before stopping`, t.Skill)))
			}
		}
	}
	return out
}
