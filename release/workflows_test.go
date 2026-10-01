package release

import (
	"os"
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
