// Command promote is the promotion gate of promote.yml: gate decides from
// npm, GitHub issues and the canaries' workflow conclusions whether a
// version of @claudinite/cli may take the latest dist-tag; check verifies
// the version's downloaded bytes before the tag moves onto them.
//
//	promote gate --version V --canaries FILE [--fixtures DIR]
//	promote check --version V --tarballs DIR --roots DIR --source DIR
//
// Both print key=value lines for $GITHUB_OUTPUT and exit 0 whatever the
// verdict; a refusal is an answer, not a failure. --fixtures reads the
// registry, the issues, the runs and the clock from DIR/fixture.json.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/dev/release"
	"github.com/missingbulb/ClaudiniteEngine/dev/release/create/releasefiles"
)

const usage = `usage:
  promote gate --version V --canaries FILE [--fixtures DIR]
  promote check --version V --tarballs DIR --roots DIR --source DIR
`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	ver := fs.String("version", "", "")
	canariesPath := fs.String("canaries", "", "")
	fixtures := fs.String("fixtures", "", "")
	tarballs := fs.String("tarballs", "", "")
	roots := fs.String("roots", "", "")
	source := fs.String("source", "", "")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || *ver == "" {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "gate":
		if *canariesPath == "" {
			fmt.Fprint(stderr, usage)
			return 2
		}
		if err := gate(*ver, *canariesPath, *fixtures, stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "promote gate: %v\n", err)
			return 1
		}
		return 0
	case "check":
		if *tarballs == "" || *roots == "" || *source == "" {
			fmt.Fprint(stderr, usage)
			return 2
		}
		head, err := exec.Command("git", "-C", *source, "rev-parse", "HEAD").Output()
		if err != nil {
			fmt.Fprintf(stderr, "promote check: reading the candidate checkout's HEAD: %v\n", err)
			return 1
		}
		result, reason, stable := Check(*tarballs, *ver, *roots, strings.TrimSpace(string(head)), func() error { return stableTest(*source, stderr) })
		fmt.Fprintf(stderr, "promote check: %s: %s\n", result, reason)
		fmt.Fprintf(stdout, "check=%s\nstable_test=%s\nreason=%s\n", result, stable, strings.ReplaceAll(reason, "\n", " "))
		return 0
	}
	fmt.Fprintf(stderr, "promote: unknown command %q\n%s", args[0], usage)
	return 2
}

