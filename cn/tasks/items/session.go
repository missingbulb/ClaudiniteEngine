package items

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/queue"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/world"
)

// The session-side commands a routine session runs. A session holds its
// GitHub access through its own tools and its subprocesses reach no
// network, so these read the item from a file the session saved and
// plan; they never write.

// Outcome is what a session may claim, and what it means for the item:
// the status it takes, whether it closes, and the record it carries ("" is
// none: an approval park succeeded and left a pull request, which the
// record vocabulary has no word for).
type Outcome struct {
	Label       string
	Closes      bool
	StateReason string
	Record      string
}

// Outcomes are the five a converge takes.
var Outcomes = map[string]Outcome{
	"done":     {Label: workitem.StatusDone, Closes: true, StateReason: "completed", Record: "success"},
	"approval": {Label: workitem.StatusNeedsHumanApprove},
	"action":   {Label: workitem.StatusNeedsHumanAction, Record: "failed"},
	"decision": {Label: workitem.StatusNeedsHumanDecide, Record: "failed"},
	"failure":  {Label: workitem.StatusNeedsHumanFailure, Record: "failed"},
}

// OutcomeNames are the outcomes in the order the usage names them.
var OutcomeNames = []string{"done", "approval", "action", "decision", "failure"}

// Plan is the session's judgment: which outcome, what happened, and the
// pull request a park waits on or a done landed.
type Plan struct {
	Issue   int
	Outcome string
	Summary string
	PR      int
}

// CheckPlan refuses a plan the transition cannot be built from.
func CheckPlan(p Plan) error {
	switch {
	case p.Issue <= 0:
		return fmt.Errorf("--issue must be the work item's issue number")
	case Outcomes[p.Outcome] == (Outcome{}):
		return fmt.Errorf("--outcome must be one of %s, got %q", strings.Join(OutcomeNames, ", "), p.Outcome)
	case strings.TrimSpace(p.Summary) == "":
		return fmt.Errorf("--summary must say what happened; it is the item's only durable account")
	case p.Outcome == "approval" && p.PR == 0:
		return fmt.Errorf("--pr must name the pull request an approval park is waiting on — a park nobody can act on is not a park")
	}
	return nil
}

// ReadItem reads an issue as a session's GitHub tools returned it.
func ReadItem(raw []byte) (workitem.Issue, error) {
	var i workitem.Issue
	if err := json.Unmarshal(raw, &i); err != nil || i.Number == 0 {
		return workitem.Issue{}, fmt.Errorf("the item file must hold the issue as your GitHub tools returned it (number, title, body, state, labels)")
	}
	return i, nil
}

func isMarked(i workitem.Issue) bool {
	for _, o := range workitem.OriginLabels {
		if i.HasLabel(o) {
			return true
		}
	}
	return false
}

// Refusal is why item may not be converged by this session, "" when it
// may: a work item by title, machine block or origin label, open, and
// with an agent.
func Refusal(item workitem.Issue, issue int) string {
	if item.Number != issue {
		return fmt.Sprintf("the item file holds #%d, not #%d", item.Number, issue)
	}
	_, titled := workitem.ParseTitle(item.Title)
	_, blocked := workitem.MachineBlockOf(item.Body)
	if !titled && !blocked && !isMarked(item) {
		return fmt.Sprintf("#%d is not a Claudinite work item", issue)
	}
	if item.State != "open" {
		return fmt.Sprintf("#%d is already closed — it was converged once already", issue)
	}
	if item.Status() != workitem.StatusRunningAgent {
		return fmt.Sprintf("#%d is not with an agent (`%s`) — this session does not hold it, so it is not this session's to converge", issue, workitem.StatusRunningAgent)
	}
	return ""
}

// RecordLine is the item's execution record, "" where the vocabulary has
// no true answer or the title names no task.
func RecordLine(item workitem.Issue, status string) string {
	t, ok := workitem.ParseTitle(item.Title)
	if status == "" || !ok {
		return ""
	}
	return queue.RenderTaskExec(queue.TaskExec{Pack: t.Pack, Task: t.Task, Slot: fmt.Sprintf("#%d", item.Number), Status: status})
}

// ConvergeComment is the session's account, the record, and, on a park,
// the episode boundary a session's own comment carries since it cannot
// strike the executor's claim.
func ConvergeComment(item workitem.Issue, p Plan) string {
	spec := Outcomes[p.Outcome]
	out := strings.TrimSpace(p.Summary)
	if p.PR != 0 {
		out += fmt.Sprintf("\n\nWaiting on a person: merge or close #%d, then close this item.", p.PR)
	}
	if rec := RecordLine(item, spec.Record); rec != "" {
		out += "\n\n```\n" + rec + "\n```"
	}
	if !spec.Closes {
		out += "\n\n" + workitem.EpisodeMarker + "\nThis session released the item without closing it; every claim before this line is spent."
	}
	return out
}

