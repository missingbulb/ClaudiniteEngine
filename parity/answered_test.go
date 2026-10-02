package parity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The verify-answers face: each Node world rule deferred.txt once sent to
// "verify" is asked over a Node member, and cn verify over the cn member
// standing for the same situation. A case is testdata/answered/<rule>/
// <case>.json holding:
//
//	nodeFiles   the Node member's files, {path: text}
//	cnShape     a fixture under lifecycle/verify/testdata/shapes, the cn
//	            member's base
//	cnFiles     files laid over that base, {path: text}, null deleting one
//	answeredBy  the verify rule that answers the Node rule
//	nodeFires   whether the Node rule fires, written by CLAUDINITE_PARITY_
//	            RECORD=1 and asserted otherwise
//	divergence  "record-<row>" where cn answers no finding on purpose
//
// cn must report a finding from answeredBy exactly when the Node rule fires,
// unless the case names a divergence, where it reports none.

// AnsweredCase is one verify-answers case.
type AnsweredCase struct {
	Rule, Name, Path string             `json:"-"`
	NodeFiles        map[string]string  `json:"nodeFiles"`
	CnShape          string             `json:"cnShape"`
	CnFiles          map[string]*string `json:"cnFiles"`
	AnsweredBy       string             `json:"answeredBy"`
	NodeFires        *bool              `json:"nodeFires"`
	Divergence       string             `json:"divergence,omitempty"`
}

// LoadAnswered reads every case under root, sorted by rule and name.
func LoadAnswered(root string) ([]AnsweredCase, error) {
	files, err := filepath.Glob(filepath.Join(root, "*", "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var out []AnsweredCase
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var c AnsweredCase
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		if c.CnShape == "" || c.AnsweredBy == "" || len(c.NodeFiles) == 0 {
			return nil, fmt.Errorf("%s: a case names nodeFiles, cnShape and answeredBy", f)
		}
		if c.Divergence != "" && !divergenceForm.MatchString(c.Divergence) {
			return nil, fmt.Errorf("%s: divergence %q is not record-<row>", f, c.Divergence)
		}
		c.Rule, c.Name, c.Path = filepath.Base(filepath.Dir(f)), strings.TrimSuffix(filepath.Base(f), ".json"), f
		out = append(out, c)
	}
	return out, nil
}

// cnMember lays the case's cn member out under dir.
func (c AnsweredCase) cnMember(dir string) error {
	src := filepath.Join("..", "lifecycle", "verify", "testdata", "shapes", c.CnShape)
	if err := copyDir(src, dir); err != nil {
		return err
	}
	for p, body := range c.CnFiles {
		abs := filepath.Join(dir, filepath.FromSlash(p))
		if body == nil {
			if err := os.Remove(abs); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(abs, []byte(*body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), raw, info.Mode().Perm())
	})
}

func TestParityVerifyAnswers(t *testing.T) {
	cases, err := LoadAnswered(filepath.Join("testdata", "answered"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no cases under testdata/answered")
	}
	deferred, err := Deferred()
	if err != nil {
		t.Fatal(err)
	}
	covered := map[string]bool{}
	recording := os.Getenv(recordEnv) == "1"
	for _, e := range engines(t) {
		for _, c := range cases {
			covered[c.Rule] = true
			if deferred[c.Rule] != "" {
				t.Errorf("%s is answered by %s, yet deferred.txt still defers it", c.Rule, c.AnsweredBy)
			}
			t.Run(e.Name()+"/"+c.Rule+"/"+c.Name, func(t *testing.T) {
				switch e := e.(type) {
				case Node:
					fires := askNodeRule(t, e, c)
					if recording {
						c.NodeFires = &fires
						if err := recordAnswered(c); err != nil {
							t.Fatal(err)
						}
						return
					}
					if c.NodeFires == nil {
						t.Fatalf("%s has no nodeFires; record it against the Node engine first (%s=1)", c.Path, recordEnv)
					}
					if fires != *c.NodeFires {
						t.Errorf("the Node rule fires=%v, the case records %v", fires, *c.NodeFires)
					}
				case Cn:
					if c.NodeFires == nil {
						t.Fatalf("%s has no nodeFires", c.Path)
					}
					dir := t.TempDir()
					if err := c.cnMember(dir); err != nil {
						t.Fatal(err)
					}
					out, stderr, code, err := run(dir, e.env(dir), "", e.Binary, "verify", "--repo", dir)
					if err != nil || code > 1 {
						t.Fatalf("cn verify: %v exit %d: %s", err, code, stderr)
					}
					fired := false
					for _, l := range strings.Split(out, "\n") {
						f := strings.Fields(l)
						fired = fired || (len(f) > 1 && (f[0] == "break" || f[0] == "deprecation") && f[1] == c.AnsweredBy)
					}
					want := *c.NodeFires && c.Divergence == ""
					if fired != want {
						t.Errorf("cn verify reports %s: %v, want %v (Node fires: %v, divergence %q):\n%s", c.AnsweredBy, fired, want, *c.NodeFires, c.Divergence, out)
					}
					if c.Divergence != "" && !*c.NodeFires {
						t.Errorf("%s names %s but the Node rule does not fire: drop the divergence", c.Path, c.Divergence)
					}
				}
			})
		}
	}
	for _, r := range []string{"conformance-workflow", "conformance-work-scope", "legacy-shape-in-use", "rules-index-current", "skills-index-current"} {
		if !covered[r] {
			t.Errorf("no case answers %s", r)
		}
	}
}

func askNodeRule(t *testing.T, e Node, c AnsweredCase) bool {
	t.Helper()
	shim, err := filepath.Abs(filepath.Join("testdata", "shims", "answered.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	in, _ := json.Marshal(c.NodeFiles)
	out, stderr, code, err := run(t.TempDir(), []string{nodeEnv + "=" + e.Root}, string(in), "node", shim, c.Rule)
	if err != nil || code != 0 {
		t.Fatalf("node %s: %v exit %d: %s", c.Rule, err, code, stderr)
	}
	var files []any
	if err := json.Unmarshal([]byte(out), &files); err != nil {
		t.Fatalf("node %s printed %q: %v", c.Rule, out, err)
	}
	return len(files) > 0
}

func recordAnswered(c AnsweredCase) error {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(c); err != nil {
		return err
	}
	return os.WriteFile(c.Path, b.Bytes(), 0o644)
}
