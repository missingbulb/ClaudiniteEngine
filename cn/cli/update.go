package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/calendar"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/execute"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/paths"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/proc"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/report"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/fetch"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/ghrelease"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/trust"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/update"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/version"
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
	d.SelfRun, _ = strconv.ParseInt(os.Getenv("GITHUB_RUN_ID"), 10, 64)
	return d, nil
}

// localDeps are the updater's dependencies but GitHub's: the registry, the
// trust roots and git over repo, whose remote token may be empty. The
// repo's owner/name is read as cn adopt reads it.
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
		Repo: repo, FullName: githubapi.RepoFullName(repo), Out: stdout, Timeout: childTimeout, Exe: exe, CheckWorld: checkWorldIn}, nil
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
	if err := report.ParseFlags(fs, args[1:]); err != nil {
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
	reader, closeReader, err := fetch.ForRepo(*repo, d.Roots, stdout)
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

// mainCIRetry is how long a requeued update waits before its next try:
// its item is readied by the first scheduler run after it.
const mainCIRetry = 15 * time.Minute

// mainCIWait bounds how long one update run waits for main's CI to
// conclude, before a step and after a landing, before it requeues instead.
const mainCIWait = 15 * time.Minute

// mainCIPoll is how often a waiting update run asks main's CI again.
const mainCIPoll = 20 * time.Second

// updateTaskResult is the engine/update task's result once its steps said
// said and left eng: requeued while main's CI, or the update PR it left
// open, has no verdict yet (a close would cover the day, so the next try
// would come a day later), done, or, where the engine PR carries staged
// workflows, a hand-off to the task's agent stage on that PR, whose
// credential may write .github/workflows/.
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

// updateSteps are the engine/update task's steps: the engine update and
// the packs update in this process, syncing the checkout to the default
// branch after a landing, and the packs update run by the engine an engine
// landing just pinned.
type updateSteps struct {
	Engine func() (update.EngineResult, error)
	Packs  func() (update.EngineResult, error)
	Sync   func() error
	// MainCI is main's CI on the checkout's head, as this engine reads
	// it: "" when green, else the skip verdict.
	MainCI           func() (string, error)
	PacksOnNewEngine func() (string, error)
	Now              func() time.Time
	Sleep            func(time.Duration)
}

// mainWaiting is a verdict that waits on main's CI: just dispatched, or
// still running.
func mainWaiting(verdict string) bool {
	if verdict == update.MainCIDispatched {
		return true
	}
	for _, s := range []string{"queued", "in_progress", "waiting", "pending", "requested"} {
		if verdict == "skipped: main is not green ("+s+")" {
			return true
		}
	}
	return false
}

// run is one update in one executor run. A step that finds main's CI
// without a verdict is asked again once it has one, until the wait's
// bound; an engine landing hands the packs update to the engine it pinned,
// once the checkout holds the landing and this engine has seen main's CI
// on it conclude, so the new engine is started once rather than per poll; a
// packs landing ends the run. Only a CI verdict still missing at the bound,
// main's or an update PR's, leaves the item requeued.
func (s updateSteps) run(out io.Writer) ([]string, update.EngineResult, *execute.CodeWorkResult) {
	var said []string
	say := func(name, verdict string) {
		fmt.Fprintln(out, name+": "+verdict)
		said = append(said, name+": "+verdict)
	}
	fail := func(name string, err error) *execute.CodeWorkResult {
		return &execute.CodeWorkResult{Why: "engine/update: " + name + " failed", Detail: err.Error(), Said: said}
	}
	deadline := s.Now().Add(mainCIWait)
	settle := func(step func() (update.EngineResult, error)) (update.EngineResult, error) {
		for {
			r, err := step()
			if err != nil || !r.MainPending || !mainWaiting(r.Verdict) || !s.Now().Before(deadline) {
				return r, err
			}
			s.Sleep(mainCIPoll)
		}
	}
	eng, err := settle(s.Engine)
	if err != nil {
		return said, eng, fail("cn update engine", err)
	}
	say("cn update engine", eng.Verdict)
	switch {
	case strings.HasPrefix(eng.Verdict, "landed "):
		eng.MainPending = false
		if err := s.Sync(); err != nil {
			return said, eng, fail("syncing the checkout to the landing", err)
		}
		for {
			v, err := s.MainCI()
			if err != nil {
				return said, eng, fail("reading main's CI on the landing", err)
			}
			if !mainWaiting(v) || !s.Now().Before(deadline) {
				break
			}
			s.Sleep(mainCIPoll)
		}
		var v string
		for {
			if v, err = s.PacksOnNewEngine(); err != nil {
				return said, eng, fail("cn update packs on the new engine", err)
			}
			if !mainWaiting(v) || !s.Now().Before(deadline) {
				break
			}
			s.Sleep(mainCIPoll)
		}
		say("cn update packs", v)
		if mainWaiting(v) || strings.HasPrefix(v, "opened ") {
			eng.PRPending, eng.Verdict = true, v
		}
	case eng.MainPending:
	default:
		pk, err := settle(s.Packs)
		if err != nil {
			return said, eng, fail("cn update packs", err)
		}
		say("cn update packs", pk.Verdict)
		waiting := pk.PRPending || (pk.MainPending && !strings.HasPrefix(pk.Verdict, "landed "))
		if waiting && !eng.PRPending {
			eng.PRPending, eng.Verdict = true, pk.Verdict
		}
	}
	return said, eng, nil
}

// runUpdateTask is the engine/update task's code-work, run in the
// executor's process from the default branch, each verdict said on the
// item's close (updateSteps.run). The update's pull requests are its own,
// landed by the step that opened them or a later run; one carrying staged
// workflows goes to the agent stage (updateTaskResult).
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
	reader, closeReader, err := fetch.ForRepo(repo, d.Roots, out)
	if err != nil {
		return execute.CodeWorkResult{Why: "engine/update: the pack sources do not read", Detail: err.Error()}
	}
	defer closeReader()
	d.Packs = reader
	steps := updateSteps{
		Engine: func() (update.EngineResult, error) { return update.EngineRun(d, update.Options{}) },
		Packs:  func() (update.EngineResult, error) { return update.PacksRun(d, update.Options{}) },
		Sync: func() error {
			if err := d.Git.Fetch("origin", branch); err != nil {
				return err
			}
			_, err := d.Git.Run("reset", "-q", "--hard", "FETCH_HEAD")
			return err
		},
		MainCI:           func() (string, error) { return update.MainCI(d) },
		PacksOnNewEngine: func() (string, error) { return packsOnPinnedEngine(repo, token, out) },
		Now:              time.Now,
		Sleep:            time.Sleep,
	}
	said, eng, failed := steps.run(out)
	if failed != nil {
		return *failed
	}
	return updateTaskResult(said, eng, time.Now())
}

// packsOnPinnedEngine runs cn update packs on the engine the checkout pins,
// through the launcher, and returns its verdict. That engine was verified
// and landed by this run, so it is handed the job token the next scheduler
// run would hand it.
func packsOnPinnedEngine(repo, token string, out io.Writer) (string, error) {
	cmd := proc.Command("sh", filepath.Join(repo, ".claudinite", "launch"), "update", "packs", "--repo", repo)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "GITHUB_TOKEN="+token)
	var stdout bytes.Buffer
	cmd.Stdout, cmd.Stderr = io.MultiWriter(&stdout, out), out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if update.IsVerdict(lines[i]) {
			return lines[i], nil
		}
	}
	return "", fmt.Errorf("the pinned engine's cn update packs ended on no verdict")
}

// checkWorldIn is cn check world over a pack branch, as the bot's pull
// request against main, in this process.
func checkWorldIn(repo string, out io.Writer) (bool, error) {
	err := cmdCheckWorld([]string{"--pr-author", gitcmd.BotName, "--base-ref", "main", "--repo", repo}, out, out)
	if report.CodeOf(err) == report.Verify {
		return true, nil
	}
	return false, err
}
