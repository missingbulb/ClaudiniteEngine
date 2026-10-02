package parity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The tasks face: the task runner's pure decision cores (the contract, the
// precondition engine, the merge policy, the work item's grammar, the
// outcome ceiling) asked one fixture at a time. A fixture is
// testdata/tasks/<kind>/<name>.json holding {"input": …, "expect": …};
// the Node engine answers it through testdata/shims/tasks.mjs over the
// frozen shelf, cn through `cn tasks <kind> --world <input>`, and both
// answers must equal expect. CLAUDINITE_PARITY_RECORD=1 writes the Node
// answer into expect: a fixture is written against the Node engine first.

// TaskFixture is one tasks-face fixture.
type TaskFixture struct {
	Kind, Name, Path string
	Input            json.RawMessage
	Expect           json.RawMessage
}

// LoadTaskFixtures reads every fixture under root, sorted by kind and name.
func LoadTaskFixtures(root string) ([]TaskFixture, error) {
	kinds, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []TaskFixture
	for _, k := range kinds {
		if !k.IsDir() {
			continue
		}
		files, err := filepath.Glob(filepath.Join(root, k.Name(), "*.json"))
		if err != nil {
			return nil, err
		}
		sort.Strings(files)
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil {
				return nil, err
			}
			var body struct {
				Input  json.RawMessage `json:"input"`
				Expect json.RawMessage `json:"expect"`
			}
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&body); err != nil {
				return nil, fmt.Errorf("%s: %w", f, err)
			}
			if len(body.Input) == 0 {
				return nil, fmt.Errorf("%s: no input", f)
			}
			out = append(out, TaskFixture{Kind: k.Name(), Name: strings.TrimSuffix(filepath.Base(f), ".json"), Path: f, Input: body.Input, Expect: body.Expect})
		}
	}
	return out, nil
}

// askTasks runs one engine over one fixture and returns its answer parsed.
func askTasks(e Engine, f TaskFixture, scratch string) (any, error) {
	in := filepath.Join(scratch, f.Kind+"-"+f.Name+".json")
	if err := os.WriteFile(in, f.Input, 0o644); err != nil {
		return nil, err
	}
	var stdout, stderr string
	var code int
	var err error
	switch e := e.(type) {
	case Node:
		shim, aerr := filepath.Abs(filepath.Join("testdata", "shims", "tasks.mjs"))
		if aerr != nil {
			return nil, aerr
		}
		stdout, stderr, code, err = run(scratch, []string{nodeEnv + "=" + e.Root}, string(f.Input), "node", shim, f.Kind)
	case Cn:
		stdout, stderr, code, err = run(scratch, e.env(scratch), "", e.Binary, "tasks", f.Kind, "--world", in)
	default:
		return nil, fmt.Errorf("no tasks face on %s", e.Name())
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

func TestParityTasks(t *testing.T) {
	fixtures, err := LoadTaskFixtures(filepath.Join("testdata", "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no tasks fixtures under testdata/tasks")
	}
	es := engines(t)
	recording := os.Getenv(recordEnv) == "1"
	scratch := t.TempDir()
	byKind := map[string]int{}
	for _, f := range fixtures {
		byKind[f.Kind]++
		t.Run(f.Kind+"/"+f.Name, func(t *testing.T) {
			for _, e := range es {
				got, err := askTasks(e, f, scratch)
				if err != nil {
					t.Fatalf("%s: %v", e.Name(), err)
				}
				if recording && e.Name() == "node" {
					if err := recordTasks(f, got); err != nil {
						t.Fatal(err)
					}
					continue
				}
				if len(f.Expect) == 0 {
					t.Fatalf("%s has no expect; record it against the Node engine first (%s=1)", f.Path, recordEnv)
				}
				var want any
				if err := json.Unmarshal(f.Expect, &want); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s disagrees with expect:\n%s", e.Name(), strings.Join(jsonDiff("$", got, want, 30), "\n"))
				}
			}
		})
	}
	kinds := make([]string, 0, len(byKind))
	for k, n := range byKind {
		kinds = append(kinds, fmt.Sprintf("%s %d", k, n))
	}
	sort.Strings(kinds)
	t.Logf("tasks fixtures: %s", strings.Join(kinds, ", "))
}

func recordTasks(f TaskFixture, answer any) error {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]json.RawMessage{"input": f.Input, "expect": mustJSON(answer)}); err != nil {
		return err
	}
	return os.WriteFile(f.Path, b.Bytes(), 0o644)
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// jsonDiff names up to max paths where got and want differ.
func jsonDiff(at string, got, want any, max int) []string {
	if reflect.DeepEqual(got, want) {
		return nil
	}
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			break
		}
		keys := map[string]bool{}
		for k := range w {
			keys[k] = true
		}
		for k := range g {
			keys[k] = true
		}
		names := make([]string, 0, len(keys))
		for k := range keys {
			names = append(names, k)
		}
		sort.Strings(names)
		var out []string
		for _, k := range names {
			out = append(out, jsonDiff(at+"."+k, g[k], w[k], max-len(out))...)
			if len(out) >= max {
				return out[:max]
			}
		}
		return out
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			break
		}
		var out []string
		for i := range w {
			out = append(out, jsonDiff(fmt.Sprintf("%s[%d]", at, i), g[i], w[i], max-len(out))...)
			if len(out) >= max {
				return out[:max]
			}
		}
		return out
	}
	gb, _ := json.Marshal(got)
	wb, _ := json.Marshal(want)
	return []string{fmt.Sprintf("%s: got %s want %s", at, gb, wb)}
}
