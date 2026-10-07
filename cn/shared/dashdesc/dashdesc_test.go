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

// Each of the check's findings, and a descriptor clean of them; a fleet
// block is the pack's own business and selects nothing here.
func TestProblems(t *testing.T) {
	for name, c := range map[string]struct{ text, want string }{
		"usable":   {`{"widgets": [{"id": "s", "kind": "stat", "noun": "stars"}], "repo": ["s"], "fleet": {"member": "zz"}}`, ""},
		"rejected": {`{"widgets": []}`, "the dashboard reader rejects it: its dashboard.json declares no usable widget"},
		"dangling": {`{"widgets": [{"id": "s", "kind": "event"}], "repo": ["s", "x", "x", "y"]}`, "selects widget id(s) it does not declare: x, y"},
		"unknown":  {`{"widgets": [{"id": "h", "kind": "heatmap"}], "repo": ["h"]}`, ""},
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
