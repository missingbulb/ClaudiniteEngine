package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
)

const shapes = "testdata/shapes"

func launcherBytes(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../launcher/launch")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// shippedHashes reads launcher/shipped.sha256 as cn does, so a fixture holding
// a launcher an earlier release shipped is accepted exactly when cn accepts it.
func shippedHashes(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("../../launcher/shipped.sha256")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range strings.Split(string(raw), "\n") {
		if f := strings.Fields(l); len(f) > 0 && !strings.HasPrefix(f[0], "#") {
			out = append(out, f[0])
		}
	}
	return out
}

func copyTree(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), raw, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func ids(fs []findings.Finding, class findings.Class) []string {
	var out []string
	for _, f := range fs {
		if f.Class == class {
			out = append(out, f.ID)
		}
	}
	sort.Strings(out)
	return out
}

// newShape is a member in the shape this release writes: no finding.
func newShape(t *testing.T) string {
	t.Helper()
	dir := copyTree(t, filepath.Join(shapes, "v1-yaml"))
	_ = os.Remove(filepath.Join(dir, ".gitignore"))
	write(t, dir, ".claudinite/.gitignore", "bin/\n")
	write(t, dir, ".claudinite/launch", string(launcherBytes(t)))
	write(t, dir, ".github/workflows/claudinite-ci.yml", "name: claudinite-ci\n")
	write(t, dir, ".github/workflows/claudinite-scheduler.yml", "name: claudinite-scheduler\n")
	write(t, dir, ".github/workflows/claudinite-executor.yml", "name: claudinite-executor\n")
	return dir
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, dir string) []findings.Finding {
	return Verify(Input{Repo: dir, Launcher: launcherBytes(t), Shipped: shippedHashes(t)})
}

func TestNewShapeHasNoFinding(t *testing.T) {
	if fs := run(t, newShape(t)); len(fs) != 0 {
		t.Errorf("findings on the new shape: %v", fs)
	}
}

func TestEmptyRepoBreaksOnTheSettingsFile(t *testing.T) {
	fs := run(t, t.TempDir())
	if len(fs) != 1 || !findings.AnyBreak(fs) {
		t.Fatalf("want exactly the settings-file break: %v", fs)
	}
	var found bool
	for _, f := range fs {
		if f.ID == "settings-file" && f.Class == findings.Break && strings.Contains(f.Sentence, "settings.yaml") {
			found = true
		}
	}
	if !found {
		t.Errorf("no settings-file break: %v", fs)
	}
}

func TestRules(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, dir string)
		breaks []string
		deps   []string
	}{
		{"two settings files", func(t *testing.T, d string) { write(t, d, ".claudinite/settings.json", "{}") }, []string{"descriptor-duplicate"}, nil},
		{"bad version", func(t *testing.T, d string) {
			raw, _ := os.ReadFile(filepath.Join(d, ".claudinite/settings.yaml"))
			write(t, d, ".claudinite/settings.yaml", strings.Replace(string(raw), `"1.60930.1"`, `"60930.1"`, 1))
		}, []string{"engine-pin"}, nil},
		{"bad package", func(t *testing.T, d string) {
			raw, _ := os.ReadFile(filepath.Join(d, ".claudinite/settings.yaml"))
			write(t, d, ".claudinite/settings.yaml", strings.Replace(string(raw), `"@claudinite/cli-rc"`, `"@claudinite/cli-beta"`, 1))
		}, []string{"engine-pin"}, nil},
		{"edited launcher", func(t *testing.T, d string) { write(t, d, ".claudinite/launch", string(launcherBytes(t))+"# edited\n") }, []string{"launcher"}, nil},
		{"no launcher", func(t *testing.T, d string) { _ = os.Remove(filepath.Join(d, ".claudinite/launch")) }, []string{"launcher"}, nil},
		{"no SessionStart", func(t *testing.T, d string) {
			write(t, d, ".claude/settings.json", `{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": ".claudinite/bin/cn hook stop"}]}]}}`)
		}, []string{"hooks"}, []string{"hooks", "hooks", "hooks", "hooks"}},
		{"no Claude settings", func(t *testing.T, d string) { _ = os.Remove(filepath.Join(d, ".claude/settings.json")) }, []string{"hooks"}, []string{"hooks", "hooks", "hooks", "hooks", "hooks"}},
		{"one guard missing", func(t *testing.T, d string) {
			raw, _ := os.ReadFile(filepath.Join(d, ".claude/settings.json"))
			write(t, d, ".claude/settings.json", strings.Replace(string(raw), "cn hook pre-tool-use", "cn hook something-else", 1))
		}, nil, []string{"hooks"}},
		{"no workflows", func(t *testing.T, d string) { _ = os.RemoveAll(filepath.Join(d, ".github")) }, nil, []string{"member-workflows", "member-workflows", "member-workflows"}},
		{"the superseded update workflow beside the queue", func(t *testing.T, d string) {
			write(t, d, ".github/workflows/claudinite-update.yml", "name: claudinite-update\n")
		}, nil, []string{"member-workflows"}},
		{"the update workflow with no queue", func(t *testing.T, d string) {
			write(t, d, ".github/workflows/claudinite-update.yml", "name: claudinite-update\n")
			_ = os.Remove(filepath.Join(d, ".github/workflows/claudinite-scheduler.yml"))
			_ = os.Remove(filepath.Join(d, ".github/workflows/claudinite-executor.yml"))
		}, nil, []string{"member-workflows", "member-workflows"}},
		{"a path-filtered ci workflow", func(t *testing.T, d string) {
			write(t, d, ".github/workflows/claudinite-ci.yml", "name: claudinite-ci\non:\n  pull_request:\n    paths: [\"src/**\"]\n  workflow_dispatch:\njobs:\n  check:\n    steps:\n      - run: x\n        paths: not-a-trigger\n")
		}, nil, []string{"member-workflows"}},
		{"a job-level paths key filters nothing", func(t *testing.T, d string) {
			write(t, d, ".github/workflows/claudinite-ci.yml", "name: claudinite-ci\non:\n  pull_request:\n  push:\n    paths: [\"src/**\"]\njobs:\n  check:\n    paths: x\n")
		}, nil, nil},
		{"no executor", func(t *testing.T, d string) {
			_ = os.Remove(filepath.Join(d, ".github/workflows/claudinite-executor.yml"))
		}, []string{"member-workflows"}, nil},
		{"no scheduler", func(t *testing.T, d string) {
			_ = os.Remove(filepath.Join(d, ".github/workflows/claudinite-scheduler.yml"))
		}, nil, []string{"member-workflows"}},
		{"old ignore shape", func(t *testing.T, d string) {
			_ = os.Remove(filepath.Join(d, ".claudinite/.gitignore"))
			write(t, d, ".gitignore", "node_modules/\n.claudinite/bin/\n")
		}, nil, []string{"bin-ignore"}},
		{"bin not ignored", func(t *testing.T, d string) { _ = os.Remove(filepath.Join(d, ".claudinite/.gitignore")) }, []string{"bin-ignore"}, nil},
		{"declared pack held", func(t *testing.T, d string) {
			declare(t, d, "acme-pack", `{"version": "1.0", "minEngineVersion": "1.60930.1"}`)
		}, nil, nil},
		{"declared pack missing", func(t *testing.T, d string) {
			declare(t, d, "acme-pack", "")
		}, []string{"pack-declared"}, nil},
		{"vendored pack undeclared", func(t *testing.T, d string) {
			write(t, d, ".claudinite/shared/packs/acme-old/pack.json", `{"version": "1.0", "minEngineVersion": "1.60930.1"}`)
		}, nil, []string{"pack-declared"}},
		{"pack needs a newer engine", func(t *testing.T, d string) {
			declare(t, d, "acme-pack", `{"version": "1.0", "minEngineVersion": "1.60930.2"}`)
		}, []string{"pack-min-engine"}, nil},
		{"pack with two-part minimum", func(t *testing.T, d string) {
			declare(t, d, "acme-pack", `{"version": "1.0", "minEngineVersion": "60928.1"}`)
		}, nil, []string{"pack-min-engine"}},
		{"pack with malformed minimum", func(t *testing.T, d string) {
			declare(t, d, "acme-pack", `{"version": "1.0", "minEngineVersion": "soon"}`)
		}, []string{"pack-min-engine"}, nil},
		{"pack.yaml", func(t *testing.T, d string) {
			declare(t, d, "acme-pack", `{"version": "1.0", "minEngineVersion": "1.60930.1"}`)
			write(t, d, ".claudinite/shared/packs/acme-pack/pack.yaml", "version: 1\n")
		}, []string{"descriptor-duplicate"}, nil},
		{"pack manifest with a stray key", func(t *testing.T, d string) {
			declare(t, d, "acme-pack", `{"version": "1.0", "minEngineVersion": "1.60930.1", "colour": "red"}`)
		}, []string{"descriptor-format"}, nil},
		{"pack manifest in yaml", func(t *testing.T, d string) {
			declare(t, d, "acme-pack", "")
			write(t, d, ".claudinite/shared/packs/acme-pack/pack.yaml", "version: \"1.0\"\nminEngineVersion: \"1.60930.1\"\n")
		}, nil, nil},
		{"local pack declared, no tree", func(t *testing.T, d string) {
			declare(t, d, "local/mine", "")
		}, []string{"pack-declared"}, nil},
		{"local pack held", func(t *testing.T, d string) {
			declare(t, d, "local/mine", "")
			write(t, d, ".claudinite/local/packs/mine/pack.json", "{}")
		}, nil, nil},
		{"override outside the three", func(t *testing.T, d string) {
			appendSettings(t, d, "checks:\n  rules:\n    acme-check: loud\n")
		}, []string{"settings-checks"}, nil},
		{"acceptance without a reason", func(t *testing.T, d string) {
			appendSettings(t, d, "checks:\n  accept:\n    - rule: acme-check\n      path: docs/\n")
		}, []string{"settings-checks"}, nil},
		{"two sources disagree", func(t *testing.T, d string) {
			appendSettings(t, d, "packs:\n  declared:\n    - id: acme-pack\n      rules: {acme-check: advise}\ncheckss: 1\n")
			raw, _ := os.ReadFile(filepath.Join(d, ".claudinite/settings.yaml"))
			write(t, d, ".claudinite/settings.yaml", strings.Replace(string(raw), "checkss: 1\n", "checks:\n  rules: {acme-check: block}\n", 1))
			write(t, d, ".claudinite/shared/packs/acme-pack/pack.json", `{"version": "1.0", "minEngineVersion": "1.60930.1"}`)
		}, []string{"settings-checks"}, nil},
		{"good overrides", func(t *testing.T, d string) {
			appendSettings(t, d, "checks:\n  rules:\n    acme-check: \"off\"\n  accept:\n    - rule: acme-other\n      reason: \"the fixture is meant to\"\n")
		}, nil, nil},
		{"a top-level sharedConstants", func(t *testing.T, d string) {
			appendSettings(t, d, "sharedConstants:\n  - name: port\n    files: [a.js, b.js]\n")
		}, nil, []string{"settings-checks"}},
		{"sharedConstants on the basics entry", func(t *testing.T, d string) {
			appendSettings(t, d, "packs:\n  declared:\n    - id: basics\n      config:\n        sharedConstants:\n          - name: port\n")
			write(t, d, ".claudinite/shared/packs/basics/pack.json", `{"version": "1.0", "minEngineVersion": "1.60930.1"}`)
		}, nil, nil},
		{"prose without the index", func(t *testing.T, d string) {
			declare(t, d, "acme-pack", `{"version": "1.0", "minEngineVersion": "1.60930.1"}`)
			write(t, d, ".claudinite/shared/packs/acme-pack/RULES.md", "- r\n")
		}, nil, []string{"claude-md-import", "rules-index-current"}},
		{"a stale index", func(t *testing.T, d string) {
			declare(t, d, "acme-pack", `{"version": "1.0", "minEngineVersion": "1.60930.1"}`)
			write(t, d, ".claudinite/shared/packs/acme-pack/RULES.md", "- r\n")
			write(t, d, ".claudinite/flat/claudinite-rules.GENERATED.md", "@../shared/packs/other/RULES.md\n")
			write(t, d, "CLAUDE.md", "@.claudinite/flat/claudinite-rules.GENERATED.md\n")
		}, []string{"rules-index-current"}, nil},
		{"a current index", func(t *testing.T, d string) {
			declare(t, d, "acme-pack", `{"version": "1.0", "minEngineVersion": "1.60930.1"}`)
			write(t, d, ".claudinite/shared/packs/acme-pack/RULES.md", "- r\n")
			write(t, d, ".claudinite/flat/claudinite-rules.GENERATED.md", "@../shared/packs/acme-pack/RULES.md\n")
			write(t, d, "CLAUDE.md", "# mine\n@.claudinite/flat/claudinite-rules.GENERATED.md\n")
		}, nil, nil},
		{"license plan public", func(t *testing.T, d string) {
			raw, _ := os.ReadFile(filepath.Join(d, ".claudinite/settings.yaml"))
			write(t, d, ".claudinite/settings.yaml", string(raw)+"license:\n  plan: \"public\"\n")
		}, nil, nil},
		{"license plan unknown", func(t *testing.T, d string) {
			raw, _ := os.ReadFile(filepath.Join(d, ".claudinite/settings.yaml"))
			write(t, d, ".claudinite/settings.yaml", string(raw)+"license:\n  plan: \"free\"\n")
		}, []string{"license-plan"}, nil},
		{"license plan unquoted", func(t *testing.T, d string) {
			raw, _ := os.ReadFile(filepath.Join(d, ".claudinite/settings.yaml"))
			write(t, d, ".claudinite/settings.yaml", string(raw)+"license:\n  plan: public\n")
		}, []string{"license-plan"}, nil},
		{"local pack with retired manifest keys", func(t *testing.T, d string) {
			declare(t, d, "local/mine", "")
			write(t, d, ".claudinite/local/packs/mine/pack.json", "{\n  \"marker\": null,\n  \"detect\": null,\n  \"contributes\": {}\n}\n")
		}, nil, []string{"local-pack-shape", "local-pack-shape", "local-pack-shape"}},
		{"local pack declaration with severity", func(t *testing.T, d string) {
			declare(t, d, "local/mine", "")
			write(t, d, ".claudinite/local/packs/mine/pack.json", "{}")
			write(t, d, ".claudinite/local/packs/mine/declared-checks.json", `[{"id": "mine-x", "severity": "advisory", "scanFiles": "a", "forbidLinesMatching": "/x/", "failureMessage": "m"}]`)
		}, nil, []string{"local-pack-shape"}},
		{"local pack with JavaScript rules", func(t *testing.T, d string) {
			declare(t, d, "local/mine", "")
			write(t, d, ".claudinite/local/packs/mine/pack.json", "{}")
			write(t, d, ".claudinite/local/packs/mine/worldRules/a.mjs", "export default {};\n")
			write(t, d, ".claudinite/local/packs/mine/workRules/b.mjs", "export default {};\n")
			write(t, d, ".claudinite/local/packs/mine/skills/s/checks.mjs", "export default [];\n")
		}, []string{"local-pack-shape", "local-pack-shape", "local-pack-shape"}, nil},
		{"temp pack with retired manifest keys", func(t *testing.T, d string) {
			write(t, d, ".claudinite/temp/packs/current_user/pack.json", `{"marker": "x"}`)
		}, nil, []string{"local-pack-shape"}},
		{"canon manifest with a retired key", func(t *testing.T, d string) {
			declare(t, d, "acme-pack", `{"version": "1.0", "minEngineVersion": "1.60930.1", "marker": null}`)
		}, []string{"descriptor-format"}, nil},
		{"local module manifest", func(t *testing.T, d string) {
			declare(t, d, "local/mine", "")
			write(t, d, ".claudinite/local/packs/mine/pack.mjs", "export default {};\n")
		}, []string{"descriptor-format"}, nil},
		{"override in the retired spelling", func(t *testing.T, d string) {
			appendSettings(t, d, "checks:\n  rules:\n    acme-check: blocking\n")
		}, nil, []string{"settings-checks"}},
		{"via and answers on an entry", func(t *testing.T, d string) {
			appendSettings(t, d, "packs:\n  declared:\n    - id: acme-pack\n      via: [basics]\n      answers: {store: \"o/r\"}\n")
			write(t, d, ".claudinite/shared/packs/acme-pack/pack.json", `{"version": "1.0", "minEngineVersion": "1.60930.1"}`)
		}, nil, nil},
		{"a renamed pack declared", func(t *testing.T, d string) {
			declare(t, d, "tidy-repo", "")
		}, []string{"pack-declared"}, nil},
		{"the Node declaration beside", func(t *testing.T, d string) {
			write(t, d, ".claudinite-settings.json", `{"packs": []}`)
		}, nil, []string{"node-leftovers"}},
		{"the Node mount left", func(t *testing.T, d string) {
			write(t, d, ".claudinite/shared/engine/hooks/x.mjs", "\n")
		}, nil, []string{"node-leftovers"}},
		{"an index at the old path", func(t *testing.T, d string) {
			write(t, d, ".claudinite/claudinite-rules.GENERATED.md", "\n")
			write(t, d, ".claudinite/claudinite-skills.GENERATED.md", "\n")
		}, nil, []string{"node-leftovers", "node-leftovers"}},
		{"a workflow step running the Node checks", func(t *testing.T, d string) {
			write(t, d, ".github/workflows/ci.yml", "jobs:\n  c:\n    steps:\n      - run: node .claudinite/shared/engine/checks/check_the_world.mjs\n")
		}, nil, []string{"node-leftovers"}},
		{"the Node hook log ignored", func(t *testing.T, d string) {
			write(t, d, ".gitignore", "node_modules/\n/.claudinite-hooks.log*\n")
		}, nil, []string{"node-leftovers"}},
		{"a declared pack's skill with no skills index", func(t *testing.T, d string) {
			declare(t, d, "acme-pack", `{"version": "1.0", "minEngineVersion": "1.60930.1"}`)
			write(t, d, ".claudinite/shared/packs/acme-pack/skills/demo/SKILL.md", "---\nname: demo\ndescription: d\n---\n")
		}, nil, []string{"skills-index-current"}},
		{"a skills index missing a held skill", func(t *testing.T, d string) {
			declare(t, d, "acme-pack", `{"version": "1.0", "minEngineVersion": "1.60930.1"}`)
			write(t, d, ".claudinite/shared/packs/acme-pack/skills/demo/SKILL.md", "---\nname: demo\ndescription: d\n---\n")
			write(t, d, ".claudinite/flat/claudinite-skills.GENERATED.md", "| `other` | acme-pack | o |\n")
		}, []string{"skills-index-current"}, nil},
		{"a skill outside its pack's manifest subset, unnamed", func(t *testing.T, d string) {
			declare(t, d, "acme-pack", `{"version": "1.0", "minEngineVersion": "1.60930.1", "skills": ["demo"]}`)
			write(t, d, ".claudinite/shared/packs/acme-pack/skills/demo/SKILL.md", "---\nname: demo\ndescription: d\n---\n")
			write(t, d, ".claudinite/shared/packs/acme-pack/skills/draft/SKILL.md", "---\nname: draft\ndescription: d\n---\n")
			write(t, d, ".claudinite/flat/claudinite-skills.GENERATED.md", "| `demo` | acme-pack | d |\n")
		}, nil, nil},
		{"hooks naming the Node engine", func(t *testing.T, d string) {
			write(t, d, ".claude/settings.json", `{"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "node .claudinite/shared/engine/hooks/run-session-start.mjs"}]}]}}`)
		}, []string{"hooks"}, nil},
		{"hooks naming the canon's own engine", func(t *testing.T, d string) {
			write(t, d, ".claude/settings.json", `{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "node $CLAUDE_PROJECT_DIR/engine/hooks/stop-command.mjs"}]}]}}`)
		}, []string{"hooks"}, nil},
		{"malformed packs block", func(t *testing.T, d string) {
			raw, _ := os.ReadFile(filepath.Join(d, ".claudinite/settings.yaml"))
			write(t, d, ".claudinite/settings.yaml", string(raw)+"packs:\n  channel: \"nightly\"\n")
		}, []string{"pack-declared"}, nil},
	}
	for _, c := range cases {
		dir := newShape(t)
		c.mutate(t, dir)
		fs := run(t, dir)
		if got := strings.Join(ids(fs, findings.Break), " "); got != strings.Join(c.breaks, " ") {
			t.Errorf("%s: breaks [%s], want %v\n%v", c.name, got, c.breaks, fs)
		}
		if got := strings.Join(ids(fs, findings.Deprecation), " "); got != strings.Join(c.deps, " ") {
			t.Errorf("%s: deprecations [%s], want %v\n%v", c.name, got, c.deps, fs)
		}
		for _, f := range fs {
			if f.Path == "" || f.Sentence == "" {
				t.Errorf("%s: finding without a path or sentence: %+v", c.name, f)
			}
		}
	}
}

