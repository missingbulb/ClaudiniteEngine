package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/shared/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// PlanBranchPrefix starts every plan correction branch; the day follows.
const PlanBranchPrefix = "claudinite/plan-"

// installTitle is the one issue a run files when the license server
// refuses the repo because the App does not cover it.
const installTitle = "Claudinite needs its GitHub App installed"

// LicenseKey is what an update run reads from its Actions key, verified
// and bound by cmd/cn before it reaches here.
type LicenseKey struct {
	Plan  string
	State string
	// UpdatesOff says the key's features leave out the updates surface.
	UpdatesOff bool
	// Notice is the sentence the key's state wants a person to read.
	Notice   string
	IssuedAt time.Time
	// Held and Revoked are engine versions; SecurityFixes is read, not
	// yet used.
	Held, Revoked []string
	SecurityFixes []string
	// SerialFloor and PackKeys reach the pack reader.
	SerialFloor int64
	PackKeys    []string
}

// KeyResult is the run's one key request: a key, or the cause there is
// none and the link that cause wants.
type KeyResult struct {
	Key                 *LicenseKey
	Cause, Detail, Link string
}

// StatesFromKey is the key's held and revoked lists as States.
func StatesFromKey(k *LicenseKey) States {
	s := States{Held: map[string]string{}, Revoked: map[string]string{}}
	if k == nil {
		return s
	}
	for _, v := range k.Held {
		s.Held[v] = KeyReason
	}
	for _, v := range k.Revoked {
		s.Revoked[v] = KeyReason
	}
	return s
}

// licenseGate is the run's key, or the skip verdict when there is none or
// it is degraded. It files or updates the install issue when the server
// says the App does not cover the repo.
func licenseGate(d Deps) (*LicenseKey, string, error) {
	if d.Key == nil {
		return nil, "", errors.New("no license key source")
	}
	r := d.Key()
	switch {
	case r.Key != nil && r.Key.State == "degraded":
		return nil, "skipped: license degraded (" + r.Key.Notice + ")", nil
	case r.Key != nil && r.Key.UpdatesOff:
		return nil, "skipped: license degraded (updates)", nil
	case r.Key != nil:
		return r.Key, "", nil
	case r.Cause == "app-not-installed":
		n, err := upsertIssue(d, installTitle, installBody(d, r.Link))
		if err != nil {
			return nil, "", err
		}
		return nil, fmt.Sprintf("skipped: the Claudinite App is not installed (#%d)", n), nil
	case r.Cause == "no-oidc":
		return nil, "skipped: no OIDC token (id-token: write is missing)", nil
	case r.Cause == "server-unreachable":
		fmt.Fprintf(d.Out, "license server: %s\n", r.Detail)
		return nil, "skipped: license server unreachable", nil
	}
	if r.Detail != "" {
		fmt.Fprintf(d.Out, "license: %s: %s\n", r.Cause, r.Detail)
	}
	return nil, "skipped: license refused (" + r.Cause + ")", nil
}

func installBody(d Deps, link string) string {
	repo := filepath.Base(d.Repo)
	if r := os.Getenv("GITHUB_REPOSITORY"); r != "" {
		repo = r
	}
	return fmt.Sprintf("The license server refused this repo's update key because the Claudinite GitHub App is not installed on %s.\n\n"+
		"Install it: %s\n\nThe nightly update's engine and pack updates stay off until the App covers this repo; sessions run degraded meanwhile. "+
		"The next update run after the install closes nothing by itself: close this issue once the update runs again.\n", repo, link)
}

// planWanted is the plan the settings file should carry for key, "" when
// it already agrees: a different plan, or none where the key is public.
func planWanted(key *LicenseKey, have string) string {
	if key == nil || key.Plan == "" || have == key.Plan || (have == "" && key.Plan != "public") {
		return ""
	}
	return key.Plan
}

func planTitle(plan string) string { return "Claudinite plan " + plan }

