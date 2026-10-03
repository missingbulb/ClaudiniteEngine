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

var (
	stepUses   = regexp.MustCompile(`^\s*(?:- )?uses:\s*\S`)
	pinnedUses = regexp.MustCompile(`^\s*(?:- )?uses:\s*[^@\s]+@[0-9a-f]{40}(?:\s+#.*)?$`)
)

// unpinnedUse reports a step's uses: that names an action by anything but a commit SHA. A bare
// `uses:` key with no value is a workflow_dispatch input of that name, not a step.
func unpinnedUse(line string) bool {
	return stepUses.MatchString(line) && !pinnedUses.MatchString(line)
}

func TestUnpinnedUse(t *testing.T) {
	for line, want := range map[string]bool{
		"      - uses: actions/checkout@v4":                                                true,
		"      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1": false,
		"        uses: ./local-action":                                                     true,
		"      uses:":                                                                      false,
	} {
		if got := unpinnedUse(line); got != want {
			t.Errorf("unpinnedUse(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestEveryActionIsPinnedBySHA(t *testing.T) {
	files, _ := filepath.Glob("../.github/workflows/*.yml")
	if len(files) == 0 {
		t.Fatal("no workflows found")
	}
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		for i, l := range strings.Split(string(raw), "\n") {
			if unpinnedUse(l) {
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
	// The member workflow templates ship inside the binary; CI lints them
	// beside the engine's own.
	if !regexp.MustCompile(`actionlint@[0-9a-f]{40} \.github/workflows/\*\.yml lifecycle/workflows/templates/\*\.yml`).Match(raw) {
		t.Error("ci.yml's actionlint does not lint both .github/workflows and the member templates")
	}
	conf, err := os.ReadFile("../.github/actionlint.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^\s+- macos-15-intel$`).Match(conf) {
		t.Errorf(".github/actionlint.yaml does not declare macos-15-intel:\n%s", conf)
	}
}

// jobBlock is one job's text in a workflow: from "  <name>:" to the next
// job at the same indentation.
func jobBlock(t *testing.T, wf, name string) string {
	t.Helper()
	raw, err := os.ReadFile(wf)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	i := strings.Index(s, "\n  "+name+":\n")
	if i < 0 {
		return ""
	}
	rest := s[i+1:]
	if j := regexp.MustCompile(`\n  [a-z][a-z0-9-]*:\n`).FindStringIndex(rest[1:]); j != nil {
		rest = rest[:j[0]+1]
	}
	return rest
}

func TestReleaseRunsTheHopBeforeSign(t *testing.T) {
	const wf = "../.github/workflows/release.yml"
	hop := jobBlock(t, wf, "hop")
	if hop == "" {
		t.Fatal("release.yml has no hop job")
	}
	for _, want := range []string{"    needs: [build, smoke]\n", "    permissions:\n      contents: read\n", "release/hop.sh"} {
		if !strings.Contains(hop, want) {
			t.Errorf("the hop job lacks %q:\n%s", want, hop)
		}
	}
	for _, not := range []string{"secrets.", "environment:"} {
		if strings.Contains(hop, not) {
			t.Errorf("the hop job has %q", not)
		}
	}
	if !strings.Contains(jobBlock(t, wf, "sign"), "    needs: [build, smoke, hop]\n") {
		t.Error("sign does not wait for the hop")
	}
}

// The live-packs rehearsal runs in the release after smoke, holding
// promotion through a release-blocker issue, and in its own workflow on
// main and nightly.
func TestLivePacksRuns(t *testing.T) {
	const wf = "../.github/workflows/release.yml"
	job := jobBlock(t, wf, "live-packs")
	if job == "" {
		t.Fatal("release.yml has no live-packs job")
	}
	invocation := regexp.MustCompile(`(?m)^\s*- run: release/rehearse\.sh --mode live-packs\b`)
	for _, want := range []*regexp.Regexp{
		regexp.MustCompile(`(?m)^    needs: \[version, build, smoke\]$`),
		invocation,
		regexp.MustCompile(`(?m)^\s*go run \./release/pipeline blocker-issue --gate live-packs\b`),
		regexp.MustCompile(`(?m)^\s*gh issue create .*--label release-blocker`),
	} {
		if !want.MatchString(job) {
			t.Errorf("the live-packs job lacks %s:\n%s", want, job)
		}
	}
	for _, not := range []string{"secrets.", "environment:"} {
		if strings.Contains(job, not) {
			t.Errorf("the live-packs job has %q", not)
		}
	}
	own := jobBlock(t, "../.github/workflows/live-packs.yml", "live-packs")
	if !invocation.MatchString(own) {
		t.Errorf("live-packs.yml does not run the mode:\n%s", own)
	}
	raw, err := os.ReadFile("../.github/workflows/live-packs.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, trigger := range []string{"\n  push:\n    branches: [main]\n", "\n  schedule:\n"} {
		if !strings.Contains(string(raw), trigger) {
			t.Errorf("live-packs.yml lacks the trigger %q", trigger)
		}
	}
}
