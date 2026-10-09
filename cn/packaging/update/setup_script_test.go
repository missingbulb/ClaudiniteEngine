package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// checkout makes dir/name a checkout whose launcher records where it ran,
// exiting with code.
func checkout(t *testing.T, dir, name, log, code string) {
	t.Helper()
	p := filepath.Join(dir, name, ".claudinite")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "echo \"$PWD $*\" >> " + log + "\nexit " + code + "\n"
	if err := os.WriteFile(filepath.Join(p, "launch"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestEnvSetupScript(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	ran := func(log string) []string {
		raw, _ := os.ReadFile(log)
		return strings.Fields(strings.ReplaceAll(string(raw), " env install", ""))
	}
	for _, c := range []struct {
		name  string
		repos []string
		code  string
		in    string
	}{
		{"the parent of one checkout", []string{"member"}, "0", ""},
		{"the parent of several checkouts", []string{"one", "two"}, "0", ""},
		{"inside the checkout", []string{"member"}, "0", "member"},
		{"no checkout holds a launcher", nil, "0", ""},
		{"a launcher that fails", []string{"member"}, "1", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			parent := t.TempDir()
			log := filepath.Join(t.TempDir(), "ran")
			if err := os.WriteFile(log, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(parent, "not-a-member"), 0o755); err != nil {
				t.Fatal(err)
			}
			for _, r := range c.repos {
				checkout(t, parent, r, log, c.code)
			}
			cmd := exec.Command("bash", "-c", EnvSetupScript)
			cmd.Dir = filepath.Join(parent, c.in)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("exit %v: %s", err, out)
			}
			got := ran(log)
			if len(got) != len(c.repos) {
				t.Fatalf("ran in %v, want one run per checkout %v", got, c.repos)
			}
			for i, r := range c.repos {
				if filepath.Base(got[i]) != r {
					t.Errorf("run %d in %s, want %s", i, got[i], r)
				}
			}
		})
	}
}

func TestDesignQuotesEnvSetupScript(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/design.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "`"+EnvSetupScript+"`") {
		t.Error("docs/design.md's Environment setup script entry must quote update.EnvSetupScript verbatim")
	}
}
