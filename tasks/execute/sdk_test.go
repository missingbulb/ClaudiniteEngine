package execute

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/tasks/sim"
	"github.com/missingbulb/ClaudiniteEngine/tasks/world"
)

// sdkWorld is the simulated repository under the job's token.
type sdkWorld struct {
	*sim.GitHub
	repo       *sim.Repo
	files      map[string]string
	dispatched []string
}

func newSDKWorld(t *testing.T) *sdkWorld {
	return &sdkWorld{GitHub: sim.NewGitHub(sim.NewClock(loopNow)), repo: sim.NewRepo(), files: map[string]string{}}
}

func (w *sdkWorld) CreatePull(title, body, head, base string) (world.Pull, error) {
	return w.repo.CreatePull(title, body, head, base)
}

func (w *sdkWorld) DispatchWorkflow(name, ref string) error {
	w.dispatched = append(w.dispatched, name+"@"+ref)
	return nil
}

func (w *sdkWorld) FileAt(path, ref string) (string, error) {
	if s, ok := w.files[ref+":"+path]; ok {
		return s, nil
	}
	return "", world.ErrGone
}

func call(t *testing.T, s *SDK, method string, args any) (any, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	out, err := s.Handle(method, raw)
	if err == nil {
		// Every answer crosses the pipe as JSON.
		if _, mErr := json.Marshal(out); mErr != nil {
			t.Fatalf("%s answered an unencodable value: %v", method, mErr)
		}
	}
	return out, err
}

func sdkFor(w *sdkWorld, granted ...string) (*SDK, *[]string) {
	var logs []string
	return &SDK{Pack: "acme-pack", Task: "a", Granted: granted, GitHub: w, DefaultBranch: "main",
		Log: func(s string) { logs = append(logs, s) }}, &logs
}

func TestAPacksGrantIsWhatItDeclaredNarrowedByTheMember(t *testing.T) {
	declared := []string{"openPr", "createComment"}
	if got := GrantedActions(declared, nil); !reflect.DeepEqual(got, declared) {
		t.Error(got)
	}
	if got := GrantedActions(declared, map[string]any{"githubActions": []any{"createComment", "dispatchWorkflow"}}); !reflect.DeepEqual(got, []string{"createComment"}) {
		t.Error("the member narrows, never widens:", got)
	}
	if got := GrantedActions(declared, map[string]any{"githubActions": []any{}}); len(got) != 0 {
		t.Error(got)
	}
}

func TestAnActionOutsideTheGrantIsRefusedAndLoggedWithThePack(t *testing.T) {
	w := newSDKWorld(t)
	s, logs := sdkFor(w, "createComment")
	if _, err := call(t, s, "github.openPr", map[string]any{"title": "t", "head": "h"}); err == nil || !strings.Contains(err.Error(), "acme-pack") {
		t.Errorf("%v", err)
	}
	if len(*logs) != 1 || !strings.Contains((*logs)[0], "refused github.openPr") || !strings.Contains((*logs)[0], "acme-pack/a") {
		t.Error(*logs)
	}
	if len(s.Opened) != 0 {
		t.Error("opened")
	}
	if _, err := call(t, s, "github.deleteRepo", nil); err == nil {
		t.Error("an unknown action ran")
	}
	if !strings.Contains(strings.Join(s.Methods(), ","), "github.openPr") {
		t.Error("an ungranted action must still be announced, so its refusal is logged here")
	}
}

func TestOpenPrOpensAgainstTheDefaultBranchAndIsRecorded(t *testing.T) {
	w := newSDKWorld(t)
	s, _ := sdkFor(w, "openPr")
	out, err := call(t, s, "github.openPr", map[string]any{"title": "t", "body": "b", "head": "claudinite/acme-pack/a/x"})
	if err != nil {
		t.Fatal(err)
	}
	n := out.(map[string]any)["number"].(int)
	if len(s.Opened) != 1 || s.Opened[0].Number != n || s.Opened[0].HeadRef != "claudinite/acme-pack/a/x" {
		t.Error(s.Opened)
	}
	if _, err := call(t, s, "github.openPr", map[string]any{"title": "t"}); err == nil {
		t.Error("a pull request with no head")
	}
}

