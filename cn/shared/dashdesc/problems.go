package dashdesc

import (
	"regexp"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/jsjson"
)

// Problem is one reason a descriptor is not one the page can use, in the
// descriptor-usable check's words.
type Problem struct {
	What string `json:"what"`
	Fix  string `json:"fix"`
}

// SchemaPath is the schema a descriptor is written against, spelled as
// the shelf lays it out: in a member, the vendored pack's copy.
const SchemaPath = "packs/claudinite-dashboard/dashboard-descriptor.schema.json"

// Problems are the check's findings over one descriptor: the reader's
// fault, else the ids its views select and it does not declare, a list
// named as the fleet mini-card, and a mini-card quantity with no noun.
func Problems(text []byte, pack string) []Problem {
	d := Parse(text, pack)
	if d.Fault != "" {
		return []Problem{{
			What: "the dashboard reader rejects it: " + d.Fault,
			Fix:  "make it satisfy " + SchemaPath + ` — at minimum a "widgets" array whose entries each carry an "id" and a "kind" from ` + strings.Join(Kinds, ", "),
		}}
	}
	doc, _ := jsjson.Decode(text)
	var out []Problem

	var selected []jsjson.Value
	selected = append(selected, list(doc, "repo")...)
	if f, ok := fleetObj(doc); ok {
		selected = append(selected, list(f, "deployment")...)
	}
	member, hasMember := MemberID(doc)
	if hasMember {
		selected = append(selected, jsjson.Value{Kind: jsjson.String, Str: member})
	}
	var dangling []string
	seen := map[string]bool{}
	for _, id := range selected {
		if _, declared := d.Widget(id.Str); declared && id.Kind == jsjson.String {
			continue
		}
		// A Set keeps one of each primitive; an object or array is its own.
		key := ""
		switch id.Kind {
		case jsjson.Object, jsjson.Array:
		default:
			key = string(rune('0'+id.Kind)) + jsjson.Stringify(id)
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		shown := ""
		if id.Kind != jsjson.Null {
			shown = jsjson.StringOfValue(id)
		}
		dangling = append(dangling, shown)
	}
	if len(dangling) > 0 {
		out = append(out, Problem{
			What: "selects widget id(s) it does not declare: " + strings.Join(dangling, ", "),
			Fix:  `declare the widget in "widgets", or drop the id from the view that names it — the page silently renders nothing for an id it cannot resolve`,
		})
	}

	if hasMember {
		if w, ok := d.Widget(member); ok && w.Known && !contains(FleetKinds, *w.Kind) {
			out = append(out, Problem{
				What: `names "` + w.ID + `" as its fleet mini-card, but a ` + *w.Kind + " cannot be one line",
				Fix:  "point fleet.member at a widget whose kind is one of " + strings.Join(FleetKinds, ", ") + ", or drop it and contribute to the repo page only",
			})
		}
		if w, ok := d.Widget(member); ok && member != "" && w.Kind != nil && (*w.Kind == "stat" || *w.Kind == "window") && w.Noun == "" {
			out = append(out, Problem{
				What: `its fleet mini-card "` + member + `" is a ` + *w.Kind + ` with no "noun", so it would render as a bare number`,
				Fix:  `give the widget a short "noun" — what the quantity is of — so the phrase reads "18 stars" rather than "18"`,
			})
		}
	}
	return out
}

// Found is one problem with the descriptor it is about.
type Found struct {
	File string `json:"file"`
	Problem
}

// MountPrefix is the vendored mount, which a member cannot fix.
const MountPrefix = ".claudinite/shared/"

// scope is a descriptor a repo can fix: the shelf's packs/<id>/ and a
// member's own .claudinite/local/packs/<id>/.
var scope = regexp.MustCompile(`^(\.claudinite/local/)?packs/([^/]+)/dashboard\.json$`)

// InScope is the pack a path's descriptor belongs to, false when the path
// is no descriptor the check judges.
func InScope(path string) (string, bool) {
	if strings.HasPrefix(path, MountPrefix) {
		return "", false
	}
	m := scope.FindStringSubmatch(path)
	if m == nil {
		return "", false
	}
	return m[2], true
}

// Scan is the check over a repo's files: every descriptor in scope that
// reads, with each of its problems, in the order the files come.
func Scan(files []string, read func(string) (string, bool)) []Found {
	var out []Found
	for _, f := range files {
		pack, ok := InScope(f)
		if !ok {
			continue
		}
		text, ok := read(f)
		if !ok {
			continue
		}
		for _, p := range Problems([]byte(text), pack) {
			out = append(out, Found{File: f, Problem: p})
		}
	}
	return out
}
