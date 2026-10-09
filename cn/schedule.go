package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/breadcrumb"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/paths"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/version"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/config"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/ghport"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/localterms"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/queue"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/runner"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/schedule"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/signals"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/world"
)

// taskRepo is what every queue command reads off the checkout: the
// declared packs, the tasks they and the engine contribute, each pack's
// config and the settings' tasks block.
type taskRepo struct {
	root   string
	set    packset.Set
	tasks  []taskspec.Task
	errs   []taskspec.DiscoveryError
	config map[string]map[string]any
	queue  config.Config
}

func loadTaskRepo(root string) (taskRepo, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return taskRepo{}, report.Wrap(report.IO, "the repository path", err)
	}
	set, err := packset.Load(root, version.Version(), false)
	if err != nil {
		return taskRepo{}, report.New(report.Verify, err.Error())
	}
	tasks, errs := taskspec.Discover(root, set.Packs)
	r := taskRepo{root: root, set: set, tasks: tasks, errs: errs, config: map[string]map[string]any{}}
	for _, e := range set.Declared.Entries {
		id := e.ID
		if e.Local {
			id = e.Token()
		}
		r.config[workitem.CanonicalPackID(id)] = e.Config
	}
	if r.queue, err = config.Read(root); err != nil {
		return taskRepo{}, report.New(report.Verify, err.Error())
	}
	return r, nil
}

func (r taskRepo) packConfig(pack string) map[string]any {
	if c := r.config[pack]; c != nil {
		return c
	}
	return map[string]any{}
}

// jobClient is the Actions job's GitHub client: the job token held in
// memory and removed from the process environment, as the updater holds it,
// so nothing the engine spawns inherits it unasked; the executor hands it to
// the work steps explicitly.
func jobClient(env world.Env) (*githubapi.Client, error) {
	token := env("GITHUB_TOKEN")
	_ = os.Unsetenv("GITHUB_TOKEN")
	if token == "" {
		return nil, report.New(report.IO, "the queue commands need GITHUB_TOKEN, the workflow job's token")
	}
	c, err := githubapi.FromEnv(token)
	if err != nil {
		return nil, report.New(report.IO, err.Error())
	}
	return c, nil
}

func cmdSchedule(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return report.New(report.Usage, "schedule takes run, drain or report-failure")
	}
	switch args[0] {
	case "run":
		return cmdScheduleRun(args[1:], stdout, world.Env(os.Getenv))
	case "drain":
		return cmdScheduleDrain(args[1:], stdout, world.Env(os.Getenv))
	case "report-failure":
		return cmdReportFailure(args[1:], stdout, world.Env(os.Getenv))
	}
	return report.New(report.Usage, fmt.Sprintf("unknown schedule command %q", args[0]))
}

func cmdScheduleRun(args []string, stdout io.Writer, env world.Env) (err error) {
	start := time.Now()
	fs := flag.NewFlagSet("schedule run", flag.ContinueOnError)
	repo := fs.String("repo", ".", "")
	dry := fs.Bool("dry-run", false, "")
	wake := fs.String("wake", "", "")
	if err := flags(fs, args); err != nil {
		return err
	}
	outcome := breadcrumb.OK
	defer func() {
		if err != nil {
			outcome = breadcrumb.Error
		}
		fmt.Fprintln(stdout, breadcrumb.Line("tasks", "schedule", outcome, time.Since(start)))
	}()
	fmt.Fprintln(stdout, "## Claudinite scheduler run")
	fmt.Fprintln(stdout)
	if env.Suspended() {
		fmt.Fprintln(stdout, world.SuspendedNotice())
		return nil
	}
	r, err := loadTaskRepo(*repo)
	if err != nil {
		return err
	}
	if r.queue.Dormant {
		fmt.Fprintln(stdout, "- this project declares its scheduler dormant — no items instantiated, readied or reclaimed")
		return nil
	}
	for _, e := range r.errs {
		fmt.Fprintln(stdout, "! "+e.What)
	}
	clock, err := env.Clock()
	if err != nil {
		return report.New(report.Usage, err.Error())
	}
	client, err := jobClient(env)
	if err != nil {
		return err
	}
	gw := ghport.New(client)
	var issues world.Issues = gw
	if *dry {
		issues = &dryIssues{Issues: gw, log: func(s string) { fmt.Fprintln(stdout, s) }}
	}
	if *wake == "" {
		*wake = env("CLAUDINITE_WAKE")
	}
	meter := &queue.CostMeter{Workflow: "scheduler", RunID: env("GITHUB_RUN_ID"), Now: time.Now,
		Calls: func() *int { n := int(client.CallCount()); return &n }}
	readFleet := fleetSignal(env.Repo())
	var terms *localterms.Asker
	if unpacked, err := runner.Unpack(paths.CacheRoot()); err != nil {
		fmt.Fprintf(stdout, "! could not unpack the task runner (%v) — a task naming its own terms is not filed and the run fails\n", err)
	} else {
		terms = &localterms.Asker{Runner: runner.Runner{Dir: unpacked, Engine: version.Version()},
			Env: termsEnv(r, envMap()), Echo: func(_, line string) { fmt.Fprintln(stdout, line) }}
	}
	out, runErr := schedule.Run(schedule.RunIn{
		Issues: issues, Tasks: r.tasks, Now: clock.Now(), Disabled: r.queue.Disabled,
		PackConfig: r.packConfig, Wake: *wake, HasFleet: readFleet != nil, LocalTerms: terms,
		Collector: func(items []workitem.Issue) *signals.Collector {
			return &signals.Collector{Issues: issues, Repo: gw, DefaultBranch: env.DefaultBranch(),
				Packs: r.set.Declared.Declared, PackConfig: r.packConfig, EngineVersion: version.Version(),
				Items: items, Local: signals.ReadLocal(r.root, r.set.Declared.Declared, r.packConfig), Fleet: readFleet}
		},
		Log: func(s string) { fmt.Fprintln(stdout, s) },
		SetOutput: func(k, v string) error {
			if *dry {
				return fmt.Errorf("a dry run publishes nothing")
			}
			return env.SetOutput(k, v)
		},
		Phase: meter.Phase,
	})
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, meter.Record())
	if runErr != nil {
		return report.New(report.IO, runErr.Error())
	}
	if len(out.Problems) > 0 {
		return report.New(report.IO, strings.Join(out.Problems, "\n"))
	}
	return nil
}

