package items

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/world"
)

func held(over func(*workitem.Issue)) workitem.Issue {
	i := workitem.Issue{Number: 7, Title: "[claudinite-work] acme-pack/a", State: "open",
		Labels: []string{workitem.StatusRunningAgent}, Body: "packs/acme-pack/tasks/a/task.md\n"}
	if over != nil {
		over(&i)
	}
	return i
}

// applied is the item after its own ops, as a session performing them
// leaves it, with the comments and pull request closes it wrote.
type applied struct {
	item     workitem.Issue
	others   map[int]*workitem.Issue
	comments map[int][]string
	records  []string
	closed   []int
}

func apply(item workitem.Issue, p Plan, others ...workitem.Issue) applied {
	a := applied{item: item, others: map[int]*workitem.Issue{}, comments: map[int][]string{}}
	for i := range others {
		a.others[others[i].Number] = &others[i]
	}
	target := func(n int) *workitem.Issue {
		if n == a.item.Number {
			return &a.item
		}
		return a.others[n]
	}
	for _, op := range ConvergeOps(item, p) {
		switch op.Kind {
		case "comment":
			a.comments[op.Issue] = append(a.comments[op.Issue], op.Body)
		case "record":
			a.records = append(a.records, op.Line)
		case "removeLabel":
			t := target(op.Issue)
			t.Labels = slices.DeleteFunc(append(workitem.LabelList{}, t.Labels...), func(l string) bool { return l == op.Name })
		case "addLabel":
			t := target(op.Issue)
			t.Labels = append(t.Labels, op.Name)
		case "setBody":
			target(op.Issue).Body = op.Body
		case "close":
			target(op.Issue).State = "closed"
		case "closePull":
			a.closed = append(a.closed, op.Number)
		}
	}
	return a
}

func TestAPlanTheTransitionCannotBeBuiltFromIsRefused(t *testing.T) {
	for _, c := range []struct {
		p    Plan
		want string
	}{
		{Plan{Outcome: "done", Summary: "x"}, "--issue"},
		{Plan{Issue: 7, Summary: "x"}, "--outcome"},
		{Plan{Issue: 7, Outcome: "nope", Summary: "x"}, "--outcome"},
		{Plan{Issue: 7, Outcome: "done", Summary: "  "}, "--summary"},
		{Plan{Issue: 7, Outcome: "approval", Summary: "x"}, "--pr"},
	} {
		if err := CheckPlan(c.p); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: %v", c.p, err)
		}
	}
	if err := CheckPlan(Plan{Issue: 7, Outcome: "done", Summary: "did it"}); err != nil {
		t.Error(err)
	}
}

func TestAnItemThisSessionDoesNotHoldIsRefused(t *testing.T) {
	if r := Refusal(held(func(i *workitem.Issue) { i.Title, i.Body = "just an issue", "please\n" }), 7); !strings.Contains(r, "not a Claudinite work item") {
		t.Error(r)
	}
	marked := held(func(i *workitem.Issue) {
		i.Title = "Implement the thing"
		i.Body = "do it\n\n<!-- claudinite-item -->\n.claudinite/shared/packs/acme-pack/tasks/acme-task/task.md\n\nRequest: #7\n<!-- /claudinite-item -->\n"
	})
	if r := Refusal(marked, 7); r != "" {
		t.Error(r)
	}
	bare := held(func(i *workitem.Issue) {
		i.Title, i.Body = "Report the variables", "packs/acme-pack/tasks/a/task.md\n\nRequest: #7\n"
		i.Labels = []string{workitem.OriginAdHoc, workitem.StatusRunningAgent}
	})
	if r := Refusal(bare, 7); r != "" {
		t.Error("a marked issue predating the block converges by its mark:", r)
	}
	if r := Refusal(held(func(i *workitem.Issue) { i.State = "closed" }), 7); !strings.Contains(r, "already closed") {
		t.Error(r)
	}
	if r := Refusal(held(func(i *workitem.Issue) { i.Labels = []string{workitem.StatusRunningExecutor} }), 7); !strings.Contains(r, workitem.StatusRunningAgent) {
		t.Error(r)
	}
	if r := Refusal(held(nil), 8); !strings.Contains(r, "not #8") {
		t.Error(r)
	}
	if r := Refusal(held(nil), 7); r != "" {
		t.Error(r)
	}
}

func TestADoneClosesWithTheStatusSwappedAndTheRecordOnIt(t *testing.T) {
	a := apply(held(nil), Plan{Issue: 7, Outcome: "done", Summary: "ran the thing"})
	if a.item.State != "closed" || !reflect.DeepEqual([]string(a.item.Labels), []string{workitem.StatusDone}) {
		t.Errorf("%+v", a.item)
	}
	c := a.comments[7][0]
	if !strings.Contains(c, "ran the thing") || !strings.Contains(c, "claudinite-task-exec v1 acme-pack/a [#7] success") || strings.Contains(c, workitem.EpisodeMarker) {
		t.Error(c)
	}
	if !reflect.DeepEqual(a.records, []string{"claudinite-task-exec v1 acme-pack/a [#7] success"}) {
		t.Error(a.records)
	}
}

