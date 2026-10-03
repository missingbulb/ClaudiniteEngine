package provenance

import (
	"regexp"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/growth/capture"
	prov "github.com/missingbulb/ClaudiniteEngine/shared/provenance"
)

var (
	entryFence    = regexp.MustCompile("^```entry (\\S+)\\s*$")
	defaultsFence = regexp.MustCompile("^```entry-defaults\\s*$")
	headingDate   = regexp.MustCompile(`^## (\d{4}-\d{2}-\d{2})`)
)

// declinedElement is how a brief's fence names the declined log.
const declinedElement = "_declined"

// Applied is what an apply wrote, or the problems that stopped it.
type Applied struct {
	Problems, Written, Skipped, Superseded, Created []string
}

type fence struct {
	element, text string
	defaults      []Field
	i             int
	date          string
}

// withDefaults is an entry's fields over the defaults fence's, in the
// convention's field order.
func withDefaults(defaults, fields []Field) []Field {
	var out []Field
	at := map[string]int{}
	for _, f := range append(append([]Field{}, defaults...), fields...) {
		if i, ok := at[f.Name]; ok {
			out[i].Value = f.Value
			continue
		}
		at[f.Name] = len(out)
		out = append(out, f)
	}
	index := func(n string) int {
		for i, f := range prov.Fields {
			if f == n {
				return i
			}
		}
		return -1
	}
	sort.SliceStable(out, func(a, b int) bool { return index(out[a].Name) < index(out[b].Name) })
	return out
}

// Apply applies an edited brief: every entry fence in it, validated as
// one batch against the files it would join and appended each once - a
// heading already in its file is skipped, so a second apply writes
// nothing - and none written while any one is refused. backfill is the
// one lane licensed to write a file's past: a file is re-rendered in
// date order, and one missing is created where the batch opens with
// born.
func Apply(io WriteIO, pack, text string, backfill bool) Applied {
	var fences []fence
	var problems []string
	var defaults []Field
	inFence, isDefaults := false, false
	element := ""
	var body []string
	for _, line := range strings.Split(text, "\n") {
		if !inFence {
			if defaultsFence.MatchString(line) {
				inFence, isDefaults, body = true, true, nil
				continue
			}
			if m := entryFence.FindStringSubmatch(line); m != nil {
				inFence, isDefaults, element, body = true, false, m[1], nil
			}
			continue
		}
		if !strings.HasPrefix(line, "```") {
			body = append(body, line)
			continue
		}
		if isDefaults {
			e, probs := ParseEntryText("## 2000-01-01 · born · defaults\n" + strings.Join(body, "\n") + "\n")
			if len(probs) > 0 {
				for _, p := range probs {
					problems = append(problems, "a defaults fence: "+p)
				}
			} else {
				defaults = e.Fields
			}
		} else {
			f := fence{element: element, text: strings.Join(body, "\n") + "\n", defaults: defaults, i: len(fences)}
			if m := headingDate.FindStringSubmatch(f.text); m != nil {
				f.date = m[1]
			}
			fences = append(fences, f)
		}
		inFence = false
	}
	sort.SliceStable(fences, func(a, b int) bool {
		x, y := fences[a], fences[b]
		if x.element != y.element {
			return x.element < y.element
		}
		if x.date != y.date {
			return x.date < y.date
		}
		return x.i < y.i
	})
	type batch struct {
		element  string
		declined bool
		entries  []Entry
	}
	var order []string
	byFile := map[string]*batch{}
	for _, f := range fences {
		declined := f.element == declinedElement
		file := pack + "/" + prov.Dir + "/" + prov.FileOfID(f.element)
		if declined {
			file = pack + "/" + prov.Dir + "/" + prov.DeclinedFile
		}
		e, probs := ParseEntryText(f.text)
		if len(probs) > 0 {
			for _, p := range probs {
				problems = append(problems, f.element+": "+p)
			}
			continue
		}
		e.Fields = withDefaults(f.defaults, e.Fields)
		if r := Render(e); capture.Scrub(r, nil) != r {
			problems = append(problems, f.element+": the entry carries what reads as a secret")
			continue
		}
		if byFile[file] == nil {
			order = append(order, file)
			byFile[file] = &batch{element: f.element, declined: declined}
		}
		byFile[file].entries = append(byFile[file].entries, e)
	}
	var a Applied
	pending := map[string]string{}
	var pendingOrder []string
	for _, file := range order {
		f := byFile[file]
		kinds, firstKind := prov.Kinds, "born"
		if f.declined {
			kinds, firstKind = []string{prov.DeclinedKind}, ""
		}
		exists := io.Exists(file)
		opens := backfill && f.entries[0].Kind == "born"
		if !exists && !f.declined && !opens {
			what := f.element + ": " + file + " does not exist"
			if backfill {
				what += ", and the batch does not open with born"
			}
			problems = append(problems, what)
			continue
		}
		if !exists && !f.declined {
			a.Created = append(a.Created, file)
		}
		existing, _ := io.Read(file)
		if backfill && !f.declined {
			out, superseded, probs := Backfilled(existing, f.entries, kinds, firstKind)
			for _, s := range superseded {
				a.Superseded = append(a.Superseded, f.element+": "+s)
			}
			if len(probs) > 0 {
				for _, p := range probs {
					problems = append(problems, f.element+": "+p)
				}
				continue
			}
			pending[file] = out
			pendingOrder = append(pendingOrder, file)
			continue
		}
		out := existing
		for _, e := range f.entries {
			heading := strings.SplitN(Render(e), "\n", 2)[0]
			if has(strings.Split(out, "\n"), heading) {
				a.Skipped = append(a.Skipped, f.element)
				continue
			}
			next, probs := Appended(out, e, kinds, firstKind)
			if len(probs) > 0 {
				for _, p := range probs {
					problems = append(problems, f.element+": "+p)
				}
				break
			}
			out = next
		}
		if out != existing {
			pending[file] = out
			pendingOrder = append(pendingOrder, file)
		}
	}
	if len(problems) > 0 {
		a.Problems = problems
		return a
	}
	for _, file := range pendingOrder {
		if err := io.Write(file, pending[file]); err != nil {
			a.Problems = append(a.Problems, err.Error())
			return a
		}
		a.Written = append(a.Written, file)
	}
	return a
}