// Op is one side effect of a transition.
type Op struct {
	Kind        string `json:"kind"`
	Issue       int    `json:"issue,omitempty"`
	Body        string `json:"body,omitempty"`
	Name        string `json:"name,omitempty"`
	Line        string `json:"line,omitempty"`
	StateReason string `json:"stateReason,omitempty"`
	Number      int    `json:"number,omitempty"`
	Successor   int    `json:"successor,omitempty"`
}

// ConvergeOps is the transition as data: comment, the record, every
// spelling of running-agent removed, the one outcome status, a park's end
// condition, the superseded pull requests once this run's own exists, the
// close, and a legacy shadow item's write-back.
func ConvergeOps(item workitem.Issue, p Plan) []Op {
	spec := Outcomes[p.Outcome]
	ops := []Op{{Kind: "comment", Issue: item.Number, Body: ConvergeComment(item, p)}}
	if rec := RecordLine(item, spec.Record); rec != "" {
		ops = append(ops, Op{Kind: "record", Line: rec})
	}
	for _, l := range workitem.SpellingsOf(workitem.StatusRunningAgent) {
		ops = append(ops, Op{Kind: "removeLabel", Issue: item.Number, Name: l})
	}
	ops = append(ops, Op{Kind: "addLabel", Issue: item.Number, Name: spec.Label})
	if !spec.Closes && p.PR != 0 {
		ops = append(ops, Op{Kind: "setBody", Issue: item.Number,
			Body: workitem.EditItemBody(item.Body, func(m string) string { return workitem.WithEndsWhen(m, p.PR) })})
	}
	fields := workitem.ParseFields(item.Body)
	if p.PR != 0 && (p.Outcome == "done" || p.Outcome == "approval") {
		for _, n := range fields.Supersedes {
			ops = append(ops, Op{Kind: "closePull", Number: n, Successor: p.PR,
				Body: fmt.Sprintf("Superseded by #%d, a later run of the same task. Closing this one.", p.PR)})
		}
	}
	if spec.Closes {
		return append(ops, Op{Kind: "close", Issue: item.Number, StateReason: spec.StateReason})
	}
	if fields.Request != nil && *fields.Request != item.Number && p.Outcome == "approval" {
		r := *fields.Request
		ops = append(ops,
			Op{Kind: "comment", Issue: r, Body: fmt.Sprintf("A pull request for this is open and waiting on you: #%d. Merge or close it.", p.PR)},
			Op{Kind: "removeLabel", Issue: r, Name: workitem.QueuedLabel},
			Op{Kind: "addLabel", Issue: r, Name: workitem.InReviewLabel})
	}
	return ops
}

func labelSet(own []string) string {
	raw, _ := json.Marshal(own)
	return string(raw)
}

// SessionScript is the same ops addressed to the GitHub tools a session
// has: the item's own label writes folded into one whole set, computed
// from the labels it was read with, and another issue's as a
// read-modify-write naming only the change.
func SessionScript(item workitem.Issue, p Plan, repo string) string {
	owner, name, _ := strings.Cut(repo, "/")
	var lines []string
	step := func(s string) { lines = append(lines, fmt.Sprintf("%d. %s", len(lines)+1, s)) }
	own := append([]string{}, item.Labels...)
	drop := func(l string) {
		out := own[:0]
		for _, x := range own {
			if x != l {
				out = append(out, x)
			}
		}
		own = out
	}
	add := func(l string) {
		for _, x := range own {
			if x == l {
				return
			}
		}
		own = append(own, l)
	}
	var foreign []Op
	newBody, hasBody := "", false
	for _, op := range ConvergeOps(item, p) {
		switch op.Kind {
		case "comment":
			step(fmt.Sprintf("`add_issue_comment` — owner `%s`, repo `%s`, issue_number `%d`, body exactly:\n\n<<<BODY\n%s\n>>>END\n", owner, name, op.Issue, op.Body))
		case "record":
			step("Output this line, on its own, in your reply. Nothing else emits it, and the usage census is read from your transcript:\n\n    " + op.Line + "\n")
		case "removeLabel", "addLabel":
			switch {
			case op.Issue != item.Number:
				foreign = append(foreign, op)
			case op.Kind == "removeLabel":
				drop(op.Name)
			default:
				add(op.Name)
			}
		case "setBody":
			newBody, hasBody = op.Body, true
		case "close":
			step(fmt.Sprintf("`issue_write` — method `update`, owner `%s`, repo `%s`, issue_number `%d`, labels `%s`, state `closed`, state_reason `%s`", owner, name, op.Issue, labelSet(own), op.StateReason))
			own = nil
		case "closePull":
			step(fmt.Sprintf("`add_issue_comment` — owner `%s`, repo `%s`, issue_number `%d` (a pull request), body exactly:\n\n<<<BODY\n%s\n>>>END\n", owner, name, op.Number, op.Body))
			step(fmt.Sprintf("`update_pull_request` — owner `%s`, repo `%s`, pullNumber `%d`, state `closed`", owner, name, op.Number))
		}
	}
	if len(own) > 0 {
		s := fmt.Sprintf("`issue_write` — method `update`, owner `%s`, repo `%s`, issue_number `%d`, labels `%s`", owner, name, item.Number, labelSet(own))
		if hasBody {
			s += ", body exactly:\n\n<<<BODY\n" + newBody + "\n>>>END\n"
		}
		step(s)
	}
	for _, op := range foreign {
		verb := "REMOVE"
		if op.Kind == "addLabel" {
			verb = "ADD"
		}
		step(fmt.Sprintf("On #%d: %s the label `%s`. Read that issue's current labels first and write them back with only this one change — `issue_write` replaces the whole set, and this process never saw the rest.", op.Issue, verb, op.Name))
	}
	return strings.Join(lines, "\n")
}

