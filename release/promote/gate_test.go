package main

import (
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/release/releasefiles"
)

type fakeRegistry struct {
	published map[string]time.Time // "pkg@version"
	manifests map[string]releasefiles.Manifest
	versions  map[string][]string
}

func (f fakeRegistry) PublishedAt(pkg, ver string) (time.Time, bool, error) {
	t, ok := f.published[pkg+"@"+ver]
	return t, ok, nil
}

func (f fakeRegistry) Manifest(pkg, ver string) (releasefiles.Manifest, error) {
	return f.manifests[pkg+"@"+ver], nil
}

func (f fakeRegistry) Versions(pkg string) ([]string, error) { return f.versions[pkg], nil }

type fakeIssues struct {
	issues []Issue
	runs   map[string]map[string]string // canary name -> commit -> conclusion
}

func (f fakeIssues) OpenIssues(label string) ([]Issue, error) {
	var out []Issue
	for _, i := range f.issues {
		for _, l := range i.Labels {
			if l == label {
				out = append(out, i)
			}
		}
	}
	return out, nil
}

func (f fakeIssues) LatestConclusion(c Canary, commit string) (string, bool, error) {
	v, ok := f.runs[c.Name][commit]
	return v, ok, nil
}

var now = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

func world() (fakeRegistry, fakeIssues) {
	reg := fakeRegistry{
		published: map[string]time.Time{
			"@claudinite/cli-rc@60930.1.0": now.Add(-25 * time.Hour),
			"@claudinite/cli-rc@61001.1.0": now.Add(-2 * time.Hour),
		},
		manifests: map[string]releasefiles.Manifest{
			"@claudinite/cli-rc@60930.1.0": {Version: "60930.1.0", Commit: "abc1234", UpdaterDigest: "d1"},
			"@claudinite/cli@60920.1.0":    {Version: "60920.1.0", Commit: "0000001", UpdaterDigest: "d0"},
			"@claudinite/cli@60925.2.0":    {Version: "60925.2.0", Commit: "0000002", UpdaterDigest: "d1"},
		},
		versions: map[string][]string{"@claudinite/cli": {"0.0.0", "60920.1.0", "60925.2.0"}},
	}
	return reg, fakeIssues{runs: map[string]map[string]string{}}
}

var twoCanaries = []Canary{{Name: "main", Repo: "missingbulb/canary-main", Workflow: "canary.yml"}, {Name: "lagging", Repo: "missingbulb/canary-lagging", Workflow: "canary.yml"}}

func TestGateVerdicts(t *testing.T) {
	cases := []struct {
		name     string
		version  string
		canaries []Canary
		edit     func(*fakeIssues)
		want     string
	}{
		{"absent", "60931.1.0", twoCanaries, nil, "refuse"},
		{"soaking", "61001.1.0", twoCanaries, nil, "soak"},
		{"blocked by title", "60930.1.0", twoCanaries, func(g *fakeIssues) {
			g.issues = []Issue{{Number: 41, Title: "60930.1.0 breaks Stop", Labels: []string{"release-blocker"}}}
		}, "blocked:41"},
		{"blocked by body", "60930.1.0", twoCanaries, func(g *fakeIssues) {
			g.issues = []Issue{{Number: 42, Title: "hold", Body: "do not promote 60930.1.0", Labels: []string{"release-blocker"}}}
		}, "blocked:42"},
		{"blocker for another version", "60930.1.0", nil, func(g *fakeIssues) {
			g.issues = []Issue{{Number: 43, Title: "60930.1.01 breaks Stop", Labels: []string{"release-blocker"}}}
		}, "no-canaries"},
		{"version text without the label", "60930.1.0", nil, func(g *fakeIssues) {
			g.issues = []Issue{{Number: 44, Title: "60930.1.0 notes", Labels: []string{"docs"}}}
		}, "no-canaries"},
		{"no canaries", "60930.1.0", nil, nil, "no-canaries"},
		{"all canaries green", "60930.1.0", twoCanaries, func(g *fakeIssues) {
			g.runs["main"] = map[string]string{"abc1234": "success"}
			g.runs["lagging"] = map[string]string{"abc1234": "success"}
		}, "pass"},
		{"a canary red", "60930.1.0", twoCanaries, func(g *fakeIssues) {
			g.runs["main"] = map[string]string{"abc1234": "success"}
			g.runs["lagging"] = map[string]string{"abc1234": "failure"}
		}, "canary:lagging:failure"},
		{"a canary never ran", "60930.1.0", twoCanaries, func(g *fakeIssues) {
			g.runs["main"] = map[string]string{"abc1234": "success"}
			g.runs["lagging"] = map[string]string{"fffffff": "success"}
		}, "canary:lagging:none"},
	}
	for _, c := range cases {
		reg, gh := world()
		if c.edit != nil {
			c.edit(&gh)
		}
		v, err := Gate(reg, gh, func() time.Time { return now }, c.version, c.canaries)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if v.Result != c.want {
			t.Errorf("%s: %s, want %s", c.name, v.Result, c.want)
		}
		if v.SecurityFix {
			t.Errorf("%s: security fix set", c.name)
		}
	}
}

func TestGateHop(t *testing.T) {
	reg, gh := world()
	gh.runs["main"] = map[string]string{"abc1234": "success"}
	gh.runs["lagging"] = map[string]string{"abc1234": "success"}
	clock := func() time.Time { return now }
	v, _ := Gate(reg, gh, clock, "60930.1.0", twoCanaries)
	if v.Hop != "hop:proven-by 60925.2.0" {
		t.Errorf("same digest as the newest promoted: %q", v.Hop)
	}

	m := reg.manifests["@claudinite/cli-rc@60930.1.0"]
	m.UpdaterDigest = "d2"
	reg.manifests["@claudinite/cli-rc@60930.1.0"] = m
	v, _ = Gate(reg, gh, clock, "60930.1.0", twoCanaries)
	if v.Hop != "hop:needed" {
		t.Errorf("changed digest: %q", v.Hop)
	}
	// Advisory until cn update engine exists (phase 2): it does not block.
	if v.Result != "pass" {
		t.Errorf("hop:needed blocked promotion: %s", v.Result)
	}

	reg.versions["@claudinite/cli"] = []string{"0.0.0"}
	v, _ = Gate(reg, gh, clock, "60930.1.0", twoCanaries)
	if v.Hop != "hop:needed" {
		t.Errorf("nothing promoted yet: %q", v.Hop)
	}
}

func TestGateCommandOnTheSoakedFixture(t *testing.T) {
	var out, errb strings.Builder
	code := run([]string{"gate", "--version", "1.1.0", "--canaries", "../canaries.json", "--fixtures", "testdata/soaked"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "verdict=no-canaries\n") || !strings.Contains(out.String(), "hop=hop:needed\n") {
		t.Errorf("stdout %q", out.String())
	}
	out.Reset()
	if code := run([]string{"gate", "--version", "9.9.0", "--canaries", "../canaries.json", "--fixtures", "testdata/soaked"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "verdict=refuse\n") {
		t.Errorf("absent version: exit %d, %q", code, out.String())
	}
}
