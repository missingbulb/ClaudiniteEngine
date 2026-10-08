package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/mergepolicy"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/calendar"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/execute"
	usagefold "github.com/missingbulb/ClaudiniteEngine/cn/tasks/usage"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/world"
)

// cmdUsage is `cn usage fold`, the engine/usage-fold task's code-work run
// by hand: the target comes from the variables the executor sets.
func cmdUsage(args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "fold" {
		return report.New(report.Usage, "usage takes fold")
	}
	fs := flag.NewFlagSet("usage fold", flag.ContinueOnError)
	repo := fs.String("repo", "", "")
	if err := flags(fs, args[1:]); err != nil {
		return err
	}
	root, err := codeWorkRoot(*repo)
	if err != nil {
		return report.Wrap(report.IO, "usage fold", err)
	}
	r, err := loadTaskRepo(root)
	if err != nil {
		return err
	}
	env := world.Env(os.Getenv)
	token := env("GITHUB_TOKEN")
	client, err := jobClient(env)
	if err != nil {
		return err
	}
	pr, _ := strconv.Atoi(env("CLAUDINITE_TARGET_PR"))
	target := execute.Target{Mode: env("CLAUDINITE_TARGET_MODE"), Branch: env("CLAUDINITE_TARGET_BRANCH"), PR: pr}
	res := runUsageFold(r, client, token, env.DefaultBranch(), target, stdout)
	if !res.OK {
		return report.New(report.IO, res.Why)
	}
	return nil
}

// runUsageFold is the usage fold run in process: both halves folded over
// the checkout and delivered on the target the executor resolved.
func runUsageFold(r taskRepo, client *githubapi.Client, token, base string, target execute.Target, out io.Writer) execute.CodeWorkResult {
	log := func(s string) { fmt.Fprintln(out, s) }
	git := gitcmd.Repo{Dir: r.root, Token: token}
	history := usagefold.History{
		Local: func(args ...string) (string, error) { return usagefold.RunLocal(r.root, nil, "", args...) },
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
	for _, p := range r.set.Packs {
		dirs = append(dirs, p.Dir)
	}
	fold := usagefold.Fold{Root: r.root, Repo: client.Repo, Base: base, BaseSha: baseSha,
		Now: calendar.ISO(time.Now()), Reader: apiReader{client}, History: history,
		Mounted: usagefold.MountedSkills(dirs), MinuteRate: usagefold.MinuteRateFrom(r.packConfig(workitem.TasksPackID)), Log: log}
	delivery := usagefold.Delivery{Root: r.root, Base: base, Branch: target.Branch, PR: target.PR, History: history,
		Trailers: usageFoldTrailers(r.tasks),
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
