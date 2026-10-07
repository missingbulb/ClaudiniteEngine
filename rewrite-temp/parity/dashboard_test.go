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

// The dashboard face's cores, asked one fixture at a time in the update
// face's format, testdata/dashboard/<core>/<name>.json:
//
//	descriptor   {pack, text}: the Node engine's parseDescriptor and the
//	             descriptor-usable findings over that one file, cn's `cn
//	             dashboard descriptor --json` over it written at
//	             packs/<pack>/dashboard.json (the file field dropped)
//	usable       {files}: the descriptor-usable rule over a tree, cn's
//	             `cndecide dashboard decide usable --world`
//	flat-member  {files}: no Node answer, since no Node engine writes the
//	             member file; cn writes it with `cn tasks flat --write` over
//	             the tree, and expect is the file written by hand once
//
// The Node half is testdata/shims/dashboard.mjs over
// packs/claudinite-dashboard at the frozen commit;
// CLAUDINITE_PARITY_RECORD=1 writes its answer into expect.

// cnOnlyDashboard are the cores no Node engine answers.
var cnOnlyDashboard = map[string]bool{"flat-member": true}

func askDashboard(e Engine, f UpdateFixture, scratch string) (any, error) {
	dir := filepath.Join(scratch, "dashboard-"+f.Core+"-"+f.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	in := filepath.Join(dir, "world.json")
	if err := os.WriteFile(in, f.Input, 0o644); err != nil {
		return nil, err
	}
	var args []string
	strip := ""
	switch e := e.(type) {
	case Node:
		shim, err := filepath.Abs(filepath.Join("testdata", "shims", "dashboard.mjs"))
		if err != nil {
			return nil, err
		}
		return answer(e, dir, []string{nodeEnv + "=" + e.Root}, string(f.Input), "node", shim, f.Core)
	case Cn:
		switch f.Core {
		case "descriptor":
			var x struct{ Pack, Text string }
			if err := json.Unmarshal(f.Input, &x); err != nil {
				return nil, err
			}
			file := filepath.Join(dir, "packs", x.Pack, "dashboard.json")
			if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(file, []byte(x.Text), 0o644); err != nil {
				return nil, err
			}
			args, strip = []string{"dashboard", "descriptor", "--json", file}, "file"
		case "usable":
			args = []string{"dashboard", "decide", "usable", "--world", in}
		case "flat-member":
			return flatMember(e, dir, f.Input)
		default:
			return nil, fmt.Errorf("no dashboard core %q", f.Core)
		}
		bin := e.Binary
		if f.Core == "usable" {
			bin = e.Decide
		}
		v, err := answer(e, dir, e.env(dir), "", bin, args...)
		if err != nil || strip == "" {
			return v, err
		}
		list, ok := v.([]any)
		if !ok || len(list) != 1 {
			return nil, fmt.Errorf("cn dashboard descriptor printed %v, not one verdict", v)
		}
		one, _ := list[0].(map[string]any)
		delete(one, strip)
		return one, nil
	}
	return nil, fmt.Errorf("no dashboard face on %s", e.Name())
}

// answer runs one engine command and reads the JSON it prints; a
// non-zero exit is the command's verdict, not a failure, when it printed
// JSON (cn dashboard descriptor exits 1 on any problem).
func answer(e Engine, dir string, env []string, stdin string, name string, args ...string) (any, error) {
	stdout, stderr, code, err := run(dir, env, stdin, name, args...)
	if err != nil {
		return nil, err
	}
	var v any
	if jerr := json.Unmarshal([]byte(stdout), &v); jerr != nil {
		return nil, fmt.Errorf("%s exited %d and printed no JSON answer: %v\n%s\n%s", e.Name(), code, jerr, stdout, stderr)
	}
	return v, nil
}

// flatMember lays the fixture's files out as a member, runs `cn tasks
// flat --write` over it and reads the member file it wrote.
func flatMember(e Cn, dir string, input json.RawMessage) (any, error) {
	var x struct{ Files map[string]string }
	if err := json.Unmarshal(input, &x); err != nil {
		return nil, err
	}
	repo := filepath.Join(dir, "repo")
	for rel, body := range x.Files {
		p := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			return nil, err
		}
	}
	_, stderr, code, err := run(dir, e.env(dir), "", e.Binary, "tasks", "flat", "--write", "--repo", repo)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("cn tasks flat --write exited %d: %s", code, strings.TrimSpace(stderr))
	}
	raw, err := os.ReadFile(filepath.Join(repo, ".claudinite", "cache", "member.GENERATED.json"))
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	if !strings.HasSuffix(string(raw), "}\n") || strings.HasSuffix(string(raw), "\n\n") {
		return nil, fmt.Errorf("the member file does not end in one newline:\n%s", raw)
	}
	return v, nil
}

func TestParityDashboard(t *testing.T) {
	fixtures, err := LoadUpdateFixtures(filepath.Join("testdata", "dashboard"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no dashboard fixtures under testdata/dashboard")
	}
	es := engines(t)
	recording := os.Getenv(recordEnv) == "1"
	scratch := t.TempDir()
	byCore := map[string]int{}
	for _, f := range fixtures {
		byCore[f.Core]++
		t.Run(f.Core+"/"+f.Name, func(t *testing.T) {
			for _, e := range es {
				if e.Name() == "node" && cnOnlyDashboard[f.Core] {
					continue
				}
				got, err := askDashboard(e, f, scratch)
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
	t.Logf("dashboard fixtures: %s", strings.Join(cores, ", "))
}

// The parity README counts the dashboard face's divergences.
func TestDashboardDivergencesAreCounted(t *testing.T) {
	fixtures, err := LoadUpdateFixtures(filepath.Join("testdata", "dashboard"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, f := range fixtures {
		if f.Divergence != "" {
			n++
		}
	}
	countedIn(t, "Dashboard", n, len(fixtures))
}

// countedIn holds parity/README.md's `<Face> face divergences: <n> of
// <total> fixtures.` line to the fixtures.
func countedIn(t *testing.T, face string, n, total int) {
	t.Helper()
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^` + face + ` face divergences: (\d+) of (\d+) fixtures\.$`).FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatalf("parity/README.md has no line `%s face divergences: <n> of <total> fixtures.`", face)
	}
	if want := fmt.Sprintf("%d of %d", n, total); m[1]+" of "+m[2] != want {
		t.Errorf("parity/README.md counts %s of %s %s divergences, the fixtures mark %s", m[1], m[2], strings.ToLower(face), want)
	}
}
