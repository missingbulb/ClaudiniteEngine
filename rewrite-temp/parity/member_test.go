package parity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// The member face: testdata/member/flat-member/<name>.json, {files}. No
// Node engine writes the member file, so the face is cn's alone: cn
// writes it with `cn tasks flat --write` over the tree, and expect is the
// file written by hand once.

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

func TestParityMember(t *testing.T) {
	fixtures, err := LoadUpdateFixtures(filepath.Join("testdata", "member"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no member fixtures under testdata/member")
	}
	scratch := t.TempDir()
	for _, f := range fixtures {
		t.Run(f.Core+"/"+f.Name, func(t *testing.T) {
			for _, e := range engines(t) {
				c, ok := e.(Cn)
				if !ok {
					continue
				}
				dir := filepath.Join(scratch, "member-"+f.Core+"-"+f.Name)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				got, err := flatMember(c, dir, f.Input)
				if err != nil {
					t.Fatal(err)
				}
				var want any
				if err := json.Unmarshal(f.Expect, &want); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("cn disagrees:\n%s", strings.Join(jsonDiff("$", got, want, 30), "\n"))
				}
			}
		})
	}
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
