package main

import (
	"fmt"
	"regexp"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/release/releasefiles"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

const (
	rcPackage     = "@claudinite/cli-rc"
	stablePackage = "@claudinite/cli"
	soakTime      = 24 * time.Hour
	blockerLabel  = "release-blocker"
)

// Registry is what the gate reads from npm.
type Registry interface {
	// PublishedAt is when pkg@ver was published, and false when it was not.
	PublishedAt(pkg, ver string) (time.Time, bool, error)
	Manifest(pkg, ver string) (releasefiles.Manifest, error)
	Versions(pkg string) ([]string, error)
}

// Issue is an open GitHub issue.
type Issue struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels"`
}

// Canary is one registered canary repo, from release/canaries.json, and
// the workflows of it the gate reads; a canary naming none yet counts for
// nothing.
type Canary struct {
	Name      string   `json:"name"`
	Repo      string   `json:"repo"`
	Workflows []string `json:"workflows"`
}

// GitHub is what the gate reads from GitHub: issues and the conclusions of
// the canaries' named workflows, never anything a canary run writes itself.
type GitHub interface {
	OpenIssues(label string) ([]Issue, error)
	// LatestConclusion is the conclusion of the newest run of c's workflow
	// on commit, and false when it never ran there.
	LatestConclusion(c Canary, workflow, commit string) (string, bool, error)
}

// Verdict is the gate's answer for one rc version.
type Verdict struct {
	// Result is refuse, soak, blocked:<issue>, no-canaries, pass or
	// canary:<name>:<conclusion>.
	Result string
	// Hop is "hop:proven-by <version>" when the newest promoted release
	// carries the candidate's updaterDigest, else "hop:needed". It is
	// advisory until phase 2's cn update engine builds the second release
	// a needed hop asks for.
	Hop string
	// SecurityFix would skip the soak; it is always false until phase 4
	// carries the flag in the license keys behind a protected environment.
	SecurityFix bool
	Reason      string
}

// Gate decides whether rc version ver may be promoted.
func Gate(reg Registry, gh GitHub, now func() time.Time, ver string, canaries []Canary) (Verdict, error) {
	published, ok, err := reg.PublishedAt(rcPackage, ver)
	if err != nil {
		return Verdict{}, err
	}
	if !ok {
		return Verdict{Result: "refuse", Hop: "hop:needed", Reason: fmt.Sprintf("%s %s is not published", rcPackage, ver)}, nil
	}
	candidate, err := reg.Manifest(rcPackage, ver)
	if err != nil {
		return Verdict{}, err
	}
	hop, err := hopVerdict(reg, candidate)
	if err != nil {
		return Verdict{}, err
	}
	v := Verdict{Hop: hop}
	if age := now().Sub(published); age < soakTime {
		v.Result, v.Reason = "soak", fmt.Sprintf("published %s ago; engines soak %s", age.Round(time.Minute), soakTime)
		return v, nil
	}
	issues, err := gh.OpenIssues(blockerLabel)
	if err != nil {
		return Verdict{}, err
	}
	names := regexp.MustCompile(`(^|[^0-9.])` + regexp.QuoteMeta(ver) + `($|[^0-9]|\.($|[^0-9]))`)
	for _, i := range issues {
		if names.MatchString(i.Title) || names.MatchString(i.Body) {
			v.Result, v.Reason = fmt.Sprintf("blocked:%d", i.Number), fmt.Sprintf("open %s issue #%d names %s", blockerLabel, i.Number, ver)
			return v, nil
		}
	}
	if !Watched(canaries) {
		v.Result, v.Reason = "no-canaries", fmt.Sprintf("no canary in release/canaries.json names a workflow (%d registered)", len(canaries))
		return v, nil
	}
	for _, c := range canaries {
		for _, w := range c.Workflows {
			conclusion, ran, err := gh.LatestConclusion(c, w, candidate.Commit)
			if err != nil {
				return Verdict{}, err
			}
			if !ran {
				conclusion = "none"
			}
			if conclusion != "success" {
				v.Result, v.Reason = fmt.Sprintf("canary:%s:%s", c.Name, conclusion), fmt.Sprintf("%s in %s on %s is %s, and only success passes", w, c.Repo, candidate.Commit, conclusion)
				return v, nil
			}
		}
	}
	v.Result, v.Reason = "pass", "soaked, unblocked, every canary green"
	return v, nil
}

// Watched reports whether any canary names a workflow the gate reads.
func Watched(canaries []Canary) bool {
	for _, c := range canaries {
		if len(c.Workflows) > 0 {
			return true
		}
	}
	return false
}

func hopVerdict(reg Registry, candidate releasefiles.Manifest) (string, error) {
	versions, err := reg.Versions(stablePackage)
	if err != nil {
		return "", err
	}
	newest := ""
	for _, v := range versions {
		if v == "0.0.0" {
			continue
		}
		if newest == "" {
			newest = v
			continue
		}
		if c, err := version.Compare(v, newest); err == nil && c > 0 {
			newest = v
		}
	}
	if newest == "" {
		return "hop:needed", nil
	}
	promoted, err := reg.Manifest(stablePackage, newest)
	if err != nil {
		return "", err
	}
	if candidate.UpdaterDigest != "" && promoted.UpdaterDigest == candidate.UpdaterDigest {
		return "hop:proven-by " + newest, nil
	}
	return "hop:needed", nil
}
