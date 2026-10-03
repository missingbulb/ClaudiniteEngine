package checksdk

import (
	"regexp"
	"strings"
	"unicode"
)

// RuleBlock is one top-level rule bullet of a prose file, 0-based lines:
// Start, End its last non-blank line, LastLine where its marker is or a
// writer puts one; Slug is the marker's, "" when it has none.
type RuleBlock struct {
	Start, End, LastLine int
	Slug, Trigger        string
}

var (
	ruleMarkerAtEnd  = regexp.MustCompile(`\s*\(([a-z][a-z0-9]*(?:-[a-z0-9]+)+)\)\s*$`)
	ruleNumericAtEnd = regexp.MustCompile(`\s*\((?:RULES-)?(\d+[a-z]?(?:\s*,\s*\d+[a-z]?)*)\)\s*$`)
	ruleBulletLine   = regexp.MustCompile(`^- \*\*`)
	ruleTopBullet    = regexp.MustCompile(`^- `)
	ruleNestedItem   = regexp.MustCompile(`^\s+(?:[-*+]|\d+\.)\s`)
	ruleHeading      = regexp.MustCompile(`^#{1,6}\s+`)
	ruleFence        = regexp.MustCompile("^\\s*```")
	ruleLead         = regexp.MustCompile(`(?s)\*\*(.+?)\*\*`)
	ruleSpaces       = regexp.MustCompile(`\s+`)
	ruleClauseCut    = regexp.MustCompile(`\s[\p{Pd}:]\s|[.:;]\s|[.!?]$`)
)

func ruleStripMarkers(line string) string {
	return ruleNumericAtEnd.ReplaceAllString(ruleMarkerAtEnd.ReplaceAllString(line, ""), "")
}

// ruleLeadIn is a bold lead-in as a trigger: emphasis off, whitespace
// collapsed, a trailing dash dropped.
func ruleLeadIn(s string) string {
	s = strings.NewReplacer("`", "", "*", "", "_", "").Replace(s)
	s = strings.TrimSpace(ruleSpaces.ReplaceAllString(s, " "))
	if r := []rune(s); len(r) > 0 && unicode.Is(unicode.Pd, r[len(r)-1]) {
		s = string(r[:len(r)-1])
	}
	return strings.TrimSpace(s)
}

// ruleOpeningWords is a plain bullet's trigger: its first clause, or its
// first eight words.
func ruleOpeningWords(s string) string {
	clause := ruleLeadIn(s)
	if loc := ruleClauseCut.FindStringIndex(clause); loc != nil {
		clause = clause[:loc[0]]
	}
	words := strings.Split(clause, " ")
	if len(words) > 8 {
		words = words[:8]
	}
	return strings.Join(words, " ")
}

// RuleBlocks are the rule bullets of a prose file — "- **lead-in** …" and
// every line belonging to it, closed by the next top-level bullet or a
// heading, a fenced block opaque — with the provenance marker each ends
// with: the first paragraph's last line, or failing that the block's.
func RuleBlocks(text string) []RuleBlock {
	lines := strings.Split(text, "\n")
	var blocks []RuleBlock
	open := false
	start, end := 0, 0
	marked := func(i int) (slug string, any bool) {
		if m := ruleMarkerAtEnd.FindStringSubmatch(lines[i]); m != nil {
			return m[1], true
		}
		return "", ruleNumericAtEnd.MatchString(lines[i])
	}
	closeBlock := func() {
		if !open {
			return
		}
		e := end
		for e > start && strings.TrimSpace(lines[e]) == "" {
			e--
		}
		para := start
		for i := start + 1; i <= e && strings.TrimSpace(lines[i]) != "" && !ruleNestedItem.MatchString(lines[i]); i++ {
			para = i
		}
		slug, found := marked(para)
		last := para
		if !found && e != para {
			if s2, f2 := marked(e); f2 {
				slug, last = s2, e
			}
		}
		body := make([]string, 0, e-start+1)
		for i := start; i <= e; i++ {
			if i == last {
				body = append(body, ruleStripMarkers(lines[i]))
			} else {
				body = append(body, lines[i])
			}
		}
		trigger := ""
		if m := ruleLead.FindStringSubmatch(strings.Join(body, "\n")); m != nil {
			trigger = ruleLeadIn(m[1])
		} else {
			trigger = ruleOpeningWords(strings.TrimPrefix(ruleStripMarkers(lines[start]), "- "))
		}
		blocks = append(blocks, RuleBlock{Start: start, End: e, LastLine: last, Slug: slug, Trigger: trigger})
		open = false
	}
	fenced := false
	for i, line := range lines {
		if ruleFence.MatchString(line) {
			fenced = !fenced
			if open {
				end = i
			}
			continue
		}
		if fenced {
			if open {
				end = i
			}
			continue
		}
		if ruleBulletLine.MatchString(line) {
			closeBlock()
			open, start, end = true, i, i
			continue
		}
		if ruleTopBullet.MatchString(line) || ruleHeading.MatchString(line) {
			closeBlock()
			continue
		}
		if open {
			end = i
		}
	}
	closeBlock()
	return blocks
}