// A repo on the Node engine, with no cn settings at all, gets the one
// settings-file break, and it names the import.
func TestANodeMemberBreaksNamingTheImport(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".claudinite-settings.json", `{"packs": ["basics"]}`)
	fs := run(t, dir)
	if len(fs) != 1 || fs[0].ID != "settings-file" || fs[0].Class != findings.Break || !strings.Contains(fs[0].Sentence, "cn settings import") {
		t.Fatalf("%v", fs)
	}
}

// The deprecations name the line: the manifest key's, and the severity's.
func TestLocalPackShapeNamesTheLine(t *testing.T) {
	dir := newShape(t)
	declare(t, dir, "local/mine", "")
	write(t, dir, ".claudinite/local/packs/mine/pack.json", "{\n  \"ruleRoutingGuidance\": {\n    \"belongs\": \"x\",\n    \"marker\": \"x\",\n    \"excludes\": \"x\"\n  },\n  \"marker\": null\n}\n")
	write(t, dir, ".claudinite/local/packs/mine/declared-checks.json", "[\n  {\n    \"id\": \"mine-x-old\",\n    \"failureMessage\": \"severity\",\n    \"scanFiles\": \"a\"\n  },\n  {\n    \"id\": \"mine-x\",\n    \"severity\": \"blocking\",\n    \"scanFiles\": \"a\"\n  }\n]\n")
	got := map[string]bool{}
	for _, f := range run(t, dir) {
		if f.ID == "local-pack-shape" {
			got[f.Location()] = true
		}
	}
	want := map[string]bool{".claudinite/local/packs/mine/pack.json:7": true, ".claudinite/local/packs/mine/declared-checks.json:9": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%v", got)
	}
}

