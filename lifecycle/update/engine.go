package update

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/shared/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
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
}

// Deps is everything one updater run reads and writes; cmd/cn supplies the
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
	// Repo is the member checkout's path.
	Repo string
	// Out receives findings and skips; the verdict is returned.
	Out io.Writer
	// Timeout bounds each run of the candidate binary.
	Timeout time.Duration
}

// Options are cn update engine's flags.
type Options struct {
	// Force proposes a candidate whose verify reports a break.
	Force bool
}

// VerdictForms are the shapes of the line a run ends on.
var VerdictForms = []string{`^landed \S+$`, `^opened #\d+ for \S+$`, `^no PR: .+$`, `^skipped: .+$`, `^up to date$`}

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

func updatePRs(prs []githubapi.PR) []githubapi.PR {
	var out []githubapi.PR
	for _, p := range prs {
		if p.HasLabel(Label) && strings.HasPrefix(p.HeadRef, BranchPrefix) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}

// Engine is one run of cn update engine. It acts at most once: a green
// main is required; a green open update PR is landed and nothing else
// happens; otherwise the newest allowed version is fetched, self-tested
// and verified against this repo, and proposed as a pin-only PR whose CI
// the run dispatches, superseding an older open update PR. It returns the
// verdict line.
func Engine(d Deps, o Options) (string, error) {
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
	open := updatePRs(all)
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
	pin, err := settings.ReadEngine(raw, f)
	if err != nil {
		return "", err
	}
	p, err := d.Registry.Packument(pin.Package)
	if err != nil {
		return "", err
	}
	states := StatesFromPackument(p)
	next, verdict, err := propose(d, o, f, raw, pin, p, states, prev, prevState)
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

// propose picks the candidate and, unless an open PR already carries it
// or its verify breaks this repo, opens its update PR. It returns the
// candidate, empty for none, and the verdict.
func propose(d Deps, o Options, f settings.Format, raw []byte, pin settings.Engine, p *npmreg.Packument, states States, prev *githubapi.PR, prevState string) (string, string, error) {
	c := Candidate(pin.Version, p, states)
	if c.Skipped != nil {
		fmt.Fprintf(d.Out, "%s skipped: %s\n", c.Skipped.Version, c.Skipped.Reason)
	}
	if c.Version == "" {
		return "", "up to date", nil
	}
	if prev != nil && prev.HeadRef == BranchPrefix+c.Version {
		why := "its CI concluded " + prevState
		switch prevState {
		case "no run":
			why = "has no CI run"
		case "queued", "in_progress", "waiting", "pending", "requested":
			why = "its CI is " + prevState
		}
		return c.Version, fmt.Sprintf("skipped: #%d for %s is open and %s", prev.Number, c.Version, why), nil
	}

	got, err := Fetch(FetchInput{Registry: d.Registry, Package: pin.Package, Version: c.Version, Packument: p,
		Roots: d.Roots, CacheRoot: d.CacheRoot, Platform: d.Platform, Now: d.Now()})
	if err != nil {
		return "", "", err
	}
	self, err := Selftest(got.Binary, c.Version, d.Timeout)
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

	n, err := openPR(d, f, raw, got, self, verifyOut, broke)
	if err != nil {
		return "", "", err
	}
	if prev != nil {
		if err := supersede(d, *prev, n, c.Version); err != nil {
			return "", "", err
		}
	}
	if err := fileWorkflowChange(d, got); err != nil {
		return "", "", err
	}
	return c.Version, fmt.Sprintf("opened #%d for %s", n, c.Version), nil
}

func title(ver string) string { return "Claudinite engine " + ver }

// openPR commits the pin on a fresh update branch, pushes it, opens and
// labels the PR and dispatches its CI, leaving the checkout where it was.
func openPR(d Deps, f settings.Format, raw []byte, got Fetched, self, verifyOut string, forced bool) (int, error) {
	moved, err := settings.SetPin(raw, f, got.Version, got.Integrity)
	if err != nil {
		return 0, err
	}
	back, err := d.Git.CurrentBranch()
	if err != nil {
		return 0, err
	}
	branch := BranchPrefix + got.Version
	if err := d.Git.CreateBranch(branch, "HEAD"); err != nil {
		return 0, err
	}
	rel := settings.RelPath(f)
	commitErr := func() error {
		if err := os.WriteFile(filepath.Join(d.Repo, filepath.FromSlash(rel)), moved, 0o644); err != nil {
			return err
		}
		if err := d.Git.Commit(title(got.Version), rel); err != nil {
			return err
		}
		return d.Git.Push(remote, branch)
	}()
	if err := d.Git.Checkout(back); err != nil {
		return 0, errors.Join(commitErr, err)
	}
	if commitErr != nil {
		return 0, commitErr
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Moves this repo's Claudinite engine pin to **%s**. Only `%s` changes: `engine.version` and `engine.manifest`.\n\n", got.Version, rel)
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
	b.WriteString("The updater merges this PR once the CI run it dispatched is green.\n")

	pr, err := d.GitHub.CreatePull(title(got.Version), b.String(), branch, mainBranch)
	if err != nil {
		return 0, err
	}
	if err := d.GitHub.AddLabel(pr.Number, Label); err != nil {
		return 0, err
	}
	if err := d.GitHub.Dispatch(CIWorkflow, branch, map[string]string{"pr": strconv.Itoa(pr.Number)}); err != nil {
		return 0, err
	}
	return pr.Number, nil
}

func supersede(d Deps, old githubapi.PR, n int, ver string) error {
	if err := d.GitHub.Comment(old.Number, fmt.Sprintf("Superseded by #%d, which moves the pin to %s.", n, ver)); err != nil {
		return err
	}
	if err := d.GitHub.ClosePull(old.Number); err != nil {
		return err
	}
	return d.Git.DeleteRemoteBranch(remote, old.HeadRef)
}

// Land squash-merges update PR n at head sha after checking it is the
// updater's own pin-only change, deletes its branch and dispatches CI on
// main, whose runs the next update needs green.
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
	case !pr.HasLabel(Label) || !strings.HasPrefix(pr.HeadRef, BranchPrefix):
		return "", fmt.Errorf("#%d is not an update PR (label %s, branch %s*)", n, Label, BranchPrefix)
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
	base := remote + "/" + mainBranch
	files, err := d.Git.ChangedFiles(base, sha)
	if err != nil {
		return "", err
	}
	var f settings.Format
	for _, ff := range settings.Formats {
		if len(files) == 1 && files[0] == settings.RelPath(ff) {
			f = ff
		}
	}
	if f == "" {
		return "", fmt.Errorf("#%d changes %v, not only the settings file", n, files)
	}
	mb, err := d.Git.MergeBase(base, sha)
	if err != nil {
		return "", err
	}
	old, _, err := d.Git.Show(mb, files[0])
	if err != nil {
		return "", err
	}
	updated, _, err := d.Git.Show(sha, files[0])
	if err != nil {
		return "", err
	}
	if err := settings.PinOnlyChange(old, updated, f); err != nil {
		return "", fmt.Errorf("#%d: %w", n, err)
	}
	e, err := settings.ReadEngine(updated, f)
	if err != nil {
		return "", err
	}
	if err := d.GitHub.MergePull(n, sha, title(e.Version)); err != nil {
		return "", err
	}
	if err := d.Git.DeleteRemoteBranch(remote, pr.HeadRef); err != nil {
		return "", err
	}
	if err := d.GitHub.Dispatch(CIWorkflow, mainBranch, map[string]string{}); err != nil {
		return "", err
	}
	return "landed " + e.Version, nil
}

// upsertIssue opens an issue labelled Label with title, or updates the
// body of the open one already carrying that title.
func upsertIssue(d Deps, title, body string) error {
	open, err := d.GitHub.OpenIssues(Label)
	if err != nil {
		return err
	}
	for _, is := range open {
		if is.Title == title {
			if is.Body == body {
				return nil
			}
			return d.GitHub.UpdateIssueBody(is.Number, body)
		}
	}
	_, err = d.GitHub.CreateIssue(title, body, Label)
	return err
}

// fence is a code fence longer than any backtick run in s.
func fence(s string) string {
	f := "```"
	for strings.Contains(s, f) {
		f += "`"
	}
	return f
}

// fileWorkflowChange files the patch that brings the member's workflows to
// the new release's templates, as the new binary computes it; nothing
// writes .github/workflows/ itself.
func fileWorkflowChange(d Deps, got Fetched) error {
	diff, err := WorkflowsDiff(got.Binary, d.Repo, d.Timeout)
	if err != nil || diff == "" {
		return err
	}
	f := fence(diff)
	body := fmt.Sprintf("Claudinite engine %s expects these changes to this repo's workflows:\n\n%sdiff\n%s%s\n\n"+
		"The update job's token cannot write `.github/workflows/`, so the nightly update stays on the current workflows until a person commits this patch (`git apply` at the repo root).\n",
		got.Version, f, diff, f)
	return upsertIssue(d, "Claudinite engine "+got.Version+" needs a workflow change", body)
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
	return upsertIssue(d, "Claudinite engine "+pin+" is revoked", b.String())
}
