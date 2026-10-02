package builtin

import (
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/growth"
)

func TestGrowthWriteScope(t *testing.T) {
	base := map[string]string{"README.md": "# member\n", "docs/old.md": "old\n", ".claudinite/local/packs/mypack/RULES.md": "# mypack\n"}
	outside := map[string]string{"README.md": "# member\n\nA lesson.\n", "docs/old.md": gone, ".claudinite/local/packs/mypack/RULES.md": "# mypack\n\n- **A** — b.\n"}
	expect(t, repo{base: base, change: outside, message: growth.SubjectExtract + " from the week"}.run(t, "growth-write-scope"),
		want{path: "README.md", what: `^a growth run touched README\.md, outside \.claudinite/local/packs/$`, fix: "claudinite-canon-curation"},
		want{path: "docs/old.md", what: `touched docs/old\.md`})
	inside := map[string]string{".claudinite/local/packs/mypack/RULES.md": "# mypack\n\n- **A** — b.\n"}
	expect(t, repo{base: base, change: inside, message: growth.SubjectDedup}.run(t, "growth-write-scope"))
	for _, msg := range []string{"change", "Claudinite growth: capture log", "Claudinite canon: dedup"} {
		expect(t, repo{base: base, change: outside, message: msg}.run(t, "growth-write-scope"))
	}
	expect(t, repo{base: base, untracked: map[string]string{"x.md": "x\n"}}.run(t, "growth-write-scope"))
}