func appendSettings(t *testing.T, dir, body string) {
	t.Helper()
	raw, _ := os.ReadFile(filepath.Join(dir, ".claudinite/settings.yaml"))
	write(t, dir, ".claudinite/settings.yaml", string(raw)+body)
}

// A rule the settings name that no declared check carries is a
// deprecation, read through the injected loader; a declared-checks fault
// it reports is a descriptor break.
func TestSettingsChecksAgainstTheDeclaredChecks(t *testing.T) {
	dir := newShape(t)
	appendSettings(t, dir, "checks:\n  rules:\n    acme-check: advise\n    acme-gone: \"off\"\n")
	in := Input{Repo: dir, Launcher: launcherBytes(t), Shipped: shippedHashes(t), Declared: func(string) DeclaredChecks {
		return DeclaredChecks{IDs: []string{"acme-check"}, Faults: []DescriptorFault{
			{Path: "p/declared-checks.json", Sentence: "does not parse"},
			{Path: "q", Sentence: "two spellings", Duplicate: true},
		}}
	}}
	fs := Verify(in)
	if got := strings.Join(ids(fs, findings.Deprecation), " "); got != "settings-checks" {
		t.Errorf("deprecations %s: %v", got, fs)
	}
	if got := strings.Join(ids(fs, findings.Break), " "); got != "descriptor-duplicate descriptor-format" {
		t.Errorf("breaks %s: %v", got, fs)
	}
}

