package main

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/license"
	"github.com/missingbulb/ClaudiniteEngine/shared/breadcrumb"
	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/shared/licenseapi"
	"github.com/missingbulb/ClaudiniteEngine/shared/mergepolicy"
	"github.com/missingbulb/ClaudiniteEngine/shared/paths"
	"github.com/missingbulb/ClaudiniteEngine/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/tasks/execute"
	"github.com/missingbulb/ClaudiniteEngine/tasks/ghport"
	"github.com/missingbulb/ClaudiniteEngine/tasks/land"
	"github.com/missingbulb/ClaudiniteEngine/tasks/queue"
	"github.com/missingbulb/ClaudiniteEngine/tasks/recover"
	"github.com/missingbulb/ClaudiniteEngine/tasks/runner"
	"github.com/missingbulb/ClaudiniteEngine/tasks/signals"
	"github.com/missingbulb/ClaudiniteEngine/tasks/world"
)

func cmdExecute(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return report.New(report.Usage, "execute takes loop or continue")
	}
	env := world.Env(os.Getenv)
	switch args[0] {
	case "loop":
		return cmdExecuteLoop(args[1:], stdout, env)
	case "continue":
		return cmdExecuteContinue(args[1:], stdout, env)
	}
	return report.New(report.Usage, fmt.Sprintf("unknown execute command %q", args[0]))
}

// taskLicense is "" when an item of t may run under the run's key, else
// the notice its park carries. The key is asked for only by an item that
// needs one.
func taskLicense(key *license.ActionsOnce, t taskspec.Task) string {
	need := execute.KeyNeedOf(t)
	if !need.Any() {
		return ""
	}
	r := key.Key()
	if r.Key == nil {
		return license.NoticeFor(nil, r.Cause, r.Detail, r.Link)
	}
	if r.Key.State == "degraded" {
		return license.NoticeFor(r.Key, "", "", "")
	}
	if need.EnginePack && !license.Gate(r.Key, false).On(license.SurfaceClaudiniteTasks) {
		return fmt.Sprintf("this run's license key does not include %s, which %s, a task of the engine's own packs, runs under", license.SurfaceClaudiniteTasks, t.Path())
	}
	return ""
}

// pullDiff reads a pull request's diff from this checkout as merge-policy
// entries: its head fetched and diffed against its merge base with the
// default branch.
func pullDiff(git gitcmd.Repo, base string) func(land.PR) ([]mergepolicy.Entry, error) {
	return func(pr land.PR) ([]mergepolicy.Entry, error) {
		if err := git.Fetch("origin", "+refs/heads/"+base+":refs/remotes/origin/"+base, "+refs/heads/"+pr.HeadRef+":refs/remotes/origin/"+pr.HeadRef); err != nil {
			return nil, err
		}
		mb, err := git.MergeBase("refs/remotes/origin/"+base, pr.HeadSHA)
		if err != nil {
			return nil, err
		}
		files, err := git.ChangedFiles(mb, pr.HeadSHA)
		if err != nil {
			return nil, err
		}
		entries := make([]mergepolicy.Entry, 0, len(files))
		for _, f := range files {
			e := mergepolicy.Entry{File: f}
			for _, side := range []struct {
				ref string
				to  **string
			}{{mb, &e.Before}, {pr.HeadSHA, &e.After}} {
				b, ok, err := git.Show(side.ref, f)
				if err != nil {
					return nil, err
				}
				if ok {
					s := string(b)
					*side.to = &s
				}
			}
			entries = append(entries, e)
		}
		return entries, nil
	}
}

// mayLand reports whether a task's automerge authorizes any landing; a
// pull request it does not authorize stands for a person.
func mayLand(automerge any) bool {
	if automerge == nil {
		return false
	}
	k := mergepolicy.Normalize(automerge).Kind
	return k != mergepolicy.Nothing && k != "invalid"
}

func newNonce(item int) string {
	b := make([]byte, 6)
	_, _ = cryptorand.Read(b)
	return fmt.Sprintf("%d-%s", item, hex.EncodeToString(b))
}

func envList(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

func envMap() map[string]string {
	out := map[string]string{}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		out[k] = v
	}
	return out
}

