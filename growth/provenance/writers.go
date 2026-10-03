package provenance

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	prov "github.com/missingbulb/ClaudiniteEngine/shared/provenance"
)

// Width is the byte width a writer wraps at.
const Width = 100

var stop = map[string]bool{}

func init() {
	for _, w := range strings.Fields("a an the of to in on for with and or that this its it is are by from at as into over under when whose which what your you own one two than then not no never every any some more most so if else where while about after before between through via per vs there here their them they he she we us our my me i how why who whom") {
		stop[w] = true
	}
}

var (
	slugStrip    = regexp.MustCompile("[`*_\"'’]")
	slugNonWord  = regexp.MustCompile(`[^a-z0-9]+`)
	slugLead     = regexp.MustCompile(`^[^a-z]+`)
	slugBad      = regexp.MustCompile(`[^a-z0-9-]`)
	slugDashEnds = regexp.MustCompile(`^-+|-+$`)
	trailingWS   = regexp.MustCompile(`\s+$`)
	leadingWS    = regexp.MustCompile(`^(\s*)`)
	metadataLine = regexp.MustCompile(`^metadata:\s*$`)
)

// ProposeSlug is two to four hyphenated words off a rule's lead-in,
// unique among taken: a proposal a maintainer refines before the change
// lands.
func ProposeSlug(trigger string, taken map[string]bool) string {
	s := slugNonWord.ReplaceAllString(slugStrip.ReplaceAllString(strings.ToLower(trigger), ""), " ")
	words := strings.Fields(strings.TrimSpace(s))
	var picked []string
	for _, w := range words {
		if !stop[w] && len(w) > 1 && len(picked) < 3 {
			picked = append(picked, w)
		}
	}
	if len(picked) < 2 {
		seen := map[string]bool{}
		var merged []string
		for _, w := range append(append([]string{}, picked...), words...) {
			if !seen[w] {
				seen[w] = true
				merged = append(merged, w)
			}
		}
		if len(merged) > 2 {
			merged = merged[:2]
		}
		picked = merged
	}
	for len(picked) < 2 {
		picked = append(picked, "rule")
	}
	base := slugLead.ReplaceAllString(strings.Join(picked, "-"), "")
	safe := base
	if !prov.SlugRE.MatchString(base) {
		trimmed := slugDashEnds.ReplaceAllString(slugBad.ReplaceAllString(base, ""), "")
		if trimmed == "" {
			trimmed = "rule"
		}
		safe = trimmed + "-rule"
	}
	slug := safe
	for n := 2; taken[slug]; n++ {
		slug = fmt.Sprintf("%s-%d", safe, n)
	}
	return slug
}

// markLine puts the marker on a rule's last line, or on a line of its own
// when the line would pass width bytes; a retired numeric marker there
// goes.
func markLine(lines []string, index int, slug string, width int) []string {
	base := trailingWS.ReplaceAllString(prov.StripNumericMarker(lines[index]), "")
	marked := base + " (" + slug + ")"
	if len(marked) <= width {
		lines[index] = marked
		return lines
	}
	indent := "  "
	if !strings.HasPrefix(base, "- ") {
		if m := leadingWS.FindStringSubmatch(base); m != nil && m[1] != "" {
			indent = m[1]
		}
	}
	lines[index] = base
	out := append([]string{}, lines[:index+1]...)
	out = append(out, indent+"("+slug+")")
	return append(out, lines[index+1:]...)
}

// WithBody puts `body: <shape>` under a SKILL.md's frontmatter metadata,
// creating the block where the file has none.
func WithBody(src, body string) string {
	if !strings.HasPrefix(src, "---") {
		return "---\nmetadata:\n  body: " + body + "\n---\n" + src
	}
	end := strings.Index(src[3:], "\n---")
	if end < 0 {
		return src
	}
	end += 3
	inner := ""
	if end > 4 {
		inner = src[4:end]
	}
	lines := strings.Split(inner, "\n")
	at := -1
	for i, l := range lines {
		if metadataLine.MatchString(l) {
			at = i
			break
		}
	}
	if at < 0 {
		lines = append(lines, "metadata:", "  body: "+body)
	} else {
		lines = append(lines[:at+1], append([]string{"  body: " + body}, lines[at+1:]...)...)
	}
	return "---\n" + strings.Join(lines, "\n") + src[end:]
}

