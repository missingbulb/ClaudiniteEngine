package builtin

import (
	"strings"
	"testing"
)

// The cases are the growth pack's own, from test/provenance-integrity.
// test.mjs at missingbulb/Claudinite@057841ac.

const pk = ".claudinite/local/packs/mypack/"

const bornEntry = "## 2026-07-18 · born · from an incident (#12)\n- **Reason:** it failed twice.\n- **Actor:** @x (owner).\n- **Mechanism:** prose.\n"
const rewordedEntry = "\n## 2026-08-01 · reworded · the pass (#40)\n- **Actor:** @x (owner).\n"

var cleanPack = map[string]string{
	pk + "RULES.md":                    "- **Doing a thing** — the settled way. (doing-thing)\n\n- **Doing another** — plainly.\n  (doing-another)\n",
	pk + "skills/how/SKILL.md":         "---\nname: how\nmetadata:\n  body: workflow\n---\n\n1. First. (3)\n",
	pk + "skills/rules/SKILL.md":       "---\nname: rules\nmetadata:\n  body: guidelines\n---\n\n- **Guideline one** — do it. (guideline-one)\n",
	pk + "worldRules/my-rule.mjs":      "const rule = { id: 'my/rule', on_fail: 'block' };\nexport default rule;\n",
	pk + "declared-checks.json":        "[{ \"id\": \"declared-one\", \"on_fail\": \"advise\", \"failureMessage\": \"m\" }]\n",
	pk + "tasks/nightly/task.json":     "{}\n",
	pk + "provenance/doing-thing.md":   bornEntry,
	pk + "provenance/doing-another.md": "",
	pk + "provenance/how.md":           "",
	pk + "provenance/rules.md":         "",
	pk + "provenance/guideline-one.md": "",
	pk + "provenance/my-rule.md":       "",
	pk + "provenance/declared-one.md":  "",
	pk + "provenance/nightly.md":       "",
	pk + "provenance/_pack.md":         "",
	pk + "provenance/_declined.md":     "## 2026-08-01 · declined · a candidate\n- **Reason:** it restated the canon.\n- **Actor:** @x (owner).\n",
	"src/app.js":                       "x\n",
}

// filled is cleanPack with every empty file holding a born entry.
func filled() map[string]string {
	out := map[string]string{}
	for k, v := range cleanPack {
		if v == "" {
			v = bornEntry
		}
		out[k] = v
	}
	return out
}

