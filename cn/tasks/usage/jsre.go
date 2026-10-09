package usage

import (
	"regexp"
	"strings"
)

// The fold's patterns are the Node task's, spelled for Go's regexp so they
// match what JavaScript's would: \s and \S are JavaScript's whitespace (its
// Unicode spaces and line terminators), a dot stops at every JavaScript
// line terminator, an /i is spelled as explicit letter classes, and a /gm
// pattern is matched per line over JavaScript's line terminators.
const (
	jsSpace    = `\t\n\x0B\f\r \x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}`
	sp         = `[` + jsSpace + `]`
	nonSp      = `[^` + jsSpace + `]`
	dot        = `[^\n\r\x{2028}\x{2029}]`
	spNotNL    = `[\t\x0B\f\r \x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}]`
	nonSpOrEqu = `[^` + jsSpace + `=]`
)

// jsPattern compiles a pattern written with the placeholders \s, \S and a
// bare dot replaced by their JavaScript meanings.
func jsPattern(src string) *regexp.Regexp {
	var b strings.Builder
	inClass := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case c == '\\' && i+1 < len(src):
			n := src[i+1]
			switch {
			case n == 's' && !inClass:
				b.WriteString(sp)
			case n == 'S' && !inClass:
				b.WriteString(nonSp)
			case n == 's' && inClass:
				b.WriteString(jsSpace)
			default:
				b.WriteByte(c)
				b.WriteByte(n)
			}
			i++
		case c == '[' && !inClass:
			inClass = true
			b.WriteByte(c)
		case c == ']' && inClass:
			inClass = false
			b.WriteByte(c)
		case c == '.' && !inClass:
			b.WriteString(dot)
		default:
			b.WriteByte(c)
		}
	}
	return regexp.MustCompile(b.String())
}

// ci spells an ASCII word case-insensitively, as JavaScript's /i folds it.
func ci(word string) string {
	var b strings.Builder
	for _, r := range word {
		lo, up := strings.ToLower(string(r)), strings.ToUpper(string(r))
		if lo == up {
			b.WriteString(regexp.QuoteMeta(string(r)))
			continue
		}
		b.WriteString("[" + lo + up + "]")
	}
	return b.String()
}

// matchLines is text.matchAll(re) for a /gm pattern anchored at ^ that
// cannot span a line: each line's first match, in order.
func matchLines(re *regexp.Regexp, text string) [][]string {
	var out [][]string
	start := 0
	for i := 0; i <= len(text); {
		end, width := i, 0
		if i < len(text) {
			switch {
			case text[i] == '\n' || text[i] == '\r':
				width = 1
			case strings.HasPrefix(text[i:], " ") || strings.HasPrefix(text[i:], " "):
				width = 3
			}
		} else {
			width = -1
		}
		if width == 0 {
			i++
			continue
		}
		if m := re.FindStringSubmatch(text[start:end]); m != nil {
			out = append(out, m)
		}
		if width < 0 {
			break
		}
		i += width
		start = i
	}
	return out
}

// splitWS is s.split(/\s+/) over a trimmed s.
func splitWS(s string) []string {
	return spaceRun.Split(s, -1)
}

var spaceRun = regexp.MustCompile(sp + `+`)
