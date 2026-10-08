package main

import (
	"crypto/ed25519"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/packs"
	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/update"
	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/workflows"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/ghrelease"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/paths"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/trust"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/version"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/calendar"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/execute"
)

// childTimeout bounds each run of a candidate binary.
const childTimeout = 2 * time.Minute

// updateDeps builds the updater's real dependencies. The job token is read
// once and removed from the environment, so no child process inherits it;
// only the git children that talk to the remote are handed it.
func updateDeps(repo string, stdout io.Writer) (update.Deps, error) {
	token := os.Getenv("GITHUB_TOKEN")
	_ = os.Unsetenv("GITHUB_TOKEN")
	return updateDepsWith(repo, token, stdout)
}

// updateDepsWith builds them over a token the caller already read.
func updateDepsWith(repo, token string, stdout io.Writer) (update.Deps, error) {
	if token == "" {
		return update.Deps{}, report.New(report.IO, "update needs GITHUB_TOKEN, the workflow job's token")
	}
	gh, err := githubapi.FromEnv(token)
	if err != nil {
		return update.Deps{}, report.Wrap(report.IO, "update", err)
	}
	d, err := localDeps(repo, token, stdout)
	if err != nil {
		return update.Deps{}, err
	}
	d.GitHub, d.FullName = gh, gh.Repo
	return d, nil
}

// localDeps are the updater's dependencies but GitHub's: the registry, the
// trust roots and git over repo, whose remote token may be empty. The
// repo's owner/name is read as cn init reads it.
func localDeps(repo, token string, stdout io.Writer) (update.Deps, error) {
	reg, err := npmreg.FromEnv()
	if err != nil {
		return update.Deps{}, report.Wrap(report.IO, "update", err)
	}
	roots, err := trust.Roots()
	if err != nil {
		return update.Deps{}, report.Wrap(report.Internal, "update", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return update.Deps{}, report.Wrap(report.Internal, "update", err)
	}
	return update.Deps{Registry: reg, ReleasesHost: ghrelease.Host(), Git: gitcmd.Repo{Dir: repo, Token: token}, Roots: roots,
		CacheRoot: paths.CacheRoot(), Platform: version.Platform(), Now: time.Now, Sleep: time.Sleep,
		Repo: repo, FullName: repoFullName(repo), Out: stdout, Timeout: childTimeout, Exe: exe}, nil
}

// packReader reads the pack indexes from the pack sources repo's settings
// name, logging which answered to out. close removes every branch's clone.
func packReader(repo string, roots []ed25519.PublicKey, out io.Writer) (*packs.Reader, func(), error) {
	list, err := repoSources(repo)
	if err != nil {
		return nil, nil, err
	}
	srcs, closer := packs.SourcesFor(list, &http.Client{Timeout: time.Minute})
	return &packs.Reader{Sources: srcs, Roots: roots, Now: time.Now, Log: out}, closer, nil
}

// repoSources is packs.sources from repo's settings: none where the repo
// holds no settings file yet, an adoption, which reads the shelf.
func repoSources(repo string) ([]string, error) {
	held := false
	for _, f := range settings.Formats {
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(settings.RelPath(f)))); err == nil {
			held = true
		}
	}
	if !held {
		return nil, nil
	}
	path, f, err := settings.Find(repo)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p, err := settings.ReadPacks(raw, f)
	if err != nil {
		return nil, err
	}
	return p.Sources, nil
}

func cmdUpdate(args []string, stdout io.Writer) error {
	if len(args) == 0 || (args[0] != "engine" && args[0] != "packs" && args[0] != "land") {
		return report.New(report.Usage, "update takes engine, packs or land")
	}
	fs := flag.NewFlagSet("update "+args[0], flag.ContinueOnError)
	repo := fs.String("repo", ".", "")
	force := fs.Bool("force", false, "")
	pr := fs.Int("pr", 0, "")
	sha := fs.String("sha", "", "")
	check := fs.Bool("check", false, "")
	base := fs.String("base", "", "")
	head := fs.String("head", "", "")
	if err := flags(fs, args[1:]); err != nil {
		return err
	}
	if *check && args[0] != "land" {
		return report.New(report.Usage, "--check is update land's")
	}
	if !*check && (*base != "" || *head != "") {
		return report.New(report.Usage, "--base and --head are update land --check's")
	}
	if *check {
		return cmdLandCheck(*repo, *base, *head, *pr, *sha, stdout)
	}
	if args[0] == "land" {
		if *pr <= 0 {
			return report.New(report.Usage, "update land needs --pr N")
		}
		if *sha == "" {
			return report.New(report.Usage, "update land needs --sha, the head CI ran on")
		}
	}
	d, err := updateDeps(*repo, stdout)
	if err != nil {
		return err
	}
	reader, closeReader, err := packReader(*repo, d.Roots, stdout)
	if err != nil {
		return report.Wrap(report.IO, "update "+args[0], err)
	}
	defer closeReader()
	d.Packs = reader
	var verdict string
	switch args[0] {
	case "land":
		verdict, err = update.Land(d, *pr, *sha)
	case "packs":
		verdict, err = update.Packs(d, update.Options{Force: *force})
	default:
		verdict, err = update.Engine(d, update.Options{Force: *force})
	}
	if err != nil {
		return report.Wrap(report.IO, "update "+args[0], err)
	}
	fmt.Fprintln(stdout, verdict)
	return nil
}

