package declared

import (
	"strings"
)

// The views a family may read a file through: raw, with JavaScript and
// TypeScript comments blanked, or with Markdown fences blanked.

func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func isASCIIWord(r rune) bool {
	return r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

var notAValue = []string{"return", "typeof", "instanceof", "in", "of", "new", "delete", "void", "case", "do", "else", "yield", "await"}

// opensRegex reports whether a / after code opens a regex literal: where
// a value may begin, which is after anything but a value's end, or after
// a keyword that is not a value.
func opensRegex(code []rune) bool {
	end := len(code)
	for end > 0 && isJSSpace(code[end-1]) {
		end--
	}
	if end == 0 {
		return true
	}
	last := code[end-1]
	endsValue := isASCIIWord(last) || last == '$' || last == ')' || last == ']'
	if !endsValue {
		return true
	}
	for _, kw := range notAValue {
		n := len(kw)
		if end < n || string(code[end-n:end]) != kw {
			continue
		}
		if end == n || !isASCIIWord(code[end-n-1]) {
			return true
		}
	}
	return false
}

// stripComments returns source with its JS/TS comments removed and every
// string, template and regex literal intact; newlines inside block
// comments are kept so lines do not shift.
func stripComments(source string) string {
	src := []rune(source)
	out := make([]rune, 0, len(src))
	state := "code"
	var holes []int
	for i := 0; i < len(src); i++ {
		c := src[i]
		var c2 rune = -1
		if i+1 < len(src) {
			c2 = src[i+1]
		}
		switch state {
		case "code":
			if c == '/' && c2 == '/' {
				state = "line"
				i++
				continue
			}
			if c == '/' && c2 == '*' {
				state = "block"
				i++
				continue
			}
			switch {
			case c == '/' && opensRegex(out):
				state = "re"
			case c == '\'':
				state = "sq"
			case c == '"':
				state = "dq"
			case c == '`':
				state = "tpl"
			case len(holes) > 0 && c == '{':
				holes[len(holes)-1]++
			case len(holes) > 0 && c == '}':
				if holes[len(holes)-1] == 0 {
					holes = holes[:len(holes)-1]
					state = "tpl"
				} else {
					holes[len(holes)-1]--
				}
			}
			out = append(out, c)
		case "re", "reClass":
			out = append(out, c)
			switch {
			case c == '\\':
				if c2 >= 0 {
					out = append(out, c2)
				}
				i++
			case c == '\n':
				state = "code"
			case state == "re" && c == '[':
				state = "reClass"
			case state == "reClass" && c == ']':
				state = "re"
			case state == "re" && c == '/':
				state = "code"
			}
		case "line":
			if c == '\n' {
				state = "code"
				out = append(out, c)
			}
		case "block":
			if c == '*' && c2 == '/' {
				state = "code"
				i++
			} else if c == '\n' {
				out = append(out, c)
			}
		default:
			out = append(out, c)
			switch {
			case c == '\\':
				if c2 >= 0 {
					out = append(out, c2)
				}
				i++
			case state == "tpl" && c == '$' && c2 == '{':
				out = append(out, c2)
				i++
				holes = append(holes, 0)
				state = "code"
			case (state == "sq" && c == '\'') || (state == "dq" && c == '"') || (state == "tpl" && c == '`'):
				state = "code"
			}
		}
	}
	return string(out)
}

func fenceLine(l string) bool {
	t := strings.TrimLeftFunc(l, isJSSpace)
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

// blankMarkdownFences empties every line of a fenced block, fences
// included.
func blankMarkdownFences(text string) string {
	lines := strings.Split(text, "\n")
	in := false
	for i, l := range lines {
		if fenceLine(l) {
			in = !in
			lines[i] = ""
			continue
		}
		if in {
			lines[i] = ""
		}
	}
	return strings.Join(lines, "\n")
}

var h2Re = mustRegex(`^##\s`, "")

type mdLine struct {
	line int
	text string
}

// markdownDoc is a page's "## " sections, read with fences blanked.
type markdownDoc struct {
	stripped     []string
	firstHeading *string
	sections     map[string][]mdLine
	absent       map[string]bool
}

func newMarkdownDoc(text string) *markdownDoc {
	d := &markdownDoc{stripped: strings.Split(blankMarkdownFences(text), "\n"), sections: map[string][]mdLine{}, absent: map[string]bool{}}
	for _, l := range d.stripped {
		if h2Re.Test(l) {
			h := strings.TrimFunc(strings.TrimLeftFunc(strings.TrimPrefix(l, "##"), isJSSpace), isJSSpace)
			d.firstHeading = &h
			break
		}
	}
	return d
}

// section is the named section's lines, or nil, false when the page has
// none.
func (d *markdownDoc) section(name string) ([]mdLine, bool) {
	key := strings.ToLower(name)
	if d.absent[key] {
		return nil, false
	}
	if s, ok := d.sections[key]; ok {
		return s, true
	}
	head := mustRegex(`^##\s+`+escapeRe(name)+`\b`, "i")
	start := -1
	for i, l := range d.stripped {
		if head.Test(l) {
			start = i
			break
		}
	}
	if start == -1 {
		d.absent[key] = true
		return nil, false
	}
	end := len(d.stripped)
	for i := start + 1; i < len(d.stripped); i++ {
		if h2Re.Test(d.stripped[i]) {
			end = i
			break
		}
	}
	out := make([]mdLine, 0, end-start-1)
	for i, t := range d.stripped[start+1 : end] {
		out = append(out, mdLine{start + 2 + i, t})
	}
	d.sections[key] = out
	return out, true
}