// dryIssues reads through and writes nothing, saying what it would write.
type dryIssues struct {
	world.Issues
	log  func(string)
	next int
}

func (d *dryIssues) CreateIssue(title, _ string, labels []string) (int, error) {
	d.next--
	d.log(fmt.Sprintf("  (dry run) would create %q [%s]", title, strings.Join(labels, " ")))
	return d.next, nil
}
func (d *dryIssues) CloseIssue(n int, reason string) error {
	d.log(fmt.Sprintf("  (dry run) would close #%d %s", n, reason))
	return nil
}
func (d *dryIssues) ReopenIssue(n int) error {
	d.log(fmt.Sprintf("  (dry run) would reopen #%d", n))
	return nil
}
func (d *dryIssues) SetIssueBody(n int, _ string) error {
	d.log(fmt.Sprintf("  (dry run) would rewrite #%d's body", n))
	return nil
}
func (d *dryIssues) SetIssueTitle(n int, _ string) error {
	d.log(fmt.Sprintf("  (dry run) would retitle #%d", n))
	return nil
}
func (d *dryIssues) AddLabel(n int, l string) error {
	d.log(fmt.Sprintf("  (dry run) would add %s to #%d", l, n))
	return nil
}
func (d *dryIssues) RemoveLabel(n int, l string) error {
	d.log(fmt.Sprintf("  (dry run) would remove %s from #%d", l, n))
	return nil
}
func (d *dryIssues) EnsureLabels([]workitem.Label) error { return nil }
func (d *dryIssues) Comment(n int, _ string) (int64, error) {
	d.log(fmt.Sprintf("  (dry run) would comment on #%d", n))
	return 0, nil
}
func (d *dryIssues) EditComment(id int64, _ string) error {
	d.log(fmt.Sprintf("  (dry run) would edit comment %d", id))
	return nil
}

func cmdScheduleDrain(args []string, stdout io.Writer, env world.Env) error {
	fs := flag.NewFlagSet("schedule drain", flag.ContinueOnError)
	if err := flags(fs, args); err != nil {
		return err
	}
	if env.Suspended() {
		fmt.Fprintln(stdout, world.SuspendedNotice())
		return nil
	}
	client, err := jobClient(env)
	if err != nil {
		return err
	}
	branch := env.DefaultBranch()
	if err := client.Dispatch(workitem.ExecutorWorkflowFile, branch, nil); err != nil {
		return report.New(report.IO, fmt.Sprintf("could not dispatch %s on %s: %v", workitem.ExecutorWorkflowFile, branch, err))
	}
	fmt.Fprintf(stdout, "- dispatched the executor on %s to drain whatever this scheduler run created\n", branch)
	return nil
}

func cmdReportFailure(args []string, stdout io.Writer, env world.Env) error {
	fs := flag.NewFlagSet("schedule report-failure", flag.ContinueOnError)
	title := fs.String("title", schedule.SchedulerFailureTitle, "")
	if err := flags(fs, args); err != nil {
		return err
	}
	client, err := jobClient(env)
	if err != nil {
		return err
	}
	n, created, err := schedule.ReportFailure(ghport.New(client), schedule.FailureLabels, *title,
		"The Claudinite scheduler run/drain run failed: "+env.RunURL())
	if err != nil {
		return report.New(report.IO, err.Error())
	}
	verb := "commented on"
	if created {
		verb = "filed"
	}
	fmt.Fprintf(stdout, "- %s #%d\n", verb, n)
	return nil
}
