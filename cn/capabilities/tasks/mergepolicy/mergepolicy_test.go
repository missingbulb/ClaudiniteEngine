package mergepolicy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/packset"
)

func str(s string) *string { return &s }

func TestDeclaredByReadsEveryFormatAndRefusesCollisions(t *testing.T) {
	repo := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("p/merge-rules.yaml", "- name: usage-files\n  pathMatching: /^usage\\/.+\\.json$/\n  changeKinds: [added, modified]\n  editShape: any\n")
	write("q/merge-rules.json", `[{"name": "usage-files", "pathMatching": "/x/", "changeKinds": ["added"], "editShape": "any"}, {"name": "doc-changes", "pathMatching": "/x/", "changeKinds": ["added"], "editShape": "any"}]`)
	d := DeclaredBy([]packset.Pack{{ID: "p", Dir: filepath.Join(repo, "p")}, {ID: "q", Dir: filepath.Join(repo, "q")}, {ID: "none", Dir: filepath.Join(repo, "none")}})
	if _, ok := d.Rules["usage-files"]; !ok || len(d.Errors) != 2 {
		t.Fatalf("rules %v errors %v", d.Rules, d.Errors)
	}
	for _, e := range d.Errors {
		if !strings.HasPrefix(e, "q/merge-rules.json: ") || !strings.Contains(e, "already taken") {
			t.Errorf("error %q", e)
		}
	}

	added := []Entry{{File: "usage/a.json", After: str("{}")}}
	if v := Judge([]any{"usage-files"}, added, d); !v.Mergeable {
		t.Errorf("a declared rule does not cover its file: %+v", v)
	}
	// A policy source is never covered by a granular policy.
	src := []Entry{{File: "q/merge-rules.json", Before: str("[]"), After: str("[1]")}}
	if v := Judge("anything", src, d); !v.Mergeable {
		t.Errorf("anything refused a policy source: %+v", v)
	}
	if v := Judge([]any{"doc-changes", "usage-files"}, src, d); v.Mergeable {
		t.Errorf("a granular policy covered a policy source: %+v", v)
	}
	if v := Judge("nothing", added, d); v.Mergeable {
		t.Errorf("nothing merged: %+v", v)
	}
}

func TestTrailerReadsTheNewestExpression(t *testing.T) {
	m := TrailerRe.FindStringSubmatch("land it\n\nClaudinite-Automerge-Policy: doc-changes  \n")
	if m == nil || m[1] != "doc-changes" {
		t.Errorf("%q", m)
	}
}

// The retired claudinite-tasks pack's copies of the usage rules give way to
// the built-ins; another pack declaring the same name still collides.
func TestTheRetiredPacksUsageRulesGiveWay(t *testing.T) {
	spec := []any{map[string]any{"name": "rolling-usage-files", "pathMatching": "/x/", "changeKinds": []any{"added"}, "editShape": "any"}}
	d := Compile([]PackRules{{ID: "claudinite-tasks", File: "merge-rules.json", Specs: spec}}) // @real-entity the retired pack the tolerance names
	if len(d.Errors) != 0 || len(d.Rules) != 0 {
		t.Errorf("retired pack: rules %v errors %v", d.Rules, d.Errors)
	}
	d = Compile([]PackRules{{ID: "acme-pack", File: "merge-rules.json", Specs: spec}})
	if len(d.Errors) != 1 || !strings.Contains(d.Errors[0], "already taken") {
		t.Errorf("another pack: errors %v", d.Errors)
	}
}
