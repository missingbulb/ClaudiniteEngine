// Package command is the tasks capability's command line: cn schedule,
// cn execute, cn work and cn tasks, and the task listing cn check list
// --tasks prints.
package command

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/execute"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/land"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/mergepolicy"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/queue"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/recover"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/runner"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/signals"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/world"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/breadcrumb"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/paths"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/report"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/ghport"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/version"
)

// Engine is what the executor and the scheduler take from the other
// segments: the engine's own tasks' code-work, which packaging and growth
// compute and the executor delivers, and the fleet signal's reader.
type Engine struct {
	// Work runs an engine task's code-work in process, keyed by task id.
	Work map[string]func(EngineRun) execute.CodeWorkResult
	// Fleet reads the fleet signal for the run's repo; nil, or a nil
	// reader, where there is none.
	Fleet func(repo string) func(sinceISO string) (any, error)
}

// EngineRun is one engine task's code-work: the checkout as the queue
// read it, the job's client and token, and the target the executor
// resolved.
type EngineRun struct {
	Repo   Repo
	Client *githubapi.Client
	Token  string
	Branch string
	Target execute.Target
	Out    io.Writer
}

func (e Engine) fleet(repo string) func(sinceISO string) (any, error) {
	if e.Fleet == nil {
		return nil
	}
	return e.Fleet(repo)
}

