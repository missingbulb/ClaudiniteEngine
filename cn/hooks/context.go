package hooks

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/jsjson"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/skilltriggers"
)

// derived is what the forced-loading moments read of the tree: the active
// packs' skills' triggers. A nil derived has none.
type derived struct {
	triggers []skilltriggers.Trigger
}

// derive reads the active packs' triggers; a repo with no settings, or
// whose packs do not load, has none (the self-check names the latter).
func derive(repo, engine string) *derived {
	if _, _, err := settings.Find(repo); err != nil {
		return nil
	}
	set, err := packset.Load(repo, engine, true)
	if err != nil {
		return nil
	}
	ts, _ := skilltriggers.FromPacks(set.Packs)
	return &derived{triggers: ts}
}

// malformedNotes are SessionStart's lines for trigger entries that bind
// nothing.
func malformedNotes(packs []packset.Pack) []string {
	_, bad := skilltriggers.FromPacks(packs)
	var out []string
	for _, b := range bad {
		out = append(out, fmt.Sprintf("[cn] skill %s (pack %s): the %s entry %q is not a trigger and binds nothing", b.Skill, b.Pack, b.Key, b.Entry))
	}
	return out
}

func (d *derived) of(kind string) []skilltriggers.Trigger {
	if d == nil {
		return nil
	}
	var out []skilltriggers.Trigger
	for _, t := range d.triggers {
		if t.Kind == kind {
			out = append(out, t)
		}
	}
	return out
}

var fileTools = map[string]bool{"Edit": true, "Write": true, "NotebookEdit": true}

// targetPath is the repo-relative POSIX path a file tool is about to
// write, "" when it names none or the file is outside the repo.
func targetPath(repo string, call Call, input jsjson.Value) string {
	key := "file_path"
	if call.Tool == "NotebookEdit" {
		key = "notebook_path"
	}
	v, ok := input.Prop(key)
	if !ok || v.Kind != jsjson.String || v.Str == "" {
		return ""
	}
	abs := v.Str
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(repo, abs)
	}
	rel, err := filepath.Rel(repo, abs)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return ""
	}
	return filepath.ToSlash(rel)
}

func decoded(raw []byte) (jsjson.Value, bool) {
	if raw == nil {
		return jsjson.Value{}, false
	}
	v, err := jsjson.Decode(raw)
	if err != nil {
		return jsjson.Value{}, false
	}
	return v, true
}

func (call Call) triggerCall() skilltriggers.Call {
	c := skilltriggers.Call{Tool: call.Tool}
	c.Input, c.HasInput = decoded(call.Input)
	c.Response, c.HasResponse = decoded(call.Response)
	return c
}

// hold is PreToolUse's forced-loading block: a file tool aimed under a
// scoped path, then a call a tool-call trigger names, whose skill the
// session has not loaded; "" for none.
func (d *derived) hold(repo string, call Call) string {
	tc := call.triggerCall()
	if paths := d.of(skilltriggers.Path); fileTools[call.Tool] && len(paths) > 0 {
		if path := targetPath(repo, call, tc.Input); path != "" {
			missing := skilltriggers.Missing(paths, call.Session.Loaded, func(t skilltriggers.Trigger) bool { return t.HitsPath(path) })
			if len(missing) > 0 {
				return fmt.Sprintf("Blocked: %s is edited only with the %s skill loaded (the %s pack's skill forces itself for %s). Load it first — %s — then retry the edit.",
					path, skilltriggers.Skills(missing), skilltriggers.Packs(missing), skilltriggers.Sources(missing), skilltriggers.LoadInstruction(missing, repo))
			}
		}
	}
	calls := d.of(skilltriggers.ToolCall)
	if len(calls) == 0 {
		return ""
	}
	missing := skilltriggers.Missing(calls, call.Session.Loaded, func(t skilltriggers.Trigger) bool { return t.HitsCall(tc) })
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf("Blocked: %s is called only with the %s skill loaded (the %s pack's skill forces itself for %s). Load it first — %s — then retry the call.",
		call.Tool, skilltriggers.Skills(missing), skilltriggers.Packs(missing), skilltriggers.Sources(missing), skilltriggers.LoadInstruction(missing, repo))
}

// harnessPrompt reports whether a prompt is the harness's pseudo-turn (a
// tag or the system-notification banner), or blank, rather than the
// owner's words.
func harnessPrompt(p string) bool {
	t := strings.TrimLeftFunc(p, jsSpace)
	return t == "" || strings.HasPrefix(t, "<") || strings.HasPrefix(t, "[SYSTEM NOTIFICATION")
}

// jsSpace is JavaScript's \s.
func jsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func (d *derived) promptNudges(repo string, call Call) []string {
	if harnessPrompt(call.Prompt) {
		return nil
	}
	prompts := d.of(skilltriggers.Prompt)
	if len(prompts) == 0 {
		return nil
	}
	var out []string
	for _, t := range skilltriggers.Missing(prompts, call.Session.Loaded, func(t skilltriggers.Trigger) bool { return t.HitsPrompt(call.Prompt) }) {
		out = append(out, fmt.Sprintf("Claudinite: this prompt matches the `%s` skill's trigger (%s, the %s pack) — load it before acting on the prompt: %s.",
			t.Skill, t.Source, t.Pack, skilltriggers.LoadInstruction([]skilltriggers.Trigger{t}, repo)))
	}
	return out
}

func (d *derived) resultNudges(repo string, call Call) []string {
	results := d.of(skilltriggers.ToolResult)
	if len(results) == 0 {
		return nil
	}
	tc := call.triggerCall()
	var out []string
	for _, t := range skilltriggers.Missing(results, call.Session.Loaded, func(t skilltriggers.Trigger) bool { return t.HitsResult(tc) }) {
		out = append(out, fmt.Sprintf("Claudinite: this %s result matches the `%s` skill's trigger (%s, the %s pack) — load it before acting on the result: %s.",
			call.Tool, t.Skill, t.Source, t.Pack, skilltriggers.LoadInstruction([]skilltriggers.Trigger{t}, repo)))
	}
	return out
}
