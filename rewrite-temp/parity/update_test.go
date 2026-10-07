package parity

import (
	"bytes"
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

// The update face: the update flow's decision cores (the pack plan, the
// migration gap, the delivery decision, the apply stage, the terminal,
// the converge scope, the pull request text) asked one fixture at a time.
// A fixture is testdata/update/<core>/<name>.json holding {"input": …,
// "expect": …}: the Node engine answers it through testdata/shims/
// update.mjs over packs/claudinite-lifecycle at the frozen commit, cn
// through `cndecide update decide <core> --world <input>`. CLAUDINITE_PARITY_
// RECORD=1 writes the Node answer into expect.
//
// Where cn decides otherwise on purpose, the fixture names the design
// record row that says why, "divergence": "record-<row>", and carries cn's
// answer as "cn"; expect stays the Node engine's.

// UpdateFixture is one update-face fixture.
type UpdateFixture struct {
	Core, Name, Path string
	Input            json.RawMessage
	Expect           json.RawMessage
	Divergence       string
	Cn               json.RawMessage
}

var divergenceForm = regexp.MustCompile(`^record-\d+$`)

// LoadUpdateFixtures reads every fixture under root, sorted by core and
// name.
func LoadUpdateFixtures(root string) ([]UpdateFixture, error) {
	cores, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []UpdateFixture
	for _, c := range cores {
		if !c.IsDir() {
			continue
		}
		files, err := filepath.Glob(filepath.Join(root, c.Name(), "*.json"))
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
				Input      json.RawMessage `json:"input"`
				Expect     json.RawMessage `json:"expect"`
				Divergence string          `json:"divergence"`
				Cn         json.RawMessage `json:"cn"`
			}
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&body); err != nil {
				return nil, fmt.Errorf("%s: %w", f, err)
			}
			if len(body.Input) == 0 {
				return nil, fmt.Errorf("%s: no input", f)
			}
			if (body.Divergence == "") != (len(body.Cn) == 0) {
				return nil, fmt.Errorf("%s: a divergence names its record row and carries cn's answer, both or neither", f)
			}
			if body.Divergence != "" && !divergenceForm.MatchString(body.Divergence) {
				return nil, fmt.Errorf("%s: divergence %q is not record-<row>", f, body.Divergence)
			}
			out = append(out, UpdateFixture{Core: c.Name(), Name: strings.TrimSuffix(filepath.Base(f), ".json"), Path: f,
				Input: body.Input, Expect: body.Expect, Divergence: body.Divergence, Cn: body.Cn})
		}
	}
	return out, nil
}

func askUpdate(e Engine, f UpdateFixture, scratch string) (any, error) {
	in := filepath.Join(scratch, "update-"+f.Core+"-"+f.Name+".json")
	if err := os.WriteFile(in, f.Input, 0o644); err != nil {
		return nil, err
	}
	var stdout, stderr string
	var code int
	var err error
	switch e := e.(type) {
	case Node:
		shim, aerr := filepath.Abs(filepath.Join("testdata", "shims", "update.mjs"))
		if aerr != nil {
			return nil, aerr
		}
		stdout, stderr, code, err = run(scratch, []string{nodeEnv + "=" + e.Root}, string(f.Input), "node", shim, f.Core)
	case Cn:
		stdout, stderr, code, err = run(scratch, e.env(scratch), "", e.Decide, "update", "decide", f.Core, "--world", in)
	default:
		return nil, fmt.Errorf("no update face on %s", e.Name())
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

func TestParityUpdate(t *testing.T) {
	fixtures, err := LoadUpdateFixtures(filepath.Join("testdata", "update"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no update fixtures under testdata/update")
	}
	es := engines(t)
	recording := os.Getenv(recordEnv) == "1"
	scratch := t.TempDir()
	byCore := map[string]int{}
	for _, f := range fixtures {
		byCore[f.Core]++
		t.Run(f.Core+"/"+f.Name, func(t *testing.T) {
			for _, e := range es {
				got, err := askUpdate(e, f, scratch)
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
					t.Errorf("%s disagrees with %s:\n%s", e.Name(), map[bool]string{true: "cn (" + f.Divergence + ")", false: "expect"}[e.Name() == "cn" && f.Divergence != ""],
						strings.Join(jsonDiff("$", got, want, 30), "\n"))
				}
				if e.Name() == "cn" && f.Divergence != "" {
					var node any
					if err := json.Unmarshal(f.Expect, &node); err == nil && reflect.DeepEqual(got, node) {
						t.Errorf("%s is marked %s but cn now gives the Node answer: drop the divergence", f.Path, f.Divergence)
					}
				}
			}
		})
	}
	cores := make([]string, 0, len(byCore))
	for k, n := range byCore {
		cores = append(cores, fmt.Sprintf("%s %d", k, n))
	}
	sort.Strings(cores)
	t.Logf("update fixtures: %s", strings.Join(cores, ", "))
}

func recordUpdate(f UpdateFixture, answer any) error {
	body := map[string]json.RawMessage{"input": f.Input, "expect": mustJSON(answer)}
	if f.Divergence != "" {
		body["divergence"] = mustJSON(f.Divergence)
		body["cn"] = f.Cn
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(orderedFixture(body)); err != nil {
		return err
	}
	return os.WriteFile(f.Path, b.Bytes(), 0o644)
}

// orderedFixture writes a fixture's keys in reading order.
type orderedFixture map[string]json.RawMessage

func (o orderedFixture) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("{")
	first := true
	for _, k := range []string{"input", "expect", "divergence", "cn"} {
		v, ok := o[k]
		if !ok {
			continue
		}
		if !first {
			b.WriteString(",")
		}
		first = false
		fmt.Fprintf(&b, "%q:%s", k, v)
	}
	b.WriteString("}")
	return b.Bytes(), nil
}

// The parity README counts the divergences; the count must be the
// fixtures' own.
func TestUpdateDivergencesAreCounted(t *testing.T) {
	fixtures, err := LoadUpdateFixtures(filepath.Join("testdata", "update"))
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
	m := regexp.MustCompile(`(?m)^Update face divergences: (\d+) of (\d+) fixtures\.$`).FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatal("parity/README.md has no line `Update face divergences: <n> of <total> fixtures.`")
	}
	if want := fmt.Sprintf("%d of %d", n, len(fixtures)); m[1]+" of "+m[2] != want {
		t.Errorf("parity/README.md counts %s of %s divergences, the fixtures mark %s", m[1], m[2], want)
	}
}
