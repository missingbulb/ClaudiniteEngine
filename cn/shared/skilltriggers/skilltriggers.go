// Package skilltriggers reads the four force-load declarations a skill
// makes under its frontmatter metadata (skillfm.Meta) and answers the one
// question each moment asks: which skills does this edit, call, prompt or
// result need that the session has not loaded. The grammar is the Node
// engine's path-scoped-skills.mjs and skill-frontmatter.mjs: a path pattern
// is the harness's glob (`**` spans directories, `**/` is zero or more
// whole segments, `*` and `?` stay inside a segment, `{a,b}` expands, the
// rest is literal, anchored over the whole repo-relative path); a tool
// entry is `Name`, `Name.field` or `/regex/` over names, optionally then a
// space and a `/regex/` over that field, or over the input (or result)
// serialized as JSON; a prompt entry is a `/regex/`. Patterns match with
// ECMAScript semantics.
package skilltriggers

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/jsjson"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/jsregex"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/skillfm"
)

// Kinds of trigger.
const (
	Path       = "path"
	ToolCall   = "toolCall"
	Prompt     = "prompt"
	ToolResult = "toolResult"
)

// Regex is one compiled ECMAScript pattern.
type Regex = jsregex.Regex

func compile(body, flags string) (*Regex, error) { return jsregex.Compile(body, flags) }

// toRegex reads a "/pattern/flags" entry, or nil.
func toRegex(s string) *Regex {
	body, flags, ok := jsregex.Form(trim(s))
	if !ok {
		return nil
	}
	r, err := compile(body, flags)
	if err != nil {
		return nil
	}
	return r
}

func trim(s string) string { return jsregex.Trim(s) }

var specials = regexp.MustCompile(`[.+^${}()|\[\]\\]`)

// ExpandBraces multiplies out {a,b} groups, innermost first.
func ExpandBraces(glob string) []string {
	open := -1
	for i := 0; i < len(glob); i++ {
		switch glob[i] {
		case '{':
			open = i
		case '}':
			if open < 0 {
				continue
			}
			var out []string
			for _, alt := range strings.Split(glob[open+1:i], ",") {
				out = append(out, ExpandBraces(glob[:open]+alt+glob[i+1:])...)
			}
			return out
		}
	}
	return []string{glob}
}

func oneGlob(glob string) string {
	var b strings.Builder
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch {
		case c == '*' && i+1 < len(glob) && glob[i+1] == '*':
			if i+2 < len(glob) && glob[i+2] == '/' {
				b.WriteString(`(?:.*/)?`)
				i += 2
			} else {
				b.WriteString(`.*`)
				i++
			}
		case c == '*':
			b.WriteString(`[^/]*`)
		case c == '?':
			b.WriteString(`[^/]`)
		default:
			b.WriteString(specials.ReplaceAllString(string(c), `\$0`))
		}
	}
	return b.String()
}

// Glob compiles a harness path pattern.
func Glob(glob string) (*Regex, error) {
	var alts []string
	for _, g := range ExpandBraces(glob) {
		alts = append(alts, oneGlob(g))
	}
	return compile("^(?:"+strings.Join(alts, "|")+")$", "")
}

// Trigger is one force-load declaration of one skill.
type Trigger struct {
	Pack, Skill string
	// Dir is the skill's directory.
	Dir  string
	Kind string
	// Tool is a tool's exact name, or ToolRe a pattern over names.
	Tool   string
	ToolRe *Regex
	// Field is the dot path the pattern reads, "" for the whole input.
	Field string
	// Pattern is the trigger's regex: over the field, input, result or
	// prompt; nil for a tool entry that names a tool alone. A path
	// trigger's Pattern is its compiled glob.
	Pattern *Regex
	// Source is the entry as written (the glob, for a path trigger).
	Source string
}

var toolEntry = regexp.MustCompile(`(?s)^(/(?:[^/\\]|\\.)*/[a-z]*|[^\s/]\S*)(?:\s+(/.*/[a-z]*))?$`)