// correctPlan opens the plan correction PR when the key's plan and the
// settings file disagree, superseding an open one naming another plan. It
// returns the verdict, empty when the run goes on to the engine update: the
// plan agrees, or a PR for this plan is already open.
func correctPlan(d Deps, key *LicenseKey, all []githubapi.PR) (string, error) {
	path, f, err := settings.Find(d.Repo)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	have, err := settings.ReadLicense(raw, f)
	if err != nil {
		return "", err
	}
	want := planWanted(key, have.Plan)
	if want == "" {
		return "", nil
	}
	open, err := botPRs(d, all, PlanBranchPrefix)
	if err != nil {
		return "", err
	}
	var stale []githubapi.PR
	for _, p := range open {
		if p.Title != planTitle(want) {
			stale = append(stale, p)
			continue
		}
		runs, err := d.GitHub.WorkflowRuns(CIWorkflow, p.HeadSHA)
		if err != nil {
			return "", err
		}
		if runState(latest(runs, "workflow_dispatch")) == "success" {
			return Land(d, p.Number, p.HeadSHA)
		}
		return "", nil
	}
	moved, err := settings.SetPlan(raw, f, want)
	if err != nil {
		return "", err
	}
	back, err := d.Git.CurrentBranch()
	if err != nil {
		return "", err
	}
	branch := PlanBranchPrefix + strconv.Itoa(version.Today(d.Now()))
	for _, p := range stale {
		if p.HeadRef == branch {
			branch += "-" + want
		}
	}
	if err := d.Git.CreateBranch(branch, "HEAD"); err != nil {
		return "", err
	}
	rel := settings.RelPath(f)
	commitErr := func() error {
		if err := os.WriteFile(filepath.Join(d.Repo, filepath.FromSlash(rel)), moved, 0o644); err != nil {
			return err
		}
		if err := d.Git.Commit(planTitle(want), rel); err != nil {
			return err
		}
		return d.Git.Push(remote, branch)
	}()
	if err := d.Git.Checkout(back); err != nil {
		return "", errors.Join(commitErr, err)
	}
	_ = d.Git.DeleteBranch(branch)
	if commitErr != nil {
		return "", commitErr
	}
	was := have.Plan
	if was == "" {
		was = "none (the paid plans' default)"
	}
	body := fmt.Sprintf("This repo's license key, issued %s, names the **%s** plan; `%s` says `license.plan` is %s. "+
		"This PR changes that one line, so sessions ask the key server for the plan the repo is on.\n\n"+
		"The updater merges this PR once the CI run it dispatched is green.\n",
		key.IssuedAt.UTC().Format(time.RFC3339), want, rel, was)
	pr, err := d.GitHub.CreatePull(planTitle(want), body, branch, mainBranch)
	if err != nil {
		return "", err
	}
	if err := d.GitHub.AddLabel(pr.Number, Label); err != nil {
		return "", err
	}
	if err := d.GitHub.Dispatch(CIWorkflow, branch, map[string]string{"pr": strconv.Itoa(pr.Number)}); err != nil {
		return "", err
	}
	for _, p := range stale {
		if err := closeUpdatePR(d, p, fmt.Sprintf("Superseded by #%d: the license key now names the %s plan.", pr.Number, want)); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("opened #%d for plan %s", pr.Number, want), nil
}

// landPlan merges a plan correction PR whose one change is license.plan.
func landPlan(d Deps, pr githubapi.PR, sha string) (string, error) {
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
		return "", fmt.Errorf("#%d changes %v, not only the settings file", pr.Number, files)
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
	if err := settings.PlanOnlyChange(old, updated, f); err != nil {
		return "", fmt.Errorf("#%d: %w", pr.Number, err)
	}
	l, err := settings.ReadLicense(updated, f)
	if err != nil {
		return "", err
	}
	if err := landPinned(d, pr, sha, planTitle(l.Plan)); err != nil {
		return "", err
	}
	if err := d.GitHub.Dispatch(CIWorkflow, mainBranch, map[string]string{}); err != nil {
		return "", err
	}
	return "landed plan " + l.Plan, nil
}

// keyStates is npm's states with the key's unioned in, the key's first.
func keyStates(key *LicenseKey, p *npmreg.Packument) States {
	return StatesFromKey(key).Union(StatesFromPackument(p))
}
