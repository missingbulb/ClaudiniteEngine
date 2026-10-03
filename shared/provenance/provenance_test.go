package provenance

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The cases are the Node module's own, from engine-tests/checks/helpers/
// provenance.test.mjs at missingbulb/Claudinite@057841ac.

const born = "## 2026-07-18 · born · promoted from a member's local pack (#319)\n" +
	"- **Source:** the member's own site rule matched a lookalike host.\n" +
	"- **Reason:** `hostSuffix` is a raw string suffix, so `example.com` also matches\n" +
	"  `evilexample.com`.\n" +
	"- **Actor:** the growth-promote run, merged by @missingbulb (owner).\n" +
	"- **Mechanism:** prose. The signature is not the violation.\n" +
	"- **Retire when:** Chrome documents `hostSuffix` as label-bounded.\n" +
	"- **Landed:** #319 · pack version 1.\n"

const reworded = "\n## 2026-07-27 · reworded · the corpus-wide pass (#467)\n" +
	"- **Actor:** @missingbulb (owner).\n" +
	"- **Landed:** #467 · pack version 1.\n"

func TestParseReadsEntriesAndJoinsContinuations(t *testing.T) {
	entries, problems := Parse(born + reworded)
	if len(problems) != 0 {
		t.Fatalf("problems %v", problems)
	}
	if len(entries) != 2 || entries[0].Kind != "born" || entries[0].Date != "2026-07-18" {
		t.Fatalf("entries %+v", entries)
	}
	if !strings.HasSuffix(entries[0].Fields["Reason"], "also matches `evilexample.com`.") {
		t.Errorf("Reason %q: the continuation line is not joined", entries[0].Fields["Reason"])
	}
	if want := []string{"Source", "Reason", "Actor", "Mechanism", "Retire when", "Landed"}; !reflect.DeepEqual(entries[0].Order, want) {
		t.Errorf("order %v, want %v", entries[0].Order, want)
	}
	if Status(entries) != "live" || len(EntryFaults(entries)) != 0 {
		t.Errorf("status %s, faults %v", Status(entries), EntryFaults(entries))
	}
}

func TestParseReportsEveryFaultAtItsLine(t *testing.T) {
	text := "# header\n" + born + "\n## 2026-07-01 · sideways · earlier\n- **Vibe:** good\n- **Reason:**\nloose text\n## 2026-08-01 · retired · gone\n- **Actor:** @x (owner).\n"
	entries, problems := Parse(text)
	var whats []string
	for _, p := range problems {
		whats = append(whats, p.What)
	}
	all := strings.Join(whats, "\n")
	if problems[0].Line != 1 || !strings.HasPrefix(problems[0].What, "text outside an entry") {
		t.Errorf("first problem %+v", problems[0])
	}
	for _, want := range []string{`"sideways" is not in the vocabulary`, "dated 2026-07-01 follows one dated 2026-07-18", `field "Vibe" is not in the vocabulary`, `field "Reason" is empty`, `neither a "- **Field:** …" bullet nor an indented continuation`} {
		if !strings.Contains(all, want) {
			t.Errorf("no problem says %q:\n%s", want, all)
		}
	}
	if Status(entries) != "retired" {
		t.Errorf("status %s, want retired", Status(entries))
	}
	if _, p := Parse("## 2026-13-01 · born · x\n- **Mechanism:** m\n"); len(p) != 1 || !strings.Contains(p[0].What, "is not a date") {
		t.Errorf("month 13: %v", p)
	}
	if _, p := Parse("## 2026-02-30 · born · x\n- **Mechanism:** m\n"); len(p) != 0 {
		t.Errorf("February 30th reads as a date to the Node engine: %v", p)
	}
}

func TestEntryFaults(t *testing.T) {
	entries, _ := Parse("## 2026-07-01 · moved · into a skill\n- **Actor:** @x (owner).\n")
	var whats []string
	for _, f := range EntryFaults(entries) {
		whats = append(whats, f.What)
	}
	all := strings.Join(whats, "\n")
	if !strings.Contains(all, "opens with born") || !strings.Contains(all, "moved entry carries no Mechanism") {
		t.Errorf("faults:\n%s", all)
	}
	for _, k := range MechanismKinds {
		if !has(Kinds, k) {
			t.Errorf("mechanism kind %s is not a kind", k)
		}
	}
}

