package roster

import (
	"fmt"
	"strings"
)

func join(parts []string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n")
}

func list(head string, items []string, sep string) string {
	if len(items) == 0 {
		return ""
	}
	return head + strings.Join(items, sep)
}

// RenderCoverage is the coverage section, naming every repository: each
// lands in one list, and the manager is named as not censused.
func RenderCoverage(owner, home string, c Coverage) string {
	covered := "**Covered:** none"
	if len(c.Covered) > 0 {
		covered = "**Covered:** " + strings.Join(c.Covered, ", ")
	}
	uncovered := "**Outside the fleet:** none 🎉"
	if len(c.Uncovered) > 0 {
		uncovered = "**Outside the fleet:** " + strings.Join(c.Uncovered, ", ") +
			". To bring one in, run `cn adopt` in a session on it (the `adopt-claudinite` skill); to leave one out, add it to `exclude` in this repo's fleet block."
	}
	return join([]string{
		"# Fleet coverage census — " + owner,
		"",
		"| covered | dormant | uncovered | ignored | skipped (fork/archived) | unknown |",
		"| --- | --- | --- | --- | --- | --- |",
		fmt.Sprintf("| %d | %d | %d | %d | %d | %d |", len(c.Covered), len(c.Dormant), len(c.Uncovered), len(c.Ignored), len(c.Skipped), len(c.Unknown)),
		"",
		covered,
		list("**Covered but dormant (self-declared, upkeep stopped — measured by nothing and swept by nothing):** ", c.Dormant, ", "),
		uncovered,
		list("**Ignored (the fleet block's exclude — nothing is read, measured or claimed about these):** ", c.Ignored, ", "),
		list("**Skipped:** ", c.Skipped, ", "),
		list("**UNKNOWN (declaration read errored — fix the token/scope):** ", c.Unknown, "; "),
		"**Not censused:** " + home + " — the enforcer itself",
	})
}

// RenderFreshness is the freshness section, naming every repository:
// measured against what each member's own update would move it to.
func RenderFreshness(owner, home string, f Freshness) string {
	count := func(state string) int {
		n := 0
		for _, u := range f.Unhealthy {
			if u.State == state {
				n++
			}
		}
		return n
	}
	behind := "**Every measured member is up to date 🎉**"
	if len(f.Unhealthy) > 0 {
		var rows []string
		for _, u := range f.Unhealthy {
			rows = append(rows, fmt.Sprintf("- `%s` — **%s**: %s", u.FullName, u.State, u.Detail))
		}
		behind = "**Behind:**\n" + strings.Join(rows, "\n")
	}
	fresh := "**Fresh:** none"
	if len(f.Fresh) > 0 {
		var rows []string
		for _, r := range f.Fresh {
			rows = append(rows, fmt.Sprintf("- `%s` — %s", r.FullName, r.Detail))
		}
		fresh = "**Fresh:**\n" + strings.Join(rows, "\n")
	}
	return join([]string{
		"# Fleet freshness sweep — " + owner + " (measured by what each member's own update would move it to: the published engine and pack versions)",
		"",
		"| fresh | behind | no scheduler | no stamp | node | dormant | ignored | out of scope | unknown |",
		"| --- | --- | --- | --- | --- | --- | --- | --- | --- |",
		fmt.Sprintf("| %d | %d | %d | %d | %d | %d | %d | %d | %d |", len(f.Fresh), count("behind"), count("no-scheduler"), count("no-stamp"),
			len(f.Node), len(f.Dormant), len(f.Ignored), len(f.OutOfScope), len(f.Unknown)),
		"",
		behind,
		fresh,
		list("**Node engine (covered, not measured — phase 9 moves them):** ", f.Node, ", "),
		list("**Dormant (scheduler stopped by declaration — not measured, and no fleet operation touches them):** ", f.Dormant, ", "),
		list("**Ignored (the fleet block's exclude — nothing is read, measured or claimed about these):** ", f.Ignored, ", "),
		list("**Out of scope (not covered members):** ", f.OutOfScope, ", "),
		list("**UNKNOWN (probe errored — fix the token/scope):** ", f.Unknown, "; "),
		"**Not measured:** `" + home + "` — the enforcer, swept by its own scheduler",
	})
}

// Unknowns is every repository either half could not classify.
func Unknowns(c Coverage, f Freshness) []string {
	return append(append([]string{}, c.Unknown...), f.Unknown...)
}

// UnknownError is the sentence a run with unknowns fails on.
func UnknownError(n int) string {
	return fmt.Sprintf("%d repo classification(s) could not be made — unknown is neither ", n) +
		"uncovered nor behind, no issues were opened for them, and this run fails so the cause is escalated"
}
