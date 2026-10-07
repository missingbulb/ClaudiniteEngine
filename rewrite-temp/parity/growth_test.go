package parity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The growth face's cores: the capture's line reader, bundle, slice,
// redaction list, scrub and log names, its transcript discovery, and the
// retention prune's plan, asked one fixture at a time. A fixture is
// testdata/growth/<core>/<name>.json in the update face's format: the
// Node engine answers it through testdata/shims/growth.mjs over
// packs/claudinite-growth at the frozen commit, cn through `cn growth
// decide <core> --world <input>`. CLAUDINITE_PARITY_RECORD=1 writes the
// Node answer into expect.

func askGrowth(e Engine, f UpdateFixture, scratch string) (any, error) {
	in := filepath.Join(scratch, "growth-"+f.Core+"-"+f.Name+".json")
	if err := os.WriteFile(in, f.Input, 0o644); err != nil {
		return nil, err
	}
	var stdout, stderr string
	var code int
	var err error
	switch e := e.(type) {
	case Node:
		shim, aerr := filepath.Abs(filepath.Join("testdata", "shims", "growth.mjs"))
		if aerr != nil {
			return nil, aerr
		}
		stdout, stderr, code, err = run(scratch, []string{nodeEnv + "=" + e.Root}, string(f.Input), "node", shim, f.Core)
	case Cn:
		stdout, stderr, code, err = run(scratch, e.env(scratch), "", e.Binary, "growth", "decide", f.Core, "--world", in)
	default:
		return nil, fmt.Errorf("no growth face on %s", e.Name())
	}
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("%s exited %d: %s", e.Name(), code, strings.TrimSpace(stderr))
	}
	var v any
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		return nil, fmt.Errorf("%s printed no JSON answer: %v\n%s\n%s", e.Name(), err, stdout, stderr)
	}
	return v, nil
}

func TestParityGrowth(t *testing.T) {
	fixtures, err := LoadUpdateFixtures(filepath.Join("testdata", "growth"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no growth fixtures under testdata/growth")
	}
	es := engines(t)
	recording := os.Getenv(recordEnv) == "1"
	scratch := t.TempDir()
	byCore := map[string]int{}
	for _, f := range fixtures {
		byCore[f.Core]++
		t.Run(f.Core+"/"+f.Name, func(t *testing.T) {
			for _, e := range es {
				got, err := askGrowth(e, f, scratch)
				if err != nil {
					t.Fatalf("%s: %v", e.Name(), err)
				}
				if recording && e.Name() == "node" {
					if err := recordUpdate(f, got); err != nil {
						t.Fatal(err)
					}
					continue
				}
				if len(f.Expect) == 0 {
					t.Fatalf("%s has no expect; record it against the Node engine first (%s=1)", f.Path, recordEnv)
				}
				wantRaw := f.Expect
				if e.Name() == "cn" && f.Divergence != "" {
					wantRaw = f.Cn
				}
				var want any
				if err := json.Unmarshal(wantRaw, &want); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s disagrees:\n%s", e.Name(), strings.Join(jsonDiff("$", got, want, 30), "\n"))
				}
			}
		})
	}
	cores := make([]string, 0, len(byCore))
	for k, n := range byCore {
		cores = append(cores, fmt.Sprintf("%s %d", k, n))
	}
	sort.Strings(cores)
	t.Logf("growth fixtures: %s", strings.Join(cores, ", "))
}

// The parity README counts the growth face's divergences.
func TestGrowthDivergencesAreCounted(t *testing.T) {
	fixtures, err := LoadUpdateFixtures(filepath.Join("testdata", "growth"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, f := range fixtures {
		if f.Divergence != "" {
			n++
		}
	}
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^Growth face divergences: (\d+) of (\d+) fixtures\.$`).FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatal("parity/README.md has no line `Growth face divergences: <n> of <total> fixtures.`")
	}
	if want := fmt.Sprintf("%d of %d", n, len(fixtures)); m[1]+" of "+m[2] != want {
		t.Errorf("parity/README.md counts %s of %s growth divergences, the fixtures mark %s", m[1], m[2], want)
	}
}
