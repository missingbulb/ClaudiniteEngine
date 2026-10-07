// Package jsregex is JavaScript's RegExp for the patterns pack data
// carries as "/body/flags" strings (a skill's force-load triggers, a
// pack's merge rules): ECMAScript semantics through regexp2, so a
// lookaround or a backreference runs as its author wrote it, and a match
// bounded by MatchTimeout. It also carries String.prototype.trim's notion
// of whitespace, which every such reader applies before parsing.
package jsregex

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/dlclark/regexp2"
)

// MatchTimeout bounds one match; a pattern past it does not match.
var MatchTimeout = time.Second

// Regex is one compiled ECMAScript pattern.
type Regex struct {
	re     *regexp2.Regexp
	Source string
	Flags  string
}

// Test reports whether s holds a match; a match that times out is none.
func (r *Regex) Test(s string) bool {
	if r == nil {
		return false
	}
	ok, err := r.re.MatchString(s)
	return err == nil && ok
}

// Compile is new RegExp(body, flags), or an error JavaScript would raise
// too; flags outside i, m, s, g, u, y and d are refused, and g, u, y and d
// change nothing a test reads here.
func Compile(body, flags string) (*Regex, error) {
	opt := regexp2.RegexOptions(regexp2.ECMAScript)
	dotAll := false
	for _, f := range flags {
		switch f {
		case 'i':
			opt |= regexp2.IgnoreCase
		case 'm':
			opt |= regexp2.Multiline
		case 's':
			dotAll = true
		case 'g', 'u', 'y', 'd':
		default:
			return nil, fmt.Errorf("invalid flag %q", f)
		}
	}
	src := body
	if dotAll {
		src = dotAllBody(body)
	}
	re, err := regexp2.Compile(src, opt)
	if err != nil {
		return nil, err
	}
	re.MatchTimeout = MatchTimeout
	return &Regex{re: re, Source: body, Flags: flags}, nil
}

// dotAllBody rewrites each unescaped dot outside a class as [\s\S]:
// ECMAScript mode refuses Singleline, and the class says the same.
func dotAllBody(body string) string {
	var b strings.Builder
	inClass := false
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case c == '\\' && i+1 < len(body):
			b.WriteByte(c)
			i++
			b.WriteByte(body[i])
			continue
		case c == '[' && !inClass:
			inClass = true
		case c == ']' && inClass:
			inClass = false
		case c == '.' && !inClass:
			b.WriteString(`[\s\S]`)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

var form = regexp.MustCompile(`(?s)^/(.*)/([a-z]*)$`)

// Form splits a "/body/flags" string, or reports false.
func Form(s string) (body, flags string, ok bool) {
	m := form.FindStringSubmatch(s)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// IsSpace is String.prototype.trim's whitespace.
func IsSpace(r rune) bool {
	switch {
	case r == '\t', r == '\n', r == '\v', r == '\f', r == '\r', r == ' ', r == 0xa0, r == 0x1680,
		r >= 0x2000 && r <= 0x200a, r == 0x2028, r == 0x2029, r == 0x202f, r == 0x205f, r == 0x3000, r == 0xfeff:
		return true
	}
	return false
}

// Trim is String.prototype.trim.
func Trim(s string) string { return strings.TrimFunc(s, IsSpace) }
