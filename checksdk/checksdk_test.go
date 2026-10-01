package checksdk

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testRegistry() *registry {
	r := &registry{}
	r.add("hello", Check{ID: "hello-check", Tags: []string{"work", "world"}, Run: func(repo Repo) []Finding {
		if repo.Exists("HELLO_FINDING") {
			return []Finding{{Class: ClassFinding, Path: "HELLO_FINDING", Sentence: "remove HELLO_FINDING"}}
		}
		return nil
	}})
	r.add("hello", Check{ID: "hello-advice", Tags: []string{"work"}, Run: func(Repo) []Finding {
		return []Finding{{Class: ClassAdvisory, Path: ".", Sentence: "consider it"}}
	}})
	r.add("other", Check{ID: "boom", Tags: []string{"world"}, Run: func(Repo) []Finding { panic("kaput") }})
	return r
}

func converse(t *testing.T, r *registry, lines ...string) ([]map[string]any, int) {
	t.Helper()
	var out bytes.Buffer
	code := r.serve(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out)
	var got []map[string]any
	sc := bufio.NewScanner(&out)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("not JSON: %q", sc.Text())
		}
		got = append(got, m)
	}
	return got, code
}

const hello = `{"proto":"claudinite-checks-v1","engine":"1.1.0"}`

func TestHandshakeListRun(t *testing.T) {
	repo := t.TempDir()
	_ = os.WriteFile(filepath.Join(repo, "HELLO_FINDING"), nil, 0o644)
	got, code := converse(t, testRegistry(), hello, `{"op":"list"}`,
		`{"op":"run","tags":["world"],"repo":`+mustJSON(repo)+`}`,
		`{"op":"run","tags":["work"],"pack":"hello","repo":`+mustJSON(repo)+`}`)
	if code != 0 || len(got) != 4 {
		t.Fatalf("code %d: %v", code, got)
	}
	if got[0]["proto"] != Proto {
		t.Errorf("handshake %v", got[0])
	}
	if s, _ := json.Marshal(got[1]["checks"]); string(s) != `[{"check":"hello/hello-check","tags":["work","world"]},{"check":"hello/hello-advice","tags":["work"]},{"check":"other/boom","tags":["world"]}]` {
		t.Errorf("list %s", s)
	}
	world, _ := json.Marshal(got[2])
	if !strings.Contains(string(world), `"check":"hello/hello-check","class":"finding","path":"HELLO_FINDING"`) || !strings.Contains(string(world), `other/boom: panic: kaput`) {
		t.Errorf("world run %s", world)
	}
	work, _ := json.Marshal(got[3])
	if !strings.Contains(string(work), `hello/hello-advice`) || strings.Contains(string(work), "boom") {
		t.Errorf("work run %s", work)
	}
}

func TestHandshakeRefusesAnotherProtocol(t *testing.T) {
	got, code := converse(t, testRegistry(), `{"proto":"claudinite-checks-v9"}`, `{"op":"list"}`)
	if code == 0 || len(got) != 1 || got[0]["error"] == nil {
		t.Errorf("code %d %v", code, got)
	}
}

func TestRequestBeforeHandshakeAndMalformed(t *testing.T) {
	if got, code := converse(t, testRegistry(), `{"op":"list"}`); code == 0 || got[0]["error"] == nil {
		t.Errorf("no handshake: %d %v", code, got)
	}
	if got, code := converse(t, testRegistry(), hello, `{nope`); code == 0 || len(got) != 2 || got[1]["error"] == nil {
		t.Errorf("malformed: %d %v", code, got)
	}
	if got, _ := converse(t, testRegistry(), hello, `{"op":"dance"}`); len(got) != 2 || got[1]["error"] == nil {
		t.Errorf("unknown op: %v", got)
	}
}

func TestPackOfReadsTheGeneratedPackagePath(t *testing.T) {
	cases := map[string]string{
		"claudinite.checks/build/packs/hello.init.0":            "hello",
		"claudinite.checks/build/packs/acme-pack/sub.init.0":    "acme-pack",
		"claudinite.checks/build/packs/hello.glob..func1":       "hello",
		"github.com/missingbulb/ClaudiniteEngine/checksdk.Test": "",
	}
	for fn, want := range cases {
		if got := packOf(fn); got != want {
			t.Errorf("packOf(%q) = %q, want %q", fn, got, want)
		}
	}
}

func TestRegisterRefusesDuplicatesAndBadIDs(t *testing.T) {
	r := &registry{}
	r.add("p", Check{ID: "a", Tags: []string{"work"}, Run: func(Repo) []Finding { return nil }})
	for name, c := range map[string]Check{
		"duplicate": {ID: "a", Tags: []string{"work"}, Run: func(Repo) []Finding { return nil }},
		"bad id":    {ID: "A b", Tags: []string{"work"}, Run: func(Repo) []Finding { return nil }},
		"no run":    {ID: "c", Tags: []string{"work"}},
		"no tags":   {ID: "d", Run: func(Repo) []Finding { return nil }},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: registered", name)
				}
			}()
			r.add("p", c)
		}()
	}
}

func TestSourceIsTheSDK(t *testing.T) {
	raw, err := os.ReadFile("checksdk.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(Source, raw) {
		t.Error("the embedded Source is not checksdk.go")
	}
}

func mustJSON(s string) string { b, _ := json.Marshal(s); return string(b) }