func TestVocabulary(t *testing.T) {
	if !reflect.DeepEqual(Fields, []string{"Source", "Reason", "Actor", "Model", "Mechanism", "Rejected", "Retire when", "Landed"}) {
		t.Errorf("fields %v", Fields)
	}
	if PackElement != "_pack" || DeclinedFile != "_declined.md" || Dir != "provenance" {
		t.Errorf("names %s %s %s", PackElement, DeclinedFile, Dir)
	}
}

func TestMarker(t *testing.T) {
	for line, want := range map[string]string{
		"- **A** — b. (doing-x)":         "doing-x",
		"  (cdp-worker-value)":           "cdp-worker-value",
		"- **A** — it is (canon).":       "",
		"- **A** — cite (#1119).":        "",
		"- **A** — numbered (3).":        "",
		"- **A** — two (3, 7).":          "",
		"- **A** — four words (a-b-c-d)": "a-b-c-d",
	} {
		got, ok := Marker(line)
		if got != want || ok != (want != "") {
			t.Errorf("Marker(%q) = %q %v, want %q", line, got, ok, want)
		}
	}
	if !SlugRE.MatchString("doing-x") || SlugRE.MatchString("canon") || SlugRE.MatchString("3") {
		t.Error("SlugRE")
	}
}

const rulesMD = "# pack\n\n" +
	"- **Passing a path from a service worker** — resolve it against the worker's own URL,\n" +
	"  never a filesystem walk. (worker-absolute-paths)\n\n" +
	"- **Wanting import/export** — bundle it. (3)\n" +
	"  - a sub-bullet stays inside the block\n\n" +
	"- **Assembling a shared global** — do it once; the consequence clause says why.\n\n" +
	"## Second surface\n\n" +
	"- **Reading a worker value over CDP** — evaluate in the worker, never the page.\n" +
	"  (cdp-worker-value)\n"

func TestRuleBlocks(t *testing.T) {
	blocks := RuleBlocks(rulesMD, false)
	var triggers, slugs, nums []string
	for _, b := range blocks {
		triggers, slugs, nums = append(triggers, b.Trigger), append(slugs, b.Slug), append(nums, b.Numeric)
	}
	if want := []string{"Passing a path from a service worker", "Wanting import/export", "Assembling a shared global", "Reading a worker value over CDP"}; !reflect.DeepEqual(triggers, want) {
		t.Errorf("triggers %q", triggers)
	}
	if want := []string{"worker-absolute-paths", "", "", "cdp-worker-value"}; !reflect.DeepEqual(slugs, want) {
		t.Errorf("slugs %q", slugs)
	}
	if want := []string{"", "3", "", ""}; !reflect.DeepEqual(nums, want) {
		t.Errorf("numerics %q", nums)
	}
	if blocks[1].LastLine != 5 {
		t.Errorf("lastLine %d, want 5: the marker ends the lead paragraph", blocks[1].LastLine)
	}
	if blocks[1].Text != "- **Wanting import/export** — bundle it. - a sub-bullet stays inside the block" {
		t.Errorf("text %q", blocks[1].Text)
	}
	after := RuleBlocks("- **Legacy** — marked after its list.\n  - item\n  - item (3)", false)[0]
	if after.Numeric != "3" || after.LastLine != 2 {
		t.Errorf("a marker after the nested list: %+v", after)
	}
	if NormalizeRuleText("- **A** — b   c. (x-y)") != "- **A** — b c." || NormalizeRuleText("- **A** — b c.\n  (x-y)") != NormalizeRuleText("- **A** — b   c. (3)") {
		t.Error("NormalizeRuleText")
	}
	for _, text := range []string{"- **Done** — it is (canon) and fine (see below).", "- **Done** — cite (#1119)."} {
		if s := RuleBlocks(text, false)[0].Slug; s != "" {
			t.Errorf("%q read slug %q", text, s)
		}
	}
	fenced := RuleBlocks("- **Appending** — write it so:\n\n```\n## 2026-01-01 · born · x\n- **Reason:** an example, not a rule\n```\n\n- **Next** — a real rule. (next-rule)\n", false)
	if len(fenced) != 2 || fenced[0].Trigger != "Appending" || fenced[0].Slug != "" || fenced[1].Slug != "next-rule" {
		t.Errorf("fenced %+v", fenced)
	}
	if n := RuleBlocks("- **Wanting a growth action** — declare a task here. (RULES-14)", false)[0].Numeric; n != "14" {
		t.Errorf("RULES-14 read %q", n)
	}
	if n := RuleBlocks("- **Suffixed** — cited so. (2a)", false)[0].Numeric; n != "2a" {
		t.Errorf("2a read %q", n)
	}
	plain := RuleBlocks("- See a test fail before you trust it: write it red first. (1)\n- **Never test that a value is set.** A test that reads a value someone declared. (2)\n  - a sub-bullet\n- Before trusting a new transform, run it over the real corpus.\n", true)
	var got [][2]string
	for _, b := range plain {
		got = append(got, [2]string{b.Trigger, b.Numeric})
	}
	if want := [][2]string{{"See a test fail before you trust it", "1"}, {"Never test that a value is set.", "2"}, {"Before trusting a new transform, run it over", ""}}; !reflect.DeepEqual(got, want) {
		t.Errorf("plain bullets %q", got)
	}
	if n := len(RuleBlocks("- See a test fail before you trust it. (1)\n", false)); n != 0 {
		t.Errorf("a plain bullet is a rule without plainBullets: %d", n)
	}
}

