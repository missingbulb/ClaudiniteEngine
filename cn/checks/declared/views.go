package declared

import (
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/checksdk"
)

// The views a family may read a file through: raw, with JavaScript and
// TypeScript comments blanked, or with Markdown fences blanked.

func isJSSpace(r rune) bool { return checksdk.IsJSSpace(r) }

func stripComments(source string) string { return checksdk.StripComments(source) }

func blankMarkdownFences(text string) string { return checksdk.BlankFences(text) }

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
