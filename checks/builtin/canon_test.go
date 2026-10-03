package builtin

import (
	"strings"
	"testing"
)

// canon is a repo declaring claudinite-canon-curation beside the folded
// packs, its shelf under packs/.
func canon(base map[string]string) repo {
	files := map[string]string{".claudinite/shared/packs/claudinite-canon-curation/pack.json": "{\"version\": \"1\"}\n"}
	for k, v := range base {
		files[k] = v
	}
	return repo{settings: strings.Replace(settingsYAML, "    - local/mypack\n", "    - local/mypack\n    - claudinite-canon-curation\n", 1), base: files}
}

const usageBlock = "---\nname: s\ndescription: d\nmetadata:\n  usage:\n    expect: judgment\n---\n"

func TestPackNoEnforcementNarration(t *testing.T) {
	r := canon(map[string]string{
		"packs/acme-pack/pack.json":  `{"version": "1.0", "prose": "RULES.md"}` + "\n",
		"packs/acme-pack/RULES.md":   "# acme\n\n- run node engine/checks/run.mjs first\n- acme-check guards this\n- not-acme-check-ish is another word\n",
		"packs/acme-pack/check.mjs":  "export default { id: 'acme-check' };\n",
		"packs/acme-pack/README.md":  "acme-check and checks/run.mjs are the README's to list\n",
		"packs/other-pack/pack.json": `{"version": "1.0"}` + "\n",
		"packs/other-pack/RULES.md":  "run checks/run.mjs\n",
	})
	expect(t, r.run(t, "pack-no-enforcement-narration"),
		want{path: "packs/acme-pack/RULES.md", line: 3, what: "checks runner"},
		want{path: "packs/acme-pack/RULES.md", line: 4, what: `"acme-check"`})
}

func TestSkillNoEnforcementNarration(t *testing.T) {
	r := canon(map[string]string{
		"packs/acme-pack/skills/acme-skill/SKILL.md":        usageBlock + "\nRun checks/run.mjs.\nacme-skill-check fires on it.\n",
		"packs/acme-pack/skills/acme-skill/checks.mjs":      "export default { id: 'acme-skill-check' };\n",
		"packs/acme-pack/skills/acme-skill/checks.test.mjs": "const x = { id: 'never-counted' };\n",
		".claude/skills/acme-skill/SKILL.md":                "Run checks/run.mjs.\n",
	})
	expect(t, r.run(t, "skill-no-enforcement-narration"),
		want{path: "packs/acme-pack/skills/acme-skill/SKILL.md", line: 9, what: "checks runner"},
		want{path: "packs/acme-pack/skills/acme-skill/SKILL.md", line: 10, what: `"acme-skill-check"`})
}

func TestSkillUsageDeclared(t *testing.T) {
	r := canon(map[string]string{
		"packs/acme-pack/skills/a/SKILL.md": "---\nname: a\ndescription: d\n---\n",
		"packs/acme-pack/skills/b/SKILL.md": "---\nname: b\ndescription: d\nmetadata:\n  usage:\n    expect: triggered\n---\n",
		"packs/acme-pack/skills/c/SKILL.md": "---\nname: c\ndescription: d\nmetadata:\n  usage:\n    expect: sometimes\n    weight: 2\n---\n",
		"packs/acme-pack/skills/d/SKILL.md": "---\nname: d\ndescription: d\nmetadata:\n  usage: triggered\n---\n",
		"packs/acme-pack/skills/e/SKILL.md": "---\nname: e\ndescription: d\nmetadata:\n  usage:\n    expect: triggered\n  force-load-on-file-edits-paths:\n    - \"x/**\"\n---\n",
		"packs/acme-pack/skills/f/SKILL.md": usageBlock,
		"other/skills/g/SKILL.md":           "---\nname: g\n---\n",
	})
	expect(t, r.run(t, "skill-usage-declared"),
		want{path: "packs/acme-pack/skills/a/SKILL.md", what: "declares no metadata.usage block"},
		want{path: "packs/acme-pack/skills/b/SKILL.md", what: `expects "triggered" and declares no force-load`},
		want{path: "packs/acme-pack/skills/c/SKILL.md", what: "expect: sometimes is outside adoption | triggered | judgment"},
		want{path: "packs/acme-pack/skills/c/SKILL.md", what: "weight is not a key of the usage block"},
		want{path: "packs/acme-pack/skills/d/SKILL.md", what: "usage is not a block of keys"})
}

func TestPackVersionLogOrdered(t *testing.T) {
	r := canon(map[string]string{
		"packs/acme-pack/provenance/VERSIONS.md": "# Version history\n\n| Version | Date | What changed |\n| --- | --- | --- |\n| 61003.2 | d | x |\n| 61003.1 | d | x |\n| 61002.4 | d | x |\n| 61003.3 | d | x |\n| 1.0 | d | not a version |\n| 60900.1 | d | x |\n",
		"packs/good-pack/provenance/VERSIONS.md": "| 61003.2 | d | x |\n| 61003.1 | d | x |\n",
		"docs/provenance/VERSIONS.md":            "| 1 | d | x |\n| 2 | d | x |\n",
	})
	expect(t, r.run(t, "pack-version-log-ordered"),
		want{path: "packs/acme-pack/provenance/VERSIONS.md", line: 8, what: `^version 61003\.3 sits below 61002\.4 \(line 7\)`})
}