func TestSkillShape(t *testing.T) {
	g := SkillShape("---\nname: g\nmetadata:\n  body: guidelines\n---\n\n# g\n\n- **Doing X** — do it. (doing-x)\n- **Doing Y** — do it.\n")
	if g.Body != "guidelines" || g.Proposed != "guidelines" || len(g.Bullets) != 2 || g.BodyOffset != 5 {
		t.Errorf("guidelines %+v", g)
	}
	w := SkillShape("---\nname: w\n---\n\n1. First do this.\n2. Then that.\n\n- **A gotcha** — mind it.\n")
	if w.Body != "" || w.Proposed != "workflow" {
		t.Errorf("workflow %+v", w)
	}
	if p := SkillShape("# no frontmatter\n\n- **Only bullets** — here.\n").Proposed; p != "guidelines" {
		t.Errorf("only bullets proposes %s", p)
	}
	if p := SkillShape("# prose only\n\nJust text.\n").Proposed; p != "workflow" {
		t.Errorf("prose only proposes %s", p)
	}
}

// mapIO is an IO over a map of files, its directories read off the keys.
type mapIO map[string]string

func (m mapIO) Exists(p string) bool { _, ok := m[p]; return ok }
func (m mapIO) Read(p string) (string, bool) {
	s, ok := m[p]
	return s, ok
}
func (m mapIO) ListDir(p string) ([]string, bool) {
	var files []string
	for k := range m {
		files = append(files, k)
	}
	return ListFrom(files, p)
}

var alpha = mapIO{
	"packs/alpha/pack.mjs":                            "export default { version: 1 };\n",
	"packs/alpha/RULES.md":                            rulesMD,
	"packs/alpha/skills/g/SKILL.md":                   "---\nname: g\nmetadata:\n  body: guidelines\n---\n\n- **Doing X** — do it. (doing-x)\n- **Doing Y** — do it. (7)\n",
	"packs/alpha/skills/w/SKILL.md":                   "---\nname: w\nmetadata:\n  body: workflow\n---\n\n1. First.\n\n- **A gotcha** — mind it. (2)\n- **Another** — marked anyway. (stray-marker)\n",
	"packs/alpha/skills/nobody/SKILL.md":              "---\nname: nobody\n---\n\n- **Unmarked guideline** — do it.\n",
	"packs/alpha/worldRules/coded.mjs":                "const rule = { id: 'cer/coded-check', on_fail: 'block' };\nexport default rule;\n",
	"packs/alpha/worldRules/coded.test.mjs":           "const rule = { id: 'not-a-rule' };\n",
	"packs/alpha/checks/go_check.go":                  "package checks\n\nfunc init() {\n\tchecksdk.Register(checksdk.Check{\n\t\tID:   \"go-check\",\n\t\tTags: []string{\"world\"},\n\t})\n}\n",
	"packs/alpha/checks/go_check_test.go":             "package checks\n\nvar x = checksdk.Check{ID: \"not-a-check\"}\n",
	"packs/alpha/declared-checks.json":                "[{ \"id\": \"declared-check\", \"on_fail\": \"advise\" }]\n",
	"packs/alpha/tasks/acme-task-i/task.json":         "{}\n",
	"packs/alpha/provenance/worker-absolute-paths.md": born,
	"packs/alpha/provenance/cdp-worker-value.md":      "",
	"packs/alpha/provenance/orphan.md":                born,
	"packs/alpha/provenance/VERSIONS.md":              "| Version | Date | What changed |\n|---|---|---|\n| 1 | 2026-08-01 | seed |\n",
	"packs/alpha/provenance/retired-one.md":           born + "\n## 2026-08-01 · retired · gone\n- **Actor:** @x (owner).\n",
	"packs/alpha/provenance/_pack.md":                 "",
	"packs/alpha/provenance/_declined.md":             "## 2026-08-01 · declined · a candidate\n- **Reason:** it restated a canon rule.\n- **Actor:** @x (owner).\n",
	"packs/alpha/references.md":                       "- **(RULES-3)** Bundle because the browser refuses bare imports.\n",
}

