package provenance

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/gitcmd"
	prov "github.com/missingbulb/ClaudiniteEngine/cn/shared/provenance"
)

// ReferencesDoc is the retired per-pack log the conversion turns into
// entries.
const ReferencesDoc = "references.md"

// Reference is one entry of a references doc: its key, what the key
// names (rule, skill, check, task or unknown), the text with its
// continuation lines joined, and its 1-based line.
type Reference struct {
	Key, Kind, Target, N, Text string
	Line                       int
}

var (
	refLine      = regexp.MustCompile(`^\s*-\s+\*\*\(([^)]+)\)\*\*\s*(.*)$`)
	refCheck     = regexp.MustCompile(`^check:(.+)$`)
	refTask      = regexp.MustCompile(`^task:(.+)$`)
	refRule      = regexp.MustCompile(`^RULES-(\d+[a-z]?)$`)
	refSkill     = regexp.MustCompile(`^(.+)-(\d+[a-z]?)$`)
	refContinued = regexp.MustCompile(`^\s+\S`)
	refLink      = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	linkAbsolute = regexp.MustCompile(`^[a-z][a-z0-9+.-]*:`)
	reaffirmRE   = regexp.MustCompile(`^(Retire|Reaffirm|Revisit)\b`)
	numberList   = regexp.MustCompile(`\s*,\s*`)
	isoDate      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// ParseReferences reads a references doc.
func ParseReferences(text string) []Reference {
	var out []Reference
	cur := -1
	for i, line := range strings.Split(text, "\n") {
		if m := refLine.FindStringSubmatch(line); m != nil {
			r := Reference{Key: strings.TrimSpace(m[1]), Text: strings.TrimSpace(m[2]), Line: i + 1}
			if c := refCheck.FindStringSubmatch(r.Key); c != nil {
				r.Kind, r.Target = "check", c[1]
			} else if t := refTask.FindStringSubmatch(r.Key); t != nil {
				r.Kind, r.Target = "task", t[1]
			} else if n := refRule.FindStringSubmatch(r.Key); n != nil {
				r.Kind, r.N = "rule", n[1]
			} else if s := refSkill.FindStringSubmatch(r.Key); s != nil {
				r.Kind, r.Target, r.N = "skill", s[1], s[2]
			} else {
				r.Kind = "unknown"
			}
			out = append(out, r)
			cur = len(out) - 1
			continue
		}
		if cur >= 0 && refContinued.MatchString(line) {
			out[cur].Text += " " + strings.TrimSpace(line)
		} else {
			cur = -1
		}
	}
	return out
}

// sentenceStarts splits text at a run of spaces that follows a closing
// mark and comes before a capital.
func sentenceStarts(text string) []string {
	rs := []rune(text)
	var out []string
	start := 0
	for i := 1; i < len(rs); i++ {
		if !isSpace(rs[i]) || !strings.ContainsRune(".!?", rs[i-1]) {
			continue
		}
		j := i
		for j < len(rs) && isSpace(rs[j]) {
			j++
		}
		if j < len(rs) && rs[j] >= 'A' && rs[j] <= 'Z' {
			out = append(out, string(rs[start:i]))
			start = j
		}
		i = j - 1
	}
	return append(out, string(rs[start:]))
}

// splitReaffirmation splits off the sentences a references entry ends
// with that say when to retire or revisit it.
func splitReaffirmation(text string) (reason, retire string) {
	var keep, ret []string
	for _, s := range sentenceStarts(text) {
		if reaffirmRE.MatchString(s) {
			ret = append(ret, s)
		} else {
			keep = append(keep, s)
		}
	}
	return strings.TrimSpace(strings.Join(keep, " ")), strings.TrimSpace(strings.Join(ret, " "))
}

// relinked gives every relative link the "../" that keeps it resolving
// from the provenance folder, one below the doc it came from.
func relinked(text string) string {
	return refLink.ReplaceAllStringFunc(text, func(m string) string {
		target := m[2 : len(m)-1]
		if linkAbsolute.MatchString(target) || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "/") {
			return m
		}
		return "](../" + target + ")"
	})
}

// remover is a WriteIO that can delete a file.
type remover interface{ Remove(p string) error }

type rewrite struct {
	lastLine int
	slug     string
}

