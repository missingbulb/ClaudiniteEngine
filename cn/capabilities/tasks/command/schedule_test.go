package command

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/world"
)

func envOf(m map[string]string) world.Env { return func(k string) string { return m[k] } }

func TestTheHoldExitsBeforeAnyRead(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	env := envOf(map[string]string{world.VarsBagEnv: `{"` + world.SuspendAllVar + `": "true"}`})
	if err := scheduleRun([]string{"--repo", filepath.Join(t.TempDir(), "absent")}, &out, env, Engine{}); err != nil {
		t.Fatalf("held run: %v", err)
	}
	if !strings.Contains(out.String(), "the queue is held") || !strings.Contains(out.String(), "[cn] tasks schedule ok ") {
		t.Fatalf("output %q", out.String())
	}
}

func TestADormantProjectIsNotScheduled(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	p := filepath.Join(repo, ".claudinite/settings.yaml")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte("tasks:\n  dormant: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := scheduleRun([]string{"--repo", repo}, &out, envOf(nil), Engine{}); err != nil {
		t.Fatalf("dormant run: %v", err)
	}
	if !strings.Contains(out.String(), "declares its scheduler dormant") {
		t.Fatalf("output %q", out.String())
	}
	_ = os.WriteFile(p, []byte("tasks:\n  dormant: \"yes\"\n"), 0o644)
	out.Reset()
	err := scheduleRun([]string{"--repo", repo}, &out, envOf(nil), Engine{})
	if err == nil || !strings.Contains(err.Error(), `"dormant" must be true or false`) {
		t.Fatalf("a mistyped dormancy fails the run naming the key: %v %q", err, out.String())
	}
}

// A relative --repo is the checkout every task worker is handed, so it
// is made absolute once: a worker runs in its task's folder.
func TestTheTaskRepoRootIsAbsolute(t *testing.T) {
	repo := t.TempDir()
	_ = os.MkdirAll(filepath.Join(repo, ".claudinite"), 0o755)
	_ = os.WriteFile(filepath.Join(repo, ".claudinite/settings.yaml"), []byte("packs:\n  declared: []\n"), 0o644)
	t.Chdir(repo)
	r, err := LoadRepo(".")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(r.Root) {
		t.Errorf("root %q", r.Root)
	}
}