func cmdExecuteLoop(args []string, stdout io.Writer, env world.Env) (err error) {
	start := time.Now()
	fs := flag.NewFlagSet("execute loop", flag.ContinueOnError)
	repoDir := fs.String("repo", ".", "")
	if err := flags(fs, args); err != nil {
		return err
	}
	outcome := breadcrumb.OK
	defer func() {
		if err != nil {
			outcome = breadcrumb.Error
		}
		fmt.Fprintln(stdout, breadcrumb.Line("tasks", "execute", outcome, time.Since(start)))
	}()
	fmt.Fprintln(stdout, "## Claudinite executor")
	fmt.Fprintln(stdout)
	if env.Suspended() {
		fmt.Fprintln(stdout, world.SuspendedNotice())
		return nil
	}
	r, err := loadTaskRepo(*repoDir)
	if err != nil {
		return err
	}
	dormant, problem := r.dormancy()
	if problem != "" {
		fmt.Fprintln(stdout, "! "+problem)
	}
	if dormant {
		fmt.Fprintln(stdout, "- this project declares its scheduler dormant — nothing is picked up")
		return nil
	}
	for _, e := range r.errs {
		fmt.Fprintln(stdout, "! "+e.What)
	}
	clock, err := env.Clock()
	if err != nil {
		return report.New(report.Usage, err.Error())
	}
	token := env("GITHUB_TOKEN")
	client, err := jobClient(env)
	if err != nil {
		return err
	}
	gw := ghport.New(client)
	if err := gw.EnsureLabels(workitem.QueueLabels); err != nil {
		return report.New(report.IO, err.Error())
	}
	roots, err := license.Roots()
	if err != nil {
		return report.Wrap(report.IO, "the embedded license roots", err)
	}
	key := sharedActionsKey(roots, stdout)
	log := func(s string) { fmt.Fprintln(stdout, s) }
	echo := func(_, line string) { fmt.Fprintln(stdout, line) }
	unpacked, err := runner.Unpack(paths.CacheRoot())
	if err != nil {
		return report.Wrap(report.IO, "could not unpack the task runner", err)
	}
	run := runner.Runner{Dir: unpacked, Engine: version.Version()}
	branch := env.DefaultBranch()
	jobEnv := envMap()
	tasksConfig := r.packConfig(workitem.TasksPackID)
	delivery := land.DeliveryFor(tasksConfig)
	endpoints, _ := tasksConfig[execute.EndpointsKey].(map[string]any)
	withheld := execute.WithheldSecrets(r.tasks, endpoints)
	termsEnv := envList(execute.TaskEnv(nil, withheld, jobEnv))
	lane := land.Lane{API: gw, Now: time.Now, Sleep: time.Sleep, Log: log}
	git := gitcmd.Repo{Dir: r.root, Token: token}
	declaredRules := mergepolicy.DeclaredBy(r.set.Packs)
	judgement := func(t taskspec.Task) *land.Judgement {
		return &land.Judgement{Automerge: t.Decl["automerge"], Declared: declaredRules, Diff: pullDiff(git, branch)}
	}
	var packs []execute.PackInfo
	for _, p := range r.set.Packs {
		packs = append(packs, execute.PackInfo{ID: p.ID, Kind: string(p.Kind)})
	}
	collector := func(items []workitem.Issue) *signals.Collector {
		return &signals.Collector{Issues: gw, Repo: gw, DefaultBranch: branch,
			Packs: r.set.Declared.Declared, PackConfig: r.packConfig, EngineVersion: version.Version(),
			Items: items, Local: signals.ReadLocal(r.root, r.set.Declared.Declared, r.packConfig)}
	}
	worker := execute.CodeWorker{
		Runner: run, Place: execute.CodeWorkPlace{Root: r.root, Repo: env.Repo(), DefaultBranch: branch},
		Env: jobEnv, Withheld: withheld, TempDir: env("RUNNER_TEMP"), Echo: echo, Log: log,
		SDK: func(t taskspec.Task, _ workitem.Issue) *execute.SDK {
			declared := []string{}
			for _, p := range r.set.Packs {
				if p.ID == t.Pack {
					declared = p.Manifest.GitHubActions
				}
			}
			return &execute.SDK{Pack: t.Pack, Task: t.ID, Granted: execute.GrantedActions(declared, r.packConfig(t.Pack)),
				Git: git.Run, GitHub: gw, DefaultBranch: branch, Config: r.packConfig, Packs: packs, Log: log}
		},
	}
	invoker := execute.Invoker{Repo: env.Repo(), Endpoints: endpoints, Env: jobEnv}
	meter := &queue.CostMeter{Workflow: "executor", RunID: env("GITHUB_RUN_ID"), Now: time.Now,
		Calls: func() *int { n := int(client.CallCount()); return &n }}
	settled, loopErr := execute.Loop(execute.In{
		Issues: gw, Pulls: gw, Lane: gw, Tasks: r.tasks,
		ExecutorID: env.ExecutorID(), RunURL: env.RunURL(), Clock: clock, Draw: rand.Float64,
		Heartbeat: queue.HeartbeatEvery, Ticker: queue.RealTicker{},
		Exists:  func(dir string) bool { _, err := os.Stat(dir); return err == nil },
		License: func(t taskspec.Task) string { return taskLicense(key, t) },
		Evaluate: execute.Picker{Collector: collector(nil), PackConfig: r.packConfig, Runner: run,
			Env: termsEnv, Echo: echo}.Evaluate,
		ResolveTarget: func(t taskspec.Task, at time.Time) execute.Target {
			return execute.ResolveTarget(execute.TargetIn{Issues: gw, Repo: gw, Pulls: gw, Lane: gw, TaskID: t.Path(),
				Outcome: t.Decl.Outcome(), Delivery: delivery, Now: at, Seed: newNonce(0), Sleep: time.Sleep, Log: log, Judgement: judgement(t)})
		},
		CodeWork: worker.Run,
		Land: func(t taskspec.Task, pr int) execute.Landed {
			if !mayLand(t.Decl["automerge"]) {
				return execute.Landed{Note: fmt.Sprintf("PR #%d stands for review — this task's automerge authorizes no landing", pr)}
			}
			p, err := gw.Pull(pr)
			if err != nil {
				return execute.Landed{Note: fmt.Sprintf("could not read PR #%d (%v) — leaving it for the next run", pr, err)}
			}
			d := lane.Deliver(land.PR{Number: p.Number, NodeID: p.NodeID, HeadRef: p.HeadRef, HeadSHA: p.HeadSHA}, branch, delivery, t.Path(), judgement(t))
			return execute.Landed{Merged: d.Merged, Refused: d.Refused}
		},
		Grant: func(_ taskspec.Task, item workitem.Issue) (string, error) {
			w, err := licenseapi.FromEnv()
			if err != nil {
				return "", err
			}
			g, err := license.Grant(w, key.Key(), item.Number)
			if err != nil {
				return "", err
			}
			return execute.GrantComment(g), nil
		},
		Invoke: invoker.Invoke,
		Nonce:  newNonce,
		Cost:   meter,
		Log:    log,
	})
	if len(settled) == 0 {
		fmt.Fprintln(stdout, "- nothing ready to pick up")
	}
	for _, s := range settled {
		fmt.Fprintf(stdout, "- #%d: %s\n", s.Issue, s.Outcome)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, meter.Record())
	if loopErr != nil {
		return report.New(report.IO, loopErr.Error())
	}
	return nil
}

func cmdExecuteContinue(args []string, stdout io.Writer, env world.Env) error {
	fs := flag.NewFlagSet("execute continue", flag.ContinueOnError)
	if err := flags(fs, args); err != nil {
		return err
	}
	client, err := jobClient(env)
	if err != nil {
		return err
	}
	branch := env.DefaultBranch()
	err = recover.Continue(recover.In{
		Issues: ghport.New(client), Branch: branch, RunURL: env.RunURL(),
		Depth: recover.NextDepth(env("CLAUDINITE_CONTINUATION_DEPTH")),
		Dispatch: func(inputs map[string]string) error {
			return client.Dispatch(workitem.ExecutorWorkflowFile, branch, inputs)
		},
		Log: func(s string) { fmt.Fprintln(stdout, s) },
	})
	if err != nil {
		return report.New(report.IO, err.Error())
	}
	return nil
}
