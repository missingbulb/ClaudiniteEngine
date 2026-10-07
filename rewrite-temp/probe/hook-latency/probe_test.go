package hooklatency

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/version"
)

var rows = []string{
	"derivation from the tree",
	"pre-tool-use Read, no declaration (no transcript)",
	"pre-tool-use Bash held, git commit (5 MB transcript)",
	"pre-tool-use Edit under a scoped path (no transcript)",
	"post-tool-use Bash result (no transcript)",
	"user-prompt-submit prompt (5 MB transcript)",
}

// The probe needs the frozen Node engine's checkout for its packs.
func TestProbeWritesResults(t *testing.T) {
	node := os.Getenv("CLAUDINITE_NODE_ENGINE")
	if node == "" || testing.Short() {
		t.Skip("CLAUDINITE_NODE_ENGINE is not set")
	}
	out := t.TempDir()
	cmd := exec.Command("sh", "rewrite-temp/probe/hook-latency/run.sh", "--node", node, "--runs", "2", "--out", out)
	cmd.Dir = "../../.."
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run.sh: %v\n%s", err, o)
	}
	md, err := os.ReadFile(filepath.Join(out, version.Platform()+"-"+time.Now().UTC().Format("2006-01-02")+".md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "# Hook latency (") {
		t.Errorf("no title:\n%s", md)
	}
	for _, r := range rows {
		if !strings.Contains(string(md), "| "+r) {
			t.Errorf("markdown lacks the %q row:\n%s", r, md)
		}
	}
}
