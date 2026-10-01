package release

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// runBlocks returns each step's run: body in a workflow file, by indentation:
// the text after "run:" and every following line indented deeper than the
// key.
func runBlocks(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimLeft(lines[i], " ")
		key := strings.TrimPrefix(trimmed, "- ")
		if !strings.HasPrefix(key, "run:") {
			continue
		}
		indent := len(lines[i]) - len(trimmed) + len(trimmed) - len(key)
		body := []string{strings.TrimPrefix(key, "run:")}
		for i+1 < len(lines) {
			next := lines[i+1]
			if strings.TrimSpace(next) != "" && len(next)-len(strings.TrimLeft(next, " ")) <= indent {
				break
			}
			body = append(body, next)
			i++
		}
		out = append(out, strings.Join(body, "\n"))
	}
	return out
}

// An expression expanded inside a run: body is spliced into the script
// before the shell parses it; through env: it arrives as data.
func TestNoExpressionInARunBody(t *testing.T) {
	for _, wf := range []string{"../.github/workflows/release.yml", "../.github/workflows/promote.yml"} {
		blocks := runBlocks(t, wf)
		if len(blocks) == 0 {
			t.Fatalf("%s: no run: blocks found", wf)
		}
		for _, b := range blocks {
			if strings.Contains(b, "${{") {
				t.Errorf("%s: a run: body expands an expression; move it to env:\n%s", wf, b)
			}
		}
	}
}

var pinnedUses = regexp.MustCompile(`^\s*(?:- )?uses:\s*[^@\s]+@[0-9a-f]{40}(?:\s+#.*)?$`)

func TestEveryActionIsPinnedBySHA(t *testing.T) {
	files, _ := filepath.Glob("../.github/workflows/*.yml")
	if len(files) == 0 {
		t.Fatal("no workflows found")
	}
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		for i, l := range strings.Split(string(raw), "\n") {
			if strings.Contains(l, "uses:") && !pinnedUses.MatchString(l) {
				t.Errorf("%s:%d: not pinned by commit SHA: %s", f, i+1, strings.TrimSpace(l))
			}
		}
	}
}

func TestCIRunsActionlintPinnedBySHA(t *testing.T) {
	raw, err := os.ReadFile("../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`go run github\.com/rhysd/actionlint/cmd/actionlint@[0-9a-f]{40}\b`).Match(raw) {
		t.Error("ci.yml does not run actionlint at a commit SHA")
	}
	conf, err := os.ReadFile("../.github/actionlint.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^\s+- macos-15-intel$`).Match(conf) {
		t.Errorf(".github/actionlint.yaml does not declare macos-15-intel:\n%s", conf)
	}
}