func TestReadFileAnswersNullForAnAbsentFile(t *testing.T) {
	w := newSDKWorld(t)
	w.files["main:a.txt"] = "hello"
	s, _ := sdkFor(w, "readFile")
	out, err := call(t, s, "github.readFile", map[string]any{"path": "a.txt"})
	if err != nil || out.(map[string]any)["content"] != "hello" {
		t.Error(out, err)
	}
	out, err = call(t, s, "github.readFile", map[string]any{"path": "b.txt", "ref": "dev"})
	if err != nil || out.(map[string]any)["content"] != nil {
		t.Error(out, err)
	}
}

func TestCommentListAndDispatch(t *testing.T) {
	w := newSDKWorld(t)
	w.Seed(sim.StoredIssue{Issue: workitem.Issue{Number: 3, Title: "x", Labels: []string{"l"}}})
	w.Seed(sim.StoredIssue{Issue: workitem.Issue{Number: 4, Title: "y"}})
	w.Seed(sim.StoredIssue{Issue: workitem.Issue{Number: 5, Title: "pr"}, PullRequest: true})
	s, _ := sdkFor(w, "createComment", "listIssues", "dispatchWorkflow")
	if _, err := call(t, s, "github.createComment", map[string]any{"issue": 3, "body": "hi"}); err != nil {
		t.Fatal(err)
	}
	if i, _ := w.Get(3); len(i.Comments) != 1 || i.Comments[0].Body != "hi" {
		t.Error(i.Comments)
	}
	out, err := call(t, s, "github.listIssues", map[string]any{"label": "l"})
	if err != nil || len(out.([]map[string]any)) != 1 || out.([]map[string]any)[0]["number"] != 3 {
		t.Error(out, err)
	}
	out, _ = call(t, s, "github.listIssues", map[string]any{})
	if len(out.([]map[string]any)) != 2 {
		t.Error("a pull request is not an issue:", out)
	}
	if _, err := call(t, s, "github.dispatchWorkflow", map[string]any{"workflow": "ci.yml"}); err != nil || !reflect.DeepEqual(w.dispatched, []string{"ci.yml@main"}) {
		t.Error(w.dispatched, err)
	}
}

func TestTheTrackerIsFoundByExactTitleInAnyStateOrCreatedClosed(t *testing.T) {
	w := newSDKWorld(t)
	w.Seed(sim.StoredIssue{Issue: workitem.Issue{Number: 2, Title: "Usage fold log (old)", State: "closed"}})
	w.Seed(sim.StoredIssue{Issue: workitem.Issue{Number: 5, Title: " Usage fold log ", State: "open"}})
	w.Seed(sim.StoredIssue{Issue: workitem.Issue{Number: 9, Title: "Usage fold log", State: "closed"}})
	s, _ := sdkFor(w, "findOrCreateTracker", "writeTracker")
	out, err := call(t, s, "github.findOrCreateTracker", map[string]any{"title": "Usage fold log"})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["number"] != 5 || m["created"] != false || m["duplicates"] != 1 {
		t.Error(m)
	}
	if i, _ := w.Get(5); i.State != "closed" {
		t.Error("a tracker found open is closed on the way back")
	}
	out, err = call(t, s, "github.findOrCreateTracker", map[string]any{"title": "Fresh log"})
	if err != nil {
		t.Fatal(err)
	}
	n := out.(map[string]any)["number"].(int)
	if i, _ := w.Get(n); i.State != "closed" || !strings.Contains(i.Body, "Fresh log") || out.(map[string]any)["created"] != true {
		t.Error(i, out)
	}
	if _, err := call(t, s, "github.writeTracker", map[string]any{"number": n, "body": "now", "comment": "run 1"}); err != nil {
		t.Fatal(err)
	}
	if i, _ := w.Get(n); i.Body != "now" || len(i.Comments) != 1 || i.State != "closed" {
		t.Error(i)
	}
}

func TestATrackerLookupThatCannotReadIsAnErrorNeverAFreshTracker(t *testing.T) {
	w := newSDKWorld(t)
	w.Faults.RateLimited = 1
	s, _ := sdkFor(w, "findOrCreateTracker")
	if _, err := call(t, s, "github.findOrCreateTracker", map[string]any{"title": "Log"}); err == nil {
		t.Error("a failed read minted a tracker")
	}
	if len(w.All()) != 0 {
		t.Error(w.All())
	}
}

