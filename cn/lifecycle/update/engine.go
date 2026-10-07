package update

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/workflows"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/flatdecl"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/version"
)

const (
	// CIWorkflow is the member workflow whose runs gate main and update
	// PRs, and which the updater dispatches.
	CIWorkflow = "claudinite-ci.yml"
	// Label marks the PRs and issues the updater owns.
	Label = "claudinite-update"
	// BranchPrefix starts every update branch; the version follows.
	BranchPrefix = "claudinite/engine-"
	remote       = "origin"
	mainBranch   = "main"
)

// GitHub is the API surface the updater calls.
type GitHub interface {
	WorkflowRuns(workflow, sha string) ([]githubapi.Run, error)
	OpenPulls() ([]githubapi.PR, error)
	Pull(n int) (githubapi.PR, error)
	CreatePull(title, body, head, base string) (githubapi.PR, error)
	ClosePull(n int) error
	MergePull(n int, sha, title string) error
	AddLabel(n int, label string) error
	Comment(n int, body string) error
	Dispatch(workflow, ref string, inputs map[string]string) error
	OpenIssues(label string) ([]githubapi.Issue, error)
	CreateIssue(title, body, label string) (int, error)
	UpdateIssueBody(n int, body string) error
	CloseIssue(n int) error
}

// Deps is everything one updater run reads and writes; cn supplies the
// real ones and tests stand-ins.
type Deps struct {
	GitHub   GitHub
	Registry npmreg.Client
	// Git is the member checkout, on main.
	Git   gitcmd.Repo
	Roots []ed25519.PublicKey
	// CacheRoot is the launcher's cache folder, .../claudinite.
	CacheRoot string
	Platform  string
	Now       func() time.Time
	// Sleep waits between tries while npm lists a version it does not
	// serve yet.
	Sleep func(time.Duration)
	// Repo is the member checkout's path.
	Repo string
	// FullName is the member's owner/name, which a scheduler cron the
	// engine's hash did not write is replaced by the hash of.
	FullName string
	// Out receives findings and skips; the verdict is returned.
	Out io.Writer
	// Timeout bounds each run of the candidate binary.
	Timeout time.Duration
	// Packs reads the pack indexes and archives (cn update packs, and
	// landing a pack PR).
	Packs PackReader
	// Exe is this cn, which runs check world over a pack branch.
	Exe string
}

// Options are cn update engine's flags.
type Options struct {
	// Force proposes a candidate whose verify reports a break.
	Force bool
}

// VerdictForms are the shapes of the line a run ends on.
var VerdictForms = []string{`^landed \S+$`, `^opened #\d+ for \S+$`, `^landed packs .+$`, `^opened #\d+ for packs .+$`, `^no PR: .+$`, `^skipped: .+$`, `^up to date$`}

var verdictRes = func() []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, f := range VerdictForms {
		out = append(out, regexp.MustCompile(f))
	}
	return out
}()

