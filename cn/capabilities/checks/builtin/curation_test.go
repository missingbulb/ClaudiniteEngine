package builtin

import (
	"testing"
)

// curationSettings is a fleet manager: canon curation runs where the
// fleet block is.
const curationSettings = settingsYAML + "fleet:\n  owner: acme\n  writePaths:\n    - skills\n"

func TestPackVersionLogOrdered(t *testing.T) {
	t.Parallel()
	r := repo{settings: curationSettings, base: map[string]string{
		"packs/acme-pack/provenance/VERSIONS.md":  "# Version history\n\n| Version | Date | What changed |\n| --- | --- | --- |\n| 61003.2 | d | x |\n| 61003.1 | d | x |\n| 61002.4 | d | x |\n| 61003.3 | d | x |\n| 1.0 | d | not a version |\n| 60900.1 | d | x |\n",
		"packs/good-pack/provenance/VERSIONS.md":  "| 61003.2 | d | x |\n| 61003.1 | d | x |\n",
		"docs/provenance/VERSIONS.md":             "| 1 | d | x |\n| 2 | d | x |\n",
		"packs/new-pack/provenance/VERSIONS.md":   "| 1.61004.10 | d | x |\n| 1.61004.2 | d | x |\n| 61003.3 | d | x |\n| 1.61004.1 | d | x |\n",
		"packs/mixed-pack/provenance/VERSIONS.md": "| 2.60101.1 | d | x |\n| 1.61231.9 | d | x |\n| 61099.9 | d | x |\n| 61003 | d | x |\n",
	}}
	expect(t, r.run(t, "pack-version-log-ordered"),
		want{path: "packs/acme-pack/provenance/VERSIONS.md", line: 8, what: `^version 61003\.3 sits below 61002\.4 \(line 7\)`},
		want{path: "packs/new-pack/provenance/VERSIONS.md", line: 4, what: `^version 1\.61004\.1 sits below 61003\.3 \(line 3\)`})
}

// On a promote branch every path changed, deleted or left untracked since
// the merge base lies under packs/ or a root the fleet block's writePaths
// names; any other branch writes anything.
func TestPromoteScope(t *testing.T) {
	t.Parallel()
	base := map[string]string{"packs/p/RULES.md": "a\n", "skills/s.md": "s\n", "engine/e.mjs": "e\n", "README.md": "r\n"}
	change := map[string]string{"packs/p/RULES.md": "b\n", "skills/s.md": "s2\n", "engine/e.mjs": "e2\n", "README.md": gone}
	untracked := map[string]string{"packsx/a.md": "x\n"}
	promote := repo{settings: curationSettings, base: base, change: change, untracked: untracked, branch: "claudinite/growth-promote-1"}
	expect(t, promote.run(t, "promote-scope"),
		want{path: "README.md", what: `^the promote phase touched README\.md, outside packs and skills$`, fix: "home it in a pack"},
		want{path: "engine/e.mjs", what: "touched engine/e.mjs"},
		want{path: "packsx/a.md", what: "touched packsx/a.md"})
	other := repo{settings: curationSettings, base: base, change: change, untracked: untracked, branch: "claude/fix"}
	expect(t, other.run(t, "promote-scope"))
}

// Canon curation's checks run only on a fleet manager.
func TestCurationChecksRunOnlyOnAFleetManager(t *testing.T) {
	t.Parallel()
	set, err := loadSet(repo{}.build(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range set.Builtins {
		if b.ID == "pack-version-log-ordered" || b.ID == "promote-scope" {
			t.Errorf("a repo with no fleet block runs %s", b.ID)
		}
	}
}
