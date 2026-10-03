package dashdesc

import (
	"reflect"
	"strings"
	"testing"
)

func whats(ps []Problem) []string {
	out := []string{}
	for _, p := range ps {
		out = append(out, p.What)
	}
	return out
}

// Each of the check's four findings, and a descriptor clean of all of
// them; the parity face holds the sentences to the Node rule.
func TestProblems(t *testing.T) {
	for name, c := range map[string]struct{ text, want string }{
		"usable":   {`{"widgets": [{"id": "s", "kind": "stat", "noun": "stars"}], "repo": ["s"], "fleet": {"member": "s"}}`, ""},
		"rejected": {`{"widgets": []}`, "the dashboard reader rejects it: its dashboard.json declares no usable widget"},
		"dangling": {`{"widgets": [{"id": "s", "kind": "event"}], "repo": ["s", "x", "x"], "fleet": {"deployment": ["y"]}}`, "selects widget id(s) it does not declare: x, y"},
		"list":     {`{"widgets": [{"id": "l", "kind": "list"}], "fleet": {"member": "l"}}`, `names "l" as its fleet mini-card, but a list cannot be one line`},
		"noun":     {`{"widgets": [{"id": "w", "kind": "window"}], "fleet": {"member": "w"}}`, `its fleet mini-card "w" is a window with no "noun", so it would render as a bare number`},
		"unknown":  {`{"widgets": [{"id": "h", "kind": "heatmap"}], "fleet": {"member": "h"}}`, ""},
	} {
		got := strings.Join(whats(Problems([]byte(c.text), "acme-pack")), "|")
		if got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

func TestScanScope(t *testing.T) {
	files := map[string]string{
		"packs/a/dashboard.json":                          "{",
		".claudinite/local/packs/b/dashboard.json":        "{",
		".claudinite/shared/packs/c/dashboard.json":       "{",
		"packs/a/sub/dashboard.json":                      "{",
		"vendor/.claudinite/local/packs/d/dashboard.json": "{",
	}
	var paths []string
	for p := range files {
		paths = append(paths, p)
	}
	var got []string
	for _, f := range Scan(paths, func(p string) (string, bool) { t, ok := files[p]; return t, ok }) {
		got = append(got, f.File)
	}
	for _, want := range []string{"packs/a/dashboard.json", ".claudinite/local/packs/b/dashboard.json"} {
		found := false
		for _, g := range got {
			found = found || g == want
		}
		if !found {
			t.Errorf("%s is not judged: %v", want, got)
		}
	}
	if len(got) != 2 {
		t.Errorf("judged %v, want the shelf's and the local pack's alone", got)
	}
}

func TestParseClipsAndDrops(t *testing.T) {
	d := Parse([]byte(`{"widgets": [{"id": "a", "kind": "stat", "label": "  `+strings.Repeat("x", 40)+`", "glyph": "★★"}, {"id": "a", "kind": "event"}], "repo": ["a","a","a","a","a","a","a"]}`), "p")
	if d.Fault != "" || len(d.Widgets) != 1 || len([]rune(d.Widgets[0].Label)) != MaxLabel || d.Widgets[0].Glyph != nil {
		t.Errorf("%+v", d)
	}
	if !reflect.DeepEqual(d.Repo, []string{"a", "a", "a", "a", "a", "a"}) {
		t.Errorf("repo %v: the clip keeps six, duplicates and all", d.Repo)
	}
}