// IsVerdict reports whether line has one of the verdict forms.
func IsVerdict(line string) bool {
	for _, re := range verdictRes {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// EngineResult is one engine run: its verdict and, when the update PR it
// opened or found carries workflow files staged for the agent stage to
// move into .github/workflows/, that PR, its branch and those files.
type EngineResult struct {
	Verdict string
	PR      int
	Branch  string
	Staged  []string
}

// latest is the newest run that counts: gated action_required runs never
// do, and keep, when non-empty, limits the events that do.
func latest(runs []githubapi.Run, keep string) *githubapi.Run {
	var best *githubapi.Run
	for i := range runs {
		r := &runs[i]
		if r.Conclusion == "action_required" || (keep != "" && r.Event != keep) {
			continue
		}
		if best == nil || r.CreatedAt > best.CreatedAt {
			best = r
		}
	}
	return best
}

// runState is "success" for a green run, else what it is instead.
func runState(r *githubapi.Run) string {
	switch {
	case r == nil:
		return "no run"
	case r.Status != "completed":
		return r.Status
	}
	return r.Conclusion
}

// updatePRs are the open PRs on an update branch that carry the label or
// that the job token opened; the second kind lost its label (a failed
// labelling call, a person removing it) and is relabelled rather than
// duplicated by a CreatePull GitHub would refuse.
func updatePRs(d Deps, prs []githubapi.PR) ([]githubapi.PR, error) {
	return botPRs(d, prs, BranchPrefix)
}

// Engine is one run of cn update engine. It acts at most once: a green
// main is required; a green open update PR is landed and nothing else
// happens; otherwise the newest allowed version is fetched, self-tested
// and verified against this repo, and proposed as a PR moving the pin,
// superseding an older open update PR. The workflows the new engine
// expects of this repo ride that PR staged (workflows.StagingDir), since
// the job token may not push .github/workflows/: a PR carrying none has
// its CI dispatched by the run, and one carrying some waits for the agent
// stage that moves them into place and dispatches it. It returns the
// verdict line.
func Engine(d Deps, o Options) (string, error) {
	r, err := EngineRun(d, o)
	return r.Verdict, err
}

// EngineRun is Engine, saying what the run left for an agent stage.
func EngineRun(d Deps, o Options) (EngineResult, error) {
	var r EngineResult
	v, err := engine(d, o, &r)
	if err != nil {
		return EngineResult{}, err
	}
	r.Verdict = v
	return r, nil
}

func engine(d Deps, o Options, res *EngineResult) (string, error) {
	head, err := d.Git.Head()
	if err != nil {
		return "", err
	}
	runs, err := d.GitHub.WorkflowRuns(CIWorkflow, head)
	if err != nil {
		return "", err
	}
	if s := runState(latest(runs, "")); s != "success" {
		return "skipped: main is not green (" + s + ")", nil
	}

	all, err := d.GitHub.OpenPulls()
	if err != nil {
		return "", err
	}
	open, err := updatePRs(d, all)
	if err != nil {
		return "", err
	}
	if len(open) > 1 {
		var names []string
		for _, p := range open {
			names = append(names, "#"+strconv.Itoa(p.Number))
		}
		return "", fmt.Errorf("several open update PRs (%s): close all but one", strings.Join(names, ", "))
	}
	var prev *githubapi.PR
	prevState := ""
	if len(open) == 1 {
		prev = &open[0]
		runs, err := d.GitHub.WorkflowRuns(CIWorkflow, prev.HeadSHA)
		if err != nil {
			return "", err
		}
		prevState = runState(latest(runs, "workflow_dispatch"))
		if prevState == "success" {
			return Land(d, prev.Number, prev.HeadSHA)
		}
	}

	path, f, err := settings.Find(d.Repo)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	// The update PR that moves a legacy pin also moves it to its channel.
	if raw, _, err = settings.FromLegacyPackage(raw, f); err != nil {
		return "", err
	}
	pin, err := settings.ReadEngine(raw, f)
	if err != nil {
		return "", err
	}
	p, err := d.Registry.Packument(pin.Package)
	if err != nil {
		return "", err
	}
	states := StatesFromPackument(p)
	if prev != nil {
		ver := strings.TrimPrefix(prev.HeadRef, BranchPrefix)
		if why := pinRefusal(p, states, ver); why != "" {
			if err := closeUpdatePR(d, *prev, fmt.Sprintf("Closed: %s is now %s, so this pin is never merged.", ver, why)); err != nil {
				return "", err
			}
			prev = nil
		}
	}
	next, verdict, err := propose(d, o, f, raw, pin, p, states, prev, prevState, res)
	if err != nil {
		return "", err
	}
	if k, reason := states.Of(pin.Version); k == npmreg.Revoked {
		if err := fileRevoked(d, pin.Version, reason, next, verdict); err != nil {
			return "", err
		}
	}
	return verdict, nil
}

// npm serves a version's tarballs minutes after its packument lists it;
// fetchServed tries every servedEvery for up to servedWait before the 404
// is an error.
const (
	servedEvery = 20 * time.Second
	servedWait  = 10 * time.Minute
)

func fetchServed(d Deps, pkg, ver string, p *npmreg.Packument) (Fetched, error) {
	for waited := time.Duration(0); ; waited += servedEvery {
		got, err := Fetch(FetchInput{Registry: d.Registry, Package: pkg, Version: ver, Packument: p,
			Roots: d.Roots, CacheRoot: d.CacheRoot, Platform: d.Platform, Now: d.Now()})
		var ns *npmreg.NotServedError
		if !errors.As(err, &ns) || waited >= servedWait {
			return got, err
		}
		fmt.Fprintf(d.Out, "npm lists %s but does not serve %s yet; trying again in %s\n", ver, ns.URL, servedEvery)
		d.Sleep(servedEvery)
	}
}

// propose picks the candidate and, unless an open PR already carries it
// or its verify breaks this repo, opens its update PR. It returns the
// candidate, empty for none, and the verdict.
func propose(d Deps, o Options, f settings.Format, raw []byte, pin settings.Engine, p *npmreg.Packument, states States, prev *githubapi.PR, prevState string, res *EngineResult) (string, string, error) {
	c := Candidate(pin.Version, pin.Channel, p, states)
	if c.Skipped != nil {
		fmt.Fprintf(d.Out, "%s skipped: %s\n", c.Skipped.Version, c.Skipped.Reason)
	}
	if c.Version == "" {
		return "", "up to date", nil
	}
	if prev != nil && prev.HeadRef == BranchPrefix+c.Version {
		staged, err := stagedOn(d, *prev)
		if err != nil {
			return "", "", err
		}
		if len(staged) > 0 {
			// Its CI cannot land it before the agent stage moves them.
			res.PR, res.Branch, res.Staged = prev.Number, prev.HeadRef, staged
			return c.Version, fmt.Sprintf("skipped: #%d for %s is open and still carries %d staged workflow file(s) for its agent stage", prev.Number, c.Version, len(staged)), nil
		}
		why := "its CI concluded " + prevState
		switch prevState {
		case "no run":
			why = "has no CI run"
		case "queued", "in_progress", "waiting", "pending", "requested":
			why = "its CI is " + prevState
		}
		switch prevState {
		case "no run", "cancelled", "timed_out":
			// No verdict will ever come for this head: ask again.
			if err := d.GitHub.Dispatch(CIWorkflow, prev.HeadRef, map[string]string{"pr": strconv.Itoa(prev.Number)}); err != nil {
				return "", "", err
			}
			why += "; dispatched its CI again"
		}
		return c.Version, fmt.Sprintf("skipped: #%d for %s is open and %s", prev.Number, c.Version, why), nil
	}

	got, err := fetchServed(d, pin.Package, c.Version, p)
	if err != nil {
		return "", "", err
	}
	self, err := Selftest(got.Binary, c.Version, d.Repo, d.Timeout)
	var failed *SelftestFailed
	if errors.As(err, &failed) {
		fmt.Fprint(d.Out, failed.Report)
		return c.Version, "skipped: selftest failed (" + strings.Join(failed.Probes, ", ") + ")", nil
	}
	if err != nil {
		return "", "", err
	}
	verifyOut, broke, err := RunVerify(got.Binary, d.Repo, d.Timeout)
	if err != nil {
		return "", "", err
	}
	fmt.Fprint(d.Out, verifyOut)
	if broke && !o.Force {
		return c.Version, "no PR: " + c.Version + " would break this repo", nil
	}

	n, staged, err := openPR(d, f, raw, got, self, verifyOut, broke)
	if err != nil {
		return "", "", err
	}
	if len(staged) > 0 {
		res.PR, res.Branch, res.Staged = n, BranchPrefix+c.Version, staged
	}
	if prev != nil {
		if err := supersede(d, *prev, n, c.Version); err != nil {
			return "", "", err
		}
	}
	return c.Version, fmt.Sprintf("opened #%d for %s", n, c.Version), nil
}

// stagedOn is the workflow files staged on an open update PR's head.
func stagedOn(d Deps, pr githubapi.PR) ([]string, error) {
	const ref = "refs/claudinite/update-head"
	if err := d.Git.Fetch(remote, "+refs/heads/"+pr.HeadRef+":"+ref); err != nil {
		return nil, err
	}
	files, err := d.Git.Tree(ref, workflows.StagingDir+"/")
	if err != nil {
		return nil, err
	}
	var out []string
	for p := range files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

// workflowIssueTitle is the title of the issue an earlier engine filed in
// place of carrying a workflow change on its update PR.
var workflowIssueTitle = regexp.MustCompile(`^Claudinite engine \S+ needs a workflow change$`)

// workflowIssues are the open issues of that title.
func workflowIssues(d Deps) ([]int, error) {
	open, err := d.GitHub.OpenIssues(Label)
	if err != nil {
		return nil, err
	}
	var out []int
	for _, is := range open {
		if workflowIssueTitle.MatchString(is.Title) {
			out = append(out, is.Number)
		}
	}
	sort.Ints(out)
	return out, nil
}

// openPR commits the pin, and the workflows the new engine expects staged
// beside it, on a fresh update branch, pushes it, opens and labels the PR,
// closes the workflow-change issues an earlier engine filed, and dispatches
// its CI unless workflows are staged, leaving the checkout where it was.
// It returns the PR and the staged paths.
func openPR(d Deps, f settings.Format, raw []byte, got Fetched, self, verifyOut string, forced bool) (int, []string, error) {
	moved, err := settings.SetPin(raw, f, got.Version, got.Integrity)
	if err != nil {
		return 0, nil, err
	}
	dropped := settings.HasRetiredLicense(moved, f)
	if dropped {
		if moved, err = settings.DropLicense(moved, f); err != nil {
			return 0, nil, err
		}
	}
	issues, err := workflowIssues(d)
	if err != nil {
		return 0, nil, err
	}
	back, err := d.Git.CurrentBranch()
	if err != nil {
		return 0, nil, err
	}
	branch := BranchPrefix + got.Version
	if err := d.Git.CreateBranch(branch, "HEAD"); err != nil {
		return 0, nil, err
	}
	rel := settings.RelPath(f)
	var staged []string
	commitErr := func() error {
		if err := os.WriteFile(filepath.Join(d.Repo, filepath.FromSlash(rel)), moved, 0o644); err != nil {
			return err
		}
		rels := []string{rel}
		member, err := writeMemberFile(d.Repo)
		if err != nil {
			return err
		}
		if member != "" {
			rels = append(rels, member)
		}
		if staged, err = StageWorkflows(got.Binary, d.Repo, d.FullName, d.Timeout); err != nil {
			return err
		}
		rels = append(rels, staged...)
		if err := d.Git.Commit(EngineTitle(got.Version), rels...); err != nil {
			return err
		}
		return d.Git.Push(remote, branch)
	}()
	if err := d.Git.Checkout(back); err != nil {
		return 0, nil, errors.Join(commitErr, err)
	}
	if commitErr != nil {
		return 0, nil, commitErr
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Moves this repo's Claudinite engine pin to **%s**. `%s` changes only `engine.version` and `engine.manifest`, with `%s` restating them.\n\n", got.Version, rel, flatdecl.MemberFile)
	if dropped {
		fmt.Fprintf(&b, "It also drops the retired `license` block from `%s`: a single repo needs no license, and nothing reads it.\n\n", rel)
	}
	fmt.Fprintf(&b, "- Manifest: `%s`\n- Key: `%s`\n\n", got.Integrity, got.KeyID)
	fmt.Fprintf(&b, "Self-test of the new binary:\n\n```\n%s```\n\n", self)
	if forced {
		b.WriteString("**Opened with `--force`:** the new binary's verify reports a break in this repo; this PR does not fix it.\n\n")
	}
	if strings.TrimSpace(verifyOut) == "" {
		b.WriteString("Verify of this repo by the new binary: no findings.\n\n")
	} else {
		fmt.Fprintf(&b, "Verify of this repo by the new binary:\n\n```\n%s```\n\n", verifyOut)
	}
	if len(issues) > 0 {
		var refs []string
		for _, n := range issues {
			refs = append(refs, "#"+strconv.Itoa(n))
		}
		fmt.Fprintf(&b, "It replaces %s, the workflow-change issue an earlier engine filed instead of carrying the change here.\n\n", strings.Join(refs, ", "))
	}
	if len(staged) == 0 {
		b.WriteString("The updater merges this PR once the CI run it dispatched is green.\n")
	} else {
		b.WriteString(stagedNote(staged))
	}

	pr, err := d.GitHub.CreatePull(EngineTitle(got.Version), b.String(), branch, mainBranch)
	if err != nil {
		return 0, nil, err
	}
	if err := d.GitHub.AddLabel(pr.Number, Label); err != nil {
		return 0, nil, err
	}
	for _, n := range issues {
		if err := d.GitHub.Comment(n, fmt.Sprintf("Closed: #%d carries this repo's workflow change on the engine update PR itself.", pr.Number)); err != nil {
			return 0, nil, err
		}
		if err := d.GitHub.CloseIssue(n); err != nil {
			return 0, nil, err
		}
	}
	if len(staged) > 0 {
		for _, s := range staged {
			fmt.Fprintf(d.Out, "staged for the agent stage: %s\n", s)
		}
		return pr.Number, staged, nil
	}
	if err := d.GitHub.Dispatch(CIWorkflow, branch, map[string]string{"pr": strconv.Itoa(pr.Number)}); err != nil {
		return 0, nil, err
	}
	return pr.Number, nil, nil
}

// stagedNote is the PR body's account of the staged workflows: what they
// are, why they wait, and how the PR lands once they are moved.
func stagedNote(staged []string) string {
	var b strings.Builder
	b.WriteString("This engine expects changes to this repo's workflows, staged on this branch because the update job's token may not push `.github/workflows/`:\n\n")
	for _, s := range staged {
		fmt.Fprintf(&b, "- `%s` → `.github/workflows/%s`\n", s, path.Base(s))
	}
	fmt.Fprintf(&b, "\nThe engine/update task's agent stage, whose credential may write workflows, moves each file into place unedited, leaving `%s/` empty, then dispatches `%s` on this branch with `pr=<this PR's number>`. GitHub refuses a workflow-changing merge to the job token, so that run's land job skips this PR: once CI is green on the head, the agent stage fetches main and this branch and runs `cn update land --check --base <main> --head <head>`, the landing gate from git alone, which accepts under `.github/workflows/` exactly what this engine expects, and squash-merges at that head only when it passes. Where no agent stage runs, a person does the same.\n", workflows.StagingDir, CIWorkflow)
	return b.String()
}

func supersede(d Deps, old githubapi.PR, n int, ver string) error {
	return closeUpdatePR(d, old, fmt.Sprintf("Superseded by #%d, which moves the pin to %s.", n, ver))
}

// closeUpdatePR comments why, closes the PR and deletes its branch: a pin
// that lost its standing is never merged.
func closeUpdatePR(d Deps, pr githubapi.PR, why string) error {
	if err := d.GitHub.Comment(pr.Number, why); err != nil {
		return err
	}
	if err := d.GitHub.ClosePull(pr.Number); err != nil {
		return err
	}
	return d.Git.DeleteRemoteBranch(remote, pr.HeadRef)
}

// retiredPlanBranchPrefix starts the plan correction branches an engine
// before record row 131 opened; one may still stand open in a member.
//
// @legacy-tolerance advisory:none retire:#83
const retiredPlanBranchPrefix = "claudinite/plan-"

// Land squash-merges update PR n at head sha after checking it is the
// updater's own change, deletes its branch and dispatches CI on main,
// whose runs the next update needs green. The branch says which shape the
// PR must have: an engine PR moves the pin and, under .github/workflows/,
// changes exactly what the pinned engine expects of this repo, with nothing
// left staged (expectedWorkflows); a pack PR changes only the vendored
// packs (landPacks). An engine PR that changes .github/workflows/ passes
// the same gate (engineGate) but is skipped, not merged: GitHub refuses
// that merge to the job token Land runs with, so the agent stage that
// moved the files merges it with its own credential once CheckLand passes.
func Land(d Deps, n int, sha string) (string, error) {
	pr, err := d.GitHub.Pull(n)
	if err != nil {
		return "", err
	}
	switch {
	case pr.State != "open":
		return "", fmt.Errorf("#%d is %s", n, pr.State)
	case pr.Author != gitcmd.BotName:
		return "", fmt.Errorf("#%d was opened by %s, not %s", n, pr.Author, gitcmd.BotName)
	case strings.HasPrefix(pr.HeadRef, retiredPlanBranchPrefix):
		return "", fmt.Errorf("#%d (branch %s) is a plan correction PR, which no engine lands any more: close #%d", n, pr.HeadRef, n)
	case !pr.HasLabel(Label) || (!strings.HasPrefix(pr.HeadRef, BranchPrefix) && !strings.HasPrefix(pr.HeadRef, PackBranchPrefix)):
		return "", fmt.Errorf("#%d is not an update PR (label %s, branch %s* or %s*)", n, Label, BranchPrefix, PackBranchPrefix)
	case pr.BaseRef != mainBranch:
		return "", fmt.Errorf("#%d targets %s, not %s", n, pr.BaseRef, mainBranch)
	case pr.HeadSHA != sha:
		return "", fmt.Errorf("#%d moved: its head is %s, CI ran on %s", n, pr.HeadSHA, sha)
	}
	const landRef = "refs/claudinite/land"
	if err := d.Git.Fetch(remote, "+refs/heads/"+mainBranch+":refs/remotes/"+remote+"/"+mainBranch, "+refs/heads/"+pr.HeadRef+":"+landRef); err != nil {
		return "", err
	}
	if got, err := d.Git.RevParse(landRef); err != nil || got != sha {
		return "", fmt.Errorf("#%d moved: its branch is at %s, CI ran on %s", n, got, sha)
	}
	if strings.HasPrefix(pr.HeadRef, PackBranchPrefix) {
		return landPacks(d, pr, sha)
	}
	ver, moved, err := engineGate(d, fmt.Sprintf("#%d", n), remote+"/"+mainBranch, sha)
	if err != nil {
		return "", err
	}
	if len(moved) > 0 {
		return fmt.Sprintf("skipped: #%d changes %s, which GitHub lets no job token merge; its agent stage merges it once cn update land --check passes", n, strings.Join(moved, ", ")), nil
	}
	if err := landPinned(d, pr, sha, EngineTitle(ver)); err != nil {
		return "", err
	}
	if err := d.GitHub.Dispatch(CIWorkflow, mainBranch, map[string]string{}); err != nil {
		return "", err
	}
	return "landed " + ver, nil
}

// CheckLand is Land's gate over an engine update PR's base and head
// commits, from git alone: no GitHub call and no write. Both must already
// be in the checkout. It answers "ok:" when the head moves base's pin and
// changes, under .github/workflows/, exactly what the pinned engine
// expects, and otherwise refuses as Land would.
func CheckLand(d Deps, base, head string) (string, error) {
	for _, c := range []string{base, head} {
		if _, err := d.Git.RevParse(c + "^{commit}"); err != nil {
			return "", fmt.Errorf("%s is not a commit in this checkout: fetch the PR's base and head first", c)
		}
	}
	ver, _, err := engineGate(d, head, base, head)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("ok: %s may land %s", head, ver), nil
}

// engineGate judges an engine update PR's head sha against base, the
// branch it merges into: it moves base's pin to a newer verified engine,
// restates the member file at most, and changes under .github/workflows/
// exactly what that engine expects (expectedWorkflows), with nothing left
// staged. It returns the new version and the workflow files changed; who
// names the PR in a refusal.
func engineGate(d Deps, who, base, sha string) (string, []string, error) {
	all, err := d.Git.ChangedFiles(base, sha)
	if err != nil {
		return "", nil, err
	}
	var files, moved, staged []string
	for _, file := range all {
		switch {
		case strings.HasPrefix(file, workflows.StagingDir+"/"):
			staged = append(staged, file)
		case strings.HasPrefix(file, ".github/workflows/"):
			moved = append(moved, file)
		default:
			files = append(files, file)
		}
	}
	if len(staged) > 0 {
		return "", nil, fmt.Errorf("%s still carries staged workflow files %v: its agent stage has not moved them into .github/workflows/", who, staged)
	}
	// The PR changes the settings file, and restates the member file
	// beside it when the declaration renders one; the workflows it moved
	// into place are checked against the new engine below.
	var f settings.Format
	member := ""
	for _, ff := range settings.Formats {
		rel := settings.RelPath(ff)
		switch {
		case len(files) == 1 && files[0] == rel:
			f = ff
		case len(files) == 2 && isMemberFile(files[0]) && files[1] == rel:
			f, member = ff, files[0]
		case len(files) == 2 && isMemberFile(files[1]) && files[0] == rel:
			f, member = ff, files[1]
		}
	}
	if f == "" {
		return "", nil, fmt.Errorf("%s changes %v, not only the settings file and the member file", who, files)
	}
	rel := settings.RelPath(f)
	if member != "" {
		if err := flatRendered(d.Git, sha, member); err != nil {
			return "", nil, fmt.Errorf("%s: %s %w", who, member, err)
		}
	}
	mb, err := d.Git.MergeBase(base, sha)
	if err != nil {
		return "", nil, err
	}
	old, _, err := d.Git.Show(mb, rel)
	if err != nil {
		return "", nil, err
	}
	updated, _, err := d.Git.Show(sha, rel)
	if err != nil {
		return "", nil, err
	}
	if err := settings.PinOnlyChange(old, updated, f); err != nil {
		return "", nil, fmt.Errorf("%s: %w", who, err)
	}
	e, err := settings.ReadEngine(updated, f)
	if err != nil {
		return "", nil, err
	}
	current, _, err := d.Git.Show(base, rel)
	if err != nil {
		return "", nil, err
	}
	if cur, err := settings.ReadEngine(current, f); err == nil {
		if c, err := version.Compare(e.Version, cur.Version); err != nil || c <= 0 {
			return "", nil, fmt.Errorf("%s pins %s, not newer than main's %s", who, e.Version, cur.Version)
		}
	}
	got, err := checkPin(d, e)
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", who, err)
	}
	if err := expectedWorkflows(d, got.Binary, mb, sha, moved); err != nil {
		return "", nil, fmt.Errorf("%s: %w", who, err)
	}
	return e.Version, moved, nil
}

// expectedWorkflows refuses an engine PR whose changes under
// .github/workflows/ are anything but what binary, the engine the PR pins,
// expects of this repo as it stood at the merge base mb: each changed file
// must be one that engine stages for this repo, byte for byte, a regular
// file, and still present.
func expectedWorkflows(d Deps, binary, mb, sha string, moved []string) error {
	if len(moved) == 0 {
		return nil
	}
	tmp, err := os.MkdirTemp("", "claudinite-land-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	for _, name := range workflows.Names {
		rel := ".github/workflows/" + name
		have, present, err := d.Git.Show(mb, rel)
		if err != nil {
			return err
		}
		if !present {
			continue
		}
		p := filepath.Join(tmp, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, have, 0o644); err != nil {
			return err
		}
	}
	staged, err := StageWorkflows(binary, tmp, d.FullName, d.Timeout)
	if err != nil {
		return fmt.Errorf("the workflows %s expects could not be computed: %w", strings.Join(moved, ", "), err)
	}
	want := map[string][]byte{}
	for _, s := range staged {
		b, err := os.ReadFile(filepath.Join(tmp, filepath.FromSlash(s)))
		if err != nil {
			return err
		}
		want[".github/workflows/"+path.Base(s)] = b
	}
	for _, file := range moved {
		exp, ok := want[file]
		if !ok {
			return fmt.Errorf("changes %s, which the pinned engine does not change", file)
		}
		have, present, err := d.Git.Show(sha, file)
		if err != nil {
			return err
		}
		if !present {
			return fmt.Errorf("deletes %s, which the pinned engine changes", file)
		}
		if regular, err := d.Git.Regular(sha, file); err != nil || !regular {
			return fmt.Errorf("%s is not a regular file", file)
		}
		if !bytes.Equal(have, exp) {
			return fmt.Errorf("%s is not what the pinned engine expects of this repo: move its staged copy unedited", file)
		}
	}
	return nil
}

// upsertIssue opens an issue labelled Label with title, or updates the
// body of the open one already carrying that title, and returns its number.
func upsertIssue(d Deps, title, body string) (int, error) {
	open, err := d.GitHub.OpenIssues(Label)
	if err != nil {
		return 0, err
	}
	for _, is := range open {
		if is.Title == title {
			if is.Body == body {
				return is.Number, nil
			}
			return is.Number, d.GitHub.UpdateIssueBody(is.Number, body)
		}
	}
	return d.GitHub.CreateIssue(title, body, Label)
}

// fence is a code fence longer than any backtick run in s.
func fence(s string) string {
	f := "```"
	for strings.Contains(s, f) {
		f += "`"
	}
	return f
}

// fileRevoked keeps one issue open per revoked pin, naming the reason, the
// next allowed version and what this run did about it.
func fileRevoked(d Deps, pin, reason, next, verdict string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Claudinite engine %s, the version this repo pins, is revoked: %s.\n\n", pin, reason)
	if next == "" {
		fmt.Fprintf(&b, "No allowed version newer than %s is published yet; the nightly update moves this repo off it as soon as one is.\n\n", pin)
	} else {
		fmt.Fprintf(&b, "The next allowed version is %s.\n\n", next)
	}
	fmt.Fprintf(&b, "The latest update run: `%s`.\n", verdict)
	_, err := upsertIssue(d, "Claudinite engine "+pin+" is revoked", b.String())
	return err
}
