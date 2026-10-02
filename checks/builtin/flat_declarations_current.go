package builtin

import (
	"regexp"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/shared/flatdecl"
	"github.com/missingbulb/ClaudiniteEngine/shared/jsjson"
	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/shared/transcript"
)

// The sibling of the rules index check for the flat declarations: a task
// edited, added or removed since the last converge leaves a file that
// answers "what runs here" wrongly, and the dashboard reading a member
// reads only that file. Asked from the repo's tracked files: does each
// flat file name every source an active pack holds here, each with the
// content it holds now. A subset test: an entry beyond the active set is
// judged by its own source file instead.
var flatDeclarationsCurrent = declared.Builtin{
	ID:     "flat-declarations-current",
	Pack:   "claudinite-lifecycle",
	OnFail: "block",
	Tags:   []string{"world", "builtin", "claudinite-lifecycle"},
	Doc:    "packs/claudinite-lifecycle/README.md",
	Why:    "the dashboard and a session asking what runs here read the flat files alone - a stale one shows a task that is gone, hides one that runs, or shows a declaration its source no longer says",
}

func init() { register(&flatDeclarationsCurrent, runFlatDeclarationsCurrent) }

type flatSource struct {
	file, key string
	match     *regexp.Regexp
	name      func(pack string, m []string) string
}

var flatSources = []flatSource{
	{flatdecl.TasksFile, "tasks", regexp.MustCompile(`^tasks/([^/]+)/task\.(json|yaml|toml)$`), func(pack string, m []string) string { return pack + "/" + m[1] }},
	{flatdecl.DashboardFile, "dashboards", regexp.MustCompile(`^dashboard\.json$`), func(pack string, _ []string) string { return pack }},
}

// heldSources is a flat file's expected names, in the order first seen.
type heldSources struct {
	names []string
	path  map[string]string
}

func (h *heldSources) set(name, path string) {
	if _, ok := h.path[name]; !ok {
		h.names = append(h.names, name)
	}
	h.path[name] = path
}

func runFlatDeclarationsCurrent(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
	held := make([]*heldSources, len(flatSources))
	for i := range held {
		held[i] = &heldSources{path: map[string]string{}}
	}
	for _, p := range ctx.Config.Packs {
		if p.Kind == packset.Temp {
			continue
		}
		localRoot := ".claudinite/local/packs/" + p.ID + "/"
		roots := []string{".claudinite/shared/packs/" + p.ID + "/", "packs/" + p.ID + "/", localRoot}
		for _, path := range ctx.TrackedList() {
			root := ""
			for _, r := range roots {
				if strings.HasPrefix(path, r) {
					root = r
					break
				}
			}
			if root == "" {
				continue
			}
			name := p.ID
			if root == localRoot {
				name = "local/" + p.ID
			}
			for i, s := range flatSources {
				if m := s.match.FindStringSubmatch(path[len(root):]); m != nil {
					held[i].set(s.name(name, m), path)
				}
			}
		}
	}

	const regenerate = "run `cn tasks flat --write` and commit the result"
	var out []findings.Finding
	for i, s := range flatSources {
		if len(held[i].names) == 0 {
			continue
		}
		flag := func(what string) { out = append(out, flatDeclarationsCurrent.Finding(s.file, 0, what, regenerate)) }
		var flat jsjson.Value
		if text, ok := ctx.Read(s.file); ok {
			if doc, err := jsjson.Decode([]byte(text)); err == nil {
				flat, _ = doc.Prop(s.key)
			}
		}
		if flat.Kind != jsjson.Object && flat.Kind != jsjson.Array || !flat.Truthy() {
			flag(s.file + " is missing or unreadable")
			continue
		}
		for _, name := range held[i].names {
			if v, ok := flat.Prop(name); !ok || !v.Truthy() {
				flag(held[i].path[name] + " is not in " + s.file)
			}
		}
		for _, name := range flat.Keys {
			have := flat.Obj[name]
			path, isStr := have.Prop("path")
			if !isStr || path.Kind != jsjson.String || !ctx.Exists(path.Str) {
				shown := "undefined"
				if isStr {
					shown = textOf(path)
				}
				flag(s.file + ` names "` + name + `" at ` + shown + ", which is not a file here")
				continue
			}
			raw, _ := ctx.Read(path.Str)
			want := flatdecl.Entry(path.Str, []byte(raw))
			if !sameJSON(declarationOrText(have), declarationOrText(want)) {
				flag(s.file + " carries a copy of " + path.Str + " that no longer matches it")
			}
		}
	}
	return out
}

// declarationOrText is `entry.declaration ?? entry.text`.
func declarationOrText(entry jsjson.Value) *jsjson.Value {
	for _, k := range []string{"declaration", "text"} {
		if v, ok := entry.Prop(k); ok && v.Kind != jsjson.Null {
			return &v
		}
	}
	return nil
}

func sameJSON(a, b *jsjson.Value) bool {
	if a == nil || b == nil {
		return a == b
	}
	return jsjson.Stringify(canonical(*a)) == jsjson.Stringify(canonical(*b))
}

func canonical(v jsjson.Value) jsjson.Value {
	switch v.Kind {
	case jsjson.Array:
		out := v
		out.Arr = make([]jsjson.Value, len(v.Arr))
		for i, e := range v.Arr {
			out.Arr[i] = canonical(e)
		}
		return out
	case jsjson.Object:
		keys := append([]string{}, v.Keys...)
		sort.Strings(keys)
		vals := map[string]jsjson.Value{}
		for _, k := range keys {
			vals[k] = canonical(v.Obj[k])
		}
		return jsjson.Value{Kind: jsjson.Object, Keys: keys, Obj: vals}
	}
	return v
}

// textOf is a value as a template literal interpolates it.
func textOf(v jsjson.Value) string {
	if v.Kind == jsjson.String {
		return v.Str
	}
	if v.Kind == jsjson.Null {
		return "null"
	}
	return jsjson.Stringify(v)
}