// Execute is `cn execute`: the executor's loop and its dispatch.
func Execute(args []string, stdout io.Writer, eng Engine) error {
	if len(args) == 0 {
		return report.New(report.Usage, "execute takes loop or dispatch")
	}
	env := world.Env(os.Getenv)
	switch args[0] {
	case "loop":
		return executeLoop(args[1:], stdout, env, eng)
	case "dispatch":
		return executeDispatch(args[1:], stdout, env)
	}
	return report.New(report.Usage, fmt.Sprintf("unknown execute command %q", args[0]))
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

func executeLoop(args []string, stdout io.Writer, env world.Env, eng Engine) (err error) {
	start := time.Now()
	fs := flag.NewFlagSet("execute loop", flag.ContinueOnError)
	repoDir := fs.String("repo", ".", "")
	if err := report.ParseFlags(fs, args); err != nil {
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
	r, err := LoadRepo(*repoDir)
	if err != nil {
		return err
	}
	if r.Queue.Dormant {
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
	client, err := JobClient(env)
	if err != nil {
		return err
	}
	gw := ghport.New(client)
	if err := gw.EnsureLabels(workitem.QueueLabels); err != nil {
		return report.New(report.IO, err.Error())
	}
	log := func(s string) { fmt.Fprintln(stdout, s) }
	echo := func(_, line string) { fmt.Fprintln(stdout, line) }
	unpacked, err := runner.Unpack(paths.CacheRoot())
	if err != nil {
		return report.Wrap(report.IO, "could not unpack the task runner", err)
	}
	run := runner.Runner{Dir: unpacked, Engine: version.Version()}
	branch := env.DefaultBranch()
	jobEnv := workStepEnv(envMap(), token)
	delivery := r.Queue.Delivery
	endpoints := r.Queue.Routines
	withheld := execute.WithheldSecrets(r.Tasks, endpoints)
	lane := land.Lane{API: gw, Now: time.Now, Sleep: time.Sleep, Log: log}
	git := gitcmd.Repo{Dir: r.Root, Token: token}
	declaredRules := mergepolicy.DeclaredBy(r.Set.Packs)
	judgement := func(t taskspec.Task) *land.Judgement {
		return &land.Judgement{Automerge: t.Decl["automerge"], Declared: declaredRules, Diff: pullDiff(git, branch)}
	}
	var packs []execute.PackInfo
	for _, p := range r.Set.Packs {
		packs = append(packs, execute.PackInfo{ID: p.ID, Kind: string(p.Kind)})
	}
	readFleet := eng.fleet(env.Repo())
	collector := func(items []workitem.Issue) *signals.Collector {
		return &signals.Collector{Issues: gw, Repo: gw, DefaultBranch: branch,
			Packs: r.Set.Declared.Declared, PackConfig: r.packConfig, EngineVersion: version.Version(),
			Items: items, Local: signals.ReadLocal(r.Root, r.Set.Declared.Declared, r.packConfig), Fleet: readFleet}
	}
	worker := execute.CodeWorker{
		Runner: run, Place: execute.CodeWorkPlace{Root: r.Root, Repo: env.Repo(), DefaultBranch: branch, EngineDir: engineDir()},
		Env: jobEnv, Withheld: withheld, TempDir: env("RUNNER_TEMP"), Echo: echo, Log: log, Rules: declaredRules,
		SDK: func(t taskspec.Task, _ workitem.Issue) *execute.SDK {
			declared := []string{}
			for _, p := range r.Set.Packs {
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
		Issues: gw, Pulls: gw, Lane: gw, Tasks: r.Tasks,
		ExecutorID: env.ExecutorID(), RunURL: env.RunURL(), Clock: clock, Draw: rand.Float64,
		Heartbeat: queue.HeartbeatEvery, Ticker: queue.RealTicker{},
		Exists: func(dir string) bool { _, err := os.Stat(dir); return err == nil },
		Evaluate: execute.Picker{Collector: collector(nil), PackConfig: r.packConfig, Runner: run,
			Env: termsEnv(r, jobEnv), Echo: echo}.Evaluate,
		ResolveTarget: func(t taskspec.Task, at time.Time) execute.Target {
			return execute.ResolveTarget(execute.TargetIn{Issues: gw, Repo: gw, Pulls: gw, Lane: gw, TaskID: t.Path(),
				Outcome: t.Decl.Outcome(), Delivery: delivery, Now: at, Seed: newNonce(0), Sleep: time.Sleep, Log: log, Judgement: judgement(t)})
		},
		CodeWork: func(t taskspec.Task, w execute.Work) execute.CodeWorkResult {
			if run, ok := eng.Work[t.ID]; ok && t.Pack == taskspec.BuiltinPack {
				return run(EngineRun{Repo: r, Client: client, Token: token, Branch: branch, Target: w.Target, Out: stdout})
			}
			return worker.Run(t, w)
		},
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

// executeDispatch is `cn execute dispatch [--continue]`: the executor
// workflow dispatched on the default branch, to drain what a scheduler run
// readied; with --continue, the next link of an executor chain that died,
// or past the chain's depth its report.
func executeDispatch(args []string, stdout io.Writer, env world.Env) error {
	fs := flag.NewFlagSet("execute dispatch", flag.ContinueOnError)
	cont := fs.Bool("continue", false, "")
	if err := report.ParseFlags(fs, args); err != nil {
		return err
	}
	if !*cont && env.Suspended() {
		fmt.Fprintln(stdout, world.SuspendedNotice())
		return nil
	}
	client, err := JobClient(env)
	if err != nil {
		return err
	}
	branch := env.DefaultBranch()
	if !*cont {
		if err := client.Dispatch(workitem.ExecutorWorkflowFile, branch, nil); err != nil {
			return report.New(report.IO, fmt.Sprintf("could not dispatch %s on %s: %v", workitem.ExecutorWorkflowFile, branch, err))
		}
		fmt.Fprintf(stdout, "- dispatched the executor on %s to drain whatever this scheduler run created\n", branch)
		return nil
	}
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

// engineDir is the directory of the running engine binary, "" where it
// cannot be read.
func engineDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return filepath.Dir(exe)
}

// termsEnv is the environment a task's own precondition terms run with:
// the job's, less every secret a task or an endpoint names.
func termsEnv(r Repo, job map[string]string) []string {
	return envList(execute.TaskEnv(nil, execute.WithheldSecrets(r.Tasks, r.Queue.Routines), job))
}

// workStepEnv is the job's environment as the work steps receive it: the job
// token the executor's client took out of the process environment handed back
// as GITHUB_TOKEN, the surface execute.TaskEnv documents every work step
// writing through. The job's own map is left as it was.
func workStepEnv(job map[string]string, token string) map[string]string {
	out := make(map[string]string, len(job)+1)
	for k, v := range job {
		out[k] = v
	}
	if token != "" {
		out["GITHUB_TOKEN"] = token
	}
	return out
}
