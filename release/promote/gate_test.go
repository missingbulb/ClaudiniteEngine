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

func (f fakeIssues) LatestConclusion(c Canary, _, commit string) (string, bool, error) {
	v, ok := f.runs[c.Name][commit]
	return v, ok, nil
}

var now = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

func world() (fakeRegistry, fakeIssues) {
	reg := fakeRegistry{
		published: map[string]time.Time{
			"@claudinite/cli-rc@1.60930.1": now.Add(-25 * time.Hour),
			"@claudinite/cli-rc@1.61001.1": now.Add(-2 * time.Hour),
		},
		manifests: map[string]releasefiles.Manifest{
			"@claudinite/cli-rc@1.60930.1": {Version: "1.60930.1", Commit: "abc1234", UpdaterDigest: "d1"},
			"@claudinite/cli@1.60920.1":    {Version: "1.60920.1", Commit: "0000001", UpdaterDigest: "d0"},
			"@claudinite/cli@1.60925.2":    {Version: "1.60925.2", Commit: "0000002", UpdaterDigest: "d1"},
		},
		versions: map[string][]string{"@claudinite/cli": {"0.0.0", "1.60920.1", "1.60925.2"}},
	}
	return reg, fakeIssues{runs: map[string]map[string]string{}}
}

var twoCanaries = []Canary{{Name: "main", Repo: "missingbulb/canary-main", Workflows: []string{"canary.yml"}}, {Name: "lagging", Repo: "missingbulb/canary-lagging", Workflows: []string{"canary.yml"}}}

// Registered canaries naming no workflow yet, as release/canaries.json
// carries them until the canary App can read their runs.
var unwatched = []Canary{{Name: "lagging", Repo: "missingbulb/ClaudiniteCanaryLagging", Workflows: []string{}}}

func TestGateVerdicts(t *testing.T) {
	cases := []struct {
		name     string
		version  string
		canaries []Canary
		edit     func(*fakeIssues)
		want     string
	}{
		{"absent", "1.60931.1", twoCanaries, nil, "refuse"},
		{"soaking", "1.61001.1", twoCanaries, nil, "soak"},
		{"blocked by title", "1.60930.1", twoCanaries, func(g *fakeIssues) {
			g.issues = []Issue{{Number: 41, Title: "1.60930.1 breaks Stop", Labels: []string{"release-blocker"}}}
		}, "blocked:41"},
		{"blocked by body", "1.60930.1", twoCanaries, func(g *fakeIssues) {
			g.issues = []Issue{{Number: 42, Title: "hold", Body: "do not promote 1.60930.1", Labels: []string{"release-blocker"}}}
		}, "blocked:42"},
		{"blocker for another version", "1.60930.1", nil, func(g *fakeIssues) {
			g.issues = []Issue{{Number: 43, Title: "1.60930.10 breaks Stop", Labels: []string{"release-blocker"}}}
		}, "no-canaries"},
		{"version text without the label", "1.60930.1", nil, func(g *fakeIssues) {
			g.issues = []Issue{{Number: 44, Title: "1.60930.1 notes", Labels: []string{"docs"}}}
		}, "no-canaries"},
		{"no canaries", "1.60930.1", nil, nil, "no-canaries"},
		{"canaries naming no workflow", "1.60930.1", unwatched, nil, "no-canaries"},
		{"an unwatched canary beside a watched one", "1.60930.1", append(append([]Canary{}, unwatched...), twoCanaries...), func(g *fakeIssues) {
			g.runs["main"] = map[string]string{"abc1234": "success"}
		}, "canary:lagging:none"},
		{"all canaries green", "1.60930.1", twoCanaries, func(g *fakeIssues) {
			g.runs["main"] = map[string]string{"abc1234": "success"}
			g.runs["lagging"] = map[string]string{"abc1234": "success"}
		}, "pass"},
		{"a canary red", "1.60930.1", twoCanaries, func(g *fakeIssues) {
			g.runs["main"] = map[string]string{"abc1234": "success"}
			g.runs["lagging"] = map[string]string{"abc1234": "failure"}
		}, "canary:lagging:failure"},
		{"a canary never ran", "1.60930.1", twoCanaries, func(g *fakeIssues) {
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
	v, _ := Gate(reg, gh, clock, "1.60930.1", twoCanaries)
	if v.Hop != "hop:proven-by 1.60925.2" {
		t.Errorf("same digest as the newest promoted: %q", v.Hop)
	}

	m := reg.manifests["@claudinite/cli-rc@1.60930.1"]
	m.UpdaterDigest = "d2"
	reg.manifests["@claudinite/cli-rc@1.60930.1"] = m
	v, _ = Gate(reg, gh, clock, "1.60930.1", twoCanaries)
	if v.Hop != "hop:needed" {
		t.Errorf("changed digest: %q", v.Hop)
	}
	// Advisory until cn update engine exists (phase 2): it does not block.
	if v.Result != "pass" {
		t.Errorf("hop:needed blocked promotion: %s", v.Result)
	}

	reg.versions["@claudinite/cli"] = []string{"0.0.0"}
	v, _ = Gate(reg, gh, clock, "1.60930.1", twoCanaries)
	if v.Hop != "hop:needed" {
		t.Errorf("nothing promoted yet: %q", v.Hop)
	}
}

func TestGateCommandOnTheSoakedFixture(t *testing.T) {
	var out, errb strings.Builder
	code := run([]string{"gate", "--version", "1.61001.1", "--canaries", "../canaries.json", "--fixtures", "testdata/soaked"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "verdict=no-canaries\n") || !strings.Contains(out.String(), "hop=hop:needed\n") {
		t.Errorf("stdout %q", out.String())
	}
	out.Reset()
	if code := run([]string{"gate", "--version", "9.61001.1", "--canaries", "../canaries.json", "--fixtures", "testdata/soaked"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "verdict=refuse\n") {
		t.Errorf("absent version: exit %d, %q", code, out.String())
	}
}
