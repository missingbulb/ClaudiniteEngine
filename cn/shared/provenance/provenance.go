// Package provenance reads a pack's decision log, read-only: the file
// grammar, the marker that binds a prose rule to its file, how a pack's
// carriers are enumerated and which file each names, and the audit an
// integrity check judges. It is the Node engine's provenance helper at
// missingbulb/Claudinite@057841ac (engine/checks/helpers/provenance.mjs)
// without its writers: the file grammar is spelled once, here, and
// nothing below carries a check's failure text.
//
// Every read goes through an IO over repo-relative, slash-separated
// paths, so the same code reads a working tree and a tree at a base ref.
package provenance

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Dir is a pack's provenance folder.
const Dir = "provenance"

// PackElement is the pack's own element, the id its manifest names;
// DeclinedFile is the log of turned-down candidates, a file beside the
// elements and never a carrier.
const (
	PackElement  = "_pack"
	DeclinedFile = "_declined.md"
	DeclinedKind = "declined"
)

// Kinds is the closed vocabulary of an entry's kind, and MechanismKinds
// the kinds whose entry always carries Mechanism. The last three are the
// shelf's own from its port onto cn: ported, an element's runtime moving
// with its carrier unchanged; gate-changed, what decides when a task or
// check runs; hardened, a guard made stricter (design record row 125).
var (
	Kinds = []string{"born", "reworded", "strengthened", "weakened", "split", "merged", "moved",
		"converted", "trigger-changed", "policy-changed", "severity-changed", "scope-changed", "reaffirmed", "promoted", "retired",
		"ported", "gate-changed", "hardened"}
	MechanismKinds = []string{"born", "converted", "moved", "trigger-changed", "policy-changed", "severity-changed", "scope-changed"}
	Fields         = []string{"Source", "Reason", "Actor", "Model", "Mechanism", "Rejected", "Retire when", "Landed"}
)

// SlugRE is a marker's slug: two to four hyphenated words, so a plain
// parenthetical ("(canon)"), an issue ("(#1119)") and the retired numeric
// marker ("(3)") never read as one.
var SlugRE = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)+$`)

var (
	markerAtEnd  = regexp.MustCompile(`\s*\(([a-z][a-z0-9]*(?:-[a-z0-9]+)+)\)\s*$`)
	numericAtEnd = regexp.MustCompile(`\s*\((?:RULES-)?(\d+[a-z]?(?:\s*,\s*\d+[a-z]?)*)\)\s*$`)
	ruleBullet   = regexp.MustCompile(`^- \*\*`)
	topBullet    = regexp.MustCompile(`^- `)
	nestedItem   = regexp.MustCompile(`^\s+(?:[-*+]|\d+\.)\s`)
	heading      = regexp.MustCompile(`^#{1,6}\s+`)
	numberedStep = regexp.MustCompile(`^\d+\.\s`)
	entryHead    = regexp.MustCompile(`^## (\d{4}-\d{2}-\d{2}) · ([a-z-]+) · (.+?)\s*$`)
	fieldLine    = regexp.MustCompile(`^- \*\*([A-Z][A-Za-z ]*?):\*\*\s*(.*)$`)
	continuation = regexp.MustCompile(`^\s{2,}\S`)
	fence        = regexp.MustCompile("^\\s*```")
	elementFile  = regexp.MustCompile(`^_?[a-z0-9]+(?:-[a-z0-9]+)*\.md$`)
	convertedRE  = regexp.MustCompile(`^converted from references\.md \(`)
	leadRE       = regexp.MustCompile(`(?s)\*\*(.+?)\*\*`)
)

// ElementID is a check id as an element id, its slash a hyphen; FileOfID
// is the file it names.
func ElementID(id string) string { return strings.ReplaceAll(id, "/", "-") }

// FileOfID is the provenance file an element id names.
func FileOfID(id string) string { return ElementID(id) + ".md" }

// Entry is one entry of a provenance file.
type Entry struct {
	Date, Kind, Title string
	// Line is the entry's heading line, 1-based.
	Line   int
	Fields map[string]string
	Order  []string
	last   string
}

// Problem is a grammar fault at a 1-based line.
type Problem struct {
	Line int
	What string
}