// A rule or acceptance may name a check by bare id or <pack>/<id>; the
// loader lists both spellings. A loader that could not list every check
// (the coded checks did not build) raises no unknown-rule deprecation.
func TestSettingsChecksTakeEitherName(t *testing.T) {
	dir := newShape(t)
	appendSettings(t, dir, "checks:\n  rules:\n    acme-pack/acme-check: advise\n    acme-check: advise\n  accept:\n    - rule: node/earn-each-dependency\n      reason: \"kept from the Node engine\"\n")
	known := []string{"acme-check", "acme-pack/acme-check", "earn-each-dependency", "node/earn-each-dependency"}
	fs := Verify(Input{Repo: dir, Launcher: launcherBytes(t), Shipped: shippedHashes(t), Declared: func(string) DeclaredChecks { return DeclaredChecks{IDs: known} }})
	if len(fs) != 0 {
		t.Errorf("either spelling: %v", fs)
	}
	fs = Verify(Input{Repo: dir, Launcher: launcherBytes(t), Shipped: shippedHashes(t), Declared: func(string) DeclaredChecks { return DeclaredChecks{IDs: known[:1], Partial: true} }})
	if len(fs) != 0 {
		t.Errorf("a partial listing: %v", fs)
	}
}

// declare adds id to the member's packs block and, unless manifest is
// empty, vendors a tree holding that pack.json.
func declare(t *testing.T, dir, id, manifest string) {
	t.Helper()
	raw, _ := os.ReadFile(filepath.Join(dir, ".claudinite/settings.yaml"))
	write(t, dir, ".claudinite/settings.yaml", string(raw)+"packs:\n  declared:\n    - "+id+"\n")
	if manifest != "" {
		write(t, dir, ".claudinite/shared/packs/"+id+"/pack.json", manifest)
	}
}

