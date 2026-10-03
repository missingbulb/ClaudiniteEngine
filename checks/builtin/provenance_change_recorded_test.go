package builtin

import (
	"strings"
	"testing"
)

// The cases are the growth pack's own work half, from test/provenance-
// integrity.test.mjs at missingbulb/Claudinite@057841ac; the member's
// manifest here is a pack.json where the Node fixture's was a pack.mjs.

func changeRecorded(t *testing.T, base, change map[string]string) []want {
	t.Helper()
	got := repo{base: base, change: change}.run(t, "provenance-change-recorded")
	out := make([]want, len(got))
	for i, f := range got {
		out[i] = want{path: f.Path, line: f.Line, what: f.Sentence, fix: f.Fix}
	}
	return out
}

func whats(ws []want) string {
	var b strings.Builder
	for _, w := range ws {
		b.WriteString(w.path + ": " + w.what + "\n")
	}
	return b.String()
}

func TestProvenanceChangeRecordedRewordedRuleOwesAndTheEntryClearsIt(t *testing.T) {
	reworded := map[string]string{pk + "RULES.md": "- **Doing a thing** — the settled way, said better. (doing-thing)\n\n- **Doing another** — plainly.\n  (doing-another)\n"}
	expect(t, repo{base: filled(), change: reworded}.run(t, "provenance-change-recorded"),
		want{path: pk + "RULES.md", line: 1, what: `"Doing a thing" reads differently from the base, and .*doing-thing\.md gained no entry`, fix: `cn provenance append mypack doing-thing`})
	expect(t, repo{base: filled(), change: with(reworded, map[string]string{pk + "provenance/doing-thing.md": bornEntry + rewordedEntry})}.run(t, "provenance-change-recorded"))
}

func TestProvenanceChangeRecordedManifestOwesOnDataNotLayout(t *testing.T) {
	base := with(filled(), map[string]string{pk + "pack.json": "{ \"requires\": [] }\n"})
	expect(t, repo{base: base, change: map[string]string{pk + "pack.json": "{ \"requires\": [\"acme-pack\"] }\n"}}.run(t, "provenance-change-recorded"),
		want{path: pk + "pack.json", what: `the manifest changed`})
	expect(t, repo{base: base, change: map[string]string{pk + "pack.json": "{\n  \"requires\": []\n}\n"}}.run(t, "provenance-change-recorded"))
}

func TestProvenanceChangeRecordedRewrapMarkerAndBodyOweNothing(t *testing.T) {
	expect(t, repo{base: filled(), change: map[string]string{
		pk + "RULES.md":              "- **Doing a thing** — the settled\n  way. (doing-thing)\n\n- **Doing another** — plainly. (doing-another)\n",
		pk + "skills/rules/SKILL.md": "---\nname: rules\nmetadata:\n  body: guidelines\n---\n\n- **Guideline one** — do it.\n  (guideline-one)\n",
	}}.run(t, "provenance-change-recorded"))
	unbodied := with(filled(), map[string]string{pk + "skills/how/SKILL.md": "---\nname: how\n---\n\n1. First. (3)\n"})
	expect(t, repo{base: unbodied, change: map[string]string{pk + "skills/how/SKILL.md": cleanPack[pk+"skills/how/SKILL.md"]}}.run(t, "provenance-change-recorded"))
}

func TestProvenanceChangeRecordedEachCarrierKindOwes(t *testing.T) {
	got := changeRecorded(t, filled(), map[string]string{
		pk + "RULES.md":                  filled()[pk+"RULES.md"] + "\n- **Doing a third** — newly. (doing-third)\n",
		pk + "provenance/doing-third.md": "",
		pk + "skills/how/SKILL.md":       "---\nname: how\nmetadata:\n  body: workflow\n---\n\n1. First, differently. (3)\n",
		pk + "worldRules/my-rule.mjs":    "const rule = { id: 'my/rule', on_fail: 'advise' };\nexport default rule;\n",
		pk + "declared-checks.json":      "[{ \"id\": \"declared-one\", \"on_fail\": \"block\", \"failureMessage\": \"m\" }]\n",
		pk + "tasks/nightly/task.json":   "{ \"automerge\": [\"nothing\"] }\n",
		pk + "pack.json":                 "{ \"requires\": [\"acme-pack\"] }\n",
	})
	all := whats(got)
	if len(got) != 6 {
		t.Fatalf("%d findings, want 6:\n%s", len(got), all)
	}
	for _, w := range []string{`"Doing a third" is new`, `skill how changed`, `check my/rule changed`, `check declared-one changed`, `task nightly changed`, `the manifest changed`} {
		if !strings.Contains(all, w) {
			t.Errorf("no finding says %q:\n%s", w, all)
		}
	}
	expect(t, repo{base: filled(), change: map[string]string{
		pk + "worldRules/my-rule.mjs": "// a comment\nconst rule = { id: 'my/rule', on_fail: 'block' };\nexport default rule;\n",
		pk + "pack.json":              "{\n}\n",
	}}.run(t, "provenance-change-recorded"))
}

