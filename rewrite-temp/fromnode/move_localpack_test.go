package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/rulesindex"
	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/verify"
)

// A local pack whose pack.json still carries the Node manifest's rule
// lists and the scaffold's numeric version, whose task names the Node tasks
// pack's schema, and whose declared check spells
// on_fail as severity, moves with the member: the move drops the lists and
// the version and renames the severity, so the
// pack loads, its prose reaches the rules index, and verify finds nothing.
func TestMoveRewritesALocalPacksNodeShapes(t *testing.T) {
	repo := nodeMember(t)
	manifest := filepath.Join(repo, ".claudinite/local/packs/mine/pack.json")
	if err := os.WriteFile(manifest, []byte(`{"version": 1, "worldRules": [], "ruleRoutingGuidance": {"belongs": "b"}, "workRules": []}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	decls := filepath.Join(repo, ".claudinite/local/packs/mine/declared-checks.json")
	if err := os.WriteFile(decls, []byte(`[{"id": "mine-x", "severity": "advisory", "scanFiles": "a", "forbidLinesMatching": "/x/", "failureMessage": "m"}]`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := filepath.Join(repo, ".claudinite/local/packs/mine/tasks/t/task.json")
	write(t, repo, ".claudinite/local/packs/mine/tasks/t/task.json", `{"$schema": "../../../../../shared/packs/claudinite-tasks/task.schema.json", "id": "t"}`+"\n")
	in, out := input(t, repo)
	if err := move(in); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got, _ := os.ReadFile(task); string(got) != "{\n  \"id\": \"t\"\n}\n" {
		t.Errorf("task.json:\n%s", got)
	}
	for _, want := range []string{`own pack: .claudinite/local/packs/mine/tasks/t/task.json: dropped "$schema"`, `own pack: .claudinite/local/packs/mine/pack.json: dropped "worldRules"`, `own pack: .claudinite/local/packs/mine/pack.json: dropped "version" 1`, `own pack: .claudinite/local/packs/mine/declared-checks.json: check mine-x: severity "advisory" became on_fail "advise"`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the report lacks %q:\n%s", want, out)
		}
	}
	if got, _ := os.ReadFile(manifest); string(got) != "{\n  \"ruleRoutingGuidance\": {\n    \"belongs\": \"b\"\n  }\n}\n" {
		t.Errorf("pack.json:\n%s", got)
	}
	if got, _ := os.ReadFile(decls); !strings.Contains(string(got), `"on_fail": "advise"`) || strings.Contains(string(got), "severity") {
		t.Errorf("declared-checks.json:\n%s", got)
	}
	index, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(rulesindex.File)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), "@../local/packs/mine/RULES.md\n") {
		t.Errorf("the rules index dropped local/mine:\n%s\n%s", index, out)
	}
	for _, f := range verify.Verify(verify.Input{Repo: repo, Launcher: []byte(launcherBody)}) {
		t.Errorf("verify: %s", f)
	}
}

// A local pack whose only manifest is pack.mjs moves with the member: the
// move writes pack.json from its default export, removes pack.mjs, and
// moves each coded rule it listed under worldRules/, where leftovers names
// it for porting.
func TestMoveWritesPackJSONFromAModuleManifest(t *testing.T) {
	repo := nodeMember(t)
	pack := ".claudinite/local/packs/mine/"
	if err := os.Remove(filepath.Join(repo, pack+"pack.json")); err != nil {
		t.Fatal(err)
	}
	write(t, repo, pack+"rule.mjs", "export default { id: 'mine/rule', check: () => [] };\n")
	write(t, repo, pack+"pack.mjs", "import rule from './rule.mjs';\n"+
		"export default {\n  id: 'mine',\n  ruleRoutingGuidance: { belongs: 'b' + 'c' },\n  detect: null,\n  worldRules: [rule, { id: 'inline' }],\n};\n")
	in, out := input(t, repo)
	if err := move(in); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got, _ := os.ReadFile(filepath.Join(repo, pack+"pack.json")); string(got) != "{\n  \"id\": \"mine\",\n  \"ruleRoutingGuidance\": {\n    \"belongs\": \"bc\"\n  }\n}\n" {
		t.Errorf("pack.json:\n%s", got)
	}
	for _, gone := range []string{"pack.mjs", "rule.mjs"} {
		if _, err := os.Stat(filepath.Join(repo, pack+gone)); err == nil {
			t.Errorf("%s is still there", gone)
		}
	}
	for _, want := range []string{
		"own pack: " + pack + "pack.json: written from pack.mjs",
		"own pack: " + pack + "pack.json: 1 coded rule(s) pack.mjs listed came from no module it imports",
		"own pack: " + pack + "worldRules/rule.mjs: moved from rule.mjs",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the report lacks %q:\n%s", want, out)
		}
	}
	found := false
	for _, f := range leftovers(repo) {
		found = found || f.Path == pack+"worldRules/rule.mjs"
	}
	if !found {
		t.Errorf("leftovers does not name the moved rule: %v", leftovers(repo))
	}
}

// A declared local pack cn would not load stops the move before it writes
// anything, naming the pack and why, rather than leaving it out of the
// rules index.
func TestMoveRefusesALocalPackItCannotLoad(t *testing.T) {
	for name, edit := range map[string]func(string){
		"unknown key": func(r string) {
			_ = os.WriteFile(filepath.Join(r, ".claudinite/local/packs/mine/pack.json"), []byte(`{"bogus": true}`), 0o644)
		},
		"module manifest": func(r string) {
			_ = os.Remove(filepath.Join(r, ".claudinite/local/packs/mine/pack.json"))
			_ = os.WriteFile(filepath.Join(r, ".claudinite/local/packs/mine/pack.mjs"), []byte("export default { bogus: true };\n"), 0o644)
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo := nodeMember(t)
			edit(repo)
			before := treeHash(t, repo)
			in, out := input(t, repo)
			err := move(in)
			if err == nil {
				t.Fatalf("moved:\n%s", out)
			}
			if !strings.Contains(err.Error(), "local/mine") {
				t.Errorf("the refusal names no pack: %v", err)
			}
			if after := treeHash(t, repo); after != before {
				t.Errorf("the refusal wrote:\n%s\nbefore:\n%s", after, before)
			}
		})
	}
}
