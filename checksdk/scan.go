package checksdk

import "strings"

// The views a check reads source through: JavaScript and TypeScript with
// their comments removed, Markdown with its fenced blocks blanked, and
// whether a change touched only comments.

// IsJSSpace reports whether r is whitespace as JavaScript's \s reads it.
func IsJSSpace(r rune) bool {
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
	for end > 0 && IsJSSpace(code[end-1]) {
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

// StripComments returns source with its JS/TS comments removed and every
// string, template and regex literal intact; newlines inside block
// comments are kept so lines do not shift.
func StripComments(source string) string {
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
	t := strings.TrimLeftFunc(l, IsJSSpace)
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

// BlankFences empties every line of a fenced block, fences
// included.
func BlankFences(text string) string {
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

// CommentCheckable are the extensions whose comments StripComments models.
// A file outside this set is never called comments-only: the parser cannot
// see its comments, so the safe answer is no.
var CommentCheckable = map[string]bool{
	".mjs": true, ".cjs": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true, ".c": true, ".h": true, ".cc": true, ".cpp": true, ".hpp": true,
	".java": true, ".go": true, ".swift": true, ".kt": true, ".dart": true, ".rs": true, ".cs": true, ".scss": true, ".css": true,
}

// Ext is a path's extension from its last dot, lower-cased, "" when it has
// none.
func Ext(file string) string {
	i := strings.LastIndex(file, ".")
	if i < 0 {
		return ""
	}
	return strings.ToLower(file[i:])
}

// CommentOnly reports whether a change to file touched only its comments:
// before and after are its two contents, nil where it was added or
// deleted, neither of which is comments-only. Indentation and blank lines
// are ignored.
func CommentOnly(file string, before, after *string) bool {
	if before == nil || after == nil || !CommentCheckable[Ext(file)] {
		return false
	}
	return codeOf(*before) == codeOf(*after)
}

func codeOf(text string) string {
	var keep []string
	for _, l := range strings.Split(StripComments(text), "\n") {
		if l = strings.TrimFunc(l, IsJSSpace); l != "" {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n")
}
