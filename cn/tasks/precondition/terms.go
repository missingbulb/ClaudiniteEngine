package precondition

import (
	"strings"
	"time"

	sharedgrowth "github.com/missingbulb/ClaudiniteEngine/cn/shared/growth"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/jsjson"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/calendar"
)

type holdsFunc func(Signals, Opts) Outcome

// ParseInstant is calendar.ParseInstant, where the term vocabulary
// reads it.
func ParseInstant(s string) (time.Time, bool) { return calendar.ParseInstant(s) }

func onOrAfter(p *string, at time.Time) bool {
	if p == nil {
		return false
	}
	t, ok := ParseInstant(*p)
	return ok && !t.Before(at)
}

func wokenReason(item *Item) *Outcome {
	if item == nil || !item.Woken {
		return nil
	}
	n := "?"
	if item.Number != nil {
		n = itoa(*item.Number)
	}
	return &Outcome{Holds: true, Reason: "#" + n + " was woken by hand — the wake stands in for the cadence"}
}

func cappedContext(items []string, lead, dropTail string) []string {
	scope := items
	if len(scope) > MaxContextItems {
		scope = scope[:MaxContextItems]
	}
	out := []string{lead + ": " + strings.Join(scope, ", ") + "."}
	if dropped := len(items) - len(scope); dropped > 0 {
		out = append(out, itoa(dropped)+" further "+dropTail)
	}
	return out
}

func runsOf(s Signals) []Run {
	if s.Runs == nil {
		return nil
	}
	return s.Runs.List
}

func commitsOf(s Signals) Commits {
	if s.Commits == nil {
		return Commits{}
	}
	return *s.Commits
}

func nonTaskIssues(s Signals) (open []OpenIssue, touched []int) {
	if s.Issues == nil {
		return nil, nil
	}
	in := map[int]bool{}
	for _, i := range s.Issues.Open {
		task := false
		for _, l := range i.Labels {
			if strings.HasPrefix(l, "task:") {
				task = true
			}
		}
		if !task {
			open = append(open, i)
			in[i.Number] = true
		}
	}
	for _, n := range s.Issues.Touched {
		if in[n] {
			touched = append(touched, n)
		}
	}
	return open, touched
}

func prsTouched(s Signals) []int {
	if s.PRs == nil {
		return nil
	}
	return s.PRs.Touched
}

func openPRs(s Signals) []OpenPR {
	if s.PRs == nil {
		return nil
	}
	return s.PRs.Open
}

// captured reports a conversation log captured inside the window; an
// unknown age is not movement.
func captured(s Signals, windowDays float64) bool {
	return s.ConversationLogs != nil && s.ConversationLogs.NewestLogAgeDays != nil && *s.ConversationLogs.NewestLogAgeDays <= windowDays
}

func numbersOf(ns []int) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = "#" + itoa(n)
	}
	return out
}

func scheduleHolds(s Signals, o Opts) Outcome {
	if w := wokenReason(o.Item); w != nil {
		return *w
	}
	arg := ""
	if o.Arg != nil {
		arg = *o.Arg
	}
	cadence := taskspec.CadenceOfScheduleArg(arg)
	if o.Now == nil {
		return Outcome{Error: taskspec.ScheduleTerm + ":" + arg + " has no instant to place in a period: the caller supplied no `now`"}
	}
	opened, _, _ := calendar.PeriodStart(cadence, *o.Now)
	for _, r := range runsOf(s) {
		if r.NeverRan() {
			continue
		}
		if onOrAfter(r.CreatedAt, opened) || onOrAfter(r.ClosedAt, opened) {
			return Outcome{Reason: "#" + itoa(r.Number) + " already ran in the " + cadence + " period that opened " + calendar.ISO(opened)}
		}
	}
	return Outcome{Holds: true, Reason: "no run in the " + cadence + " period that opened " + calendar.ISO(opened)}
}

var holds map[string]holdsFunc

