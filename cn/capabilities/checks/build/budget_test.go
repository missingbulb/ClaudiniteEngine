package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/packset"
)

// The checks binary compiles inside a session, between its first hook and
// the hooks that need it, so the compile is held to a budget a session
// does not notice: a repo with 50 checks over the standard library's
// heavier packages builds within coldBudget on a machine that has never
// compiled Go, and within warmBudget once Go's compile cache holds the
// standard library (a second session on the machine, or a setup script
// that pre-built it).
const (
	budgetPacks    = 10
	checksPerPack  = 5
	coldBudget     = 5 * time.Second
	warmBudget     = 3 * time.Second
	budgetEnv      = "CN_BUILD_BUDGET"
	budgetSkipNote = "set " + budgetEnv + "=1 to hold the checks build to its budgets: it times a cold compile, so the fast check runs it alone"
)

// budgetCheck is one generated check, written the way the shelf's checks
// are: a pattern, a JSON shape and a walk over the tree's files.
func budgetCheck(pack, n int) string {
	return fmt.Sprintf(`package checks

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"claudinite.com/checksdk"
)

var pattern%[2]d = regexp.MustCompile(`+"`"+`(?m)^\s*(acme-%[1]d-%[2]d)\s*[:=]\s*(\S+)\s*$`+"`"+`)

type shape%[2]d struct {
	ID    string            `+"`json:\"id\"`"+`
	Order int               `+"`json:\"order\"`"+`
	Tags  []string          `+"`json:\"tags\"`"+`
	Meta  map[string]string `+"`json:\"meta\"`"+`
}

func init() {
	checksdk.Register(checksdk.Check{
		ID:     "acme-check-%[1]d-%[2]d",
		Tags:   []string{"world"},
		OnFail: "advise",
		Why:    "a generated check the build budget compiles",
		Run:    run%[2]d,
	})
}

func run%[2]d(repo checksdk.Repo) []checksdk.Finding {
	var out []checksdk.Finding
	files := repo.Files()
	sort.Strings(files)
	for _, f := range files {
		switch path.Ext(f) {
		case ".json":
			body, ok := repo.Read(f)
			if !ok {
				continue
			}
			var s shape%[2]d
			if err := json.Unmarshal([]byte(body), &s); err != nil || s.ID == "" {
				continue
			}
			if !strings.HasPrefix(s.ID, "acme-%[1]d") {
				out = append(out, checksdk.Finding{Path: f, Sentence: fmt.Sprintf("id %%q is outside pack %[1]d", s.ID)})
			}
		case ".md", ".txt":
			body, ok := repo.Read(f)
			if !ok {
				continue
			}
			sc := bufio.NewScanner(bytes.NewReader([]byte(body)))
			for line := 1; sc.Scan(); line++ {
				m := pattern%[2]d.FindStringSubmatch(sc.Text())
				if m == nil {
					continue
				}
				if _, err := strconv.Atoi(m[2]); err != nil && strings.IndexFunc(m[2], unicode.IsUpper) < 0 {
					out = append(out, checksdk.Finding{Path: f, Line: line, Sentence: m[1] + " takes a number"})
				}
			}
		}
	}
	return out
}
`, pack, n)
}

// budgetRepo is a member declaring budgetPacks canon packs of
// checksPerPack generated checks each.
func budgetRepo(t *testing.T) (string, []string) {
	t.Helper()
	repo := t.TempDir()
	var ids []string
	for p := 0; p < budgetPacks; p++ {
		id := fmt.Sprintf("acme-pack-%d", p)
		ids = append(ids, id)
		dir := filepath.Join(repo, ".claudinite/shared/packs", id, "checks")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for n := 0; n < checksPerPack; n++ {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("check_%d.go", n)), []byte(budgetCheck(p, n)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return repo, ids
}

// timedBuild builds the budget repo's binary under a fresh cache root,
// with Go's compile cache at gocache, and returns the wall time.
func timedBuild(t *testing.T, repo string, ids []string, gocache, engine string) time.Duration {
	t.Helper()
	t.Setenv("GOCACHE", gocache)
	c := Config{CacheRoot: filepath.Join(t.TempDir(), "claudinite"), Engine: engine, SDK: sdkSource(t)}
	var packs []packset.Pack
	for _, id := range ids {
		packs = append(packs, packset.Pack{ID: id, Kind: packset.Canon, Dir: packset.Tree(repo, id)})
	}
	srcs, err := Sources(packs)
	if err != nil {
		t.Fatal(err)
	}
	key := Key(c, srcs)
	start := time.Now()
	if err := Build(c, key, srcs); err != nil {
		log, _ := os.ReadFile(filepath.Join(c.Dir(key), "build.log"))
		t.Fatalf("%v\n%s", err, log)
	}
	took := time.Since(start)
	j, err := Judges(c, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(j) != 0 {
		t.Errorf("judges %v: the generated checks are all world checks", j)
	}
	return took
}

func TestChecksBuildBudget(t *testing.T) {
	if os.Getenv(budgetEnv) == "" {
		t.Skip(budgetSkipNote)
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatal("no go on PATH")
	}
	repo, ids := budgetRepo(t)
	if n := strings.Count(strings.Join(ids, ","), ",") + 1; n*checksPerPack != 50 {
		t.Fatalf("the scenario holds %d checks", n*checksPerPack)
	}
	gocache := filepath.Join(t.TempDir(), "go-build")
	cold := timedBuild(t, repo, ids, gocache, "1.1.0")
	// A new engine is a new key and a new SDK unpack: everything but the
	// standard library compiles again.
	warm := timedBuild(t, repo, ids, gocache, "1.1.1")
	t.Logf("50 checks: cold %v (budget %v), warm %v (budget %v)", cold.Round(time.Millisecond), coldBudget, warm.Round(time.Millisecond), warmBudget)
	if cold > coldBudget {
		t.Errorf("a cold build of 50 checks took %v, over its %v budget", cold.Round(time.Millisecond), coldBudget)
	}
	if warm > warmBudget {
		t.Errorf("a build of 50 checks with the standard library cached took %v, over its %v budget", warm.Round(time.Millisecond), warmBudget)
	}
}