// ParseTool reads a tool-call or tool-result entry; ok is false for a
// malformed one, which binds nothing.
func ParseTool(entry string) (Trigger, bool) {
	src := trim(entry)
	m := toolEntry.FindStringSubmatch(src)
	if m == nil {
		return Trigger{}, false
	}
	t := Trigger{Source: src}
	if strings.HasPrefix(m[1], "/") {
		if t.ToolRe = toRegex(m[1]); t.ToolRe == nil {
			return Trigger{}, false
		}
	} else if name, field, ok := strings.Cut(m[1], "."); ok {
		t.Tool, t.Field = name, field
	} else {
		t.Tool = m[1]
	}
	if m[2] != "" {
		if t.Pattern = toRegex(m[2]); t.Pattern == nil {
			return Trigger{}, false
		}
	}
	return t, true
}

// Malformed is a trigger entry that did not parse, named for a note.
type Malformed struct {
	Pack, Skill, Key, Entry string
}

// Of are the triggers of one skill, and the entries that bind nothing.
func Of(pack, skill, dir string, m skillfm.Meta) ([]Trigger, []Malformed) {
	var out []Trigger
	var bad []Malformed
	base := Trigger{Pack: pack, Skill: skill, Dir: dir}
	for _, g := range m.ForceLoadPaths {
		re, err := Glob(g)
		if err != nil {
			bad = append(bad, Malformed{pack, skill, skillfm.ForceLoadPaths, g})
			continue
		}
		t := base
		t.Kind, t.Pattern, t.Source = Path, re, g
		out = append(out, t)
	}
	for _, kind := range []struct {
		kind, key string
		list      []string
	}{{ToolCall, skillfm.ForceLoadToolCalls, m.ToolCalls}, {Prompt, skillfm.ForceLoadPrompts, m.Prompts}, {ToolResult, skillfm.ForceLoadToolResults, m.ToolResults}} {
		for _, e := range kind.list {
			var t Trigger
			ok := false
			if kind.kind == Prompt {
				if re := toRegex(e); re != nil {
					t, ok = Trigger{Pattern: re, Source: trim(e)}, true
				}
			} else {
				t, ok = ParseTool(e)
			}
			if !ok {
				bad = append(bad, Malformed{pack, skill, kind.key, e})
				continue
			}
			t.Pack, t.Skill, t.Dir, t.Kind = pack, skill, dir, kind.kind
			out = append(out, t)
		}
	}
	return out, bad
}

func (t Trigger) namesTool(name string) bool {
	if t.ToolRe != nil {
		return t.ToolRe.Test(name)
	}
	return t.Tool == name
}

// subject is the text a tool trigger's pattern reads of value: the field
// it names (absent or null is ""), or the whole value; a string is its
// text, anything else JSON.
func (t Trigger) subject(value jsjson.Value, present bool) string {
	if !present {
		value = jsjson.Value{Kind: jsjson.Object, Obj: map[string]jsjson.Value{}}
	}
	if t.Field == "" {
		if value.Kind == jsjson.Null {
			return "{}"
		}
		return jsjson.Text(value)
	}
	at, ok := jsjson.FieldAt(value, t.Field)
	if !ok || at.Kind == jsjson.Null {
		return ""
	}
	return jsjson.Text(at)
}

// HitsPath reports whether a path trigger admits a repo-relative path.
func (t Trigger) HitsPath(path string) bool { return t.Kind == Path && t.Pattern.Test(path) }

// Call is a tool call, its input and, after it ran, its result.
type Call struct {
	Tool        string
	Input       jsjson.Value
	HasInput    bool
	Response    jsjson.Value
	HasResponse bool
}

// HitsCall reports whether a tool-call trigger admits a call.
func (t Trigger) HitsCall(c Call) bool {
	return t.Kind == ToolCall && t.namesTool(c.Tool) && (t.Pattern == nil || t.Pattern.Test(t.subject(c.Input, c.HasInput)))
}