func init() {
	holds = map[string]holdsFunc{
		taskspec.ScheduleTerm: scheduleHolds,
		taskspec.DueTerm: func(s Signals, o Opts) Outcome {
			arg := taskspec.AtMostPrefix
			if o.Arg != nil {
				arg += *o.Arg
			}
			o.Arg = &arg
			return scheduleHolds(s, o)
		},
		taskspec.NotFailedTerm: func(s Signals, _ Opts) Outcome {
			runs := runsOf(s)
			if len(runs) == 0 {
				return Outcome{Holds: true, Reason: "no run of this task to have failed"}
			}
			n := runs[0]
			if n.Park != nil && *n.Park == "failure" {
				return Outcome{Reason: "the newest run, #" + itoa(n.Number) + ", stands at a failure park — this task declares it does not run past its own failure"}
			}
			return Outcome{Holds: true, Reason: "the newest run, #" + itoa(n.Number) + ", did not fail"}
		},
		taskspec.NotParkedTerm: func(s Signals, _ Opts) Outcome {
			runs := runsOf(s)
			if len(runs) == 0 {
				return Outcome{Holds: true, Reason: "no run of this task to be parked"}
			}
			n := runs[0]
			if n.Park != nil && *n.Park != "" {
				return Outcome{Reason: "the newest run, #" + itoa(n.Number) + ", stands at a " + *n.Park + " park — this task declares it does not run past a park of its own"}
			}
			return Outcome{Holds: true, Reason: "the newest run, #" + itoa(n.Number) + ", is not parked"}
		},
		"repo-active": func(s Signals, o Opts) Outcome {
			var moved []string
			if commitsOf(s).SubstantiveChange {
				moved = append(moved, "a substantive commit landed")
			}
			if _, touched := nonTaskIssues(s); len(touched) > 0 {
				moved = append(moved, itoa(len(touched))+" issue(s) moved")
			}
			if t := prsTouched(s); len(t) > 0 {
				moved = append(moved, itoa(len(t))+" open PR(s) moved")
			}
			if captured(s, o.WindowDays) {
				moved = append(moved, "a session was captured")
			}
			if len(moved) > 0 {
				return Outcome{Holds: true, Reason: "the repo was active in the window — " + strings.Join(moved, ", ")}
			}
			return Outcome{Reason: "the repo was silent in the window — no substantive commit, no issue or PR of its own moved, and no session was captured"}
		},
		"substantive-change": func(s Signals, _ Opts) Outcome {
			c := commitsOf(s)
			if !c.SubstantiveChange {
				return Outcome{Reason: "no substantive default-branch change in the window"}
			}
			var shas []string
			for _, cm := range c.List {
				if cm.Substantive {
					sha := cm.SHA
					if len(sha) > 7 {
						sha = sha[:7]
					}
					shas = append(shas, sha)
				}
			}
			n := "a"
			var ctx []string
			if len(shas) > 0 {
				n = itoa(len(shas))
				ctx = cappedContext(shas, "Substantive commits in the window", "commit(s) are not named here.")
			}
			return Outcome{Holds: true, Reason: n + " substantive default-branch commit(s) in the window", Context: ctx}
		},
		"any-commit": func(s Signals, _ Opts) Outcome {
			if n := commitsOf(s).Count; n > 0 {
				return Outcome{Holds: true, Reason: jsjson.FormatNumber(n) + " default-branch commit(s) in the window"}
			}
			return Outcome{Reason: "no default-branch commit in the window"}
		},
		"session-captured": func(s Signals, o Opts) Outcome {
			if captured(s, o.WindowDays) {
				return Outcome{Holds: true, Reason: "a conversation log was captured in the window"}
			}
			return Outcome{Reason: "no conversation log was captured in the window"}
		},
		taskspec.LogPastRetention: logPastRetention,
		"issues-touched": func(s Signals, _ Opts) Outcome {
			if _, touched := nonTaskIssues(s); len(touched) > 0 {
				return Outcome{Holds: true, Reason: itoa(len(touched)) + " issue(s) moved in the window",
					Context: cappedContext(numbersOf(touched), "Issues touched in the window", "touched issue(s) are not named here.")}
			}
			return Outcome{Reason: "no issue of this repo's own moved in the window"}
		},
		"prs-touched": func(s Signals, _ Opts) Outcome {
			if t := prsTouched(s); len(t) > 0 {
				return Outcome{Holds: true, Reason: itoa(len(t)) + " open PR(s) moved in the window",
					Context: cappedContext(numbersOf(t), "PRs opened or updated in the window", "moved PR(s) are not named here.")}
			}
			return Outcome{Reason: "no open PR was opened or updated in the window"}
		},
		"mount-moved": func(s Signals, _ Opts) Outcome {
			if s.SharedMount != nil && len(s.SharedMount.ChangedPacks) > 0 {
				packs := strings.Join(s.SharedMount.ChangedPacks, ", ")
				return Outcome{Holds: true, Reason: "declared pack(s) changed in the mounted canon: " + packs, Context: []string{"Canon packs that changed in the window: " + packs + "."}}
			}
			return Outcome{Reason: "no declared pack's vendored files changed in the window"}
		},
		"commits-under": func(s Signals, o Opts) Outcome {
			arg := *o.Arg
			var under []string
			for _, p := range commitsOf(s).TouchedPaths {
				if strings.HasPrefix(p, arg) {
					under = append(under, p)
				}
			}
			if len(under) > 0 {
				return Outcome{Holds: true, Reason: itoa(len(under)) + " path(s) under " + arg + " changed in the window",
					Context: cappedContext(under, "Paths under "+arg+" that changed in the window", "path(s) under "+arg+" are not named here.")}
			}
			return Outcome{Reason: "no path under " + arg + " changed in the window"}
		},
		"commits-outside": func(s Signals, o Opts) Outcome {
			arg := *o.Arg
			var outside []string
			for _, p := range commitsOf(s).TouchedPaths {
				if !strings.HasPrefix(p, arg) {
					outside = append(outside, p)
				}
			}
			if len(outside) > 0 {
				return Outcome{Holds: true, Reason: itoa(len(outside)) + " path(s) outside " + arg + " changed in the window",
					Context: cappedContext(outside, "Paths outside "+arg+" that changed in the window — work exactly these, and no others",
						"path(s) changed in the window and are NOT in scope this round — say so in the wrap-up, so it is not read as a full sweep.")}
			}
			return Outcome{Reason: "nothing outside " + arg + " changed in the window"}
		},
		"no-open-pr-touching": func(s Signals, o Opts) Outcome {
			arg := *o.Arg
			for _, p := range openPRs(s) {
				for _, f := range p.ChangedPaths {
					if strings.HasPrefix(f, arg) {
						return Outcome{Reason: "PR #" + itoa(p.Number) + " has a pending " + arg + " change — this round waits for its review rather than stack a second unreviewed one on it"}
					}
				}
			}
			for _, p := range openPRs(s) {
				if p.ChangedPaths == nil {
					return Outcome{Reason: "PR #" + itoa(p.Number) + "'s changed paths could not be read, so whether a " + arg + " change is pending is unknown — a skipped round is cheaper than an unreviewed one stacked on it"}
				}
			}
			return Outcome{Holds: true, Reason: "no open PR changes a path under " + arg}
		},
		"no-open-pr-titled": func(s Signals, o Opts) Outcome {
			arg := *o.Arg
			for _, p := range openPRs(s) {
				if strings.HasPrefix(p.Title, arg) {
					return Outcome{Reason: "PR #" + itoa(p.Number) + " is this pass's previous round, still open — this round waits for it to land rather than stack a second sweep on it"}
				}
			}
			return Outcome{Holds: true, Reason: `no open PR titled "` + arg + `…" — the previous round has landed`}
		},
	}
}

