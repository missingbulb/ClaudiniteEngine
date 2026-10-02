package growth

import (
	"strings"
	"testing"
)

// Each value is copied from missingbulb/Claudinite@057841ac:
//
//	RunSubject     packs/claudinite-growth/workRules/growth-write-scope.mjs GROWTH_RUN
//	DedupSubject   packs/claudinite-growth/workRules/dedup-integrity.mjs DEDUP_RUN
//	RestatesCanon  packs/claudinite-growth/workRules/dedup-integrity.mjs RESTATES_CANON
//	SubjectExtract             packs/claudinite-growth/tasks/growth-extract/task.md
//	SubjectDedup               packs/claudinite-growth/tasks/growth-dedup/task.md
//	SubjectProseToChecks       packs/claudinite-growth/tasks/prose-to-checks-sweep/task.md
//	SubjectRuleRevalidation    packs/claudinite-growth/tasks/rule-revalidation/task.md
//	SubjectConversationExtract growth-write-scope.mjs, the pre-merge title it still accepts
func TestTheSourcesAreCopiedVerbatim(t *testing.T) {
	for re, src := range map[string]string{
		RunSubject.String():    `^Claudinite growth: (?:extract lessons|conversation extract|dedup\b|prose to checks|rule revalidation)`,
		DedupSubject.String():  `(?i)\bdedup\b|\bcanon now (?:covers|owns)\b`,
		RestatesCanon.String(): `(?i)\b(?:is|are) portable\s*\(canon\)|\bpack owns\b|\bcanon (?:now )?owns\b|\bowned by (?:the )?canon\b`,
	} {
		if re != src {
			t.Errorf("%s is not its source %s", re, src)
		}
	}
}

func TestEveryPinnedSubjectIsARun(t *testing.T) {
	for _, s := range []string{SubjectExtract, SubjectConversationExtract, SubjectDedup, SubjectProseToChecks, SubjectRuleRevalidation} {
		if !RunSubject.MatchString(s + " (#12)") {
			t.Errorf("%q is not matched by RunSubject", s)
		}
		if !strings.HasPrefix(s, "Claudinite growth: ") {
			t.Errorf("%q does not carry the lifecycle's prefix", s)
		}
	}
	for _, s := range []string{"Claudinite growth: capture log", "Claudinite canon: dedup", "growth: extract lessons"} {
		if RunSubject.MatchString(s) {
			t.Errorf("%q is matched by RunSubject", s)
		}
	}
	if !DedupSubject.MatchString(SubjectDedup) || !DedupSubject.MatchString("the canon now covers it") || DedupSubject.MatchString("deduplicate") {
		t.Error("DedupSubject")
	}
	for s, want := range map[string]bool{
		"This rule is portable (canon): the basics pack owns it.": true,
		"(canon): here only the residue":                          false,
		"Owned by the canon now.":                                 true,
		"the canon now owns this":                                 true,
	} {
		if RestatesCanon.MatchString(s) != want {
			t.Errorf("RestatesCanon(%q) != %v", s, want)
		}
	}
}
