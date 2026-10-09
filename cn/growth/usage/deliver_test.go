package usage

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// member is a checkout of a bare origin whose main carries files, and a
// second clone that lands commits on it after the checkout was cloned.
type member struct {
	t                   *testing.T
	origin, root, other string
	calls               [][]string
	opened              []string
}

func gitT(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func newMember(t *testing.T, files map[string]string) *member {
	dir := t.TempDir()
	m := &member{t: t, origin: filepath.Join(dir, "origin.git"), root: filepath.Join(dir, "member"), other: filepath.Join(dir, "other")}
	gitT(t, "init", "-q", "--bare", "-b", "main", m.origin)
	gitT(t, "clone", "-q", m.origin, m.other)
	gitT(t, "-C", m.other, "checkout", "-q", "-b", "main")
	m.land(files, "adopt")
	gitT(t, "clone", "-q", m.origin, m.root)
	return m
}

func (m *member) land(files map[string]string, message string) string {
	for p, body := range files {
		full := filepath.Join(m.other, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			m.t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			m.t.Fatal(err)
		}
	}
	gitT(m.t, "-C", m.other, "add", "-A")
	gitT(m.t, "-C", m.other, "commit", "-q", "-m", message)
	gitT(m.t, "-C", m.other, "push", "-q", "origin", "HEAD:main")
	return strings.TrimSpace(gitT(m.t, "-C", m.other, "rev-parse", "HEAD"))
}

func (m *member) rev(ref string) string {
	return strings.TrimSpace(gitT(m.t, "--git-dir", m.origin, "rev-parse", ref))
}

func (m *member) show(ref, path string) *string {
	out, err := exec.Command("git", "--git-dir", m.origin, "show", ref+":"+path).Output()
	if err != nil {
		return nil
	}
	s := string(out)
	return &s
}

func (m *member) message(ref string) string {
	return gitT(m.t, "--git-dir", m.origin, "log", "-1", "--format=%B", ref)
}

func (m *member) history() History {
	return History{
		Local: func(args ...string) (string, error) { return RunLocal(m.root, nil, "", args...) },
		Remote: func(args ...string) (string, error) {
			m.calls = append(m.calls, args)
			return RunLocal(m.root, nil, "", args...)
		},
	}
}

func (m *member) delivery(branch string, pr int) Delivery {
	return Delivery{Root: m.root, Base: "main", Branch: branch, PR: pr, History: m.history(),
		Trailers: "Claudinite-Task: engine/usage-fold\nClaudinite-Automerge-Policy: anything",
		OpenPr: func(title, body, head, base string) (int, error) {
			m.opened = append(m.opened, title+"|"+body+"|"+head+"|"+base)
			return 41, nil
		}}
}

func TestTheFoldedFilesLandOnTheTargetBranchOnTopOfTheRemoteBase(t *testing.T) {
	m := newMember(t, map[string]string{"README.md": "hi\n"})
	tip := m.land(map[string]string{"x.txt": "landed after the clone\n"}, "another writer")
	out, err := m.delivery("claudinite/usage-fold", 0).Deliver(Change{Files: []File{{".claudinite/usage/a.json", "{\"a\":1}\n"}},
		Subject: "Claudinite: fold usage", Title: "Claudinite: usage fold", Body: "b"})
	if err != nil || out != (Delivered{Branch: "claudinite/usage-fold", Number: 41}) {
		t.Fatalf("%+v %v", out, err)
	}
	if got := m.rev("claudinite/usage-fold~1"); got != tip {
		t.Errorf("built on %s, not the remote tip %s", got, tip)
	}
	if got := m.show("claudinite/usage-fold", ".claudinite/usage/a.json"); got == nil || *got != "{\"a\":1}\n" {
		t.Errorf("%v", got)
	}
	if !regexp.MustCompile(`Claudinite-Task: engine/usage-fold\nClaudinite-Automerge-Policy: anything`).MatchString(m.message("claudinite/usage-fold")) {
		t.Errorf("%q", m.message("claudinite/usage-fold"))
	}
	if len(m.opened) != 1 || m.opened[0] != "Claudinite: usage fold|b|claudinite/usage-fold|main" {
		t.Errorf("%v", m.opened)
	}
}

func TestAnExecutorNamedPullRequestIsAmendedAndNothingNewOpens(t *testing.T) {
	m := newMember(t, map[string]string{"README.md": "hi\n"})
	out, err := m.delivery("b", 9).Deliver(Change{Files: []File{{"a.json", "1\n"}}, Subject: "s", Title: "t", Body: "b"})
	if err != nil || out != (Delivered{Branch: "b", Number: 9, Reused: true}) {
		t.Fatalf("%+v %v", out, err)
	}
	if len(m.opened) != 0 {
		t.Fatalf("%v", m.opened)
	}
}

func TestARecomputeTheBranchAlreadyHoldsPushesNothing(t *testing.T) {
	m := newMember(t, map[string]string{"README.md": "hi\n"})
	change := Change{Files: []File{{"a.json", "1\n"}}, Subject: "s", Title: "t", Body: "b"}
	if _, err := m.delivery("b", 9).Deliver(change); err != nil {
		t.Fatal(err)
	}
	head := m.rev("b")
	m.calls = nil
	if _, err := m.delivery("b", 9).Deliver(change); err != nil {
		t.Fatal(err)
	}
	for _, c := range m.calls {
		if c[0] == "push" {
			t.Fatalf("pushed %v", c)
		}
	}
	if m.rev("b") != head {
		t.Fatal("the branch has a new head")
	}
}

func TestARollingFileMovesWithItsBytesIntactBeforeTheFoldWritesOnTopOfIt(t *testing.T) {
	m := newMember(t, map[string]string{"old/u.json": "history\n"})
	base, err := BaseTip(m.history(), "main")
	if err != nil {
		t.Fatal(err)
	}
	text, moves := ReadRollingAt(m.history(), base, "new/u.json", "old/u.json")
	if text == nil || *text != "history\n" || len(moves) != 1 || moves[0] != (Move{"old/u.json", "new/u.json"}) {
		t.Fatalf("%v %v", text, moves)
	}
	if _, err := m.delivery("b", 0).Deliver(Change{Files: []File{{"new/u.json", "history\nmore\n"}}, Moves: moves, Subject: "s", Title: "t", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	if got := m.show("b~1", "new/u.json"); got == nil || *got != "history\n" {
		t.Errorf("the move commit carries the old bytes unchanged: %v", got)
	}
	if got := m.show("b~1", "old/u.json"); got != nil {
		t.Errorf("%q", *got)
	}
	if got := m.show("b", "new/u.json"); got == nil || *got != "history\nmore\n" {
		t.Errorf("%v", got)
	}
	if !strings.HasPrefix(m.message("b~1"), "Move a rolling file to their new home, content unchanged\n\nold/u.json -> new/u.json\n\nClaudinite-Task:") {
		t.Errorf("%q", m.message("b~1"))
	}
}

func TestNoTargetBranchIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	m := newMember(t, map[string]string{"README.md": "hi\n"})
	_, err := m.delivery("", 0).Deliver(Change{Subject: "s"})
	if err == nil || !strings.Contains(err.Error(), "no branch to deliver on") || len(m.calls) != 0 {
		t.Fatalf("%v %v", err, m.calls)
	}
}

func TestTheBaseBranchIsRefusedAsATargetBeforeAnythingIsPushed(t *testing.T) {
	m := newMember(t, map[string]string{"README.md": "hi\n"})
	before := m.rev("main")
	_, err := m.delivery("main", 0).Deliver(Change{Files: []File{{"a.json", "1\n"}}, Subject: "s", Title: "t", Body: "b"})
	if err == nil || !strings.Contains(err.Error(), "refusing to force-push to main") {
		t.Fatal(err)
	}
	if m.rev("main") != before {
		t.Fatal("main moved")
	}
	for _, c := range m.calls {
		if c[0] == "push" {
			t.Fatalf("pushed %v", c)
		}
	}
}
