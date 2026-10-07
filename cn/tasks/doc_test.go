package tasks

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

var (
	citation = regexp.MustCompile(`\b([a-z]+)\.(Test[A-Za-z0-9_]+)`)
	testFunc = regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
)

// scenarioSuites are the files every one of whose tests a claim cites.
var scenarioSuites = []string{"schedule/run_test.go", "execute/loop_test.go"}

func testsIn(t *testing.T, files ...string) map[string]bool {
	out := map[string]bool{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range testFunc.FindAllStringSubmatch(string(raw), -1) {
			out[m[1]] = true
		}
	}
	return out
}

// The claims in doc.go cite tests that exist, and the scenario suites
// carry no test no claim cites.
func TestTheClaimsCiteRealTestsAndCoverTheScenarios(t *testing.T) {
	raw, err := os.ReadFile("doc.go")
	if err != nil {
		t.Fatal(err)
	}
	cited := map[string]bool{}
	var missing []string
	for _, m := range citation.FindAllStringSubmatch(string(raw), -1) {
		files, _ := filepath.Glob(filepath.Join(m[1], "*_test.go"))
		if len(files) == 0 || !testsIn(t, files...)[m[2]] {
			missing = append(missing, m[0])
		}
		cited[m[1]+"/"+m[2]] = true
	}
	var uncited []string
	for _, suite := range scenarioSuites {
		for name := range testsIn(t, suite) {
			if !cited[filepath.Dir(suite)+"/"+name] {
				uncited = append(uncited, suite+": "+name)
			}
		}
	}
	sort.Strings(uncited)
	if len(cited) == 0 {
		t.Fatal("doc.go cites no test")
	}
	if len(missing) > 0 {
		t.Errorf("doc.go cites tests that do not exist: %v", missing)
	}
	if len(uncited) > 0 {
		t.Errorf("no claim cites: %v", uncited)
	}
}
