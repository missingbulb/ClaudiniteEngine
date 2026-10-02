package builtin

import (
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/missingbulb/ClaudiniteEngine/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/shared/growth"
	"github.com/missingbulb/ClaudiniteEngine/shared/provenance"
	"github.com/missingbulb/ClaudiniteEngine/shared/transcript"
)

// The backstop for the dedup task's rule that a dedup edit only removes
// portable text. Two signals: an added local-pack prose line restating a
// canon rule is a corruption on any branch; and a dedup run, one whose
// commits announce a dedup and whose change stays inside the local packs
// (a change fixing the routine says dedup too, and reaches outside), must
// shrink each prose file it modifies, in lines or, past a re-wrap, in
// characters. A pack's provenance files always grow on a prune, so they
// are exempt from the shrink measure, never from the fingerprint.
var dedupPruneIntegrity = declared.Builtin{
	ID:     "dedup-prune-integrity",
	Pack:   "claudinite-growth",
	OnFail: "block",
	Tags:   []string{"work", "builtin", "claudinite-growth"},
	Doc:    "packs/claudinite-growth/skills/growth-dedup/SKILL.md",
	Why:    "the growth-dedup routine has reworded partially-covered items instead of stripping them — restating the canon rule inside the local pack, the inverse of dedup — and every dedup edit must shrink the pack, not grow it",
}

func init() { register(&dedupPruneIntegrity, runDedupPruneIntegrity) }

func isLocalPackProse(f string) bool {
	return strings.HasSuffix(f, ".md") && strings.HasPrefix(f, growth.LocalPacks)
}

func isProvenance(f string) bool {
	parts := strings.Split(strings.TrimPrefix(f, growth.LocalPacks), "/")
	return len(parts) > 1 && parts[1] == provenance.Dir
}

// jsLength is a string's length as JavaScript counts it, in UTF-16 code
// units, which the Node rule compared.
func jsLength(s string) int { return len(utf16.Encode([]rune(s))) }

// jsSlice80 is JavaScript's s.slice(0, 80).
func jsSlice80(s string) string {
	u := utf16.Encode([]rune(s))
	if len(u) > 80 {
		u = u[:80]
	}
	return string(utf16.Decode(u))
}

func runDedupPruneIntegrity(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
	if onDefaultBranch(ctx) {
		return nil
	}
	var prose []string
	for _, f := range ctx.ChangedFiles() {
		if isLocalPackProse(f) {
			prose = append(prose, f)
		}
	}
	if len(prose) == 0 {
		return nil
	}
	var out []findings.Finding
	for _, f := range prose {
		for _, l := range ctx.AddedLines(f) {
			if growth.RestatesCanon.MatchString(l.Text) {
				out = append(out, dedupPruneIntegrity.Finding(f, l.Line,
					fmt.Sprintf(`local-pack prose re-imports a canon rule: "%s"`, jsSlice80(strings.TrimSpace(l.Text))),
					`delegate the portable rule to the canon and keep only this project's residue — never restate the canon rule, its fix, or which pack owns it (use the pack's "(canon): here …" convention)`))
			}
		}
	}
	confined := true
	for _, f := range ctx.ChangedFiles() {
		confined = confined && strings.HasPrefix(f, growth.LocalPacks)
	}
	if !confined || !anyMatches(ctx.Commits(), growth.DedupSubject.MatchString) {
		return out
	}
	for _, f := range prose {
		if isProvenance(f) {
			continue
		}
		base, okBase := ctx.ReadBase(f)
		head, okHead := ctx.Read(f)
		if !okBase || !okHead {
			continue
		}
		bl, hl := strings.Count(base, "\n")+1, strings.Count(head, "\n")+1
		grew := ""
		switch {
		case hl > bl:
			grew = fmt.Sprintf("from %d to %d lines", bl, hl)
		case jsLength(head) > jsLength(base):
			grew = fmt.Sprintf("from %d to %d characters", jsLength(base), jsLength(head))
		}
		if grew != "" {
			out = append(out, dedupPruneIntegrity.Finding(f, 0,
				fmt.Sprintf("a dedup run grew %s %s — a prune/strip removes duplicated text, it never grows the pack", f, grew),
				"strip each covered item down to its project residue (a deletion that shrinks the entry); if you are keeping an item, leave it unchanged rather than rewording it"))
		}
	}
	return out
}
