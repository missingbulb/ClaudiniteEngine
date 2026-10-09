package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	usagefold "github.com/missingbulb/ClaudiniteEngine/cn/growth/usage"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/calendar"
	taskscmd "github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/command"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/execute"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/mergepolicy"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/world"
	fleetcmd "github.com/missingbulb/ClaudiniteEngine/cn/fleet/command"
	"github.com/missingbulb/ClaudiniteEngine/cn/growth/capture"
	"github.com/missingbulb/ClaudiniteEngine/cn/growth/prune"
	"github.com/missingbulb/ClaudiniteEngine/cn/growth/retention"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/jsjson"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/hooks"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/settings"
)

// engine is what the scheduler and executor take from the other segments:
// the engine's own tasks, which deliver what packaging (the update) and
// growth (the usage fold, the logs prune) compute, and the fleet signal.
var engine = taskscmd.Engine{
	Work: map[string]func(taskscmd.EngineRun) execute.CodeWorkResult{
		taskspec.UpdateTask: func(e taskscmd.EngineRun) execute.CodeWorkResult {
			return runUpdateTask(e.Repo.Root, e.Token, e.Branch, e.Out)
		},
		taskspec.UsageFoldTask: func(e taskscmd.EngineRun) execute.CodeWorkResult {
			return runUsageFold(e.Repo, e.Client, e.Token, e.Branch, e.Target, e.Out)
		},
		taskspec.LogsPruneTask: func(e taskscmd.EngineRun) execute.CodeWorkResult {
			return runLogsPrune(e.Repo.Root, e.Token, e.Out)
		},
	},
	Fleet: fleetcmd.Signal,
}

// runLogsPrune is the engine/logs-prune task's code-work, run in the
// executor's process: the captures past the repo's retention_days removed
// from the conversation-logs branch in one commit.
func runLogsPrune(root, token string, out io.Writer) execute.CodeWorkResult {
	var said bytes.Buffer
	w := io.MultiWriter(out, &said)
	res := execute.CodeWorkResult{OK: true}
	declared, present, readable := retentionDeclared(root)
	switch days := retention.ResolveRetentionDays(declared, present); {
	case !readable:
		fmt.Fprintln(w, "this repo's claudinite-growth declaration could not be read for a retention — deleting nothing")
	case days == nil:
		if n, ok := retention.Number(declared); ok {
			fmt.Fprintf(w, "retention_days is %s — capture-only by this repo's own choice, deleting nothing\n", jsjson.FormatNumber(n))
		} else {
			fmt.Fprintf(w, "retention_days is unreadable (%v, not a number) — deleting nothing\n", declared)
		}
	default:
		if !present {
			fmt.Fprintf(w, "retention_days is undeclared — using the %vd default\n", *days)
		}
		clock, err := world.Env(os.Getenv).Clock()
		if err != nil {
			return execute.CodeWorkResult{Why: "engine/logs-prune: " + err.Error()}
		}
		git := gitcmd.Repo{Dir: root, Token: token}
		if err := prune.Run(git, capture.DefaultBranch, *days, clock.Now, w); err != nil {
			return execute.CodeWorkResult{Why: "engine/logs-prune: the prune failed", Detail: err.Error()}
		}
	}
	for _, l := range strings.Split(strings.TrimSpace(said.String()), "\n") {
		if l != "" {
			res.Said = append(res.Said, l)
		}
	}
	return res
}

// retentionDeclared is the claudinite-growth entry's retention_days as
// declared: present false where the entry says nothing, readable false
// where the declaration cannot be read or declares no such pack, or the
// value is not a finite number (an unknown is never a licence to delete).
func retentionDeclared(root string) (value any, present, readable bool) {
	path, f, err := settings.Find(root)
	if err != nil {
		return nil, false, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false, false
	}
	p, err := settings.ReadPacks(raw, f)
	if err != nil {
		return nil, false, false
	}
	for _, e := range p.Entries {
		if e.Local || e.ID != hooks.GrowthPack {
			continue
		}
		v, ok := e.Config["retention_days"]
		if !ok {
			return nil, false, true
		}
		if _, num := retention.Number(v); !num {
			return v, true, false
		}
		return v, true, true
	}
	return nil, false, false
}

// runUsageFold is the usage fold run in process: both halves folded over
// the checkout and delivered on the target the executor resolved.
func runUsageFold(r taskscmd.Repo, client *githubapi.Client, token, base string, target execute.Target, out io.Writer) execute.CodeWorkResult {
	log := func(s string) { fmt.Fprintln(out, s) }
	git := gitcmd.Repo{Dir: r.Root, Token: token}
	history := usagefold.History{
		Local: func(args ...string) (string, error) { return usagefold.RunLocal(r.Root, nil, "", args...) },
		Remote: func(args ...string) (string, error) {
			ran, err := git.Run(args...)
			if err != nil {
				return "", err
			}
			if ran.Code != 0 {
				return "", fmt.Errorf("git %s exited %d: %s", args[0], ran.Code, strings.TrimSpace(ran.Stderr))
			}
			return ran.Stdout, nil
		},
	}
	baseSha, err := usagefold.BaseTip(history, base)
	if err != nil {
		return execute.CodeWorkResult{Why: "usage fold: the base branch could not be fetched", Detail: err.Error()}
	}
	var dirs []string
	for _, p := range r.Set.Packs {
		dirs = append(dirs, p.Dir)
	}
	fold := usagefold.Fold{Root: r.Root, Repo: client.Repo, Base: base, BaseSha: baseSha,
		Now: calendar.ISO(time.Now()), Reader: apiReader{client}, History: history,
		Mounted: usagefold.MountedSkills(dirs), MinuteRate: r.Queue.MinuteRate, Log: log}
	delivery := usagefold.Delivery{Root: r.Root, Base: base, Branch: target.Branch, PR: target.PR, History: history,
		Trailers: usageFoldTrailers(r.Tasks),
		OpenPr: func(title, body, head, base string) (int, error) {
			p, err := client.CreatePull(title, body, head, base)
			return p.Number, err
		}}
	landed, err := usagefold.DeliverFolds(fold.Halves(), delivery.Deliver, log)
	res := execute.CodeWorkResult{OK: err == nil}
	if landed != nil {
		res.DeliveredPR, res.Branch = landed.Number, landed.Branch
	}
	if err != nil {
		res.Why = err.Error()
	}
	return res
}

// usageFoldTrailers are the trailers every fold commit carries: the task,
// and the automerge it declares.
func usageFoldTrailers(tasks []taskspec.Task) string {
	path := taskspec.BuiltinPack + "/" + taskspec.UsageFoldTask
	lines := workitem.TaskTrailer + ": " + path
	for _, t := range tasks {
		if t.Path() == path {
			if expr := mergepolicy.Expression(t.Decl["automerge"]); expr != "" {
				lines += "\n" + workitem.AutomergeTrailer + ": " + expr
			}
		}
	}
	return lines
}

// apiReader is the folds' REST surface over the job's client.
type apiReader struct{ c *githubapi.Client }

func (a apiReader) JSON(path string) (any, error) {
	status, body, err := a.c.Raw("GET", path, nil)
	if err != nil || status != 200 {
		return nil, err
	}
	v, err := usagefold.ParseJSON(string(body))
	if err != nil {
		return nil, nil
	}
	return v, nil
}

func (a apiReader) Text(path string) (*string, error) {
	status, body, err := a.c.Raw("GET", path, nil)
	if err != nil || status != 200 {
		return nil, err
	}
	s := string(body)
	return &s, nil
}