// HitsPrompt reports whether a prompt trigger admits a prompt.
func (t Trigger) HitsPrompt(text string) bool { return t.Kind == Prompt && t.Pattern.Test(text) }

// HitsResult reports whether a result trigger admits a call's result. A
// result trigger with no pattern matches nothing, as in the Node engine.
func (t Trigger) HitsResult(c Call) bool {
	return t.Kind == ToolResult && t.namesTool(c.Tool) && t.Pattern != nil && t.Pattern.Test(t.subject(c.Response, c.HasResponse))
}

// Missing are the triggers hit admits whose skill is not loaded, deduped
// by skill. loaded is called only once a trigger hits, so a transcript
// behind it is read only when the answer depends on it.
func Missing(triggers []Trigger, loaded func() map[string]bool, hit func(Trigger) bool) []Trigger {
	var have map[string]bool
	seen := map[string]bool{}
	var out []Trigger
	for _, t := range triggers {
		if seen[t.Skill] || !hit(t) {
			continue
		}
		if have == nil {
			have = loaded()
			if have == nil {
				have = map[string]bool{}
			}
		}
		if have[t.Skill] {
			continue
		}
		seen[t.Skill] = true
		out = append(out, t)
	}
	return out
}

// LoadInstruction is how to load the missing skills: the Skill tool, or a
// Read of each SKILL.md, repo-relative where the skill lives in the repo.
func LoadInstruction(missing []Trigger, repo string) string {
	var skills, reads []string
	for _, t := range missing {
		skills = append(skills, fmt.Sprintf("skill: %q", t.Skill))
		p := filepath.Join(t.Dir, "SKILL.md")
		p = strings.Replace(p, repo+string(filepath.Separator), "", 1)
		reads = append(reads, filepath.ToSlash(p))
	}
	return "Skill tool, " + strings.Join(skills, ", ") + ", or Read " + strings.Join(reads, " or ")
}

// Skills are the backquoted names of the missing skills, joined by "and".
func Skills(missing []Trigger) string {
	var n []string
	for _, t := range missing {
		n = append(n, "`"+t.Skill+"`")
	}
	return strings.Join(n, " and ")
}

// Packs are the missing skills' packs, each once, comma-joined.
func Packs(missing []Trigger) string {
	seen := map[string]bool{}
	var out []string
	for _, t := range missing {
		if !seen[t.Pack] {
			seen[t.Pack] = true
			out = append(out, t.Pack)
		}
	}
	return strings.Join(out, ", ")
}

// Sources are the missing triggers' sources, comma-joined.
func Sources(missing []Trigger) string {
	var out []string
	for _, t := range missing {
		out = append(out, t.Source)
	}
	return strings.Join(out, ", ")
}

// FromPacks are the triggers of every skill the packs offer, in pack
// order, read from each skill's SKILL.md, and the entries that bind
// nothing. A skill whose SKILL.md cannot be read has none. Under
// packset.Memoize the first reading stands for the process.
func FromPacks(packs []packset.Pack) ([]Trigger, []Malformed) {
	var key strings.Builder
	key.WriteString("skilltriggers.FromPacks")
	for _, p := range packs {
		key.WriteString("\x00" + p.ID + "\x00" + p.Dir + "\x00" + strings.Join(p.Skills, "\x01"))
	}
	r := packset.Remember(key.String(), func() any {
		ts, bad := fromPacks(packs)
		return read{ts, bad}
	}).(read)
	return r.triggers, r.bad
}

type read struct {
	triggers []Trigger
	bad      []Malformed
}

func fromPacks(packs []packset.Pack) ([]Trigger, []Malformed) {
	var out []Trigger
	var bad []Malformed
	for _, p := range packs {
		for _, s := range p.Skills {
			dir := filepath.Join(p.Dir, "skills", s)
			raw, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
			if err != nil {
				continue
			}
			ts, b := Of(p.ID, s, dir, skillfm.Read(string(raw)))
			out = append(out, ts...)
			bad = append(bad, b...)
		}
	}
	return out, bad
}