func TestGitConfigAndPacks(t *testing.T) {
	var seen []string
	s := &SDK{Pack: "acme-pack", Task: "a",
		Git: func(args ...string) (gitcmd.Ran, error) {
			seen = args
			return gitcmd.Ran{Code: 1, Stdout: "o", Stderr: "e"}, nil
		},
		Config: func(pack string) map[string]any {
			if pack == "acme-pack" {
				return map[string]any{"k": "v"}
			}
			return nil
		},
		Packs: []PackInfo{{ID: "acme-pack", Version: "1.0", Kind: "canon"}},
	}
	out, err := call(t, s, "git", map[string]any{"args": []string{"status", "--short"}})
	if err != nil || !reflect.DeepEqual(seen, []string{"status", "--short"}) || out.(map[string]any)["code"] != 1 || out.(map[string]any)["stderr"] != "e" {
		t.Error(out, err, seen)
	}
	if out, _ := call(t, s, "config", map[string]any{"pack": "acme-pack"}); !reflect.DeepEqual(out, map[string]any{"k": "v"}) {
		t.Error(out)
	}
	if out, _ := call(t, s, "config", map[string]any{"pack": "other"}); !reflect.DeepEqual(out, map[string]any{}) {
		t.Error("a pack with no config answers {}:", out)
	}
	if out, _ := call(t, s, "packs", nil); !reflect.DeepEqual(out, s.Packs) {
		t.Error(out)
	}
	if _, err := call(t, s, "shell", nil); err == nil {
		t.Error("an unknown method")
	}
}

// A script's push may not force or delete the default branch, nor push
// every ref at once; the pushes the canon's tasks make (a delivery branch
// forced, a release commit onto the default branch, a Pages branch forced)
// pass through.
func TestGitRefusesRewritingTheDefaultBranch(t *testing.T) {
	var ran [][]string
	var logs []string
	s := &SDK{Pack: "acme-pack", Task: "acme-task", DefaultBranch: "main",
		Git: func(args ...string) (gitcmd.Ran, error) {
			ran = append(ran, args)
			return gitcmd.Ran{}, nil
		},
		Log: func(l string) { logs = append(logs, l) },
	}
	refused := [][]string{
		{"push", "--force", "origin", "HEAD:refs/heads/main"},
		{"push", "-f", "origin", "abc:main"},
		{"push", "origin", "+HEAD:refs/heads/main"},
		{"push", "--force-with-lease", "origin", "main"},
		{"-c", "x=y", "push", "--quiet", "--force", "origin", "HEAD:main"},
		{"push", "origin", ":refs/heads/main"},
		{"push", "--delete", "origin", "main"},
		{"push", "--mirror", "origin"},
		{"push", "--all", "origin"},
		{"push", "--force", "origin"},
	}
	for _, args := range refused {
		if _, err := call(t, s, "git", map[string]any{"args": args}); err == nil || !strings.Contains(err.Error(), "git push refused") {
			t.Errorf("%v ran: %v", args, err)
		}
	}
	if len(ran) != 0 {
		t.Errorf("a refused push reached git: %v", ran)
	}
	if len(logs) != len(refused) || !strings.Contains(logs[0], "force-pushes the default branch main") {
		t.Errorf("logs %v", logs)
	}
	passed := [][]string{
		{"push", "--quiet", "--force", "origin", "abc:refs/heads/claudinite/acme-pack/acme-task/1-x"},
		{"push", "--quiet", "origin", "abc:refs/heads/main"},
		{"push", "--quiet", "--force", "origin", "abc:refs/heads/gh-pages"},
		{"push", "origin", "main-extra"},
		{"fetch", "--force", "origin", "main"},
		{"push"},
	}
	for _, args := range passed {
		if _, err := call(t, s, "git", map[string]any{"args": args}); err != nil {
			t.Errorf("%v refused: %v", args, err)
		}
	}
	if len(ran) != len(passed) {
		t.Errorf("ran %v", ran)
	}
}