// MarkPack brings one pack onto the convention: every skill declares a
// body, every unmarked RULES.md rule gets a proposed marker and its file,
// every carrier that names a file and has none an empty one. It is
// idempotent and never writes an entry; history is the backfill's. It
// returns the report lines.
func MarkPack(packDir string, io WriteIO) ([]string, error) {
	var report []string
	provDir := packDir + "/" + prov.Dir
	ensure := func(id, why string) error {
		f := provDir + "/" + prov.FileOfID(id)
		if io.Exists(f) {
			return nil
		}
		if err := io.Write(f, ""); err != nil {
			return err
		}
		report = append(report, f+": created for "+why)
		return nil
	}
	for _, s := range prov.PackCarriers(packDir, io).Skills {
		if !s.Present || s.Body != "" {
			continue
		}
		src, _ := io.Read(s.File)
		if err := io.Write(s.File, WithBody(src, s.Proposed)); err != nil {
			return nil, err
		}
		why := "numbered steps, or no bullets"
		if s.Proposed == "guidelines" {
			why = "bold-trigger bullets and no numbered steps"
		}
		report = append(report, fmt.Sprintf("%s: body: %s proposed (%s)", s.File, s.Proposed, why))
	}
	c := prov.PackCarriers(packDir, io)
	taken := map[string]bool{}
	for _, f := range prov.Files(packDir, io) {
		taken[f.ID] = true
	}
	prose := append(append([]prov.Rule{}, c.Rules...), c.Guidelines...)
	for _, r := range prose {
		if r.Slug != "" {
			taken[r.Slug] = true
		}
	}
	var order []string
	byFile := map[string][]prov.Rule{}
	for _, r := range c.Rules {
		if r.Slug != "" {
			continue
		}
		if _, ok := byFile[r.File]; !ok {
			order = append(order, r.File)
		}
		byFile[r.File] = append(byFile[r.File], r)
	}
	for _, file := range order {
		text, _ := io.Read(file)
		lines := strings.Split(text, "\n")
		unmarked := append([]prov.Rule{}, byFile[file]...)
		sort.SliceStable(unmarked, func(a, b int) bool { return unmarked[a].LastLine > unmarked[b].LastLine })
		for _, r := range unmarked {
			slug := ProposeSlug(r.Trigger, taken)
			taken[slug] = true
			lines = markLine(lines, r.LastLine-1, slug, Width)
			line := fmt.Sprintf("%s:%d: \"%s\" marked (%s)", file, r.Line, r.Trigger, slug)
			if r.Numeric != "" {
				line += " - numeric marker (" + r.Numeric + ") replaced"
			}
			report = append(report, line)
			if err := ensure(slug, `rule "`+r.Trigger+`"`); err != nil {
				return nil, err
			}
		}
		if err := io.Write(file, strings.Join(lines, "\n")); err != nil {
			return nil, err
		}
	}
	for _, r := range prose {
		if r.Slug != "" {
			if err := ensure(r.Slug, `rule "`+r.Trigger+`"`); err != nil {
				return nil, err
			}
		}
	}
	for _, s := range c.Skills {
		if s.Present {
			if err := ensure(s.Name, "skill "+s.Name); err != nil {
				return nil, err
			}
		}
	}
	for _, ch := range c.Checks {
		if err := ensure(prov.ElementID(ch.ID), "check "+ch.ID); err != nil {
			return nil, err
		}
	}
	for _, d := range c.Decls {
		if err := ensure(d.ID, "declared rule "+d.ID); err != nil {
			return nil, err
		}
	}
	for _, t := range c.Tasks {
		if err := ensure(t.ID, "task "+t.ID); err != nil {
			return nil, err
		}
	}
	if c.ManifestFile != "" {
		if err := ensure(prov.PackElement, "the manifest"); err != nil {
			return nil, err
		}
	}
	return report, nil
}

