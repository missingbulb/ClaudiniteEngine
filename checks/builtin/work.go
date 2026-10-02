package builtin

import (
	"sort"

	"github.com/missingbulb/ClaudiniteEngine/checks/declared"
)

// onDefaultBranch is the Node work surface's test: the checked-out branch
// is main or master, where a work check judges no change.
func onDefaultBranch(ctx *declared.Ctx) bool {
	b := ctx.Branch()
	return b == "main" || b == "master"
}

// unique is xs deduplicated and sorted.
func unique(xs ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range xs {
		for _, x := range l {
			if !seen[x] {
				seen[x] = true
				out = append(out, x)
			}
		}
	}
	sort.Strings(out)
	return out
}