// A pack vendored while the shelf still named the Node engine's two-part
// version is a deprecation the update clears, never a break.
func TestTwoPartMinEngineShapeRaisesOnlyItsDeprecation(t *testing.T) {
	fs := Verify(Input{Repo: filepath.Join(shapes, "v3-two-part-min-engine"), Launcher: launcherBytes(t), Shipped: shippedHashes(t)})
	if findings.AnyBreak(fs) {
		t.Fatalf("%v", fs)
	}
	var legacy int
	for _, f := range fs {
		if f.ID == "pack-min-engine" && f.Class == findings.Deprecation && f.Path == ".claudinite/shared/packs/acme-pack/pack.json" {
			legacy++
		}
	}
	if legacy != 1 {
		t.Errorf("want one pack-min-engine deprecation: %v", fs)
	}
}

func TestOldIgnoreShapeNamesBothFiles(t *testing.T) {
	dir := newShape(t)
	_ = os.Remove(filepath.Join(dir, ".claudinite/.gitignore"))
	write(t, dir, ".gitignore", ".claudinite/bin/\n")
	fs := run(t, dir)
	if len(fs) != 1 || !strings.Contains(fs[0].Sentence, ".gitignore") || !strings.Contains(fs[0].Sentence, ".claudinite/.gitignore") {
		t.Errorf("%v", fs)
	}
}

