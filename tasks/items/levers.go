// Package items holds the operator levers, which are the whole of
// urgency, forcing, fan-out and retry. Forcing a scheduled task is waking
// its standing item: its wait cleared, every status it wears replaced by
// ready, the same lever as the re-queue out of a park, since "run this
// now" and "retry this now" are one operation on one object. Forcing
// ad-hoc work is creating an item: a parameterized run, an unscheduled
// task's run, a fan-out target, structurally distinct from the standing
// item by its qualifier. The executor still evaluates the precondition at
// pick, so a force that finds no work says so.
package items

import (
	"fmt"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/tasks/calendar"
	"github.com/missingbulb/ClaudiniteEngine/tasks/queue"
	"github.com/missingbulb/ClaudiniteEngine/tasks/world"
)

// ForcedContext is the Context a hand-created item carries when the
// operator names none: an item's Context is the agent's binding scope,
// and a forced item carrying none would read as a scope of nothing.
const ForcedContext = "Created by hand — no precondition asserts there is work to do. Do only what the task file specifies, and converge to a no-op if there is nothing."

// Wake clears an item's wait and puts it back in the queue: Not-before
// cleared, Woken stamped (the wake stands in for the cadence), the
// episode boundary commented, every status it wears cleared in every
// spelling, ready added.
func Wake(gh world.Issues, number int, urgent bool, now time.Time) error {
	issue, err := gh.Issue(number)
	if err != nil {
		return fmt.Errorf("#%d could not be read: %w", number, err)
	}
	body := workitem.WithWoken(workitem.WithNotBefore(issue.Body, ""), calendar.ISO(now))
	if body != issue.Body {
		if err := gh.SetIssueBody(number, body); err != nil {
			return err
		}
	}
	if _, err := gh.Comment(number, workitem.EpisodeMarker+"\nWoken by hand — cleared `Not-before` and returned this item to the queue."); err != nil {
		return err
	}
	for _, s := range workitem.StatusesOn(issue.Labels) {
		if err := queue.ClearStatus(gh, number, s); err != nil {
			return err
		}
	}
	if err := gh.AddLabel(number, workitem.StatusReady); err != nil {
		return err
	}
	if urgent {
		return gh.AddLabel(number, workitem.Urgent)
	}
	return nil
}

// CreateOpts are a hand-created item's parameters.
type CreateOpts struct {
	Urgent     bool
	Context    []string
	NotBefore  string
	BlockedBy  []int
	Qualifier  string
	Supersedes int
}

// Create files an item for a declared task. An unqualified item for a
// scheduled task is refused: it would be that task's standing item, and
// the scheduler's dedupe would close one of the two.
func Create(gh world.Issues, pack, task, taskPath string, scheduled bool, o CreateOpts, now time.Time, log func(string)) (int, error) {
	title := workitem.Title{Pack: pack, Task: task, Qualifier: o.Qualifier}.String()
	if scheduled && o.Qualifier == "" {
		return 0, fmt.Errorf("%s/%s is on the schedule, so an unqualified item for it IS its standing item — the scheduler run would close one of the two as a duplicate. To run it now, wake its standing item (`cn work wake #N`, or the scheduler workflow's `wake` input); to run it beside the schedule, give this item a `--qualifier` naming what makes it a different run", pack, task)
	}
	open, err := queue.ListOpen(gh)
	if err != nil {
		return 0, err
	}
	for _, i := range open {
		if strings.TrimSpace(i.Title) == title {
			log(fmt.Sprintf("! #%d is an open item with this exact title — the new item will queue behind it, not run beside it", i.Number))
			break
		}
	}
	if err := gh.EnsureLabels(workitem.QueueLabels); err != nil {
		return 0, err
	}
	context := o.Context
	if len(context) == 0 {
		context = []string{ForcedContext}
	}
	status := workitem.StatusReady
	if o.NotBefore != "" || len(o.BlockedBy) > 0 {
		status = workitem.StatusBlocked
	}
	labels := []string{workitem.OriginManual, status}
	if o.Urgent {
		labels = append(labels, workitem.Urgent)
	}
	n, err := gh.CreateIssue(title, workitem.Body(workitem.BodySpec{TaskPath: taskPath, NotBefore: o.NotBefore, BlockedBy: o.BlockedBy,
		Context: context, Woken: calendar.ISO(now)}), labels)
	if err != nil {
		return 0, fmt.Errorf("could not create the item: %w", err)
	}
	if o.Supersedes != 0 {
		if _, err := gh.Comment(o.Supersedes, fmt.Sprintf("Superseded by #%d, a retry of this work created by hand.", n)); err != nil {
			return n, err
		}
		if err := gh.AddLabel(o.Supersedes, workitem.StatusRejected); err != nil {
			return n, err
		}
		if err := gh.CloseIssue(o.Supersedes, "not_planned"); err != nil {
			return n, err
		}
	}
	return n, nil
}
