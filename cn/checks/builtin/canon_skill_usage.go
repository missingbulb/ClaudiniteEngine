package builtin

import (
	"regexp"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/skillfm"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/transcript"
)

// A corpus SKILL.md declares the usage it expects of itself under its
// frontmatter metadata.usage, read by the reader the usage review uses.
// Without it, a skill that never loads and one that is never needed read
// the same zero. A skill expecting "triggered" must carry a force-load
// declaration to be triggered by; the converse is no fault, since a skill
// may carry a trigger and still expect most loads by judgment. Anchored at
// packs/<pack>/skills/, so it is inert in a repo with no shelf.
var skillUsage = declared.Builtin{
	ID:     "skill-usage-declared",
	Pack:   "claudinite-canon-curation",
	OnFail: "block",
	Tags:   []string{"world", "builtin", "claudinite-canon-curation"},
	Since:  "2026-09-21",
	Doc:    "packs/claudinite-canon-curation/skills/writing-claudinite-skills/SKILL.md",
	Why:    "without a declared expectation, a skill that never loads and one that is never needed read the same zero, and the usage review cannot tell a broken skill from a healthy one",
}

func init() { register(&skillUsage, runSkillUsage) }

var forceLoadKey = regexp.MustCompile(`(?m)^\s*force-load-on-\S+:`)

// triggerCount is how many force-load keys the frontmatter carries, read
// as text up to the closing fence.
func triggerCount(text string) int {
	end := -1
	if len(text) > 3 {
		if i := strings.Index(text[3:], "\n---"); i >= 0 {
			end = i + 3
		}
	}
	return len(forceLoadKey.FindAllStringIndex(text[:end+1], -1))
}

func runSkillUsage(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
	expects := strings.Join(skillfm.Expects, " | ")
	var out []findings.Finding
	for _, f := range ctx.Files() {
		if !skillDoc.MatchString(f) {
			continue
		}
		text, ok := ctx.Read(f)
		if !ok {
			continue
		}
		u := skillfm.UsageOf(skillfm.Parse(text))
		if u == nil {
			out = append(out, skillUsage.Finding(f, 0, "declares no metadata.usage block",
				`add one under metadata - "usage:" then "expect:" one of `+expects+`; a skill that loads at its own force-load moments and nowhere else is "triggered"`))
			continue
		}
		if u.Expect == "triggered" && triggerCount(text) == 0 {
			out = append(out, skillUsage.Finding(f, 0, `expects "triggered" and declares no force-load trigger to be triggered by`,
				`declare the force-load moment it loads at, or expect "judgment" - loaded when the model judges its description fits`))
		}
		for _, p := range u.Problems {
			out = append(out, skillUsage.Finding(f, 0, "its metadata.usage "+p, "correct the block - the usage review reads it exactly as this check does"))
		}
	}
	return out
}
