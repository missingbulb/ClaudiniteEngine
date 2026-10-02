package builtin

import (
	"sort"

	"github.com/missingbulb/ClaudiniteEngine/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/shared/mergepolicy"
	"github.com/missingbulb/ClaudiniteEngine/shared/transcript"
	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
)

// The armed-auto-merge gate: a branch whose newest trailer says it lands
// itself under a granular policy must carry a diff inside that policy,
// re-judged at the Stop hook and on every pull request, so a run that
// mis-measured its own diff goes red before a queued auto-merge fires.
// No trailer, no findings: a wide branch left for review stays green. The
// policy is read from the trailer rather than the declaration, because the
// branch may be the thing changing declarations; the policy's own refusal
// to cover a policy source keeps the trailer honest.
var automergePolicyScope = declared.Builtin{
	ID:     "automerge-policy-scope",
	Pack:   workitem.TasksPackID,
	OnFail: "block",
	Tags:   []string{"work", "builtin", workitem.TasksPackID},
	Doc:    "packs/claudinite-tasks/README.md",
	Why:    "the trailer is a claim that this diff may merge with nobody looking; a diff outside the declared classes is exactly the unreviewed change the policy exists to stop, and only a check can hold the claim to the measurement",
}

func init() { register(&automergePolicyScope, runAutomergePolicyScope) }

func runAutomergePolicyScope(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
	if onDefaultBranch(ctx) {
		return nil
	}
	armed := ""
	for _, m := range ctx.Commits() {
		if hit := mergepolicy.TrailerRe.FindStringSubmatch(m); hit != nil && hit[1] != "" {
			armed = hit[1]
			break
		}
	}
	if armed == "" {
		return nil
	}
	deleted := map[string]bool{}
	for _, f := range ctx.Deleted() {
		deleted[f] = true
	}
	files := unique(ctx.ChangedFiles(), ctx.Deleted())
	sort.Strings(files)
	entries := make([]mergepolicy.Entry, len(files))
	for i, f := range files {
		e := mergepolicy.Entry{File: f}
		if before, ok := ctx.ReadBase(f); ok {
			e.Before = &before
		}
		if after, ok := ctx.Read(f); ok && !deleted[f] {
			e.After = &after
		}
		entries[i] = e
	}
	verdict := mergepolicy.Judge(armed, entries, mergepolicy.DeclaredBy(ctx.Config.Packs))
	if verdict.Mergeable {
		return nil
	}
	var out []findings.Finding
	for _, p := range verdict.Problems {
		file := "(branch)"
		fix := "make the diff satisfy the armed policy, or drop the trailer and leave the PR for review"
		if p.File != nil {
			file = *p.File
			fix = "revert or split out " + file + ", or drop the trailer from the branch's last commit and leave the PR for review — never widen the policy to fit the diff"
		}
		out = append(out, automergePolicyScope.Finding(file, 0,
			"this branch armed auto-merge ("+workitem.AutomergeTrailer+": "+armed+") but "+p.What, fix))
	}
	return out
}