func TestPackCarriers(t *testing.T) {
	c := PackCarriers("packs/alpha", alpha)
	var rules [][3]any
	for _, r := range c.Rules {
		rules = append(rules, [3]any{r.Line, r.LastLine, r.Slug})
	}
	if want := [][3]any{{3, 4, "worker-absolute-paths"}, {6, 6, ""}, {9, 9, ""}, {13, 14, "cdp-worker-value"}}; !reflect.DeepEqual(rules, want) {
		t.Errorf("rules %v", rules)
	}
	var gl [][4]any
	for _, g := range c.Guidelines {
		gl = append(gl, [4]any{g.Skill, g.Line, g.Slug, g.Numeric})
	}
	if want := [][4]any{{"g", 7, "doing-x", ""}, {"g", 8, "", "7"}}; !reflect.DeepEqual(gl, want) {
		t.Errorf("guidelines %v", gl)
	}
	var skills [][3]string
	for _, s := range c.Skills {
		skills = append(skills, [3]string{s.Name, s.Body, s.Proposed})
	}
	if want := [][3]string{{"g", "guidelines", "guidelines"}, {"nobody", "", "guidelines"}, {"w", "workflow", "workflow"}}; !reflect.DeepEqual(skills, want) {
		t.Errorf("skills %v", skills)
	}
	var checks []string
	for _, ch := range c.Checks {
		checks = append(checks, ch.ID)
	}
	if want := []string{"cer/coded-check", "go-check", "declared-check"}; !reflect.DeepEqual(checks, want) {
		t.Errorf("checks %v: a test module is not a rule module", checks)
	}
	if len(c.Tasks) != 1 || c.Tasks[0].ID != "acme-task-i" || c.ManifestFile != "pack.mjs" {
		t.Errorf("tasks %v, manifest %q", c.Tasks, c.ManifestFile)
	}
	if ElementID("cer/coded-check") != "cer-coded-check" {
		t.Error("ElementID")
	}
	files := FileMap(Files("packs/alpha", alpha))
	ids := sortedKeys(files)
	if want := []string{"_pack", "cdp-worker-value", "orphan", "retired-one", "worker-absolute-paths"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("files %v: neither the declined log nor the version log is an element", ids)
	}
	if files["retired-one"].Status != "retired" || !files["cdp-worker-value"].Empty {
		t.Error("status or empty")
	}
}

func TestPackDirsIn(t *testing.T) {
	got := PackDirsIn([]string{"packs/a/RULES.md", "packs/a/skills/x/SKILL.md", ".claudinite/local/packs/b/pack.mjs", "packs/README.md", "src/x.js", "packs/directory.GENERATED.md"})
	if want := []string{".claudinite/local/packs/b", "packs/a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("PackDirsIn %v", got)
	}
}

func TestAuditPack(t *testing.T) {
	a := AuditPack("packs/alpha", alpha)
	var unmarked []string
	for _, u := range a.Unmarked {
		unmarked = append(unmarked, u.Trigger)
	}
	if want := []string{"Wanting import/export", "Assembling a shared global"}; !reflect.DeepEqual(unmarked, want) {
		t.Errorf("unmarked %v", unmarked)
	}
	var dangling []string
	for _, d := range a.Dangling {
		dangling = append(dangling, d.Carrier+"|"+d.ID)
	}
	sort.Strings(dangling)
	want := []string{"check cer/coded-check|cer-coded-check", "check declared-check|declared-check", "check go-check|go-check",
		`guideline "Doing X"|doing-x`, "skill g|g", "skill nobody|nobody", "skill w|w", "task acme-task-i|acme-task-i"}
	if !reflect.DeepEqual(dangling, want) {
		t.Errorf("dangling %v", dangling)
	}
	if len(a.Unnamed) != 1 || a.Unnamed[0].ID != "orphan" {
		t.Errorf("unnamed %v: a retired file no carrier names is not a fault", a.Unnamed)
	}
	if len(a.NoBody) != 1 || a.NoBody[0].Name != "nobody" {
		t.Errorf("no body %v", a.NoBody)
	}
	if len(a.MarkerInWorkflow) != 1 || a.MarkerInWorkflow[0].Slug != "stray-marker" {
		t.Errorf("marker in workflow %v", a.MarkerInWorkflow)
	}
	var empty []string
	for _, e := range a.Empty {
		empty = append(empty, e.ID)
	}
	if !reflect.DeepEqual(empty, []string{"_pack", "cdp-worker-value"}) || len(a.ParseErrors)+len(a.EntryFaults) != 0 {
		t.Errorf("empty %v, parse %v, entry %v", empty, a.ParseErrors, a.EntryFaults)
	}
}