func TestAParkStaysOpenWearingOneStatusAndReleasesItsEpisode(t *testing.T) {
	a := apply(held(nil), Plan{Issue: 7, Outcome: "failure", Summary: "it broke"})
	if a.item.State != "open" || !reflect.DeepEqual([]string(a.item.Labels), []string{workitem.StatusNeedsHumanFailure}) {
		t.Errorf("%+v", a.item)
	}
	if !strings.Contains(a.comments[7][0], workitem.EpisodeMarker) || !strings.Contains(a.comments[7][0], "[#7] failed") {
		t.Error(a.comments[7][0])
	}
	if r := RecordLine(held(nil), Outcomes["approval"].Record); r != "" {
		t.Error("an approval park carries no record:", r)
	}
}

func superseding() workitem.Issue {
	return held(func(i *workitem.Issue) {
		i.Body = "packs/acme-pack/tasks/a/task.md\n\nTarget-branch: claudinite/acme-pack/a/2026-09-04-ab12\nSupersedes: #3, #4\n"
	})
}

func TestARunThatLeftItsPullRequestClosesWhatItSupersedes(t *testing.T) {
	for _, o := range []string{"done", "approval"} {
		if a := apply(superseding(), Plan{Issue: 7, Outcome: o, Summary: "s", PR: 9}); !reflect.DeepEqual(a.closed, []int{3, 4}) {
			t.Error(o, a.closed)
		}
	}
	if a := apply(superseding(), Plan{Issue: 7, Outcome: "done", Summary: "nothing"}); len(a.closed) != 0 {
		t.Error(a.closed)
	}
	if a := apply(superseding(), Plan{Issue: 7, Outcome: "failure", Summary: "broke", PR: 9}); len(a.closed) != 0 {
		t.Error(a.closed)
	}
	script := SessionScript(superseding(), Plan{Issue: 7, Outcome: "approval", Summary: "s", PR: 9}, "o/r")
	if !strings.Contains(script, "pullNumber `3`, state `closed`") || !strings.Contains(script, "pullNumber `4`, state `closed`") || !strings.Contains(script, "Superseded by #9") {
		t.Error(script)
	}
}

func TestAParkNamingAPullRequestStampsItsEndCondition(t *testing.T) {
	a := apply(held(nil), Plan{Issue: 7, Outcome: "approval", Summary: "opened", PR: 9})
	if f := workitem.ParseFields(a.item.Body); f.EndsWhen == nil || *f.EndsWhen != 9 || strings.Split(a.item.Body, "\n")[0] != "packs/acme-pack/tasks/a/task.md" {
		t.Error(a.item.Body)
	}
	if a := apply(held(nil), Plan{Issue: 7, Outcome: "failure", Summary: "x"}); workitem.ParseFields(a.item.Body).EndsWhen != nil {
		t.Error(a.item.Body)
	}
	script := SessionScript(held(nil), Plan{Issue: 7, Outcome: "approval", Summary: "opened", PR: 9}, "o/r")
	if strings.Count(script, "`issue_write`") != 1 || !strings.Contains(script, "Ends-when: #9 closed") || !strings.Contains(script, "labels `[\"task:status:needs-human-approval\"]`") {
		t.Error(script)
	}
}

func TestTheEndConditionLandsInAMarkedIssuesMachineBlock(t *testing.T) {
	marked := held(func(i *workitem.Issue) {
		i.Title = "Please do the thing"
		i.Body = "please do the thing\n\n<!-- claudinite-item -->\npacks/acme-pack/tasks/a/task.md\n<!-- /claudinite-item -->\n"
	})
	a := apply(marked, Plan{Issue: 7, Outcome: "approval", Summary: "opened", PR: 9})
	block, _ := workitem.MachineBlockOf(a.item.Body)
	if !strings.HasPrefix(a.item.Body, "please do the thing\n") || !strings.Contains(block, "Ends-when: #9") {
		t.Error(a.item.Body)
	}
}

func TestAnApprovalHandsALegacyRequestToTheReviewerAndAFailureLeavesItArmed(t *testing.T) {
	item := held(func(i *workitem.Issue) { i.Body = "packs/acme-pack/tasks/a/task.md\n\nRequest: #42\n" })
	req := workitem.Issue{Number: 42, State: "open", Labels: []string{workitem.QueuedLabel}}
	a := apply(item, Plan{Issue: 7, Outcome: "approval", Summary: "s", PR: 9}, req)
	if len(a.others[42].Labels) != 0 || !strings.Contains(a.comments[42][0], "#9") {
		t.Error(a.others[42], a.comments)
	}
	a = apply(item, Plan{Issue: 7, Outcome: "failure", Summary: "s"}, req)
	if !reflect.DeepEqual([]string(a.others[42].Labels), []string{workitem.QueuedLabel}) || len(a.comments[42]) != 0 {
		t.Error(a.others[42], a.comments)
	}
	script := SessionScript(item, Plan{Issue: 7, Outcome: "approval", Summary: "s", PR: 9}, "o/r")
	if !strings.Contains(script, "On #42: REMOVE the label `claude-queued`") || strings.Contains(script, "claude-in-review") {
		t.Error(script)
	}
}

