package curation

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type fixture struct {
	t   *testing.T
	dir string
	n   int
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{t: t, dir: t.TempDir()}
	f.git("init", "-q")
	return f
}

func (f *fixture) git(args ...string) string {
	f.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = f.dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t",
		"GIT_COMMITTER_EMAIL=t@example.com", "GIT_AUTHOR_DATE=2026-07-0"+string(rune('1'+f.n))+"T12:00:00Z",
		"GIT_COMMITTER_DATE=2026-07-0"+string(rune('1'+f.n))+"T12:00:00Z")
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func (f *fixture) commit(msg string, files map[string]string) {
	f.t.Helper()
	for rel, body := range files {
		p := filepath.Join(f.dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			f.t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
	f.git("add", "-A")
	f.git("commit", "-q", "-m", msg)
	f.n++
}
