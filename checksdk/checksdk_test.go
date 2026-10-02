package checksdk

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
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
	if s, _ := json.Marshal(got[1]["checks"]); string(s) != `[{"check":"hello/hello-check","on_fail":"block","tags":["work","world"]},{"check":"hello/hello-advice","on_fail":"block","tags":["work"]},{"check":"other/boom","on_fail":"block","tags":["world"]}]` {
		t.Errorf("list %s", s)
	}
	world, _ := json.Marshal(got[2])
	if !strings.Contains(string(world), `"check":"hello/hello-check","class":"finding","on_fail":"block","path":"HELLO_FINDING"`) || !strings.Contains(string(world), `other/boom: panic: kaput`) {
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
		"claudinite.checks/build/packs/local/probe.init.0":      "local/probe",
		"claudinite.checks/build/packs/local/probe/sub.init.0":  "local/probe",
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
		"duplicate":                  {ID: "a", Tags: []string{"work"}, Run: func(Repo) []Finding { return nil }},
		"bad id":                     {ID: "A b", Tags: []string{"work"}, Run: func(Repo) []Finding { return nil }},
		"no run":                     {ID: "c", Tags: []string{"work"}},
		"no tags":                    {ID: "d", Run: func(Repo) []Finding { return nil }},
		"a judge with no hook event": {ID: "e", Tags: []string{"work"}, Judge: func(Repo, Call) []Finding { return nil }},
		"a hook event with no judge": {ID: "f", Tags: []string{"pre-tool-use"}, Run: func(Repo) []Finding { return nil }},
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

// A judge reads the call; the judge op selects the judges tagged with the
// event, and a check with both a Run and a Judge serves both ops; list
// marks the judges.
func TestJudge(t *testing.T) {
	r := testRegistry()
	r.add("hello", Check{ID: "hello-judge", Tags: []string{"pre-tool-use"}, Judge: func(_ Repo, c Call) []Finding {
		var in struct{ Command string }
		_ = json.Unmarshal(c.Input, &in)
		if c.Tool == "Bash" && strings.Contains(in.Command, "HELLO_JUDGE") {
			return []Finding{{Class: ClassFinding, Path: "(tool call)", Sentence: "no HELLO_JUDGE"}}
		}
		return nil
	}})
	r.add("hello", Check{ID: "both", Tags: []string{"work", "post-tool-use"}, Run: func(Repo) []Finding { return nil },
		Judge: func(_ Repo, c Call) []Finding {
			return []Finding{{Class: ClassAdvisory, Sentence: "result " + string(c.Response)}}
		}})
	r.add("other", Check{ID: "judge-boom", Tags: []string{"pre-tool-use"}, Judge: func(Repo, Call) []Finding { panic("kaput") }})
	got, code := converse(t, r, hello, `{"op":"list"}`,
		`{"op":"judge","event":"pre-tool-use","call":{"tool":"Bash","input":{"command":"echo HELLO_JUDGE"}},"repo":"/r"}`,
		`{"op":"judge","event":"post-tool-use","call":{"tool":"Bash","input":{},"response":"out"},"repo":"/r"}`,
		`{"op":"run","tags":["pre-tool-use"],"repo":"/r"}`)
	if code != 0 || len(got) != 5 {
		t.Fatalf("code %d: %v", code, got)
	}
	list, _ := json.Marshal(got[1]["checks"])
	if !strings.Contains(string(list), `{"check":"hello/hello-judge","judge":true,"on_fail":"block","tags":["pre-tool-use"]}`) || !strings.Contains(string(list), `{"check":"hello/hello-check","on_fail":"block","tags":["work","world"]}`) {
		t.Errorf("list %s", list)
	}
	pre, _ := json.Marshal(got[2])
	if !strings.Contains(string(pre), `"check":"hello/hello-judge","class":"finding","on_fail":"block","path":"(tool call)","sentence":"no HELLO_JUDGE"`) || !strings.Contains(string(pre), "other/judge-boom: panic: kaput") || strings.Contains(string(pre), "both") {
		t.Errorf("pre-tool-use %s", pre)
	}
	post, _ := json.Marshal(got[3])
	if !strings.Contains(string(post), `"check":"hello/both","class":"advisory","on_fail":"block","path":"","sentence":"result \"out\""`) || strings.Contains(string(post), "hello-judge") {
		t.Errorf("post-tool-use %s", post)
	}
	if run, _ := json.Marshal(got[4]); strings.Contains(string(run), "judge") {
		t.Errorf("a run ran a judge: %s", run)
	}
}

// Every non-test file of the package but this embedding is the SDK.
func TestSourcesAreTheSDK(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	src := Sources()
	n := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "embed.go" {
			continue
		}
		n++
		raw, _ := os.ReadFile(name)
		if !bytes.Equal(src[name], raw) {
			t.Errorf("%s is not embedded as it is", name)
		}
	}
	if n != len(src) {
		t.Errorf("%d files, %d embedded", n, len(src))
	}
}

func mustJSON(s string) string { b, _ := json.Marshal(s); return string(b) }

func mustRe(s string) *regexp.Regexp { return regexp.MustCompile(s) }

// The floor can never be raised past a version that exists: its day part
// is never after today's.
func TestEngineFloorIsBehindTheClock(t *testing.T) {
	day, err := strconv.Atoi(strings.SplitN(EngineFloor, ".", 2)[0])
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if today := (now.Year()-2020)*10000 + int(now.Month())*100 + now.Day(); day > today {
		t.Errorf("EngineFloor %s is after today's day %d", EngineFloor, today)
	}
}