// cmdLandCheck is update land --check: the landing gate over an engine
// update PR's base and head commits, from git alone, so the agent stage
// that merges a workflow-changing PR needs no GitHub token to run it.
func cmdLandCheck(repo, base, head string, pr int, sha string, stdout io.Writer) error {
	if pr != 0 || sha != "" {
		return report.New(report.Usage, "--check takes --base and --head, not --pr or --sha")
	}
	if base == "" || head == "" {
		return report.New(report.Usage, "--check needs --base and --head, the PR's base and head commits, both fetched")
	}
	d, err := localDeps(repo, "", stdout)
	if err != nil {
		return err
	}
	verdict, err := update.CheckLand(d, base, head)
	if err != nil {
		return report.Wrap(report.IO, "update land --check", err)
	}
	fmt.Fprintln(stdout, verdict)
	return nil
}

// mainCIRetry is how long a requeued update waits for a CI verdict, main's
// or its own PR's; the item is readied by the first scheduler run after it.
const mainCIRetry = 15 * time.Minute

// updateTaskResult is the engine/update task's result once its steps said
// said and the engine step left eng at now: requeued while main's CI, or
// the update PR's it left open, has no verdict yet (a close would cover
// the day, so the next try would come a day later), done, or, where the engine PR carries staged workflows, a
// hand-off to the task's agent stage on that PR, whose credential may
// write .github/workflows/.
func updateTaskResult(said []string, eng update.EngineResult, now time.Time) execute.CodeWorkResult {
	r := execute.CodeWorkResult{OK: true, Said: said}
	if eng.MainPending || eng.PRPending {
		r.Requeue = &execute.Requeue{Until: calendar.ISO(now.Add(mainCIRetry)), Reason: eng.Verdict}
		return r
	}
	if len(eng.Staged) == 0 {
		return r
	}
	r.AgentRequested = true
	r.DeliveredPR, r.Branch = eng.PR, eng.Branch
	r.Reason = fmt.Sprintf("Withheld workflow files: #%d carries %s, staged because the update job's token may not push .github/workflows/.",
		eng.PR, strings.Join(eng.Staged, ", "))
	r.HandOff = &execute.Target{Mode: execute.ModeAmend, Branch: eng.Branch, PR: eng.PR, Supersedes: []int{}}
	return r
}

// runUpdateTask is the engine/update task's code-work, run in the
// executor's process: the engine update, then the packs update, from the
// default branch, each verdict said on the item's close; while main's CI
// has no verdict the packs step waits with the engine's. The update's pull
// requests are its own, landed by the step that opened them or a later
// run; one carrying staged workflows goes to the agent stage
// (updateTaskResult).
func runUpdateTask(repo, token, branch string, out io.Writer) execute.CodeWorkResult {
	d, err := updateDepsWith(repo, token, out)
	if err != nil {
		return execute.CodeWorkResult{Why: "engine/update: " + err.Error()}
	}
	if cur, err := d.Git.CurrentBranch(); err != nil || cur != branch {
		if err := d.Git.Checkout(branch); err != nil {
			return execute.CodeWorkResult{Why: "engine/update: could not check out " + branch, Detail: err.Error()}
		}
	}
	reader, closeReader, err := packReader(repo, d.Roots, out)
	if err != nil {
		return execute.CodeWorkResult{Why: "engine/update: the pack sources do not read", Detail: err.Error()}
	}
	defer closeReader()
	d.Packs = reader
	var said []string
	var eng update.EngineResult
	for _, step := range []struct {
		name string
		run  func(update.Deps, update.Options) (string, error)
	}{{"cn update engine", func(d update.Deps, o update.Options) (string, error) {
		var err error
		eng, err = update.EngineRun(d, o)
		return eng.Verdict, err
	}}, {"cn update packs", func(d update.Deps, o update.Options) (string, error) {
		pk, err := update.PacksRun(d, o)
		if (pk.MainPending || pk.PRPending) && !eng.MainPending && !eng.PRPending {
			eng.PRPending, eng.Verdict = true, pk.Verdict
		}
		return pk.Verdict, err
	}}} {
		verdict, err := step.run(d, update.Options{})
		if err != nil {
			return execute.CodeWorkResult{Why: "engine/update: " + step.name + " failed", Detail: err.Error(), Said: said}
		}
		fmt.Fprintln(out, step.name+": "+verdict)
		said = append(said, step.name+": "+verdict)
		if eng.MainPending {
			break
		}
	}
	return updateTaskResult(said, eng, time.Now())
}

// cmdWorkflows is workflows diff, the patch to the workflows this version
// expects, and workflows stage, which writes them into the staging
// directory an engine update PR carries. Both read the repo's owner/name
// from --name, else repoFullName; a scheduler whose cron needs it and
// finds none fails rather than takes the template's placeholder.
func cmdWorkflows(args []string, stdout io.Writer) error {
	if len(args) == 0 || (args[0] != "diff" && args[0] != "stage") {
		return report.New(report.Usage, "workflows takes diff or stage")
	}
	fs := flag.NewFlagSet("workflows "+args[0], flag.ContinueOnError)
	repo := fs.String("repo", ".", "")
	name := fs.String("name", "", "")
	if err := flags(fs, args[1:]); err != nil {
		return err
	}
	full := *name
	if full == "" {
		full = repoFullName(*repo)
	}
	if args[0] == "stage" {
		staged, err := workflows.Stage(*repo, full)
		if err != nil {
			return report.Wrap(report.IO, "workflows stage", err)
		}
		for _, s := range staged {
			fmt.Fprintln(stdout, s)
		}
		return nil
	}
	d, err := workflows.Diff(*repo, full)
	if err != nil {
		return report.Wrap(report.IO, "workflows diff", err)
	}
	fmt.Fprint(stdout, d)
	return nil
}
