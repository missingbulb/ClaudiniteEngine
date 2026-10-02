package schedule

import (
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/tasks/world"
)

// WorkflowFailureLabel marks the one issue a failing workflow reports on.
const WorkflowFailureLabel = "workflow-failure"

// SchedulerFailureTitle is the scheduler's failure issue.
const SchedulerFailureTitle = "Claudinite scheduler run failed"

// ReportFailure comments on the one open failure issue titled title, or
// files it: a failing workflow reports on one issue however often it
// fails. The issue is found through the list API by its label, never the
// search index, which can miss an issue filed seconds earlier and mint a
// second.
func ReportFailure(gh world.Issues, labels []workitem.Label, title, body string) (int, bool, error) {
	if err := gh.EnsureLabels(labels); err != nil {
		return 0, false, err
	}
	want := strings.TrimSpace(title)
	found := 0
	for page := 1; found == 0; page++ {
		got, err := gh.IssuesPage(world.Query{State: "open", Label: WorkflowFailureLabel}, page)
		if err != nil {
			return 0, false, err
		}
		for _, i := range got {
			if !i.PullRequest && strings.TrimSpace(i.Title) == want && (found == 0 || i.Number < found) {
				found = i.Number
			}
		}
		if len(got) < world.PageSize {
			break
		}
	}
	if found != 0 {
		_, err := gh.Comment(found, body)
		return found, false, err
	}
	names := make([]string, len(labels))
	for k, l := range labels {
		names[k] = l.Name
	}
	n, err := gh.CreateIssue(title, body, names)
	return n, true, err
}
