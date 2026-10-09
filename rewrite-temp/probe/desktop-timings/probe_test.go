package desktoptimings

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/version"
)

var rows = []string{
	"cn hook session-start",
	"Go calls Node once",
	"checks cold build",
	"checks warm rebuild",
	"launcher, warm cache",
}

func TestProbeWritesResults(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a release and the check program twice")
	}
	out := t.TempDir()
	cmd := exec.Command("sh", "rewrite-temp/probe/desktop-timings/run.sh", "--runs", "2", "--out", out)
	cmd.Dir = "../../.."
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run.sh: %v\n%s", err, o)
	}
	base := filepath.Join(out, version.Platform()+"-"+time.Now().UTC().Format("2006-01-02"))
	md, err := os.ReadFile(base + ".md")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if !strings.Contains(string(md), "| "+r) {
			t.Errorf("markdown lacks the %q row:\n%s", r, md)
		}
	}
	for _, want := range []string{"uname -a", "go version", "node --version", "CPU"} {
		if !strings.Contains(string(md), want) {
			t.Errorf("markdown lacks %q", want)
		}
	}
	raw, err := os.ReadFile(base + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Items []struct {
			Name     string    `json:"name"`
			MedianMs float64   `json:"medianMs"`
			P95Ms    float64   `json:"p95Ms"`
			MaxMs    float64   `json:"maxMs"`
			Samples  []float64 `json:"samplesMs"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != len(rows) {
		t.Fatalf("%d items, want %d", len(res.Items), len(rows))
	}
	for i, it := range res.Items {
		if !strings.HasPrefix(it.Name, rows[i]) || len(it.Samples) != 2 || it.MedianMs <= 0 || it.MaxMs < it.P95Ms {
			t.Errorf("item %d: %+v", i, it)
		}
	}
}