func TestChecksOfModule(t *testing.T) {
	agg := mapIO{
		"packs/py/skills/imports/checks.mjs":                       "export { lazy } from './optional-import-lazy.mjs';\nexport * from './optional-import-install-hint.mjs';\n",
		"packs/py/skills/imports/optional-import-lazy.mjs":         "export const lazy = { id: 'optional-import-lazy', fix: 'x' };\n",
		"packs/py/skills/imports/optional-import-install-hint.mjs": "export const hint = { id: 'optional-import-install-hint' };\n",
		"packs/py/skills/imports/SKILL.md":                         "---\nname: imports\nmetadata:\n  body: workflow\n---\n\n1. Do.\n",
		"packs/py/pack.mjs":                                        "export default {};\n",
	}
	byID := map[string]string{}
	for _, c := range PackCarriers("packs/py", agg).Checks {
		byID[c.ID] = c.File
	}
	if byID["optional-import-lazy"] != "packs/py/skills/imports/optional-import-lazy.mjs" || byID["optional-import-install-hint"] != "packs/py/skills/imports/optional-import-install-hint.mjs" || len(byID) != 2 {
		t.Errorf("aggregator carriers %v", byID)
	}
	consts := mapIO{
		"packs/acme-pack/worldRules/acme-check.mjs":  "const id = 'acme-pack/acme-check';\nconst on_fail = 'block';\nexport default {\n  id,\n  on_fail,\n  run: () => [],\n};\n",
		"packs/acme-pack/worldRules/acme-inline.mjs": "export default { id, run: () => [] };\nconst id = 'acme-inline';\n",
		"packs/acme-pack/worldRules/acme-helper.mjs": "const id = 'not-exported';\nexport default { key: id, run: () => [] };\n",
		"packs/acme-pack/pack.json":                  "{}\n",
	}
	var ids []string
	for _, c := range PackCarriers("packs/acme-pack", consts).Checks {
		ids = append(ids, c.ID)
	}
	sort.Strings(ids)
	if !reflect.DeepEqual(ids, []string{"acme-inline", "acme-pack/acme-check"}) {
		t.Errorf("const ids %v", ids)
	}
	own := mapIO{
		"p/skills/s/checks.mjs":    "import { finding } from '../../../engine/findings.mjs';\nimport { state } from './interview.mjs';\nconst rule = { id: 'interview-answer-stale' };\nexport default [rule];\n",
		"p/skills/s/interview.mjs": "// questions: [{ id: 'goals', prompt: 'x' }]\nexport const state = 1;\n",
	}
	if got := checksOfModule(own, "p/skills/s/checks.mjs", "p", map[string]bool{}); len(got) != 1 || got[0].ID != "interview-answer-stale" {
		t.Errorf("a module declaring its own id is not an aggregator: %v", got)
	}
	cycle := mapIO{
		"p/skills/s/checks.mjs": "export * from './mid.mjs';\nexport * from '../../../engine/shared.mjs';\n",
		"p/skills/s/mid.mjs":    "export * from './leaf.mjs';\nexport * from './checks.mjs';\n",
		"p/skills/s/leaf.mjs":   "export const c = { id: 'leaf-check' };\n",
		"engine/shared.mjs":     "export const helper = { id: 'not-a-pack-carrier' };\n",
	}
	if got := checksOfModule(cycle, "p/skills/s/checks.mjs", "p", map[string]bool{}); len(got) != 1 || got[0].ID != "leaf-check" {
		t.Errorf("cycle %v", got)
	}
}

