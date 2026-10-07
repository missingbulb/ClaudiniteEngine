package builtin

import (
	"regexp"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/transcript"
)

// The Stop-time backstop behind the per-call guard on the vendored mount:
// the guard sees only an agent's own tool calls, so a script, a git apply
// or a session with no hook lands an edit there unseen. This reads what
// the branch committed. It advises rather than blocks because rewriting
// the tree whole is the update task's own operation, exempt by the title
// that task composes. The scanned file set never holds the mount, so the
// branch's commits are the only surface that still sees it.
const sharedRoot = ".claudinite/shared/"

var updateRun = regexp.MustCompile(`^Claudinite update\b`)

var sharedTreeImmutable = declared.Builtin{
	ID:     "shared-tree-immutable",
	Pack:   "claudinite-lifecycle",
	OnFail: "advise",
	Since:  "2026-09-06",
	Tags:   []string{"work", "builtin", "claudinite-lifecycle"},
	Doc:    "packs/claudinite-lifecycle/RULES.md",
	Why:    "the update flow overwrites .claudinite/shared/ on its next run, so a hand-edit there is either silently lost or briefly masks a mount gone stale — the fix belongs in the canon, or as a local override under .claudinite/local/packs/; rewriting that tree is the update task's own operation, which is why this advises rather than blocks",
}

func init() { register(&sharedTreeImmutable, runSharedTreeImmutable) }

func runSharedTreeImmutable(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
	if onDefaultBranch(ctx) {
		return nil
	}
	for _, m := range ctx.Commits() {
		if updateRun.MatchString(m) {
			return nil
		}
	}
	var touched []string
	for _, c := range ctx.CommitsWithFiles() {
		touched = append(touched, c.Files...)
	}
	var out []findings.Finding
	for _, p := range unique(touched) {
		if !strings.HasPrefix(p, sharedRoot) {
			continue
		}
		out = append(out, sharedTreeImmutable.Finding(p, 0,
			"this branch commits "+p+", inside the vendored "+sharedRoot+" tree, under a title the update task does not compose",
			"if this branch is updating the mount, the update task's own operation, which legitimately rewrites this tree whole, leave it; otherwise revert that file and make the change in the canon instead, or — for a difference this repo alone needs — carry it under .claudinite/local/packs/, which sits beside the mount and survives every update"))
	}
	return out
}
