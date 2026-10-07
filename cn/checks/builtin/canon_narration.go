package builtin

import (
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/provenance"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/transcript"
)

// A canon pack's injected prose and its corpus skills must not narrate
// their own enforcement: checks run on their own at every Stop and in CI,
// and each failure message carries its rule. The pack scan reads exactly
// the file each shelf manifest names as its prose, never the README, whose
// catalog lists the rules and how each is enforced; the skill scan reads
// every SKILL.md under packs/<pack>/skills/, on a shelf or in a member's
// own packs, and never a mounted .claude/skills/. Both are silent in a
// repo that keeps no shelf, which carries neither.
var packNarration = declared.Builtin{
	ID:     "pack-no-enforcement-narration",
	Pack:   "claudinite-canon-curation",
	OnFail: "block",
	Tags:   []string{"world", "builtin", "claudinite-canon-curation"},
	Doc:    "packs/claudinite-canon-curation/README.md",
	Why:    "checks run automatically at every Stop and in CI, and each failure message carries its rule — prose narrating its own enforcement duplicates the mechanism and drifts from it",
}

var skillNarration = declared.Builtin{
	ID:     "skill-no-enforcement-narration",
	Pack:   "claudinite-canon-curation",
	OnFail: "block",
	Tags:   []string{"world", "builtin", "claudinite-canon-curation"},
	Doc:    "packs/claudinite-canon-curation/skills/writing-claudinite-skills/SKILL.md",
	Why:    "checks run automatically at every Stop and in CI, and each failure message carries its rule — a skill narrating its own enforcement duplicates the mechanism and drifts from it",
}

func init() {
	register(&packNarration, func(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
		return narration(ctx, &packNarration, proseDocs(ctx))
	})
	register(&skillNarration, func(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
		var docs []string
		for _, f := range ctx.Files() {
			if skillDoc.MatchString(f) {
				docs = append(docs, f)
			}
		}
		return narration(ctx, &skillNarration, docs)
	})
}

var (
	runnerRe   = regexp.MustCompile(`checks/run\.mjs`)
	shelfManRe = regexp.MustCompile(`^packs/[^/]+/pack\.(?:json|mjs)$`)
	proseRe    = regexp.MustCompile(`(?:^|[\s{,])"?prose"?:\s*['"]([^'"]+)['"]`)
	coreIDRe   = regexp.MustCompile(`\bid:\s*'([a-z][\w-]+)'`)
	// skillDoc spans a shelf's packs/ and a member's .claudinite/local/packs/.
	skillDoc = regexp.MustCompile(`(^|/)packs/[^/]+/skills/[^/]+/SKILL\.md$`)
)

// proseDocs are the files the shelf's manifests name as their prose.
func proseDocs(ctx *declared.Ctx) []string {
	var docs []string
	for _, f := range ctx.Files() {
		if !shelfManRe.MatchString(f) {
			continue
		}
		text, _ := ctx.Read(f)
		if m := proseRe.FindStringSubmatch(text); m != nil {
			docs = append(docs, path.Join(path.Dir(f), m[1]))
		}
	}
	return docs
}

// narration flags each doc's lines that tell the reader to run the checks
// runner or name a rule its directory's own coded checks declare.
func narration(ctx *declared.Ctx, b *declared.Builtin, docs []string) []findings.Finding {
	var out []findings.Finding
	lines := func(doc string, re *regexp.Regexp, what, fix string) {
		text, ok := ctx.Read(doc)
		if !ok {
			return
		}
		for i, l := range strings.Split(text, "\n") {
			if re.MatchString(l) {
				out = append(out, b.Finding(doc, i+1, what, fix))
			}
		}
	}
	for _, doc := range docs {
		lines(doc, runnerRe, "tells the reader to run the checks runner", "delete the instruction — the Stop hook and CI run every check on their own")
	}
	for _, doc := range docs {
		for _, id := range codedIDsIn(ctx, path.Dir(doc)) {
			word := regexp.MustCompile(`(^|[^\w-])` + regexp.QuoteMeta(id) + `([^\w-]|$)`)
			lines(doc, word, `names its own check rule "`+id+`"`, "remove the mention — the rule announces itself when it fires, and its failure message carries the instruction")
		}
	}
	return out
}

// codedIDsIn are the rule ids dir's coded checks declare, sorted: the
// JavaScript modules directly in dir, and the Go package in dir/checks
// (its _test.go files aside), sit beside the prose they would narrate.
func codedIDsIn(ctx *declared.Ctx, dir string) []string {
	seen := map[string]bool{}
	var goSources []string
	for _, f := range ctx.Files() {
		if path.Dir(f) == dir+"/"+provenance.GoChecksDir && strings.HasSuffix(f, ".go") && !strings.HasSuffix(f, "_test.go") {
			if text, ok := ctx.Read(f); ok {
				goSources = append(goSources, text)
			}
		}
	}
	for _, id := range provenance.GoCheckIDs(goSources) {
		seen[id] = true
	}
	for _, f := range ctx.Files() {
		if path.Dir(f) != dir || !strings.HasSuffix(f, ".mjs") || strings.HasSuffix(f, "/pack.mjs") || strings.HasSuffix(f, ".test.mjs") {
			continue
		}
		text, ok := ctx.Read(f)
		if !ok {
			continue
		}
		for _, m := range coreIDRe.FindAllStringSubmatch(text, -1) {
			seen[m[1]] = true
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
