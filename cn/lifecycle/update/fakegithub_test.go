package update

import (
	"fmt"
	"strings"
	"sync"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/githubapi"
)

// fakeGitHub records every call the updater makes and answers from its
// fields.
type fakeGitHub struct {
	mu     sync.Mutex
	runs   map[string][]githubapi.Run // by head sha
	pulls  []githubapi.PR
	issues []githubapi.Issue
	labels map[int][]string
	calls  []string
	next   int
	failOn string
	// headOf reads a branch's head, which CreatePull answers as GitHub
	// does; nil answers none.
	headOf func(ref string) string
	// holdOnOpen holds a claudinite-ci.yml pull_request run at
	// action_required on each PR CreatePull opens, as GitHub holds a
	// job-token PR's runs.
	holdOnOpen bool
	// approved is what an approved run concludes; "" leaves it queued.
	approved string
	runID    int64
	// onOpen are other workflows' runs GitHub starts on each PR
	// CreatePull opens, on its head.
	onOpen []githubapi.Run
}

func newFake() *fakeGitHub {
	return &fakeGitHub{runs: map[string][]githubapi.Run{}, labels: map[int][]string{}, next: 1}
}

func (f *fakeGitHub) record(format string, args ...any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := fmt.Sprintf(format, args...)
	f.calls = append(f.calls, c)
	if f.failOn != "" && strings.HasPrefix(c, f.failOn) {
		return fmt.Errorf("%s: 500", c)
	}
	return nil
}

func (f *fakeGitHub) called(prefix string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeGitHub) WorkflowRuns(workflow, sha string) ([]githubapi.Run, error) {
	if err := f.record("runs %s %s", workflow, sha); err != nil {
		return nil, err
	}
	// A run is the workflow named for its file; an unnamed one is
	// claudinite-ci's.
	f.mu.Lock()
	defer f.mu.Unlock()
	name := strings.TrimSuffix(workflow, ".yml")
	var out []githubapi.Run
	for _, r := range f.runs[sha] {
		if r.Name == name || (r.Name == "" && workflow == CIWorkflow) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeGitHub) HeadRuns(sha string) ([]githubapi.Run, error) {
	if err := f.record("head-runs %s", sha); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]githubapi.Run{}, f.runs[sha]...), nil
}

func (f *fakeGitHub) ApproveRun(id int64) error {
	if err := f.record("approve %d", id); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for sha, runs := range f.runs {
		for i := range runs {
			if runs[i].ID == id && runs[i].Conclusion == "action_required" {
				runs[i].Status, runs[i].Conclusion = "queued", ""
				if f.approved != "" {
					runs[i].Status, runs[i].Conclusion = "completed", f.approved
				}
				f.runs[sha] = runs
				return nil
			}
		}
	}
	return fmt.Errorf("approve %d: 403 This run is not waiting for approval", id)
}

func (f *fakeGitHub) OpenPulls() ([]githubapi.PR, error) {
	if err := f.record("pulls"); err != nil {
		return nil, err
	}
	var out []githubapi.PR
	for _, p := range f.pulls {
		if p.State == "open" {
			p.Labels = append(p.Labels, f.labels[p.Number]...)
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeGitHub) Pull(n int) (githubapi.PR, error) {
	if err := f.record("pull %d", n); err != nil {
		return githubapi.PR{}, err
	}
	for _, p := range f.pulls {
		if p.Number == n {
			p.Labels = append(p.Labels, f.labels[n]...)
			return p, nil
		}
	}
	return githubapi.PR{}, fmt.Errorf("no PR %d", n)
}

func (f *fakeGitHub) CreatePull(title, body, head, base string) (githubapi.PR, error) {
	if err := f.record("create-pull %s|%s|%s|%s", title, head, base, body); err != nil {
		return githubapi.PR{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p := githubapi.PR{Number: f.next, Title: title, Author: "github-actions[bot]", HeadRef: head, BaseRef: base, State: "open"}
	f.next++
	if f.headOf != nil {
		p.HeadSHA = f.headOf(head)
	}
	if f.holdOnOpen && p.HeadSHA != "" {
		f.runID++
		f.runs[p.HeadSHA] = append(f.runs[p.HeadSHA], githubapi.Run{ID: f.runID, Name: "claudinite-ci", HeadSHA: p.HeadSHA, Event: "pull_request",
			Status: "completed", Conclusion: "action_required", CreatedAt: "2026-10-01T00:00:00Z"})
	}
	for _, r := range f.onOpen {
		if p.HeadSHA != "" {
			r.HeadSHA = p.HeadSHA
			f.runs[p.HeadSHA] = append(f.runs[p.HeadSHA], r)
		}
	}
	f.pulls = append(f.pulls, p)
	return p, nil
}

func (f *fakeGitHub) setPull(n int, state string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.pulls {
		if f.pulls[i].Number == n {
			f.pulls[i].State = state
		}
	}
}

func (f *fakeGitHub) ClosePull(n int) error {
	if err := f.record("close-pull %d", n); err != nil {
		return err
	}
	f.setPull(n, "closed")
	return nil
}

func (f *fakeGitHub) MergePull(n int, sha, title string) error {
	if err := f.record("merge %d %s %s", n, sha, title); err != nil {
		return err
	}
	f.setPull(n, "merged")
	return nil
}

func (f *fakeGitHub) AddLabel(n int, label string) error {
	if err := f.record("label %d %s", n, label); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.labels[n] = append(f.labels[n], label)
	return nil
}

func (f *fakeGitHub) Comment(n int, body string) error { return f.record("comment %d %s", n, body) }

func (f *fakeGitHub) Dispatch(workflow, ref string, inputs map[string]string) error {
	return f.record("dispatch %s %s pr=%s", workflow, ref, inputs["pr"])
}

func (f *fakeGitHub) OpenIssues(label string) ([]githubapi.Issue, error) {
	if err := f.record("issues %s", label); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]githubapi.Issue{}, f.issues...), nil
}

func (f *fakeGitHub) CreateIssue(title, body, label string) (int, error) {
	if err := f.record("create-issue %s|%s", title, label); err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.next
	f.next++
	f.issues = append(f.issues, githubapi.Issue{Number: n, Title: title, Body: body})
	return n, nil
}

func (f *fakeGitHub) CloseIssue(n int) error {
	if err := f.record("close-issue %d", n); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var open []githubapi.Issue
	for _, is := range f.issues {
		if is.Number != n {
			open = append(open, is)
		}
	}
	f.issues = open
	return nil
}

func (f *fakeGitHub) UpdateIssueBody(n int, body string) error {
	if err := f.record("update-issue %d", n); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.issues {
		if f.issues[i].Number == n {
			f.issues[i].Body = body
		}
	}
	return nil
}

// writes is calls with the reads (pull, pulls, runs, issues) left out.
func writes(calls []string) []string {
	var out []string
	for _, c := range calls {
		switch strings.SplitN(c, " ", 2)[0] {
		case "pull", "pulls", "runs", "head-runs", "issues":
		default:
			out = append(out, c)
		}
	}
	return out
}