func TestAShippedLauncherIsAccepted(t *testing.T) {
	dir := newShape(t)
	old := string(launcherBytes(t)) + "# an older launcher\n"
	write(t, dir, ".claudinite/launch", old)
	sum := sha256.Sum256([]byte(old))
	fs := Verify(Input{Repo: dir, Launcher: launcherBytes(t), Shipped: []string{hex.EncodeToString(sum[:])}})
	if len(fs) != 0 {
		t.Errorf("%v", fs)
	}
}

func TestPackManifestMinEngineVersion(t *testing.T) {
	if fs := PackManifest(".claudinite/shared/packs/basics/pack.json", "60928.1"); len(fs) != 1 || fs[0].Class != findings.Deprecation || fs[0].ID != "pack-min-engine" || !strings.Contains(fs[0].Sentence, "Node engine") {
		t.Errorf("two-part: %v", fs)
	}
	if fs := PackManifest("p/pack.json", "1.60928.1"); len(fs) != 0 {
		t.Errorf("three-part: %v", fs)
	}
	if fs := PackManifest("p/pack.json", "60928"); len(fs) != 1 || fs[0].Class != findings.Break {
		t.Errorf("malformed: %v", fs)
	}
}

// Every shape an earlier release accepted still raises only deprecations.
func TestShapeCorpus(t *testing.T) {
	dirs, _ := filepath.Glob(filepath.Join(shapes, "*-*"))
	if len(dirs) < 5 {
		t.Fatalf("shape corpus holds %d fixtures", len(dirs))
	}
	for _, d := range dirs {
		fs := Verify(Input{Repo: d, Launcher: launcherBytes(t), Shipped: shippedHashes(t)})
		if findings.AnyBreak(fs) {
			t.Errorf("%s: a shape an earlier release accepted now breaks: %v", filepath.Base(d), fs)
		}
	}
}

