package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/calendar"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/execute"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/mergepolicy"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/precondition"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/land"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/queue"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/world"
)

// tasksAnswers are the pure decision cores `cndecide tasks <kind> --world F`
// answers over a fixture: the parity harness's tasks face.
var tasksAnswers = map[string]func(raw []byte) (any, error){
	"contract":     answerContract,
	"precondition": answerPrecondition,
	"policy":       answerPolicy,
	"grammar":      answerGrammar,
	"outcome":      answerOutcome,
	"queue":        answerQueue,
	"schedule":     answerSchedule,
	"claim":        answerClaim,
}

// answerClaim is the claim arbiter: each comment list's winning claim id
// (null for none), and whether each won claim yields to an earlier one.
func answerClaim(raw []byte) (any, error) {
	var in struct {
		Winners   [][]world.Comment `json:"winners"`
		Conflicts []struct {
			Item      workitem.Issue `json:"item"`
			MyClaimID int64          `json:"myClaimId"`
			Others    []struct {
				workitem.Issue
				ClaimID int64 `json:"claimId"`
			} `json:"others"`
			TaskAfter map[string][]string `json:"taskAfter"`
			Scheduled map[string]bool     `json:"scheduled"`
		} `json:"conflicts"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	winners := []any{}
	for _, cs := range in.Winners {
		if w := execute.ClaimWinner(cs); w != nil {
			winners = append(winners, w.ID)
		} else {
			winners = append(winners, nil)
		}
	}
	conflicts := []bool{}
	for _, k := range in.Conflicts {
		others := make([]execute.Claimed, 0, len(k.Others))
		for _, o := range k.Others {
			others = append(others, execute.Claimed{Issue: o.Issue, ClaimID: o.ClaimID})
		}
		conflicts = append(conflicts, execute.ConflictsWithEarlierClaim(k.Item, k.MyClaimID, others, queue.PickOpts{
			TaskAfter: func(id string) []string { return k.TaskAfter[id] },
			ScheduledOf: func(id string) workitem.Scheduled {
				v, ok := k.Scheduled[id]
				switch {
				case !ok:
					return workitem.Unknown
				case v:
					return workitem.Yes
				}
				return workitem.No
			},
		}))
	}
	return map[string]any{"winners": winners, "conflicts": conflicts}, nil
}

type fixtureTerm struct {
	Signals   []string `json:"signals"`
	NeedsItem bool     `json:"needsItem"`
	TakesArg  bool     `json:"takesArg"`
}

func fixtureTerms(m map[string]fixtureTerm) taskspec.Terms {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	var out taskspec.Terms
	for _, n := range names {
		t := m[n]
		out = append(out, taskspec.TermSpec{Name: n, Signals: t.Signals, NeedsItem: t.NeedsItem, TakesArg: t.TakesArg})
	}
	return out
}

func answerContract(raw []byte) (any, error) {
	var in struct {
		Declarations []struct {
			Declaration any                    `json:"declaration"`
			Terms       map[string]fixtureTerm `json:"terms"`
		} `json:"declarations"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	out := []any{}
	for _, d := range in.Declarations {
		terms := fixtureTerms(d.Terms)
		n := taskspec.Normalize(d.Declaration)
		decl, _ := n.(taskspec.Decl)
		signals := taskspec.Signals(decl.Preconditions(), terms)
		if decl == nil {
			signals = taskspec.Signals([]any{}, terms)
		}
		sort.Strings(signals)
		out = append(out, map[string]any{
			"normalized": n,
			"problems":   taskspec.Validate(d.Declaration, terms),
			"signals":    signals,
			"needsItem":  decl != nil && taskspec.NeedsItem(decl.Preconditions(), terms),
			"cadence":    decl.Cadence(),
			"scheduled":  decl.IsScheduled(),
			"codeWork":   decl.DeclaresCodeWork(),
		})
	}
	return out, nil
}

func answerPrecondition(raw []byte) (any, error) {
	var in struct {
		Cases []struct {
			Preconditions any                  `json:"preconditions"`
			Signals       precondition.Signals `json:"signals"`
			Config        map[string]any       `json:"config"`
			Item          *precondition.Item   `json:"item"`
			Now           *string              `json:"now"`
			Partial       bool                 `json:"partial"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	out := []any{}
	for _, c := range in.Cases {
		var now *time.Time
		if c.Now != nil {
			if t, ok := precondition.ParseInstant(*c.Now); ok {
				now = &t
			}
		}
		decl := taskspec.Decl{"preconditions": c.Preconditions}
		out = append(out, precondition.Evaluate(precondition.Input{
			Preconditions: c.Preconditions, Signals: c.Signals, Config: c.Config, Item: c.Item,
			WindowDays: precondition.WindowDays(decl, c.Signals), Now: now, Partial: c.Partial,
		}))
	}
	return out, nil
}

func answerPolicy(raw []byte) (any, error) {
	var in struct {
		Packs []struct {
			ID    string `json:"id"`
			Rules any    `json:"rules"`
		} `json:"packs"`
		Cases []struct {
			Policy  any                 `json:"policy"`
			Entries []mergepolicy.Entry `json:"entries"`
		} `json:"cases"`
		Normalize []any    `json:"normalize"`
		Classify  []string `json:"classify"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	var packs []mergepolicy.PackRules
	for _, p := range in.Packs {
		packs = append(packs, mergepolicy.PackRules{ID: p.ID, File: mergepolicy.RulesFile + ".json", Specs: p.Rules})
	}
	declared := mergepolicy.Compile(packs)
	norm := []any{}
	for _, r := range in.Normalize {
		norm = append(norm, map[string]any{"policy": mergepolicy.Normalize(r), "expression": mergepolicy.Expression(r)})
	}
	classes := []string{}
	for _, f := range in.Classify {
		classes = append(classes, mergepolicy.ClassifyPath(f))
	}
	verdicts := []any{}
	for _, c := range in.Cases {
		verdicts = append(verdicts, mergepolicy.Judge(c.Policy, c.Entries, declared))
	}
	errs := declared.Errors
	if errs == nil {
		errs = []string{}
	}
	return map[string]any{"ruleErrors": errs, "normalized": norm, "classes": classes, "verdicts": verdicts}, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func answerGrammar(raw []byte) (any, error) {
	var in struct {
		Titles []string         `json:"titles"`
		Bodies []string         `json:"bodies"`
		Issues []workitem.Issue `json:"issues"`
		Build  []struct {
			TaskPath  string   `json:"taskPath"`
			NotBefore string   `json:"notBefore"`
			BlockedBy []int    `json:"blockedBy"`
			Context   []string `json:"context"`
			Delivered []string `json:"delivered"`
			Reason    string   `json:"reason"`
			Request   int      `json:"request"`
			Model     string   `json:"model"`
			Merge     string   `json:"merge"`
			Woken     string   `json:"woken"`
		} `json:"build"`
		Edits []struct {
			Op      string          `json:"op"`
			Body    string          `json:"body"`
			Value   json.RawMessage `json:"value"`
			Heading string          `json:"heading"`
			Lines   []string        `json:"lines"`
		} `json:"edits"`
		Messages []struct {
			Message string `json:"message"`
			Task    string `json:"task"`
		} `json:"messages"`
		TaskPaths []string `json:"taskPaths"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	titles := []any{}
	for _, t := range in.Titles {
		if p, ok := workitem.ParseTitle(t); ok {
			titles = append(titles, map[string]any{"pack": p.Pack, "task": p.Task, "qualifier": nullable(p.Qualifier)})
		} else {
			titles = append(titles, nil)
		}
	}
	bodies := []any{}
	for _, b := range in.Bodies {
		var lv any
		if v, ok := workitem.ParseLastVerdict(b); ok {
			lv = v
		}
		bodies = append(bodies, map[string]any{
			"fields": workitem.ParseFields(b), "context": workitem.ContextLines(b), "progress": workitem.ProgressLines(b),
			"lastVerdict": lv, "request": workitem.ParseRequestFields(b, true), "requestUngated": workitem.ParseRequestFields(b, false),
			"human": workitem.HumanTextOf(b),
		})
	}
	issues := []any{}
	for _, i := range in.Issues {
		issues = append(issues, map[string]any{
			"status": nullable(i.Status()), "statuses": nonNilStrings(workitem.StatusesOn(i.Labels)), "park": nullable(i.ParkKind()),
			"origin": nullable(i.Origin()), "outcome": nullable(i.Outcome()), "queueItem": i.IsQueueItem(),
			"blockingPark": i.IsBlockingPark(), "facts": workitem.ItemFacts(i),
		})
	}
	built := []string{}
	for _, b := range in.Build {
		built = append(built, workitem.Body(workitem.BodySpec{
			TaskPath: b.TaskPath, NotBefore: b.NotBefore, BlockedBy: b.BlockedBy, Context: b.Context, Delivered: b.Delivered,
			Reason: b.Reason, Request: b.Request, Model: b.Model, Merge: b.Merge, Woken: b.Woken,
		}))
	}
	edits := []string{}
	for _, e := range in.Edits {
		var s string
		var n int
		var target workitem.Target
		switch e.Op {
		case "notBefore":
			_ = json.Unmarshal(e.Value, &s)
			edits = append(edits, workitem.WithNotBefore(e.Body, s))
		case "woken":
			_ = json.Unmarshal(e.Value, &s)
			edits = append(edits, workitem.WithWoken(e.Body, s))
		case "endsWhen":
			_ = json.Unmarshal(e.Value, &n)
			edits = append(edits, workitem.WithEndsWhen(e.Body, n))
		case "target":
			if err := json.Unmarshal(e.Value, &target); err != nil {
				return nil, err
			}
			edits = append(edits, workitem.WithTarget(e.Body, target))
		case "section":
			edits = append(edits, workitem.WithSection(e.Body, e.Heading, e.Lines))
		default:
			return nil, fmt.Errorf("unknown edit %q", e.Op)
		}
	}
	trailers := []any{}
	for _, m := range in.Messages {
		trailers = append(trailers, map[string]any{"task": nullable(workitem.TaskFromMessage(m.Message)), "stamped": workitem.WithTaskTrailer(m.Message, m.Task)})
	}
	paths := []any{}
	for _, p := range in.TaskPaths {
		if t, ok := workitem.TaskIDFromPath(p); ok {
			paths = append(paths, map[string]any{"pack": t.Pack, "task": t.Task})
		} else {
			paths = append(paths, nil)
		}
	}
	return map[string]any{"titles": titles, "bodies": bodies, "issues": issues, "built": built, "edits": edits, "trailers": trailers, "taskPaths": paths}, nil
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func answerOutcome(raw []byte) (any, error) {
	var in struct {
		Cases []struct {
			Outcome   string `json:"outcome"`
			Automerge any    `json:"automerge"`
			OpenedPR  bool   `json:"openedPr"`
			MergedPR  bool   `json:"mergedPr"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	out := []any{}
	for _, c := range in.Cases {
		out = append(out, land.VerifyOutcome(c.Outcome, c.Automerge, c.OpenedPR, c.MergedPR))
	}
	return out, nil
}

func answerQueue(raw []byte) (any, error) {
	var in struct {
		Picks []struct {
			Open      []workitem.Issue    `json:"open"`
			Draws     []float64           `json:"draws"`
			TaskAfter map[string][]string `json:"taskAfter"`
			Scheduled map[string]bool     `json:"scheduled"`
		} `json:"picks"`
		Releasable []struct {
			Item   workitem.Issue    `json:"item"`
			States map[string]string `json:"states"`
			Now    string            `json:"now"`
		} `json:"releasable"`
		Liveness [][]world.Comment `json:"liveness"`
		Progress []struct {
			Body, Line string
		} `json:"progress"`
		Beats []struct {
			Executor string  `json:"executor"`
			At       string  `json:"at"`
			Minutes  int     `json:"minutes"`
			Session  *string `json:"session"`
			Note     string  `json:"note"`
		} `json:"beats"`
		Records    []string         `json:"records"`
		RenderExec []queue.TaskExec `json:"renderExec"`
		RenderCost []queue.RunCost  `json:"renderCost"`
		Anchors    []struct {
			Frequency, Now string
		} `json:"anchors"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	iso := func(t time.Time) any {
		if t.IsZero() {
			return nil
		}
		return calendar.ISO(t)
	}
	picks := []any{}
	for _, k := range in.Picks {
		draws := append([]float64{}, k.Draws...)
		order := queue.PickOrder(k.Open, queue.PickOpts{
			TaskAfter: func(id string) []string { return k.TaskAfter[id] },
			ScheduledOf: func(id string) workitem.Scheduled {
				v, ok := k.Scheduled[id]
				switch {
				case !ok:
					return workitem.Unknown
				case v:
					return workitem.Yes
				}
				return workitem.No
			},
			Draw: func() float64 {
				if len(draws) == 0 {
					return 0
				}
				d := draws[0]
				draws = draws[1:]
				return d
			},
		})
		nums := []int{}
		for _, i := range order {
			nums = append(nums, i.Number)
		}
		picks = append(picks, nums)
	}
	releasable := []bool{}
	for _, k := range in.Releasable {
		now, _ := calendar.ParseInstant(k.Now)
		releasable = append(releasable, queue.IsReleasable(k.Item, func(n int) string { return k.States[strconv.Itoa(n)] }, now))
	}
	liveness := []any{}
	for _, cs := range in.Liveness {
		liveness = append(liveness, map[string]any{"live": iso(queue.LastLivenessAt(cs)), "progress": iso(queue.LastProgressAt(cs))})
	}
	progress := []string{}
	for _, k := range in.Progress {
		progress = append(progress, queue.WithProgress(k.Body, k.Line))
	}
	beats := []string{}
	for _, k := range in.Beats {
		if k.Session == nil {
			beats = append(beats, queue.HeartbeatComment(k.Executor, k.At, k.Minutes))
		} else {
			beats = append(beats, queue.AgentBeatComment(*k.Session, k.At, k.Note))
		}
	}
	records := []any{}
	orNil := func(v any, ok bool) any {
		if !ok {
			return nil
		}
		return v
	}
	for _, l := range in.Records {
		exec, eok := queue.ParseTaskExec(l)
		run, rok := queue.ParseTaskRun(l)
		cost, cok := queue.ParseRunCost(l)
		var runOut any
		if rok {
			runOut = map[string]string{"pack": run.Pack, "task": run.Task, "slotId": run.Slot, "outcome": run.Status}
		}
		records = append(records, map[string]any{"exec": orNil(exec, eok), "run": runOut, "cost": orNil(cost, cok)})
	}
	rendered := map[string][]string{"exec": {}, "cost": {}}
	for _, r := range in.RenderExec {
		rendered["exec"] = append(rendered["exec"], queue.RenderTaskExec(r))
	}
	for _, r := range in.RenderCost {
		rendered["cost"] = append(rendered["cost"], queue.RenderRunCost(r))
	}
	anchors := []any{}
	for _, k := range in.Anchors {
		now, _ := calendar.ParseInstant(k.Now)
		var next, period any
		if t, ok := calendar.NextAnchor(k.Frequency, now); ok {
			next = calendar.ISO(t)
		}
		if d, ok := calendar.Period(k.Frequency); ok {
			period = d.Milliseconds()
		}
		anchors = append(anchors, map[string]any{"next": next, "period": period})
	}
	return map[string]any{
		"picks": picks, "releasable": releasable, "liveness": liveness, "progress": progress,
		"beats": beats, "records": records, "rendered": rendered, "anchors": anchors,
	}, nil
}