func TestConvertedOnly(t *testing.T) {
	io := mapIO{
		"packs/a/pack.mjs":                     "export default {};\n",
		"packs/a/provenance/converted-rule.md": "## 2026-09-14 · born · converted from references.md (RULES-3)\n- **Reason:** it kept biting.\n- **Retire when:** the platform fixes it.\n",
		"packs/a/provenance/real.md":           born,
		"packs/a/provenance/empty.md":          "",
	}
	files := FileMap(Files("packs/a", io))
	if !files["converted-rule"].ConvertedOnly || files["converted-rule"].Empty || files["real"].ConvertedOnly || files["empty"].ConvertedOnly {
		t.Errorf("files %+v", files)
	}
}

// A folded pack's checks are Go under checks/, some registered through a
// helper, some engine built-ins; each names its element's file, a check
// the Node engine named <pack>/<id> keeping <pack>-<id>.md (#71).
// isolateEngineChecks gives one test an empty engine-check registry and
// puts the shared one back when it ends.
func isolateEngineChecks(t *testing.T) {
	t.Helper()
	engineMu.Lock()
	saved := engineChecks
	engineChecks = map[string][]string{}
	engineMu.Unlock()
	t.Cleanup(func() {
		engineMu.Lock()
		engineChecks = saved
		engineMu.Unlock()
	})
}

func TestGoAndEngineChecksAreCarriers(t *testing.T) {
	io := mapIO{
		"packs/acme/pack.json":                         "{\"version\": \"1\"}\n",
		"packs/acme/checks/literal.go":                 "package checks\n\nfunc init() {\n\tchecksdk.Register(checksdk.Check{\n\t\tID: \"flat-check\",\n\t})\n}\n",
		"packs/acme/checks/lib.go":                     "package checks\n\nfunc register(id, why string) {\n\tchecksdk.Register(checksdk.Check{ID: id, Why: why})\n}\n",
		"packs/acme/checks/calls.go":                   "package checks\n\nfunc init() {\n\tregister(\"helper-check\", \"why\")\n\tregister(\"prefixed-check\", \"why\")\n}\n",
		"packs/acme/checks/closure.go":                 "package checks\n\nfunc init() {\n\treg := func(id string) { checksdk.Register(checksdk.Check{ID: id}) }\n\treg(\"closure-check\")\n}\n",
		"packs/acme/checks/calls_test.go":              "package checks\n\nvar _ = checksdk.Check{ID: \"test-check\"}\n",
		"packs/acme/provenance/_pack.md":               born,
		"packs/acme/provenance/flat-check.md":          born,
		"packs/acme/provenance/helper-check.md":        born,
		"packs/acme/provenance/acme-prefixed-check.md": born,
		"packs/acme/provenance/closure-check.md":       born,
		"packs/acme/provenance/engine-check.md":        born,
	}
	isolateEngineChecks(t)
	RegisterEngineCheck("acme", "engine-check")
	RegisterEngineCheck("other", "not-acmes")
	a := AuditPack("packs/acme", io)
	if len(a.Unnamed) != 0 || len(a.Dangling) != 0 {
		t.Errorf("unnamed %v, dangling %v", a.Unnamed, a.Dangling)
	}
	got := map[string]string{}
	for _, c := range a.Carriers.Checks {
		got[c.ID] = ElementOf(c) + " " + c.File
	}
	want := map[string]string{
		"flat-check":     "flat-check packs/acme/checks/literal.go",
		"helper-check":   "helper-check packs/acme/checks/calls.go",
		"prefixed-check": "acme-prefixed-check packs/acme/checks/calls.go",
		"closure-check":  "closure-check packs/acme/checks/closure.go",
		"engine-check":   "engine-check " + EngineCarrierFile,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("checks %v, want %v", got, want)
	}
}

// The shelf wrote ported, gate-changed and hardened while porting onto
// cn; the vocabulary reads them, so a file carrying one still takes an
// append (ClaudinitePacks#33).
func TestVocabularyReadsTheShelfsPortKinds(t *testing.T) {
	text := born + "\n## 2026-09-30 · ported · onto the SDK (#20)\n- **Reason:** the runtime moved.\n" +
		"\n## 2026-10-01 · gate-changed · the precondition reads the change\n- **Reason:** it fired on nothing.\n" +
		"\n## 2026-10-02 · hardened · a missing secret parks the item\n- **Reason:** it degraded silently.\n"
	entries, problems := Parse(text)
	if len(problems) != 0 || len(entries) != 4 {
		t.Fatalf("entries %d, problems %v", len(entries), problems)
	}
	if faults := EntryFaults(entries); len(faults) != 0 {
		t.Errorf("faults %v: none of the three owes a Mechanism", faults)
	}
}