// ConvertReferences turns a pack's references doc into entries on the
// elements its keys name, rewrites each numeric marker to the slug its
// element takes (a workflow skill's markers leave the step) and deletes
// the doc. dateOf dates an entry, "" where it cannot, and the entry is
// then dated today with a title saying so. It returns the report; a
// pack with no doc reports nothing.
func ConvertReferences(packDir string, io WriteIO, dateOf func(key string) string, today string) []string {
	doc := packDir + "/" + ReferencesDoc
	text, ok := io.Read(doc)
	if !ok {
		return nil
	}
	var report []string
	refs := ParseReferences(text)
	provDir := packDir + "/" + prov.Dir
	c := prov.PackCarriers(packDir, io)
	taken := map[string]bool{}
	for _, f := range prov.Files(packDir, io) {
		taken[f.ID] = true
	}
	for _, r := range append(append([]prov.Rule{}, c.Rules...), c.Guidelines...) {
		if r.Slug != "" {
			taken[r.Slug] = true
		}
	}
	skills := map[string]*prov.Skill{}
	for i := range c.Skills {
		skills[c.Skills[i].Name] = &c.Skills[i]
	}
	var rewriteOrder []string
	rewrites := map[string][]rewrite{}
	plan := func(file string, lastLine int, slug string) {
		if _, ok := rewrites[file]; !ok {
			rewriteOrder = append(rewriteOrder, file)
		}
		rewrites[file] = append(rewrites[file], rewrite{lastLine, slug})
	}
	write := func(id string, key string, e Entry) {
		f := provDir + "/" + prov.FileOfID(id)
		existing, _ := io.Read(f)
		e.Kind = "strengthened"
		if entries, _ := prov.Parse(existing); len(entries) == 0 {
			e.Kind = "born"
		}
		out, problems := Appended(existing, e, prov.Kinds, "born")
		if len(problems) > 0 {
			report = append(report, f+": not written - "+strings.Join(problems, "; "))
			return
		}
		if err := io.Write(f, out); err != nil {
			report = append(report, f+": not written - "+err.Error())
			return
		}
		report = append(report, f+": "+e.Kind+" entry from references.md ("+key+")")
	}
	entryFor := func(r Reference, mechanism string) Entry {
		reason, retire := splitReaffirmation(relinked(r.Text))
		date := dateOf(r.Key)
		title := "converted from references.md (" + r.Key + ")"
		if date == "" {
			date = today
			title += ", dated by the conversion"
		}
		return Entry{Date: date, Title: title, Fields: []Field{{"Reason", reason}, {"Mechanism", mechanism}, {"Retire when", retire}}}
	}
	cites := func(blocks []prov.Rule, n string) []*prov.Rule {
		var out []*prov.Rule
		for i := range blocks {
			if blocks[i].Numeric != "" && has(numberList.Split(blocks[i].Numeric, -1), n) {
				out = append(out, &blocks[i])
			}
		}
		return out
	}
	slugFor := func(b *prov.Rule) string {
		if b.Slug != "" {
			return b.Slug
		}
		slug := ProposeSlug(b.Trigger, taken)
		taken[slug] = true
		b.Slug = slug
		plan(b.File, b.LastLine, slug)
		return slug
	}
	at := func(r Reference) string { return doc + ":" + strconv.Itoa(r.Line) + ": " + r.Key }
	for _, r := range refs {
		switch r.Kind {
		case "rule":
			targets := cites(c.Rules, r.N)
			if len(targets) == 0 {
				report = append(report, at(r)+" cited by no rule - dropped")
				continue
			}
			for _, b := range targets {
				write(slugFor(b), r.Key, entryFor(r, "prose"))
			}
		case "skill":
			s := skills[r.Target]
			if s == nil {
				report = append(report, at(r)+" names no skill - dropped")
				continue
			}
			targets := cites(s.Bullets, r.N)
			if s.Body == "guidelines" {
				if len(targets) == 0 {
					report = append(report, at(r)+" cited by no guideline - dropped")
					continue
				}
				for _, b := range targets {
					write(slugFor(b), r.Key, entryFor(r, "prose, a guideline of the "+s.Name+" skill"))
				}
				continue
			}
			for _, b := range targets {
				plan(b.File, b.LastLine, "")
			}
			e := entryFor(r, "a step of the "+s.Name+" skill, a workflow")
			if len(targets) > 0 {
				e.Title += `: "` + targets[0].Trigger + `"`
			}
			write(s.Name, r.Key, e)
		case "check":
			found := false
			for _, ch := range c.Checks {
				found = found || ch.ID == r.Target
			}
			if !found {
				report = append(report, at(r)+" names no check the pack carries - dropped")
				continue
			}
			write(prov.ElementID(r.Target), r.Key, entryFor(r, "a check"))
		case "task":
			found := false
			for _, t := range c.Tasks {
				found = found || t.ID == r.Target
			}
			if !found {
				report = append(report, at(r)+" names no task the pack carries - dropped")
				continue
			}
			write(r.Target, r.Key, entryFor(r, "a task"))
		default:
			report = append(report, at(r)+" is not a RULES-n, <skill>-n, check:<id> or task:<id> key - dropped")
		}
	}
	for _, file := range rewriteOrder {
		src, _ := io.Read(file)
		lines := strings.Split(src, "\n")
		todo := rewrites[file]
		sort.SliceStable(todo, func(a, b int) bool { return todo[a].lastLine > todo[b].lastLine })
		seen := map[int]bool{}
		for _, w := range todo {
			if seen[w.lastLine] {
				continue
			}
			seen[w.lastLine] = true
			if w.slug != "" {
				lines = markLine(lines, w.lastLine-1, w.slug, Width)
			} else {
				lines[w.lastLine-1] = prov.StripNumericMarker(lines[w.lastLine-1])
			}
		}
		if err := io.Write(file, strings.Join(lines, "\n")); err != nil {
			report = append(report, file+": not rewritten - "+err.Error())
		}
	}
	if rm, ok := io.(remover); ok {
		if err := rm.Remove(doc); err != nil {
			return append(report, doc+": converted; not deleted - "+err.Error())
		}
		return append(report, doc+": converted and deleted")
	}
	return append(report, doc+": converted; this io cannot delete a file, so the doc stays until a hand removes it")
}

// ReferenceDates dates a references entry by the commit that added its
// key to the doc, "" where git cannot say.
func ReferenceDates(git gitcmd.Repo, doc string) func(string) string {
	return func(key string) string {
		out := strings.TrimSpace(gitOut(git, "log", "--reverse", "--format=%as", "-S**("+key+")**", "--", doc))
		first, _, _ := strings.Cut(out, "\n")
		if isoDate.MatchString(first) {
			return first
		}
		return ""
	}
}

// Today is the conversion's own date, UTC.
func Today() string { return time.Now().UTC().Format("2006-01-02") }