func TestTheScriptCarriesEveryNonStatusLabelThrough(t *testing.T) {
	item := held(func(i *workitem.Issue) {
		i.Labels = []string{workitem.OriginAdHoc, workitem.StatusRunningAgent, "other"}
	})
	script := SessionScript(item, Plan{Issue: 7, Outcome: "done", Summary: "x"}, "o/r")
	if !strings.Contains(script, `labels `+"`"+`["task:origin:ad-hoc","other","task:status:done"]`+"`"+`, state `+"`closed`, state_reason `completed`") {
		t.Error(script)
	}
	if !strings.Contains(script, "Output this line") {
		t.Error(script)
	}
}

func TestTheExecRecordIsPrintedFromItsThreeParts(t *testing.T) {
	if line, err := ExecRecord("acme-pack/a", "#7", "success"); err != nil || line != "claudinite-task-exec v1 acme-pack/a [#7] success" {
		t.Error(line, err)
	}
	for _, args := range [][3]string{{"a", "#7", "success"}, {"acme-pack/a", "", "success"}, {"acme-pack/a", "#7", "great"}, {"a/b/c", "#7", "failed"}} {
		if _, err := ExecRecord(args[0], args[1], args[2]); err == nil {
			t.Error(args)
		}
	}
}

func sessionTask() taskspec.Task {
	return taskspec.Task{Pack: "acme-pack", ID: "a", Rel: "packs/acme-pack/tasks/a",
		Decl: taskspec.Decl{"id": "a", "agent_model": "sonnet", "expected_outcome": "fresh_pr"}}
}

func validSession() Session {
	return Session{
		Item:     held(nil),
		Comments: []world.Comment{{ID: 5, Body: workitem.HandoffMarker + "\nHanded off by executor `E1` — invocation nonce `7-old`."}, {ID: 9, Body: workitem.HandoffMarker + "\nHanded off by executor `E1` — invocation nonce `7-new`."}},
		Nonce:    "7-new",
		Tasks:    []taskspec.Task{sessionTask()},
	}
}

func TestTheSessionGateNamesTheCheckThatFailed(t *testing.T) {
	v, err := Validate(validSession())
	if err != nil || v.Task.Path() != "acme-pack/a" || v.Model != "sonnet" || v.Outcome != "fresh_pr" {
		t.Fatal(v, err)
	}
	cases := map[string]func(*Session){
		"not a task this checkout carries": func(s *Session) { s.Tasks = nil },
		"the title names":                  func(s *Session) { s.Item.Title = "[claudinite-work] acme-pack/b" },
		"does not carry":                   func(s *Session) { s.Item.Labels = []string{workitem.StatusRunningExecutor} },
		"this fire's nonce":                func(s *Session) { s.Nonce = "7-old" },
		"no hand-off":                      func(s *Session) { s.Comments = s.Comments[2:] },
		"names no task path":               func(s *Session) { s.Item.Body = "" },
	}
	for want, mutate := range cases {
		s := validSession()
		s.Comments = append([]world.Comment{}, s.Comments...)
		mutate(&s)
		if _, err := Validate(s); err == nil || !strings.Contains(err.Error(), want) || !strings.HasPrefix(err.Error(), "not this item's session") {
			t.Errorf("%s: %v", want, err)
		}
	}
}

func TestARequestItImplementsMustBeOpenAndStillMarked(t *testing.T) {
	s := validSession()
	s.Item.Body = "packs/acme-pack/tasks/a/task.md\n\nRequest: #42\n"
	if _, err := Validate(s); err == nil || !strings.Contains(err.Error(), "request file") {
		t.Error(err)
	}
	s.Request = &workitem.Issue{Number: 42, State: "open", Labels: []string{workitem.OriginAdHoc}}
	if _, err := Validate(s); err != nil {
		t.Error(err)
	}
	s.Request.State = "closed"
	if _, err := Validate(s); err == nil || !strings.Contains(err.Error(), "withdrawn") {
		t.Error(err)
	}
	s.Request = &workitem.Issue{Number: 42, State: "open"}
	if _, err := Validate(s); err == nil || !strings.Contains(err.Error(), "mark") {
		t.Error(err)
	}
	// The item itself as its own request.
	self := validSession()
	self.Item.Body = "packs/acme-pack/tasks/a/task.md\n\nRequest: #7\n"
	self.Item.Labels = append(self.Item.Labels, workitem.OriginAdHoc)
	if _, err := Validate(self); err != nil {
		t.Error(err)
	}
}