func has(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// validDate is a date the Node engine's Date.parse reads: a month of the
// year and a day of at most 31.
func validDate(d string) bool {
	m, _ := strconv.Atoi(d[5:7])
	day, _ := strconv.Atoi(d[8:10])
	return m >= 1 && m <= 12 && day >= 1 && day <= 31
}

// Parse reads a provenance file's entries in file order, with the
// grammar faults found on the way; text outside an entry is a fault.
func Parse(text string) ([]Entry, []Problem) { return ParseKinds(text, Kinds) }

// ParseKinds is Parse admitting kinds: the element kinds, or the declined
// log's one kind.
func ParseKinds(text string, kinds []string) ([]Entry, []Problem) {
	var entries []Entry
	var problems []Problem
	cur := -1
	lastDate := ""
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSuffix(raw, "\r")
		n := i + 1
		if m := entryHead.FindStringSubmatch(line); m != nil {
			date, kind, title := m[1], m[2], m[3]
			if !has(kinds, kind) {
				problems = append(problems, Problem{n, `entry kind "` + kind + `" is not in the vocabulary`})
			}
			if !validDate(date) {
				problems = append(problems, Problem{n, `entry date "` + date + `" is not a date`})
			} else if date < lastDate {
				problems = append(problems, Problem{n, "entry dated " + date + " follows one dated " + lastDate + "; entries are appended in date order"})
			}
			if date > lastDate {
				lastDate = date
			}
			entries = append(entries, Entry{Date: date, Kind: kind, Title: title, Line: n, Fields: map[string]string{}})
			cur = len(entries) - 1
			continue
		}
		if strings.HasPrefix(line, "## ") {
			problems = append(problems, Problem{n, `entry heading does not read "## <YYYY-MM-DD> · <kind> · <one line>"`})
			cur = -1
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if cur < 0 {
			problems = append(problems, Problem{n, "text outside an entry - a file is its entries and nothing else"})
			continue
		}
		e := &entries[cur]
		if m := fieldLine.FindStringSubmatch(line); m != nil {
			name, value := m[1], m[2]
			if !has(Fields, name) {
				problems = append(problems, Problem{n, `field "` + name + `" is not in the vocabulary (` + strings.Join(Fields, ", ") + `)`})
			}
			if strings.TrimSpace(value) == "" {
				problems = append(problems, Problem{n, `field "` + name + `" is empty - a field with nothing behind it is omitted, never filled`})
			}
			if _, dup := e.Fields[name]; dup {
				problems = append(problems, Problem{n, `field "` + name + `" repeats within one entry`})
			}
			e.Fields[name] = strings.TrimSpace(value)
			e.Order = append(e.Order, name)
			e.last = name
			continue
		}
		if continuation.MatchString(line) && e.last != "" {
			e.Fields[e.last] = e.Fields[e.last] + " " + strings.TrimSpace(line)
			continue
		}
		problems = append(problems, Problem{n, `line is neither a "- **Field:** …" bullet nor an indented continuation of one`})
	}
	return entries, problems
}

// Status is "retired" when the last entry retired the element, else
// "live" (an empty file is an element whose history is not written yet).
func Status(entries []Entry) string {
	if len(entries) > 0 && entries[len(entries)-1].Kind == "retired" {
		return "retired"
	}
	return "live"
}

// EntryFaults are a parsed file's faults beyond the line grammar: the
// first entry is born, and every mechanism-bearing kind carries
// Mechanism.
func EntryFaults(entries []Entry) []Problem {
	var out []Problem
	for i, e := range entries {
		if i == 0 && e.Kind != "born" {
			out = append(out, Problem{e.Line, `the first entry is "` + e.Kind + `", and a file with entries opens with born`})
		}
		if _, ok := e.Fields["Mechanism"]; has(MechanismKinds, e.Kind) && !ok {
			out = append(out, Problem{e.Line, "a " + e.Kind + " entry carries no Mechanism"})
		}
	}
	return out
}

// Marker is the slug a rule's last line ends with.
func Marker(line string) (string, bool) {
	if m := markerAtEnd.FindStringSubmatch(line); m != nil {
		return m[1], true
	}
	return "", false
}

func numericMarker(line string) string {
	if m := numericAtEnd.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	return ""
}

func stripMarkers(line string) string {
	return numericAtEnd.ReplaceAllString(markerAtEnd.ReplaceAllString(line, ""), "")
}

var spaces = regexp.MustCompile(`\s+`)

// NormalizeLeadIn is a rule's bold lead-in as a trigger: emphasis off,
// whitespace collapsed, a trailing dash dropped.
func NormalizeLeadIn(s string) string {
	s = strings.NewReplacer("`", "", "*", "", "_", "").Replace(s)
	s = strings.TrimSpace(spaces.ReplaceAllString(s, " "))
	if r := []rune(s); len(r) > 0 && unicode.Is(unicode.Pd, r[len(r)-1]) {
		s = string(r[:len(r)-1])
	}
	return strings.TrimSpace(s)
}

var clauseCut = regexp.MustCompile(`\s[\p{Pd}:]\s|[.:;]\s|[.!?]$`)

// openingWords is a plain bullet's trigger: its first clause, or its
// first eight words.
func openingWords(s string) string {
	clean := NormalizeLeadIn(s)
	clause := clean
	if loc := clauseCut.FindStringIndex(clean); loc != nil {
		clause = clean[:loc[0]]
	}
	words := strings.Split(clause, " ")
	if len(words) > 8 {
		words = words[:8]
	}
	return strings.Join(words, " ")
}

// NormalizeRuleText is a rule's text as a decision reads it: the marker
// off, whitespace collapsed.
func NormalizeRuleText(block string) string {
	lines := strings.Split(block, "\n")
	for i, l := range lines {
		lines[i] = stripMarkers(l)
	}
	return strings.TrimSpace(spaces.ReplaceAllString(strings.Join(lines, " "), " "))
}

// Block is one top-level rule bullet of a prose file, 0-based lines:
// Start, End its last non-blank line, LastLine where its marker is or a
// writer puts one.
type Block struct {
	Start, End, LastLine int
	Slug, Numeric        string
	Trigger, Text        string
}

// RuleBlocks are the top-level rule bullets of a prose file: "- **lead-in**
// …" and every line belonging to it, closed by the next top-level bullet
// or a heading; with plainBullets, a bullet with no bold lead-in is a
// rule too. A fenced code block is opaque.
func RuleBlocks(text string, plainBullets bool) []Block {
	lines := strings.Split(text, "\n")
	var blocks []Block
	open := false
	start, end := 0, 0
	readAt := func(i int) (string, string) {
		if s, ok := Marker(lines[i]); ok {
			return s, ""
		}
		return "", numericMarker(lines[i])
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
		for i := start + 1; i <= e && strings.TrimSpace(lines[i]) != "" && !nestedItem.MatchString(lines[i]); i++ {
			para = i
		}
		slug, num := readAt(para)
		last := para
		if slug == "" && num == "" && e != para {
			if s2, n2 := readAt(e); s2 != "" || n2 != "" {
				slug, num, last = s2, n2, e
			}
		}
		body := make([]string, 0, e-start+1)
		for i := start; i <= e; i++ {
			if i == last {
				body = append(body, stripMarkers(lines[i]))
			} else {
				body = append(body, lines[i])
			}
		}
		joined := strings.Join(body, "\n")
		trigger := ""
		if ruleBullet.MatchString(lines[start]) {
			if m := leadRE.FindStringSubmatch(joined); m != nil {
				trigger = NormalizeLeadIn(m[1])
			} else {
				trigger = openingWords(strings.TrimPrefix(stripMarkers(lines[start]), "- "))
			}
		} else {
			trigger = openingWords(strings.TrimPrefix(stripMarkers(lines[start]), "- "))
		}
		blocks = append(blocks, Block{Start: start, End: e, LastLine: last, Slug: slug, Numeric: num, Trigger: trigger, Text: NormalizeRuleText(joined)})
		open = false
	}
	fenced := false
	for i, line := range lines {
		if fence.MatchString(line) {
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
		if ruleBullet.MatchString(line) || (plainBullets && topBullet.MatchString(line)) {
			closeBlock()
			open, start, end = true, i, i
			continue
		}
		if topBullet.MatchString(line) || heading.MatchString(line) {
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

// sortedKeys are m's keys, sorted.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// StripNumericMarker is line without the retired numeric marker at its
// end.
func StripNumericMarker(line string) string { return numericAtEnd.ReplaceAllString(line, "") }

// ValidDate reports whether a YYYY-MM-DD string is a date the Node
// engine's Date.parse reads.
func ValidDate(d string) bool {
	return regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(d) && validDate(d)
}

// Converted reports whether an entry's title is the one the references
// conversion writes.
func Converted(title string) bool { return convertedRE.MatchString(title) }
