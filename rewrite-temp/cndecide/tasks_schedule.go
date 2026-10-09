package main

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/calendar"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/precondition"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/schedule"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/queue"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/workitem"
)

type fixtureTask struct {
	Pack     string         `json:"pack"`
	ID       string         `json:"id"`
	TaskPath string         `json:"taskPath"`
	Decl     map[string]any `json:"decl"`
}

func fixtureTasks(specs []fixtureTask) []taskspec.Task {
	out := make([]taskspec.Task, 0, len(specs))
	for _, s := range specs {
		t := taskspec.Task{Pack: s.Pack, ID: s.ID, Decl: taskspec.Normalize(s.Decl).(taskspec.Decl)}
		t.Rel = strings.TrimSuffix(s.TaskPath, "/task.md")
		out = append(out, t)
	}
	return out
}

type fixtureItem struct {
	workitem.Issue
	LivenessAt string `json:"livenessAt"`
}

type fixtureRequest struct {
	workitem.Issue
	AuthorHasPush *bool `json:"authorHasPush"`
}

type fixtureVerdict struct {
	Run    *bool  `json:"run"`
	Reason string `json:"reason"`
	Error  string `json:"error"`
}

func answerSchedule(raw []byte) (any, error) {
	var in struct {
		Plans []struct {
			Tasks       []fixtureTask             `json:"tasks"`
			Items       []fixtureItem             `json:"items"`
			Requests    []fixtureRequest          `json:"requests"`
			Now         string                    `json:"now"`
			Disabled    []string                  `json:"disabled"`
			States      map[string]string         `json:"states"`
			Verdicts    map[string]fixtureVerdict `json:"verdicts"`
			Progress    map[string]string         `json:"progress"`
			Resolutions map[string]string         `json:"resolutions"`
			Done        []workitem.Issue          `json:"done"`
		} `json:"plans"`
		Wakes []struct {
			Spec  string           `json:"spec"`
			Tasks []fixtureTask    `json:"tasks"`
			Items []workitem.Issue `json:"items"`
		} `json:"wakes"`
		Pickable []struct {
			Open      []workitem.Issue `json:"open"`
			Readied   []int            `json:"readied"`
			Scheduled map[string]bool  `json:"scheduled"`
		} `json:"pickable"`
		Blockers []struct {
			Items    []workitem.Issue  `json:"items"`
			Requests []fixtureRequest  `json:"requests"`
			Known    map[string]string `json:"known"`
		} `json:"blockers"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	at := func(s string) time.Time {
		t, _ := calendar.ParseInstant(s)
		return t
	}
	key := strconv.Itoa
	plans := []any{}
	for _, k := range in.Plans {
		items := make([]workitem.Issue, len(k.Items))
		liveness := map[int]time.Time{}
		for i, it := range k.Items {
			items[i] = it.Issue
			if it.LivenessAt != "" {
				liveness[it.Number] = at(it.LivenessAt)
			}
		}
		reqs := make([]schedule.Request, len(k.Requests))
		for i, r := range k.Requests {
			reqs[i] = schedule.Request{Issue: r.Issue, AuthorHasPush: r.AuthorHasPush}
		}
		ops, asked, err := schedule.Plan(schedule.PlanIn{
			Tasks: fixtureTasks(k.Tasks), Items: items, Requests: reqs, Now: at(k.Now), Disabled: k.Disabled,
			LivenessAt: func(n int) time.Time { return liveness[n] },
			StateOf:    func(n int) string { return k.States[key(n)] },
			Evaluate: func(t taskspec.Task) precondition.Verdict {
				v, ok := k.Verdicts[t.Path()]
				if !ok {
					f := false
					return precondition.Verdict{Run: &f, Reason: "unstated"}
				}
				return precondition.Verdict{Run: v.Run, Reason: v.Reason, Error: v.Error}
			},
			ProgressAt: func(i workitem.Issue) time.Time {
				if s, ok := k.Progress[key(i.Number)]; ok {
					return at(s)
				}
				return time.Time{}
			},
			ResolutionOf: func(n int) string { return k.Resolutions[key(n)] },
			DoneAfter:    schedule.DoneRunLookup(k.Done),
		})
		if err != nil {
			return nil, err
		}
		after := make([]any, len(items))
		for i, it := range items {
			after[i] = map[string]any{"number": it.Number, "state": it.State, "labels": nonNilStrings(it.Labels)}
		}
		plans = append(plans, map[string]any{"ops": ops, "asked": asked, "items": after})
	}
	wakes := []any{}
	for _, k := range in.Wakes {
		wakes = append(wakes, schedule.PlanWake(k.Spec, fixtureTasks(k.Tasks), k.Items))
	}
	pickable := []int{}
	for _, k := range in.Pickable {
		pickable = append(pickable, schedule.PickableCount(k.Open, k.Readied, queue.PickOpts{
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
	blockers := []any{}
	for _, k := range in.Blockers {
		known := map[int]string{}
		for n, s := range k.Known {
			v, _ := strconv.Atoi(n)
			known[v] = s
		}
		reqs := make([]schedule.Request, len(k.Requests))
		for i, r := range k.Requests {
			reqs[i] = schedule.Request{Issue: r.Issue}
		}
		got := schedule.BlockersToResolve(k.Items, reqs, known)
		sort.Ints(got)
		if got == nil {
			got = []int{}
		}
		blockers = append(blockers, got)
	}
	return map[string]any{"plans": plans, "wakes": wakes, "pickable": pickable, "blockers": blockers}, nil
}
