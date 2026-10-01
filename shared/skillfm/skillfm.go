// Package skillfm reads a SKILL.md's frontmatter with the deliberate YAML
// subset the Node engine's skill-frontmatter.mjs reads: scalars, a block
// list, nested maps, a flow list split on commas, quotes stripped. It is not
// a YAML parser on purpose: a skill's frontmatter is the harness's file, and
// a malformed one must read as empty metadata exactly as that reader reads
// it, so the guards slice acts on the same triggers.
package skillfm

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// The metadata keys a skill's triggers live under.
const (
	ForceLoadPaths       = "force-load-on-file-edits-paths"
	ForceLoadToolCalls   = "force-load-on-tool-calls"
	ForceLoadPrompts     = "force-load-on-prompts-matching"
	ForceLoadToolResults = "force-load-on-tool-results-matching"
	BodyKey              = "body"
)

// Bodies are the values metadata.body may take.
var Bodies = []string{"workflow", "guidelines"}

// jsSpace is the character class JavaScript's \s and String.trim match.
const jsSpace = "\t\n\v\f\r \u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000\ufeff"

// jsDot is JavaScript's ".": anything but a line terminator.
const jsDot = "[^\n\r\u2028\u2029]"

var (
	itemRe = regexp.MustCompile(`^[` + jsSpace + `]+-[` + jsSpace + `]*(` + jsDot + `*)$`)
	kvRe   = regexp.MustCompile(`^[` + jsSpace + `]*([A-Za-z_][A-Za-z0-9_-]*):[` + jsSpace + `]*(` + jsDot + `*)$`)
	flowRe = regexp.MustCompile(`^\[(` + jsDot + `*)\]$`)
)

func isJSSpace(r rune) bool {
	switch {
	case r == '\t', r == '\n', r == '\v', r == '\f', r == '\r', r == ' ', r == 0xa0, r == 0x1680,
		r >= 0x2000 && r <= 0x200a, r == 0x2028, r == 0x2029, r == 0x202f, r == 0x205f, r == 0x3000, r == 0xfeff:
		return true
	}
	return false
}

func trim(s string) string { return strings.TrimFunc(s, isJSSpace) }

func unquote(s string) string {
	t := trim(s)
	if len(t) >= 2 && ((t[0] == '"' && t[len(t)-1] == '"') || (t[0] == '\'' && t[len(t)-1] == '\'')) {
		return t[1 : len(t)-1]
	}
	return t
}

func scalar(rest string) any {
	if m := flowRe.FindStringSubmatch(trim(rest)); m != nil {
		out := []any{}
		for _, p := range strings.Split(m[1], ",") {
			if u := unquote(p); u != "" {
				out = append(out, u)
			}
		}
		return out
	}
	return unquote(rest)
}

type frame struct {
	key    string
	indent int
	parent map[string]any
}

// Parse reads text's frontmatter into strings, lists of strings and maps;
// text with no frontmatter reads as an empty map.
func Parse(text string) map[string]any {
	out := map[string]any{}
	if !strings.HasPrefix(text, "---") {
		return out
	}
	end := strings.Index(text[3:], "\n---")
	if end == -1 {
		return out
	}
	end += 3
	first := strings.Index(text, "\n")
	if first+1 > end {
		return out
	}
	var stack []frame
	for _, line := range strings.Split(text[first+1:end], "\n") {
		if trim(line) == "" {
			continue
		}
		indent := utf8.RuneCountInString(line) - utf8.RuneCountInString(strings.TrimLeftFunc(line, isJSSpace))
		for len(stack) > 0 && indent <= stack[len(stack)-1].indent {
			stack = stack[:len(stack)-1]
		}
		var open *frame
		if len(stack) > 0 {
			open = &stack[len(stack)-1]
		}
		if m := itemRe.FindStringSubmatch(line); m != nil && open != nil {
			if l, ok := open.parent[open.key].([]any); ok {
				open.parent[open.key] = append(l, unquote(m[1]))
			}
			continue
		}
		m := kvRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key, rest := m[1], m[2]
		target := out
		if open != nil {
			if l, ok := open.parent[open.key].([]any); ok && len(l) == 0 {
				open.parent[open.key] = map[string]any{}
			}
			t, ok := open.parent[open.key].(map[string]any)
			if !ok {
				continue
			}
			target = t
		}
		if trim(rest) == "" {
			target[key] = []any{}
			stack = append(stack, frame{key: key, indent: indent, parent: target})
			continue
		}
		target[key] = scalar(rest)
	}
	return out
}

// Meta is what the engine keeps of a skill's frontmatter.
type Meta struct {
	Name, Description string
	// Body is metadata.body when it is one of Bodies, else "".
	Body string
	// The force-load triggers, as written; the guards slice acts on them.
	ForceLoadPaths, ToolCalls, Prompts, ToolResults []string
}

func metadata(fm map[string]any) map[string]any {
	m, _ := fm["metadata"].(map[string]any)
	return m
}

func strings_(v any) []string {
	var out []string
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
	case string:
		if trim(x) != "" {
			out = []string{x}
		}
	}
	return out
}

// Read is the metadata of a SKILL.md's text.
func Read(text string) Meta {
	fm := Parse(text)
	m := Meta{}
	m.Name, _ = fm["name"].(string)
	m.Description, _ = fm["description"].(string)
	md := metadata(fm)
	if b, ok := md[BodyKey].(string); ok {
		for _, want := range Bodies {
			if trim(b) == want {
				m.Body = want
			}
		}
	}
	switch v := md[ForceLoadPaths].(type) {
	case []any:
		m.ForceLoadPaths = strings_(v)
	case string:
		for _, p := range strings.Split(v, ",") {
			if t := trim(p); t != "" {
				m.ForceLoadPaths = append(m.ForceLoadPaths, t)
			}
		}
	}
	m.ToolCalls = strings_(md[ForceLoadToolCalls])
	m.Prompts = strings_(md[ForceLoadPrompts])
	m.ToolResults = strings_(md[ForceLoadToolResults])
	return m
}
