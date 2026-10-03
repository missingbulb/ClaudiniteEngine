package capture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Scrubbing is enumeration-first: a transcript records tool output
// verbatim and the harness redacts nothing, so every value the capturing
// environment holds (its variables, Claude Code's credential store) is
// redacted wherever it appears, whatever its shape, because a pattern
// list misses shapes it never heard of. A variable StructuralEnv does not
// name is redacted; it names only the paths, locale and CI metadata that
// saturate a transcript, where redaction would mangle the log and protect
// nothing.

// StructuralEnv are the variables never redacted.
var StructuralEnv = []string{
	"PATH", "HOME", "PWD", "OLDPWD", "SHELL", "SHLVL", "TERM", "COLORTERM", "_",
	"LANG", "LANGUAGE", "USER", "LOGNAME", "HOSTNAME", "HOSTTYPE", "OSTYPE", "MACHTYPE",
	"TMPDIR", "TMP", "TEMP", "EDITOR", "VISUAL", "PAGER", "DISPLAY", "NODE_ENV", "CI",
	"CLAUDE_PROJECT_DIR", "CLAUDE_CODE_SESSION_ID", "PLAYWRIGHT_BROWSERS_PATH",
	"GITHUB_REPOSITORY", "GITHUB_REF", "GITHUB_REF_NAME", "GITHUB_SHA", "GITHUB_WORKSPACE",
	"GITHUB_EVENT_NAME", "GITHUB_RUN_ID", "GITHUB_RUN_NUMBER", "GITHUB_JOB", "GITHUB_ACTOR",
	"RUNNER_OS", "RUNNER_ARCH", "RUNNER_TEMP", "RUNNER_TOOL_CACHE",
}

// StructuralEnvPrefixes are the variable prefixes never redacted.
var StructuralEnvPrefixes = []string{"LC_", "XDG_"}

// MinSecretLength is the shortest value redacted, in UTF-16 units: a
// shorter one collides with prose too often, and is beyond saving anyway.
const MinSecretLength = 8

// Value is one named value for the redaction list.
type Value struct{ Name, Value string }

// Redaction is one form a value takes, and the name it is redacted as.
type Redaction struct {
	Name string `json:"name"`
	Form string `json:"form"`
}

// units is s's length in UTF-16 units.
func units(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// jsEscape is s as JSON.stringify writes it, without the quotes: the
// shape a value takes inside a raw JSONL line.
func jsEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func structural(name string) bool {
	for _, s := range StructuralEnv {
		if s == name {
			return true
		}
	}
	for _, p := range StructuralEnvPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// Redactions is the redaction list over env ("K=V" entries, in order)
// and extra: each value long enough, in its raw form and, where it
// differs, its JSON-escaped form, longest first so a value holding
// another is consumed whole.
func Redactions(env []string, extra []Value) []Redaction {
	var out []Redaction
	seen := map[string]bool{}
	add := func(name, value string) {
		if units(value) < MinSecretLength {
			return
		}
		forms := []string{value}
		if e := jsEscape(value); e != value {
			forms = append(forms, e)
		}
		for _, f := range forms {
			if units(f) >= MinSecretLength && !seen[f] {
				seen[f] = true
				out = append(out, Redaction{name, f})
			}
		}
	}
	for _, kv := range env {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || structural(name) {
			continue
		}
		add(name, value)
	}
	for _, v := range extra {
		add(v.Name, v.Value)
	}
	sort.SliceStable(out, func(a, b int) bool { return units(out[a].Form) > units(out[b].Form) })
	return out
}

// CredentialName names a credential store's values in a redaction.
const CredentialName = "claude-credentials"

// CredentialValues are the strings a credential store's JSON holds, in
// the order a JavaScript walk of it visits them (an object's integer keys
// ascending, then its others as written); nothing for a store that does
// not parse.
func CredentialValues(raw []byte) []Value {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, err := orderedValue(dec)
	if err != nil {
		return nil
	}
	var out []Value
	var walk func(any)
	walk = func(n any) {
		switch x := n.(type) {
		case string:
			out = append(out, Value{CredentialName, x})
		case []any:
			for _, e := range x {
				walk(e)
			}
		case orderedObject:
			for _, k := range x.jsOrder() {
				walk(x.vals[k])
			}
		}
	}
	walk(v)
	return out
}

type orderedObject struct {
	keys []string
	vals map[string]any
}

// jsOrder is the order JavaScript enumerates the object's keys in.
func (o orderedObject) jsOrder() []string {
	var ints, rest []string
	for _, k := range o.keys {
		if n, err := strconv.ParseUint(k, 10, 32); err == nil && n < 1<<32-1 && strconv.FormatUint(n, 10) == k {
			ints = append(ints, k)
		} else {
			rest = append(rest, k)
		}
	}
	sort.SliceStable(ints, func(a, b int) bool {
		x, _ := strconv.ParseUint(ints[a], 10, 32)
		y, _ := strconv.ParseUint(ints[b], 10, 32)
		return x < y
	})
	return append(ints, rest...)
}

func orderedValue(dec *json.Decoder) (any, error) {
	t, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch d := t.(type) {
	case json.Delim:
		switch d {
		case '{':
			o := orderedObject{vals: map[string]any{}}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k, _ := kt.(string)
				v, err := orderedValue(dec)
				if err != nil {
					return nil, err
				}
				if _, dup := o.vals[k]; !dup {
					o.keys = append(o.keys, k)
				}
				o.vals[k] = v
			}
			_, err := dec.Token()
			return o, err
		case '[':
			var a []any
			for dec.More() {
				v, err := orderedValue(dec)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
			_, err := dec.Token()
			return a, err
		}
	}
	return t, nil
}

// secretPatterns are the backstop for secrets that never lived in the
// environment (a token pasted into chat, a key read from a file): high
// confidence credential shapes only, so prose naming a prefix is
// untouched.
var secretPatterns = []struct {
	kind string
	re   *regexp.Regexp
}{
	{"github-token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`)},
	{"github-token", regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`)},
	{"anthropic-key", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}\b`)},
	{"api-key", regexp.MustCompile(`\bsk-[A-Za-z0-9]{32,}\b`)},
	{"aws-key-id", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"slack-token", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`)},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{5,}\b`)},
}

// Scrub redacts every enumerated form, then every credential shape.
func Scrub(text string, redactions []Redaction) string {
	out := text
	for _, r := range redactions {
		out = strings.ReplaceAll(out, r.Form, "[REDACTED:env:"+r.Name+"]")
	}
	for _, p := range secretPatterns {
		out = p.re.ReplaceAllLiteralString(out, "[REDACTED:"+p.kind+"]")
	}
	return out
}

// validText is text as a UTF-8 decode reads it, each invalid sequence
// replaced.
func validText(raw []byte) string {
	if utf8.Valid(raw) {
		return string(raw)
	}
	return strings.ToValidUTF8(string(raw), "�")
}

// EnvFromJSON is a JSON object of variables as "K=V" entries, in the
// order JavaScript enumerates it; a value that is not a string is not a
// variable.
func EnvFromJSON(raw []byte) ([]string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, err := orderedValue(dec)
	if err != nil {
		return nil, err
	}
	o, ok := v.(orderedObject)
	if !ok {
		return nil, nil
	}
	var out []string
	for _, k := range o.jsOrder() {
		if s, ok := o.vals[k].(string); ok {
			out = append(out, k+"="+s)
		}
	}
	return out, nil
}