func with(base map[string]string, over map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

const guidelines = "---\nname: rules\nmetadata:\n  body: guidelines\n---\n\n- **Guideline one** — do it. (guideline-one)\n- **Guideline two** — do it too.\n- **Guideline three** — and this.\n"
const sharedRules = "- **Doing a thing** — the settled way. (doing-thing)\n\n- **Doing it again** — the same way. (doing-thing)\n\n- **Doing another** — plainly.\n  (doing-another)\n"

func TestProvenanceIntegrityOverAPackWithOnlyAManifest(t *testing.T) {
	expect(t, repo{base: map[string]string{"src/app.js": "x\n", "packs/README.md": "not a pack\n"}}.run(t, "provenance-integrity"),
		want{path: ".claudinite/local/packs/mypack/pack.json", what: `the manifest names .*_pack\.md, which is not a file`})
}

func TestProvenanceIntegrityPendingHistoryIsOneAdvisory(t *testing.T) {
	expect(t, repo{base: cleanPack}.run(t, "provenance-integrity"),
		want{path: pk + "provenance", what: `^8 provenance files are empty .*\(_pack\.md, declared-one\.md, doing-another\.md, …\)`, advise: true})
	expect(t, repo{base: with(filled(), map[string]string{pk + "skills/rules/SKILL.md": guidelines, pk + "RULES.md": sharedRules})}.run(t, "provenance-integrity"))
}

func TestProvenanceIntegrityFlags(t *testing.T) {
	expect(t, repo{base: with(filled(), map[string]string{pk + "RULES.md": "- **Doing a thing** — no marker.\n\n- **Doing another** — none either.\n"})}.run(t, "provenance-integrity"),
		want{path: pk + "RULES.md", line: 1, what: `2 rules end with no marker.*first: "Doing a thing"`, fix: `provenance\.mjs mark mypack`},
		want{path: pk + "provenance/doing-another.md", what: `live, and no carrier of \.claudinite/local/packs/mypack names doing-another\.md`},
		want{path: pk + "provenance/doing-thing.md", what: `live, and no carrier of \.claudinite/local/packs/mypack names doing-thing\.md`})
	expect(t, repo{base: with(filled(), map[string]string{
		pk + "RULES.md":                    "- **Doing a thing** — the settled way. (doing-thing-gone)\n\n- **Doing another** — plainly. (doing-another)\n",
		pk + "provenance/doing-another.md": bornEntry + "\n## 2026-09-01 · retired · superseded by the canon\n- **Actor:** @x (owner).\n",
	})}.run(t, "provenance-integrity"),
		want{path: pk + "RULES.md", line: 1, what: `rule "Doing a thing" names .*doing-thing-gone\.md, which is not a file`, fix: "mark mypack"},
		want{path: pk + "RULES.md", line: 3, what: `rule "Doing another" names .*doing-another\.md, which is retired`, fix: "retired element carries no live carrier"},
		want{path: pk + "provenance/doing-thing.md", what: "no carrier of"})
	expect(t, repo{base: with(filled(), map[string]string{
		pk + "skills/how/SKILL.md":   "---\nname: how\n---\n\n1. First.\n",
		pk + "skills/rules/SKILL.md": "---\nname: rules\nmetadata:\n  body: workflow\n---\n\n- **Guideline one** — do it. (guideline-one)\n",
	})}.run(t, "provenance-integrity"),
		want{path: pk + "provenance/guideline-one.md", what: "no carrier of"},
		want{path: pk + "skills/how/SKILL.md", what: "skill how declares no body", fix: "body: workflow"},
		want{path: pk + "skills/rules/SKILL.md", line: 7, what: `\(guideline-one\) inside a skill whose body is a workflow`})
	expect(t, repo{base: with(filled(), map[string]string{
		pk + "provenance/doing-thing.md":   "# header\n## 2026-07-18 · reworded · first\n- **Actor:** @x (owner).\n",
		pk + "provenance/doing-another.md": "## 2026-07-18 · born · no mechanism\n- **Actor:** @x (owner).\n",
	})}.run(t, "provenance-integrity"),
		want{path: pk + "provenance/doing-another.md", line: 1, what: "born entry carries no Mechanism"},
		want{path: pk + "provenance/doing-thing.md", line: 1, what: "text outside an entry"},
		want{path: pk + "provenance/doing-thing.md", line: 2, what: "opens with born"})
	expect(t, repo{base: with(filled(), map[string]string{
		"packs/somepack/pack.json":           "{}\n",
		"packs/somepack/RULES.md":            "- **Doing a thing** — no marker.\n",
		"packs/somepack/provenance/_pack.md": bornEntry,
	})}.run(t, "provenance-integrity"),
		want{path: "packs/somepack/RULES.md", line: 1, what: "1 rule ends with no marker", fix: `\.claudinite/shared/packs/claudinite-growth/provenance\.mjs mark somepack`})
}

func TestProvenanceIntegrityReadsGoChecks(t *testing.T) {
	got := repo{base: with(filled(), map[string]string{pk + "checks/go_rule.go": "package checks\n\nvar c = checksdk.Check{ID: \"go-rule\"}\n"})}.run(t, "provenance-integrity")
	if len(got) != 1 || !strings.Contains(got[0].Sentence, "check go-rule names") {
		t.Fatalf("a Go check with no provenance file: %v", got)
	}
}