// PushPermissions are the permissions that count as push access.
var PushPermissions = []string{"admin", "maintain", "write"}

var engineHolds = map[string]holdsFunc{
	taskspec.RequestEligible: func(s Signals, o Opts) Outcome {
		req := s.Request
		if req == nil {
			field := "missing"
			if o.Item != nil && o.Item.Request != nil {
				field = "#" + itoa(*o.Item.Request)
			}
			return Outcome{Error: "this item names no readable request (its `Request:` field is " + field + ")"}
		}
		n := "#" + itoa(req.Number)
		switch {
		case req.Unreadable:
			return Outcome{Error: "issue " + n + " could not be read: " + req.Error + " — refusing to guess"}
		case req.Gone:
			return Outcome{Reason: "issue " + n + " does not exist"}
		case req.State != "open":
			return Outcome{Reason: "issue " + n + " was closed before this ran"}
		case !req.Queued:
			return Outcome{Reason: "issue " + n + " no longer carries the mark — the request was withdrawn"}
		}
		if has(PushPermissions, req.AuthorPermission) {
			return Outcome{Holds: true, Reason: n + ": opened by @" + req.Author + ", who has push access"}
		}
		for _, a := range req.Approvals {
			if has(PushPermissions, a.Permission) {
				return Outcome{Holds: true, Reason: n + ": approved by @" + a.Login + " with `/claude go`"}
			}
		}
		return Outcome{Reason: n + ": neither opened nor approved with `/claude go` by anyone with push access on this repository"}
	},
}

// EngineJudged reports whether the engine answers the term itself: a
// built-in, or the engine's own task's term. Anything else a task names
// is its preconditions.mjs's, asked through the runner.
func EngineJudged(name string) bool {
	_, builtin := holds[name]
	_, engine := engineHolds[name]
	return builtin || engine
}

// logPastRetention holds on no reading at all: nothing asks the prune, so
// its item exists only because a person made one, and the code-work reads
// the branch first-hand. An absent branch, retention off, an unreadable
// retention or an unknown oldest age declines.
func logPastRetention(s Signals, _ Opts) Outcome {
	logs := s.ConversationLogs
	if logs == nil {
		return Outcome{Holds: true, Reason: "no conversation-logs reading — the worker decides what is deletable"}
	}
	if !logs.Present {
		return Outcome{Reason: "no conversation-logs branch — nothing captured yet"}
	}
	if logs.RetentionUnreadable {
		return Outcome{Reason: "retention_days is unreadable (not a number) — the prune deletes nothing"}
	}
	var declared any
	if logs.RetentionDays != nil {
		declared = *logs.RetentionDays
	}
	retention := sharedgrowth.ResolveRetentionDays(declared, true)
	if retention == nil {
		return Outcome{Reason: "retention_days is " + jsjson.FormatNumber(*logs.RetentionDays) + " — capture-only by this repo's own choice, so the prune deletes nothing"}
	}
	days := jsjson.FormatNumber(*retention)
	oldest := logs.OldestLogAgeDays
	if oldest == nil || !(*oldest > *retention) {
		return Outcome{Reason: "no log older than retention " + days + "d — nothing to prune"}
	}
	return Outcome{Holds: true, Reason: "oldest log " + jsjson.ToFixed(*oldest, 1) + "d old vs retention " + days + "d"}
}
