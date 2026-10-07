package checksdk

import (
	"path"
	"regexp"
	"strings"
)

// jsWS is JavaScript's \s, which the link pattern is written in.
const jsWS = `\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

var (
	linkRe    = regexp.MustCompile(`!?\[([^\]]*)\]\(([^)` + jsWS + `]+)(?:[` + jsWS + `]+"[^"]*")?\)`)
	ticksRe   = regexp.MustCompile("`+")
	fenceRe   = regexp.MustCompile("^[" + jsWS + "]*(```|~~~)")
	schemeRe  = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*:`)
	pathCharB = func(b byte) bool {
		return b == '_' || b == '.' || b == '/' || b == '@' || b == '-' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
	}
)

// Link is one relative Markdown link: its line, its target with any
// anchor dropped, and its label with backticks dropped.
type Link struct {
	Line   int
	Target string
	Label  string
}

// codeSpans are the byte ranges of a line's inline code spans.
func codeSpans(line string) [][2]int {
	var spans [][2]int
	open, fence := -1, ""
	for _, m := range ticksRe.FindAllStringIndex(line, -1) {
		tick := line[m[0]:m[1]]
		if open < 0 {
			open, fence = m[0], tick
		} else if tick == fence {
			spans = append(spans, [2]int{open, m[1]})
			open = -1
		}
	}
	return spans
}

// ExtractLinks are the relative links of a Markdown text, outside fenced
// blocks and code spans; external, mailto and pure-anchor links are
// skipped.
func ExtractLinks(text string) []Link {
	var links []Link
	inFence := false
	for i, line := range strings.Split(text, "\n") {
		if fenceRe.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		spans := codeSpans(line)
	match:
		for _, m := range linkRe.FindAllStringSubmatchIndex(line, -1) {
			for _, s := range spans {
				if m[0] >= s[0] && m[0] < s[1] {
					continue match
				}
			}
			target := line[m[4]:m[5]]
			if schemeRe.MatchString(target) || strings.HasPrefix(target, "#") {
				continue
			}
			target, _, _ = strings.Cut(target, "#")
			if target == "" {
				continue
			}
			links = append(links, Link{Line: i + 1, Target: target, Label: strings.ReplaceAll(line[m[2]:m[3]], "`", "")})
		}
	}
	return links
}

// Resolve is target as written in file, joined onto the file's folder
// and cleaned.
func Resolve(file, target string) string { return path.Join(path.Dir(file), target) }

// DeadLink is a relative link that resolves to nothing.
type DeadLink struct {
	Path     string
	Line     int
	Target   string
	Resolved string
}

// DeadLinks are the relative links in files' Markdown (nil files: every
// scanned file) that resolve to nothing in the working tree; a link
// reaching outside the repo is not verifiable and is skipped.
func DeadLinks(r Repo, files []string) []DeadLink {
	if files == nil {
		files = r.Files()
	}
	var out []DeadLink
	for _, f := range files {
		if !strings.HasSuffix(f, ".md") {
			continue
		}
		text, ok := r.Read(f)
		if !ok {
			continue
		}
		for _, l := range ExtractLinks(text) {
			resolved := Resolve(f, l.Target)
			if strings.HasPrefix(resolved, "..") || r.Exists(resolved) {
				continue
			}
			out = append(out, DeadLink{Path: f, Line: l.Line, Target: l.Target, Resolved: resolved})
		}
	}
	return out
}

// Dangling is a tracked line still naming a path the change deletes.
type Dangling struct {
	Path string
	Line int
	Text string
	Gone string
}

// DanglingReferences are the tracked lines still naming a path the change
// deletes. A hit whose every occurrence widens to a path token that still
// resolves is a rename that kept the basename, and is dropped; tolerated
// (nil for none) exempts a deleted path something else governs.
func DanglingReferences(r Repo, tolerated func(gone string) bool) []Dangling {
	var out []Dangling
	for _, gone := range r.Deleted() {
		if tolerated != nil && tolerated(gone) {
			continue
		}
		for _, hit := range r.GrepTracked(gone) {
			if hit.Path == gone || survives(r, hit, gone) {
				continue
			}
			out = append(out, Dangling{Path: hit.Path, Line: hit.Line, Text: hit.Text, Gone: gone})
		}
	}
	return out
}

func survives(r Repo, hit Line, gone string) bool {
	idx := strings.Index(hit.Text, gone)
	if idx < 0 {
		return false
	}
	for idx >= 0 {
		s, e := idx, idx+len(gone)
		for s > 0 && pathCharB(hit.Text[s-1]) {
			s--
		}
		for e < len(hit.Text) && pathCharB(hit.Text[e]) {
			e++
		}
		token := hit.Text[s:e]
		if (token == gone || !r.Exists(token)) && !r.Exists(Resolve(hit.Path, token)) {
			return false
		}
		next := strings.Index(hit.Text[idx+1:], gone)
		if next < 0 {
			break
		}
		idx += 1 + next
	}
	return true
}
