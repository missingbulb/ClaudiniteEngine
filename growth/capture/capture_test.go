package capture

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// A member with a bare origin, a transcript, and a credential store
// holding a value no environment variable carries.
func TestACaptureWritesTheRawLinesWithTheStoreValueRedacted(t *testing.T) {
	base := t.TempDir()
	origin, repo, home := filepath.Join(base, "origin.git"), filepath.Join(base, "m"), filepath.Join(base, "home")
	git(t, base, "init", "-q", "--bare", origin)
	git(t, base, "init", "-q", repo)
	git(t, repo, "remote", "add", "origin", origin)
	const secret = "sk-store-only-0123456789abcdef"
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", ".credentials.json"), []byte(`{"claudeAiOauth":{"accessToken":"`+secret+`"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Key order, spacing and escapes a JSON round trip would change.
	lines := []string{
		`{"z":1,  "timestamp":"2026-10-01T10:00:00.000Z","a":"é said ` + secret + `"}`,
		`{"type":"x","timestamp":"2026-10-01T10:00:01.000Z" ,"m":{"b":2,"a":1}}`,
	}
	transcript := filepath.Join(base, "s1.jsonl")
	if err := os.WriteFile(transcript, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	req, ok := ParseArgs([]string{"--pr", "1", "--transcript", transcript, "--session", "s1"}, repo)
	if !ok {
		t.Fatal("args refused")
	}
	vars := map[string]string{"HOME": home, "GIT_CONFIG_NOSYSTEM": "1"}
	env := Env{Getenv: func(k string) string { return vars[k] }, Environ: []string{"HOME=" + home}, Home: home,
		Now: func() time.Time { return time.Date(2026, 10, 1, 11, 0, 0, 0, time.UTC) }}
	t.Setenv("HOME", home)
	var out, errb bytes.Buffer
	if r := Run(req, env, &out, &errb); r.Outcome != OK {
		t.Fatalf("%+v\n%s\n%s", r, out.String(), errb.String())
	}
	names := strings.Fields(git(t, origin, "ls-tree", "--name-only", DefaultBranch))
	if len(names) != 2 || names[0] != "2026-10-01T1100Z--pr-1--s1.jsonl" || names[1] != "README.md" {
		t.Fatalf("branch holds %v", names)
	}
	got := git(t, origin, "show", DefaultBranch+":"+names[0])
	want := strings.Replace(strings.Join(lines, "\n")+"\n", secret, "[REDACTED:env:claude-credentials]", 1)
	if strings.Contains(got, secret) {
		t.Fatalf("the store's value reached the branch:\n%s", got)
	}
	if got != want {
		t.Fatalf("the written bytes are not the raw lines scrubbed:\ngot  %q\nwant %q", got, want)
	}
}