// ExecRecord is one execution record from its three parts.
func ExecRecord(family, slot, status string) (string, error) {
	pack, task, ok := strings.Cut(family, "/")
	if !ok || pack == "" || task == "" || strings.ContainsAny(family, " \t") || strings.Count(family, "/") != 1 {
		return "", fmt.Errorf("the first argument must be <pack>/<task>, got %q", family)
	}
	if slot == "" {
		return "", fmt.Errorf("the second argument must be the slot id")
	}
	known := false
	for _, s := range queue.TaskExecStatuses {
		known = known || s == status
	}
	if !known {
		return "", fmt.Errorf("the third argument must be one of %s, got %q", strings.Join(queue.TaskExecStatuses, ", "), status)
	}
	return queue.RenderTaskExec(queue.TaskExec{Pack: pack, Task: task, Slot: slot, Status: status}), nil
}

// Session is what validation reads: the item, its comments, the nonce the
// fire carried, the tasks this checkout carries, and the request issue
// where the item names one other than itself.
type Session struct {
	Item     workitem.Issue
	Comments []world.Comment
	Nonce    string
	Tasks    []taskspec.Task
	Request  *workitem.Issue
}

// Validated is the item this session holds.
type Validated struct {
	Task     taskspec.Task
	TaskPath string
	Model    string
	Outcome  string
}

// Validate is the session's entry gate, in code before any judgment: the
// task file is one this checkout carries from a declared pack, the title
// or the machine block names it, the item is with an agent, the newest
// hand-off carries the fire's nonce, and a request it implements is open and still marked. The error names the
// check that failed.
func Validate(s Session) (Validated, error) {
	item := s.Item
	notMine := func(why string) (Validated, error) {
		return Validated{}, fmt.Errorf("not this item's session: %s", why)
	}
	path := workitem.ParseBody(item.Body).TaskPath
	if path == "" {
		return notMine(fmt.Sprintf("#%d names no task path", item.Number))
	}
	var task *taskspec.Task
	for i := range s.Tasks {
		if s.Tasks[i].TaskPath() == path {
			task = &s.Tasks[i]
			break
		}
	}
	if task == nil {
		return notMine(fmt.Sprintf("%s is not a task this checkout carries from a declared pack", path))
	}
	if t, ok := workitem.ParseTitle(item.Title); ok {
		if t.ID() != task.Path() {
			return notMine(fmt.Sprintf("the title names %s, the task path %s", t.ID(), task.Path()))
		}
	} else if _, blocked := workitem.MachineBlockOf(item.Body); !blocked {
		return notMine("the title names no task and the body carries no machine block")
	}
	if item.State != "open" || item.Status() != workitem.StatusRunningAgent {
		return notMine(fmt.Sprintf("#%d does not carry %s", item.Number, workitem.StatusRunningAgent))
	}
	comments := append([]world.Comment{}, s.Comments...)
	sort.SliceStable(comments, func(a, b int) bool { return comments[a].ID < comments[b].ID })
	var handoff *world.Comment
	for i := range comments {
		if strings.Contains(comments[i].Body, workitem.HandoffMarker) {
			handoff = &comments[i]
		}
	}
	if handoff == nil {
		return notMine("the item carries no hand-off comment")
	}
	if s.Nonce == "" || !strings.Contains(handoff.Body, "`"+s.Nonce+"`") {
		return notMine("the newest hand-off does not carry this fire's nonce — the fire named a hand-off that is not the current one")
	}
	if r := workitem.ParseFields(item.Body).Request; r != nil {
		req := &item
		if *r != item.Number {
			req = s.Request
		}
		switch {
		case req == nil:
			return notMine(fmt.Sprintf("the item implements #%d; read it and pass it as the request file", *r))
		case req.Number != *r:
			return notMine(fmt.Sprintf("the request file holds #%d, not #%d", req.Number, *r))
		case req.State != "open":
			return notMine(fmt.Sprintf("the request #%d is closed — withdrawn before this run started", *r))
		case !req.HasLabel(workitem.OriginAdHoc) && !req.HasLabel(workitem.QueuedLabel):
			return notMine(fmt.Sprintf("the request #%d no longer carries its mark", *r))
		}
	}
	model := task.Decl.AgentModel()
	if fromRequest, _ := task.Decl["model_from_request"].(bool); fromRequest {
		if m := workitem.ParseBody(item.Body).Model; m != "" {
			model = m
		}
	}
	return Validated{Task: *task, TaskPath: path, Model: model, Outcome: task.Decl.Outcome()}, nil
}