// Entry is one entry a writer renders: its fields in order.
type Entry struct {
	Date, Kind, Title string
	Fields            []Field
}

// Field is one named field of an entry.
type Field struct{ Name, Value string }

func (e Entry) field(name string) (string, bool) {
	for _, f := range e.Fields {
		if f.Name == name {
			return f.Value, true
		}
	}
	return "", false
}

func (e Entry) heading() string { return e.Date + " · " + e.Kind + " · " + e.Title }

var spaces = regexp.MustCompile(`\s+`)

// Render writes an entry in the file grammar: a field with nothing behind
// it is not written, and a long field wraps at Width bytes with an
// indented continuation.
func Render(e Entry) string {
	lines := []string{"## " + e.heading()}
	for _, f := range e.Fields {
		if strings.TrimSpace(f.Value) == "" {
			continue
		}
		lines = append(lines, wrap("- **"+f.Name+":** "+strings.TrimSpace(spaces.ReplaceAllString(f.Value, " ")), Width)...)
	}
	return strings.Join(lines, "\n") + "\n"
}

func wrap(text string, width int) []string {
	if len(text) <= width {
		return []string{text}
	}
	var out []string
	cur := ""
	for _, w := range strings.Split(text, " ") {
		next := w
		if cur != "" {
			next = cur + " " + w
		}
		if cur != "" && len(next) > width {
			out = append(out, cur)
			cur = "  " + w
		} else {
			cur = next
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func has(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// Problems are an entry's grammar faults on its own, before the file it
// would join is read.
func Problems(e Entry, kinds []string) []string {
	var out []string
	if !has(kinds, e.Kind) {
		out = append(out, fmt.Sprintf("kind %q is not in the vocabulary (%s)", e.Kind, strings.Join(kinds, ", ")))
	}
	if !prov.ValidDate(e.Date) {
		out = append(out, fmt.Sprintf("date %q is not a YYYY-MM-DD date", e.Date))
	}
	if strings.TrimSpace(e.Title) == "" {
		out = append(out, "an entry needs its one-line title")
	}
	for _, f := range e.Fields {
		if !has(prov.Fields, f.Name) {
			out = append(out, fmt.Sprintf("field %q is not in the vocabulary", f.Name))
		}
	}
	if m, _ := e.field("Mechanism"); has(prov.MechanismKinds, e.Kind) && strings.TrimSpace(m) == "" {
		out = append(out, "a "+e.Kind+" entry carries Mechanism: the carrier and its trigger, and why")
	}
	return out
}

// fromParsed is a parsed entry as a writer takes it, its fields in the
// file's own order.
func fromParsed(p prov.Entry) Entry {
	e := Entry{Date: p.Date, Kind: p.Kind, Title: p.Title}
	seen := map[string]bool{}
	for _, n := range p.Order {
		if seen[n] {
			continue
		}
		seen[n] = true
		e.Fields = append(e.Fields, Field{n, p.Fields[n]})
	}
	return e
}

// ParseEntryText reads one entry written in the file grammar.
func ParseEntryText(text string) (Entry, []string) {
	entries, problems := prov.ParseKinds(text, append(append([]string{}, prov.Kinds...), prov.DeclinedKind))
	if len(entries) != 1 {
		return Entry{}, []string{fmt.Sprintf("expected exactly one entry, found %d", len(entries))}
	}
	if len(problems) > 0 {
		var out []string
		for _, p := range problems {
			out = append(out, fmt.Sprintf("line %d: %s", p.Line, p.What))
		}
		return Entry{}, out
	}
	return fromParsed(entries[0]), nil
}

// Appended validates an entry against the file it joins and returns the
// text to write back. It is append-only: an entry dated before the
// file's last is refused, since a change recorded now cannot have
// happened before the change recorded last. firstKind is what an empty
// file's first entry must be ("" for anything).
func Appended(existing string, e Entry, kinds []string, firstKind string) (string, []string) {
	problems := Problems(e, kinds)
	entries, errs := prov.ParseKinds(existing, kinds)
	if len(errs) > 0 {
		problems = append(problems, fmt.Sprintf("the file does not parse (line %d: %s)", errs[0].Line, errs[0].What))
	}
	if n := len(entries); n > 0 {
		last := entries[n-1]
		if e.Date < last.Date {
			problems = append(problems, "the file's last entry is dated "+last.Date+"; an entry is appended in date order")
		}
		if last.Kind == "retired" {
			problems = append(problems, "the element is retired; a retired file is appended to by nothing")
		}
	} else if firstKind != "" && e.Kind != firstKind {
		problems = append(problems, "the first entry of a file is "+firstKind+", not "+e.Kind)
	}
	if len(problems) > 0 {
		return "", problems
	}
	body := Render(e)
	if strings.TrimSpace(existing) == "" {
		return body, nil
	}
	glue := "\n\n"
	switch {
	case strings.HasSuffix(existing, "\n\n"):
		glue = ""
	case strings.HasSuffix(existing, "\n"):
		glue = "\n"
	}
	return existing + glue + body, nil
}

// Backfilled is the backfill's write, the one place a file is rewritten
// rather than appended to: its entries are re-rendered and the batch
// merged in date order. A conversion's placeholder born dated on or after
// the batch's born is the date being corrected, so it is dropped and
// named in superseded.
func Backfilled(existing string, batch []Entry, kinds []string, firstKind string) (text string, superseded, problems []string) {
	for _, e := range batch {
		problems = append(problems, Problems(e, kinds)...)
	}
	held, errs := prov.ParseKinds(existing, kinds)
	if len(errs) > 0 {
		problems = append(problems, fmt.Sprintf("the file does not parse (line %d: %s)", errs[0].Line, errs[0].What))
	}
	if len(problems) > 0 {
		return "", nil, problems
	}
	born := ""
	for _, e := range batch {
		if e.Kind == "born" && (born == "" || e.Date < born) {
			born = e.Date
		}
	}
	var merged []Entry
	for _, p := range held {
		e := fromParsed(p)
		if born != "" && e.Kind == "born" && e.Date >= born && prov.Converted(e.Title) {
			superseded = append(superseded, e.heading())
			continue
		}
		merged = append(merged, e)
	}
	for _, e := range batch {
		dup := false
		for _, m := range merged {
			dup = dup || m.heading() == e.heading()
		}
		if !dup {
			merged = append(merged, e)
		}
	}
	sort.SliceStable(merged, func(a, b int) bool { return merged[a].Date < merged[b].Date })
	if len(merged) > 0 && firstKind != "" && merged[0].Kind != firstKind {
		problems = append(problems, "the file would open with "+merged[0].Kind+"; the first entry of a file is "+firstKind)
	}
	borns := 0
	for i, e := range merged {
		if e.Kind == "retired" && i != len(merged)-1 && !hasRetiredBefore(merged, i) {
			problems = append(problems, fmt.Sprintf("a retired entry is a file's last; %q would sit above %d more", e.heading(), len(merged)-1-i))
		}
		if e.Kind == "born" {
			borns++
		}
	}
	if borns > 1 {
		problems = append(problems, "the batch would leave two born entries in one file; an element is born once")
	}
	if len(problems) > 0 {
		return "", superseded, problems
	}
	parts := make([]string, len(merged))
	for i, e := range merged {
		parts[i] = Render(e)
	}
	return strings.Join(parts, "\n"), superseded, nil
}

func hasRetiredBefore(es []Entry, i int) bool {
	for _, e := range es[:i] {
		if e.Kind == "retired" {
			return true
		}
	}
	return false
}