func TestProvenanceChangeRecordedAProvenanceFileIsMeantToGrow(t *testing.T) {
	expect(t, repo{base: filled(), change: map[string]string{pk + "provenance/doing-thing.md": strings.Replace(bornEntry, "failed twice", "failed thrice", 1)}}.run(t, "provenance-change-recorded"),
		want{path: pk + "provenance/doing-thing.md", what: `lost or altered a line it had at the base`, fix: `leave it where the rewrite is the correct history`, advise: true})
	expect(t, repo{base: filled(), change: map[string]string{pk + "provenance/doing-thing.md": bornEntry + rewordedEntry}}.run(t, "provenance-change-recorded"))
}

func TestProvenanceChangeRecordedGuidelinesOweOnTheSkillsFile(t *testing.T) {
	base := with(filled(), map[string]string{pk + "skills/rules/SKILL.md": guidelines})
	expect(t, repo{base: base, change: map[string]string{pk + "skills/rules/SKILL.md": strings.Replace(guidelines, "do it too", "do it as well", 1)}}.run(t, "provenance-change-recorded"),
		want{path: pk + "skills/rules/SKILL.md", line: 8, what: `"Guideline two" reads differently from the base, and .*provenance/rules\.md gained no entry`})
	withoutThree := strings.Replace(guidelines, "- **Guideline three** — and this.\n", "", 1)
	expect(t, repo{base: base, change: map[string]string{pk + "skills/rules/SKILL.md": withoutThree}}.run(t, "provenance-change-recorded"),
		want{path: pk + "provenance/rules.md", what: `guideline "Guideline three" is gone from .*mypack in this change, and .*provenance/rules\.md gained no entry`})
	expect(t, repo{base: base, change: map[string]string{pk + "skills/rules/SKILL.md": withoutThree, pk + "provenance/rules.md": bornEntry + rewordedEntry}}.run(t, "provenance-change-recorded"))
}

func TestProvenanceChangeRecordedTwoRulesSharingAFile(t *testing.T) {
	base := with(filled(), map[string]string{pk + "RULES.md": sharedRules})
	kept := "- **Doing a thing** — the settled way. (doing-thing)\n\n- **Doing another** — plainly.\n  (doing-another)\n"
	expect(t, repo{base: base, change: map[string]string{pk + "RULES.md": kept}}.run(t, "provenance-change-recorded"),
		want{path: pk + "provenance/doing-thing.md", what: `rule "Doing it again" is gone from .*mypack in this change, and .*provenance/doing-thing\.md gained no entry`})
	expect(t, repo{base: base, change: map[string]string{pk + "RULES.md": kept, pk + "provenance/doing-thing.md": bornEntry + rewordedEntry}}.run(t, "provenance-change-recorded"))
}

func TestProvenanceChangeRecordedADeletedCarrierRetiresItsFile(t *testing.T) {
	without := map[string]string{pk + "RULES.md": "- **Doing another** — plainly.\n  (doing-another)\n"}
	expect(t, repo{base: filled(), change: without}.run(t, "provenance-change-recorded"),
		want{path: pk + "provenance/doing-thing.md", what: `rule "Doing a thing" is gone from .*mypack in this change, and its file's last entry is not retired`, fix: `retired`})
	expect(t, repo{base: filled(), change: with(without, map[string]string{pk + "provenance/doing-thing.md": bornEntry + "\n## 2026-09-01 · retired · superseded by the canon\n- **Actor:** @x (owner).\n"})}.run(t, "provenance-change-recorded"))
	// A deleted check module is the same decision, the file gone whole.
	expect(t, repo{base: filled(), change: map[string]string{pk + "worldRules/my-rule.mjs": gone}}.run(t, "provenance-change-recorded"),
		want{path: pk + "provenance/my-rule.md", what: `check my/rule is gone from .*mypack in this change, and its file's last entry is not retired`})
}

