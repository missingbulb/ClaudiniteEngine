package cispeed

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runs is a list-workflow-runs response holding one run per duration, in seconds.
func runs(t *testing.T, conclusion string, secs ...int) string {
	t.Helper()
	start := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	var rs []map[string]any
	for i, s := range secs {
		at := start.Add(time.Duration(i) * time.Hour)
		rs = append(rs, map[string]any{
			"id": i + 1, "status": "completed", "conclusion": conclusion,
			"run_started_at": at.Format(time.RFC3339), "updated_at": at.Add(time.Duration(s) * time.Second).Format(time.RFC3339),
			"html_url": "https://example.test/runs/" + string(rune('a'+i)),
		})
	}
	b, err := json.Marshal(map[string]any{"total_count": len(rs), "workflow_runs": rs})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func p75(t *testing.T, in string) (string, map[string]string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "github-output")
	cmd := exec.Command("sh", "probe/ci-speed/p75.sh", "--budget", "60")
	cmd.Dir = "../.."
	cmd.Stdin = strings.NewReader(in)
	cmd.Env = append(os.Environ(), "GITHUB_OUTPUT="+out)
	report, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("p75.sh: %v\n%s", err, report)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	kv := map[string]string{}
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		k, v, _ := strings.Cut(l, "=")
		kv[k] = v
	}
	return string(report), kv
}

func TestP75OverTheBudget(t *testing.T) {
	report, kv := p75(t, runs(t, "success", 30, 40, 50, 60, 70, 80, 90, 100))
	if kv["verdict"] != "over" || kv["p75"] != "80" || kv["runs"] != "8" {
		t.Errorf("outputs %v, want over at 80 over 8 runs", kv)
	}
	if !strings.Contains(report, "80s") || !strings.Contains(report, "https://example.test/runs/h") {
		t.Errorf("the report names neither the p75 nor the slowest run:\n%s", report)
	}
}

func TestP75UnderTheBudget(t *testing.T) {
	_, kv := p75(t, runs(t, "success", 40, 42, 44, 46, 90))
	if kv["verdict"] != "under" || kv["p75"] != "46" {
		t.Errorf("outputs %v, want under at 46", kv)
	}
}

func TestP75NeedsFiveSuccessfulRuns(t *testing.T) {
	in := runs(t, "success", 90, 90, 90, 90)
	if _, kv := p75(t, in); kv["verdict"] != "too-few" {
		t.Errorf("four runs: outputs %v, want too-few", kv)
	}
	if _, kv := p75(t, runs(t, "failure", 90, 90, 90, 90, 90)); kv["verdict"] != "too-few" {
		t.Errorf("five failed runs: outputs %v, want too-few", kv)
	}
}
