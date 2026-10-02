package builtin

import (
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/growth"
)

func TestDedupPruneIntegrity(t *testing.T) {
	rules := ".claudinite/local/packs/mypack/RULES.md"
	base := map[string]string{rules: "# mypack\n\n- **A** — b.\n\n- **W** — check v before w, every time.\n", ".claudinite/local/packs/mypack/provenance/w.md": "## 2026-09-01 · born · w\n- **Mechanism:** prose.\n"}
	restated := map[string]string{rules: "# mypack\n\n- **A** — b.\n\n- **W** — this rule is portable (canon): the basics pack owns it.\n  Check v before w, every time.\n"}
	expect(t, repo{base: base, change: restated, message: growth.SubjectDedup}.run(t, "dedup-prune-integrity"),
		want{path: rules, what: `^a dedup run grew .*RULES\.md from 6 to 7 lines`},
		want{path: rules, line: 5, what: `^local-pack prose re-imports a canon rule: "- \*\*W\*\* — this rule is portable \(canon\)`, fix: `\(canon\): here`})
	expect(t, repo{base: base, change: restated, message: "change"}.run(t, "dedup-prune-integrity"),
		want{path: rules, line: 5, what: "re-imports a canon rule"})
	pruned := map[string]string{rules: "# mypack\n\n- **A** — b.\n", ".claudinite/local/packs/mypack/provenance/w.md": "## 2026-09-01 · born · w\n- **Mechanism:** prose.\n\n## 2026-10-01 · retired · the canon covers it\n- **Reason:** basics carries it.\n"}
	expect(t, repo{base: base, change: pruned, message: growth.SubjectDedup}.run(t, "dedup-prune-integrity"))
	rewrapped := map[string]string{rules: "# mypack\n\n- **A** — b.\n\n- **W** — check v before w, every single time, whatever.\n"}
	expect(t, repo{base: base, change: rewrapped, message: growth.SubjectDedup}.run(t, "dedup-prune-integrity"),
		want{path: rules, what: "characters"})
	reaching := map[string]string{rules: "# mypack\n\n- **A** — b, longer now than it was.\n\n- **W** — check v before w, every time.\n", "README.md": "fix the dedup routine\n"}
	expect(t, repo{base: base, change: reaching, message: "Fix the dedup routine"}.run(t, "dedup-prune-integrity"))
}