func TestProvenanceChangeRecordedAPackComingOntoTheConventionOwesNothing(t *testing.T) {
	base := map[string]string{}
	for k, v := range filled() {
		if !strings.Contains(k, "/provenance/") {
			base[k] = v
		}
	}
	base[pk+"RULES.md"] = "- **Doing a thing** — the settled way.\n\n- **Doing another** — plainly.\n"
	expect(t, repo{base: base, change: cleanPack}.run(t, "provenance-change-recorded"))
}

func TestProvenanceChangeRecordedIsInertOnMainAndOutsidePacks(t *testing.T) {
	expect(t, repo{base: filled(), change: map[string]string{"src/app.js": "y\n"}}.run(t, "provenance-change-recorded"))
	expect(t, repo{base: filled()}.run(t, "provenance-change-recorded"))
}

func TestProvenanceChangeRecordedAGoCheckOwesOnCodeNotComments(t *testing.T) {
	base := with(filled(), map[string]string{
		pk + "checks/mine.go":     "package checks\n\nvar Mine = Check{ID: \"mine\", OnFail: \"block\"}\n",
		pk + "provenance/mine.md": bornEntry,
	})
	expect(t, repo{base: base, change: map[string]string{pk + "checks/mine.go": "package checks\n\nvar Mine = Check{ID: \"mine\", OnFail: \"advise\"}\n"}}.run(t, "provenance-change-recorded"),
		want{path: pk + "checks/mine.go", what: `check mine changed`})
	expect(t, repo{base: base, change: map[string]string{pk + "checks/mine.go": "package checks\n\n// Mine is mine.\nvar Mine = Check{ID: \"mine\", OnFail: \"block\"}\n"}}.run(t, "provenance-change-recorded"))
}

// A manifest owes an entry when its data moves, in each of the three
// formats, and never for layout or a comment.
func TestProvenanceChangeRecordedManifestFormats(t *testing.T) {
	jsonBase := with(filled(), map[string]string{pk + "pack.json": "{ \"requires\": [], \"version\": \"1.0.0\" }\n"})
	expect(t, repo{base: jsonBase, change: map[string]string{pk + "pack.json": "{\n    \"requires\": [],\n    \"version\": \"1.0.0\"\n}\n"}}.run(t, "provenance-change-recorded"))
	// JSON carries no comments: a // line is not a comment to skip but a
	// manifest that no longer parses, which is a change.
	expect(t, repo{base: jsonBase, change: map[string]string{pk + "pack.json": "{ \"requires\": [], \"version\": \"1.0.0\" }\n// note\n"}}.run(t, "provenance-change-recorded"),
		want{path: pk + "pack.json", what: `the manifest changed`})

	tomlBase := with(filled(), map[string]string{pk + "pack.toml": "requires = []\nversion = \"1.0.0\"\n"})
	expect(t, repo{base: tomlBase, change: map[string]string{pk + "pack.toml": "# the pack's manifest\nrequires = [] # none yet\nversion = \"1.0.0\"\n"}}.run(t, "provenance-change-recorded"))

	yamlBase := with(filled(), map[string]string{pk + "pack.yaml": "requires: []\nversion: 1.0.0\n"})
	expect(t, repo{base: yamlBase, change: map[string]string{pk + "pack.yaml": "requires: []\nversion: 1.1.0\n"}}.run(t, "provenance-change-recorded"),
		want{path: pk + "pack.yaml", what: `the manifest changed`})
	expect(t, repo{base: yamlBase, change: map[string]string{pk + "pack.yaml": "# manifest\nrequires: [ ]\nversion: 1.0.0\n"}}.run(t, "provenance-change-recorded"))
}