func gate(ver, canariesPath, fixtures string, stdout, stderr io.Writer) error {
	raw, err := os.ReadFile(canariesPath)
	if err != nil {
		return err
	}
	var list struct {
		Canaries []Canary `json:"canaries"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return fmt.Errorf("%s: %w", canariesPath, err)
	}
	var (
		reg   Registry
		gh    GitHub
		clock = time.Now
	)
	if fixtures != "" {
		f, err := loadFixture(fixtures)
		if err != nil {
			return err
		}
		reg, gh, clock = f, f, func() time.Time { return f.Now }
	} else {
		reg = npmRegistry{base: "https://registry.npmjs.org", client: http.DefaultClient}
		gh = githubAPI{base: "https://api.github.com", repo: os.Getenv("GITHUB_REPOSITORY"), token: os.Getenv("GITHUB_TOKEN"), client: http.DefaultClient}
	}
	v, err := Gate(reg, gh, clock, ver, list.Canaries)
	if err != nil {
		return err
	}
	fmt.Fprintf(stderr, "promote gate: %s %s: %s; %s\n", release.CLI, ver, v.Result, v.Reason)
	fmt.Fprintf(stdout, "verdict=%s\nhop=%s\n", v.Result, v.Hop)
	return nil
}

// stableTest runs the trust roots' stable build check at the
// candidate's source, which fails while the development roots are embedded.
func stableTest(source string, stderr io.Writer) error {
	cmd := exec.Command("go", "test", "-count=1", "-tags", "stable", "./cn/shared/trust")
	cmd.Dir = source
	cmd.Stdout, cmd.Stderr = stderr, stderr
	return cmd.Run()
}

// fixture is a recorded world for the gate: registry, issues, runs, clock.
type fixture struct {
	Now      time.Time                    `json:"now"`
	Tags     map[string]map[string]string `json:"distTags"`
	Packages map[string]map[string]struct {
		PublishedAt time.Time       `json:"publishedAt"`
		Manifest    json.RawMessage `json:"manifest"`
	} `json:"registry"`
	Issues []Issue                      `json:"issues"`
	Runs   map[string]map[string]string `json:"runs"`
}

func loadFixture(dir string) (*fixture, error) {
	raw, err := os.ReadFile(dir + "/fixture.json")
	if err != nil {
		return nil, err
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%s/fixture.json: %w", dir, err)
	}
	return &f, nil
}

func (f *fixture) PublishedAt(pkg, ver string) (time.Time, bool, error) {
	p, ok := f.Packages[pkg][ver]
	return p.PublishedAt, ok, nil
}

func (f *fixture) Manifest(pkg, ver string) (releasefiles.Manifest, error) {
	p, ok := f.Packages[pkg][ver]
	if !ok {
		return releasefiles.Manifest{}, fmt.Errorf("fixture has no %s@%s", pkg, ver)
	}
	return releasefiles.ParseManifest(p.Manifest)
}

func (f *fixture) DistTags(pkg string) (map[string]string, error) { return f.Tags[pkg], nil }

func (f *fixture) OpenIssues(label string) ([]Issue, error) {
	var out []Issue
	for _, i := range f.Issues {
		for _, l := range i.Labels {
			if l == label {
				out = append(out, i)
			}
		}
	}
	return out, nil
}

func (f *fixture) LatestConclusion(c Canary, _, commit string) (string, bool, error) {
	v, ok := f.Runs[c.Name][commit]
	return v, ok, nil
}

// npmRegistry reads the public npm registry.
type npmRegistry struct {
	base   string
	client *http.Client
}

type packument struct {
	DistTags map[string]string `json:"dist-tags"`
	Time     map[string]string `json:"time"`
	Versions map[string]struct {
		Dist struct {
			Tarball string `json:"tarball"`
		} `json:"dist"`
	} `json:"versions"`
}

func (r npmRegistry) packument(pkg string) (*packument, error) {
	resp, err := r.client.Get(r.base + "/" + url.PathEscape(pkg))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return &packument{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", pkg, resp.Status)
	}
	var p packument
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, fmt.Errorf("%s: %w", pkg, err)
	}
	return &p, nil
}

func (r npmRegistry) PublishedAt(pkg, ver string) (time.Time, bool, error) {
	p, err := r.packument(pkg)
	if err != nil {
		return time.Time{}, false, err
	}
	if _, ok := p.Versions[ver]; !ok {
		return time.Time{}, false, nil
	}
	t, err := time.Parse(time.RFC3339, p.Time[ver])
	if err != nil {
		return time.Time{}, false, fmt.Errorf("%s@%s publish time: %w", pkg, ver, err)
	}
	return t, true, nil
}

func (r npmRegistry) Manifest(pkg, ver string) (releasefiles.Manifest, error) {
	p, err := r.packument(pkg)
	if err != nil {
		return releasefiles.Manifest{}, err
	}
	v, ok := p.Versions[ver]
	if !ok || v.Dist.Tarball == "" {
		return releasefiles.Manifest{}, fmt.Errorf("%s@%s has no tarball", pkg, ver)
	}
	resp, err := r.client.Get(v.Dist.Tarball)
	if err != nil {
		return releasefiles.Manifest{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return releasefiles.Manifest{}, fmt.Errorf("%s: %s", v.Dist.Tarball, resp.Status)
	}
	raw, err := tarFileFrom(resp.Body, v.Dist.Tarball, "package/manifest.json")
	if err != nil {
		return releasefiles.Manifest{}, err
	}
	return releasefiles.ParseManifest(raw)
}

func (r npmRegistry) DistTags(pkg string) (map[string]string, error) {
	p, err := r.packument(pkg)
	if err != nil {
		return nil, err
	}
	return p.DistTags, nil
}

// githubAPI reads issues and workflow runs over the REST API.
type githubAPI struct {
	base, repo, token string
	client            *http.Client
}

func (g githubAPI) get(path string, out any) error {
	if g.repo == "" || g.token == "" {
		return errors.New("GITHUB_REPOSITORY and GITHUB_TOKEN must be set")
	}
	req, err := http.NewRequest(http.MethodGet, g.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (g githubAPI) OpenIssues(label string) ([]Issue, error) {
	var raw []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
		PullRequest json.RawMessage `json:"pull_request"`
	}
	if err := g.get(fmt.Sprintf("/repos/%s/issues?state=open&per_page=100&labels=%s", g.repo, url.QueryEscape(label)), &raw); err != nil {
		return nil, err
	}
	var out []Issue
	for _, r := range raw {
		if r.PullRequest != nil {
			continue
		}
		i := Issue{Number: r.Number, Title: r.Title, Body: r.Body}
		for _, l := range r.Labels {
			i.Labels = append(i.Labels, l.Name)
		}
		out = append(out, i)
	}
	return out, nil
}

func (g githubAPI) LatestConclusion(c Canary, workflow, commit string) (string, bool, error) {
	var runs struct {
		WorkflowRuns []struct {
			HeadSHA    string `json:"head_sha"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"workflow_runs"`
	}
	if err := g.get(fmt.Sprintf("/repos/%s/actions/workflows/%s/runs?per_page=100", c.Repo, url.PathEscape(workflow)), &runs); err != nil {
		return "", false, err
	}
	for _, r := range runs.WorkflowRuns {
		if commit != "" && strings.HasPrefix(r.HeadSHA, commit) {
			if r.Status != "completed" {
				return r.Status, true, nil
			}
			return r.Conclusion, true, nil
		}
	}
	return "", false, nil
}
