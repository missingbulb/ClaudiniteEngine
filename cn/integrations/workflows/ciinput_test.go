package workflows

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// whoStep is the run: body of claudinite-ci.yml's "who opened the change"
// step, dedented.
func whoStep(t *testing.T) string {
	t.Helper()
	lines := strings.Split(string(Templates()["claudinite-ci.yml"]), "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != "- name: who opened the change" {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) != "run: |" {
				continue
			}
			indent := len(lines[j+1]) - len(strings.TrimLeft(lines[j+1], " "))
			var body []string
			for _, b := range lines[j+1:] {
				if strings.TrimSpace(b) != "" && len(b)-len(strings.TrimLeft(b, " ")) < indent {
					break
				}
				if len(b) >= indent {
					b = b[indent:]
				}
				body = append(body, b)
			}
			return strings.Join(body, "\n")
		}
	}
	t.Fatal("no who-opened-the-change step")
	return ""
}

// The pr input reaches gh api only as digits.
func TestTheCIPRInputIsDigitsOnly(t *testing.T) {
	script := whoStep(t)
	bin := t.TempDir()
	marker := filepath.Join(bin, "called")
	_ = os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\ntouch '"+marker+"'\necho someone\n"), 0o755)
	run := func(pr string) (string, error) {
		_ = os.Remove(marker)
		out := filepath.Join(t.TempDir(), "out")
		cmd := exec.Command("bash", "-eo", "pipefail", "-c", script)
		cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "EVENT=workflow_dispatch", "PR="+pr, "GITHUB_OUTPUT="+out, "GITHUB_REPOSITORY=acme/member", "ACTOR=a")
		o, err := cmd.CombinedOutput()
		return string(o), err
	}
	for _, bad := range []string{"1; echo x", "../1", "1/merge", "-1", "1 2"} {
		if o, err := run(bad); err == nil {
			t.Errorf("pr %q accepted:\n%s", bad, o)
		}
		if _, err := os.Stat(marker); err == nil {
			t.Errorf("pr %q reached gh", bad)
		}
	}
	if o, err := run("12"); err != nil {
		t.Errorf("pr 12 refused: %v\n%s", err, o)
	} else if _, err := os.Stat(marker); err != nil {
		t.Error("pr 12 did not reach gh")
	}
}
