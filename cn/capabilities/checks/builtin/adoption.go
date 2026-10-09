package builtin

import (
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/transcript"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/interview"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/settings"
)

// The adoption interview's two checks. A pending answer is a SessionStart
// line everywhere except the branch that declares the pack: the person
// is present there by construction, so a pack left unanswered is a work
// defect that branch fixes. A stored answer its pack no longer asks is
// advisory, so a question renamed in a pack never fails a member's CI.
var adoptionAnswersPending = declared.Builtin{
	ID:     "adoption-answers-pending",
	Pack:   "claudinite-lifecycle",
	OnFail: "block",
	Tags:   []string{"work", "builtin", "claudinite-lifecycle"},
	Doc:    "packs/claudinite-lifecycle/README.md",
	Why:    "a pack that asks the project for its intent is a no-op until answered, and the adding branch is where the owner is present to answer — so that is where the answer is required",
}

var interviewAnswerStale = declared.Builtin{
	ID:     "interview-answer-stale",
	Pack:   "claudinite-lifecycle",
	OnFail: "advise",
	Tags:   []string{"world", "builtin", "claudinite-lifecycle"},
	Doc:    "packs/claudinite-lifecycle/README.md",
	Why:    "a stale answer silently stops matching its question, so the stored intent goes unread and the interview re-asks",
}

func init() {
	register(&adoptionAnswersPending, runAdoptionAnswersPending)
	register(&interviewAnswerStale, runInterviewAnswerStale)
}

// settingsPacks parses the packs block of the settings text read.
func settingsPacks(ctx *declared.Ctx, text string) (settings.Packs, bool) {
	for _, f := range settings.Formats {
		if settings.RelPath(f) != ctx.Config.SettingsPath {
			continue
		}
		p, err := settings.ParseFile([]byte(text), f)
		if err != nil {
			return settings.Packs{}, false
		}
		return p.Packs, true
	}
	return settings.Packs{}, false
}

// writtenIDs are the declared entries as written, local/ prefix kept.
func writtenIDs(p settings.Packs) map[string]bool {
	out := map[string]bool{}
	for _, e := range p.Entries {
		out[e.Token()] = true
	}
	return out
}

// runAdoptionAnswersPending judges the entries the change adds by their
// written spelling against the bare pack id, as the Node rule did, so an
// added local/<name> entry never matches.
func runAdoptionAnswersPending(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
	if onDefaultBranch(ctx) {
		return nil
	}
	text, ok := ctx.Read(ctx.Config.SettingsPath)
	if !ok {
		return nil
	}
	head, ok := settingsPacks(ctx, text)
	if !ok {
		return nil
	}
	var base map[string]bool
	if raw, ok := ctx.ReadBase(ctx.Config.SettingsPath); ok {
		if b, ok := settingsPacks(ctx, raw); ok {
			base = writtenIDs(b)
		}
	}
	added := map[string]bool{}
	for id := range writtenIDs(head) {
		if !base[id] {
			added[id] = true
		}
	}
	if len(added) == 0 {
		return nil
	}
	pending, _ := interview.State(packset.Set{Declared: head, Packs: ctx.Config.Packs})
	var out []findings.Finding
	for _, p := range pending {
		if !added[p.Pack.ID] {
			continue
		}
		for _, q := range p.Questions {
			fix := "ask the owner and record it with `cn settings answer " + p.Pack.ID + "/" + q.ID + " <answer>` (\"n/a — none wanted\" is an answer)"
			if q.Distill != "" {
				fix += " — " + strings.Join(strings.Fields(q.Distill), " ")
			}
			out = append(out, adoptionAnswersPending.Finding(ctx.Config.SettingsPath, 0,
				"the newly declared \""+p.Pack.ID+"\" pack asks \""+q.ID+"\" but its entry records no answer", fix))
		}
	}
	return out
}

func runInterviewAnswerStale(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
	text, ok := ctx.Read(ctx.Config.SettingsPath)
	if !ok {
		return nil
	}
	head, ok := settingsPacks(ctx, text)
	if !ok {
		return nil
	}
	_, stale := interview.State(packset.Set{Declared: head, Packs: ctx.Config.Packs})
	var out []findings.Finding
	for _, s := range stale {
		out = append(out, interviewAnswerStale.Finding(ctx.Config.SettingsPath, 0,
			"the \""+s.Pack.ID+"\" pack entry stores an answer for \""+s.Answer+"\", a question the pack no longer declares",
			"remove the stale answer, or re-key it to the renamed question id"))
	}
	return out
}