// A change to the rule set must grow the corpus: rules.txt's newest line
// names the registered rules, and every line's prefix has fixtures.
func TestCorpusGrowsWithTheRuleSet(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(shapes, "rules.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var last []string
	seen := map[string]bool{}
	for _, l := range strings.Split(string(raw), "\n") {
		f := strings.Fields(l)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if seen[f[0]] {
			t.Errorf("rules.txt repeats prefix %s", f[0])
		}
		seen[f[0]] = true
		if m, _ := filepath.Glob(filepath.Join(shapes, f[0]+"-*")); len(m) == 0 {
			t.Errorf("rules.txt names prefix %s, which has no fixture", f[0])
		}
		last = f[1:]
	}
	got := RuleIDs()
	sort.Strings(last)
	if strings.Join(got, " ") != strings.Join(last, " ") {
		t.Errorf("verify registers [%s] but rules.txt's newest line says [%s]: add a line with a new prefix and its fixture", strings.Join(got, " "), strings.Join(last, " "))
	}
}

// NodeHook matches the Node engine's hooks under either root, and nothing
// that merely ends in engine/hooks/.
func TestNodeHookMatchesBothRoots(t *testing.T) {
	for cmd, want := range map[string]bool{
		"node $CLAUDE_PROJECT_DIR/.claudinite/shared/engine/hooks/stop-command.mjs": true,
		"node $CLAUDE_PROJECT_DIR/engine/hooks/stop-command.mjs":                    true,
		"node engine/hooks/stop-command.mjs":                                        true,
		"bash \"$CLAUDE_PROJECT_DIR\"/engine/hooks/session-start-command.sh":        true,
		"sh tools/myengine/hooks/stop.sh":                                           false,
		"sh tools/my.engine/hooks/stop.sh":                                          false,
		".claudinite/bin/cn hook stop":                                              false,
	} {
		if got := NodeHook.MatchString(cmd); got != want {
			t.Errorf("%q: %v, want %v", cmd, got, want)
		}
	}
}
