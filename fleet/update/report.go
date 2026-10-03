package update

import (
	"fmt"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/fleet"
)

type section struct{ outcome, heading, gloss string }

// sections are the outcomes in the order a reader acts on them.
var sections = []section{
	{Updated, "Updated during this run", "was behind the published versions and is now at them"},
	{Moved, "Moved during this run", "a Node engine member whose declaration's stamp moved; it is judged against nothing until phase 9 moves it"},
	{AlreadyCurrent, "Already current", "was at the published versions before the dispatch, so its own update correctly declined"},
	{DidNotUpdate, "Started but did not reach the published versions", "its scheduler ran — go and read it"},
	{NeverStarted, "Never started", "the dispatch was accepted and no run followed it"},
	{Unknown, "Could not be determined", "the member could not be read"},
}

func runsURL(repo string) string {
	return "https://github.com/" + repo + "/actions/workflows/" + fleet.Scheduler
}

// Report is everything the lever observed.
type Report struct {
	Owner    string
	DryRun   bool
	Filter   Filter
	Fired    []Fired
	Followed []Followed
	Skipped  []Row
	Failed   []Row
}

// Render is the whole report; the headline is current of dispatched,
// never a count of dispatches.
func (r Report) Render() string {
	current, notCurrent := 0, 0
	for _, f := range r.Followed {
		if IsSuccess(f.Outcome) {
			current++
		} else {
			notCurrent++
		}
	}
	var out []string
	add := func(s ...string) { out = append(out, s...) }
	dry := ""
	if r.DryRun {
		dry = " (DRY RUN — nothing was dispatched)"
	}
	add("# Fleet update — "+r.Owner+dry, "")
	if r.DryRun {
		add("Would ask each covered member's own `" + fleet.Scheduler + "` to wake its `" + ForcedTask + "` item.")
	} else {
		add("Asked each covered member's own `" + fleet.Scheduler + "` to wake its `" + ForcedTask + "` item, then followed each" +
			" one until it pinned the engine and held every declared pack at the versions its own update would move to.")
	}
	if r.Filter != nil {
		add("Filtered to: " + strings.Join(r.Filter, ", "))
	}
	add("")
	if r.DryRun {
		add(fmt.Sprintf("| would fire | skipped |\n| --- | --- |\n| %d | %d |", len(r.Fired), len(r.Skipped)))
	} else {
		add(fmt.Sprintf("| current | not current | not dispatched | skipped |\n| --- | --- | --- | --- |\n| %d of %d | %d | %d | %d |",
			current, len(r.Fired), notCurrent, len(r.Failed), len(r.Skipped)))
	}
	add("")
	if r.DryRun && len(r.Fired) > 0 {
		var rows []string
		for _, f := range r.Fired {
			rows = append(rows, fmt.Sprintf("- [`%s`](%s)", f.FullName, runsURL(f.FullName)))
		}
		add("**Would fire:**\n" + strings.Join(rows, "\n"))
	}
	if !r.DryRun && len(r.Fired) == 0 && len(r.Failed) == 0 {
		add("**No member was dispatched** — check the filter, or whether anything under this owner is covered.")
	}
	for _, s := range sections {
		var rows []string
		for _, f := range r.Followed {
			if f.Outcome == s.outcome {
				rows = append(rows, fmt.Sprintf("- [`%s`](%s) — %s", f.FullName, runsURL(f.FullName), f.Detail))
			}
		}
		if len(rows) > 0 {
			add("**" + s.heading + "** — " + s.gloss + ":\n" + strings.Join(rows, "\n"))
		}
	}
	if len(r.Skipped) > 0 {
		var rows []string
		for _, s := range r.Skipped {
			rows = append(rows, fmt.Sprintf("- `%s` — **%s**: %s", s.FullName, s.State, s.Detail))
		}
		add("**Skipped:**\n" + strings.Join(rows, "\n"))
	}
	if len(r.Failed) > 0 {
		var rows []string
		for _, s := range r.Failed {
			rows = append(rows, fmt.Sprintf("- `%s` — **%s**: %s", s.FullName, s.State, s.Detail))
		}
		add("**Could not be dispatched:**\n" + strings.Join(rows, "\n"))
	}
	add("",
		"_Current means the member is at the published engine and pack versions its own update reads. Content_",
		"_that shipped without a version bump moves no number, so it is invisible here._")
	var kept []string
	for _, l := range out {
		if l != "" {
			kept = append(kept, l)
		}
	}
	return strings.Join(kept, "\n")
}

// Verdict is what fails the run, "" when the fleet is current: a dispatch
// that never landed first, then a dispatched member not current.
// Grant reports whether the token's grant explains any failure.
func (r Report) Grant() bool {
	for _, f := range r.Failed {
		if f.Grant {
			return true
		}
	}
	return false
}

func (r Report) Verdict() string {
	if len(r.Failed) > 0 {
		var parts []string
		for _, f := range r.Failed {
			parts = append(parts, f.FullName+": "+f.State)
		}
		return fmt.Sprintf("%d member(s) could not be dispatched (%s) — ", len(r.Failed), strings.Join(parts, "; ")) +
			"the rest are reported above, and this run fails so the cause is escalated"
	}
	var parts []string
	for _, f := range r.Followed {
		if !IsSuccess(f.Outcome) {
			parts = append(parts, f.FullName+": "+f.Outcome)
		}
	}
	if len(parts) > 0 {
		return fmt.Sprintf("%d of %d dispatched member(s) did not reach the published versions ", len(parts), len(r.Fired)) +
			"(" + strings.Join(parts, "; ") + ") — " +
			"reported above, and this run fails rather than counting the dispatch as the outcome"
	}
	return ""
}
