package provenance

import (
	"regexp"
	"strings"
)

var (
	sessionRef = regexp.MustCompile(`\bsession_[A-Za-z0-9]{8,}\b`)
	captureRef = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}T\d{4}Z--(?:pr|issue)-\d+--[A-Za-z0-9-]+(?:\.jsonl)?\b`)
	handleRole = regexp.MustCompile(`@[A-Za-z0-9-]+ \((owner|maintainer|contributor)\)`)
	memberRef  = regexp.MustCompile(`\b[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+#\d+\b`)
)

const quoteDropped = "(quote dropped)"

// ReduceText is text reduced by what the boundary it crosses is: a
// session id, a capture's name and a long quoted phrase never cross; a
// handle becomes its role and a member's repo reference "a member
// repository" once the canon is public.
func ReduceText(text string, publicCanon bool) string {
	out := sessionRef.ReplaceAllString(text, "a session")
	out = captureRef.ReplaceAllString(out, "a capture")
	out = dropQuotes(out, '"', '"')
	out = dropQuotes(out, '“', '”')
	if publicCanon {
		out = handleRole.ReplaceAllString(out, "the ${1}")
		out = memberRef.ReplaceAllString(out, "a member repository")
	}
	return out
}

// dropQuotes replaces a quoted phrase of twelve characters or more that
// opens the text or follows a space and closes before a space, a
// punctuation mark or the end.
func dropQuotes(text string, open, close rune) string {
	rs := []rune(text)
	var b strings.Builder
	i := 0
	for i < len(rs) {
		if rs[i] == open && (i == 0 || isSpace(rs[i-1])) {
			j := i + 1
			for j < len(rs) && rs[j] != close && rs[j] != '\n' {
				j++
			}
			if j < len(rs) && rs[j] == close && j-i-1 >= 12 && (j+1 == len(rs) || closesQuote(rs[j+1])) {
				b.WriteString(quoteDropped)
				i = j + 1
				continue
			}
		}
		b.WriteRune(rs[i])
		i++
	}
	return b.String()
}

func closesQuote(r rune) bool {
	return isSpace(r) || strings.ContainsRune(".,;:)", r)
}

// ReduceFile is a whole provenance file reduced: a heading keeps its
// kind and date, every other line is reduced as text.
func ReduceFile(text string, publicCanon bool) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if !strings.HasPrefix(l, "## ") {
			lines[i] = ReduceText(l, publicCanon)
		}
	}
	return strings.Join(lines, "\n")
}
