package checksdk

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// engineSide plays cn on the other end of serve: the handshake announcing
// methods, then each request in turn, answering every sdk call through
// handle until the request's answer arrives. It returns the answers and
// the calls the child made, by method.
func engineSide(t *testing.T, r *registry, methods []string, handle func(string, json.RawMessage) (any, error), reqs ...string) ([]map[string]any, map[string]int) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan int, 1)
	go func() {
		done <- r.serve(inR, outW)
		_ = outW.Close()
	}()
	sc := bufio.NewScanner(outR)
	sc.Buffer(make([]byte, 64<<10), MaxLine)
	send := func(v any) {
		b, _ := json.Marshal(v)
		if _, err := inW.Write(append(b, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	calls := map[string]int{}
	next := func() map[string]any {
		for sc.Scan() {
			var m map[string]any
			if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
				t.Fatalf("not JSON: %q", sc.Text())
			}
			method, ok := m["sdk"].(string)
			if !ok {
				return m
			}
			calls[method]++
			args, _ := json.Marshal(m["args"])
			res, err := handle(method, args)
			if err != nil {
				send(map[string]any{"id": m["id"], "error": err.Error()})
			} else {
				send(map[string]any{"id": m["id"], "result": res})
			}
		}
		t.Fatal("the child closed its stdout")
		return nil
	}
	hs := map[string]any{"proto": Proto, "engine": "1.61001.1"}
	if methods != nil {
		hs["methods"] = methods
	}
	send(hs)
	var got []map[string]any
	got = append(got, next())
	for _, req := range reqs {
		if _, err := inW.Write([]byte(req + "\n")); err != nil {
			t.Fatal(err)
		}
		got = append(got, next())
	}
	_ = inW.Close()
	if code := <-done; code != 0 {
		t.Errorf("serve exited %d", code)
	}
	return got, calls
}

func TestRunCallsBackToTheEngine(t *testing.T) {
	r := &registry{}
	r.add("acme-pack", Check{ID: "acme-check", Tags: []string{"world"}, OnFail: "advise", Since: "2026-10-01", Why: "a reason", Doc: "README.md",
		Run: func(repo Repo) []Finding {
			var out []Finding
			for _, f := range repo.Files() {
				out = append(out, Finding{Path: f, Line: 2, Sentence: "saw " + f, Fix: "do less"})
			}
			// A second read is memoised: one call per method per run.
			_ = repo.Files()
			if text, ok := repo.ReadBase("a.md"); ok {
				out = append(out, Finding{Class: ClassFinding, Path: "a.md", Sentence: "base " + text, Why: "its own"})
			}
			if string(repo.PackConfig("acme-pack")) != `{"probe":true}` {
				out = append(out, Finding{Path: "x", Sentence: "config " + string(repo.PackConfig("acme-pack"))})
			}
			if turns := repo.Session().OwnerTurns(); len(turns) != 1 || !turns[0].Has("feature") || turns[0].Time().IsZero() {
				out = append(out, Finding{Path: "x", Sentence: fmt.Sprintf("turns %v", turns)})
			}
			return out
		}})
	handle := func(method string, args json.RawMessage) (any, error) {
		switch method {
		case "tree.files":
			return []string{"a.md", "b.md"}, nil
		case "change.readBase":
			var a struct{ Path string }
			_ = json.Unmarshal(args, &a)
			return map[string]any{"text": "at base " + a.Path, "ok": true}, nil
		case "config.pack":
			return json.RawMessage(`{"probe":true}`), nil
		case "session.ownerTurns":
			return []map[string]any{{"index": 3, "timestamp": "2026-10-01T10:00:00Z", "text": "do it", "reply": "Comment class: feature", "classes": []string{"feature"}}}, nil
		}
		return nil, fmt.Errorf("unknown method %s", method)
	}
	got, calls := engineSide(t, r, []string{"tree.files", "change.readBase", "config.pack", "session.ownerTurns"}, handle,
		`{"op":"list"}`, `{"op":"run","tags":["world"],"repo":"/r"}`)
	list, _ := json.Marshal(got[1]["checks"])
	if string(list) != `[{"check":"acme-pack/acme-check","on_fail":"advise","since":"2026-10-01","tags":["world"]}]` {
		t.Errorf("list %s", list)
	}
	run, _ := json.Marshal(got[2]["findings"])
	for _, want := range []string{
		`{"check":"acme-pack/acme-check","class":"advisory","fix":"do less","line":2,"on_fail":"advise","path":"a.md","sentence":"saw a.md","since":"2026-10-01","why":"a reason"}`,
		`"path":"b.md"`,
		`"class":"finding","on_fail":"advise","path":"a.md","sentence":"base at base a.md","since":"2026-10-01","why":"its own"`,
	} {
		if !strings.Contains(string(run), want) {
			t.Errorf("run lacks %s:\n%s", want, run)
		}
	}
	if strings.Contains(string(run), `"path":"x"`) || got[2]["errors"] != nil {
		t.Errorf("run %s errors %v", run, got[2]["errors"])
	}
	if calls["tree.files"] != 1 || calls["change.readBase"] != 1 {
		t.Errorf("calls %v", calls)
	}
}

// A method the engine's handshake did not announce fails the check that
// asked, naming the method and the engine it needs; the other checks run.
func TestAnUnannouncedMethodFailsOneCheck(t *testing.T) {
	r := &registry{}
	r.add("p", Check{ID: "needs-files", Tags: []string{"world"}, Run: func(repo Repo) []Finding { repo.Files(); return nil }})
	r.add("p", Check{ID: "plain", Tags: []string{"world"}, Run: func(Repo) []Finding { return []Finding{{Path: ".", Sentence: "ran"}} }})
	for _, methods := range [][]string{nil, {"config.pack"}} {
		got, calls := engineSide(t, r, methods, func(string, json.RawMessage) (any, error) { return nil, nil }, `{"op":"run","tags":["world"],"repo":"/r"}`)
		errs, _ := json.Marshal(got[1]["errors"])
		if !strings.Contains(string(errs), "p/needs-files: checksdk: the engine does not answer tree.files; it needs cn "+EngineFloor+" or newer") {
			t.Errorf("errors %s", errs)
		}
		if f, _ := json.Marshal(got[1]["findings"]); !strings.Contains(string(f), `"sentence":"ran"`) {
			t.Errorf("findings %s", f)
		}
		if len(calls) != 0 {
			t.Errorf("calls %v", calls)
		}
	}
}

// An engine error answers the call; the check reading it fails, not the
// run.
func TestAnEngineErrorFailsTheCheck(t *testing.T) {
	r := &registry{}
	r.add("p", Check{ID: "parses", Tags: []string{"world"}, Run: func(repo Repo) []Finding {
		if _, err := repo.Parsed("a.yaml"); err != nil {
			return []Finding{{Path: "a.yaml", Sentence: err.Error()}}
		}
		return nil
	}})
	got, _ := engineSide(t, r, []string{"doc.parse"}, func(string, json.RawMessage) (any, error) { return nil, fmt.Errorf("bad yaml") },
		`{"op":"run","tags":["world"],"repo":"/r"}`)
	if f, _ := json.Marshal(got[1]["findings"]); !strings.Contains(string(f), `"sentence":"bad yaml"`) {
		t.Errorf("findings %s", f)
	}
}

// A check past its clock is that check's error; the others still run.
func TestTheCheckClock(t *testing.T) {
	old := CheckDeadline
	CheckDeadline = 100 * time.Millisecond
	defer func() { CheckDeadline = old }()
	r := &registry{}
	r.add("p", Check{ID: "slow", Tags: []string{"world"}, Run: func(Repo) []Finding { time.Sleep(time.Hour); return nil }})
	r.add("p", Check{ID: "quick", Tags: []string{"world"}, Run: func(Repo) []Finding { return []Finding{{Path: ".", Sentence: "ran"}} }})
	got, _ := engineSide(t, r, []string{}, nil, `{"op":"run","tags":["world"],"repo":"/r"}`)
	errs, _ := json.Marshal(got[1]["errors"])
	if !strings.Contains(string(errs), "p/slow: deadline (100ms) passed in run (0 ms waiting on the engine)") {
		t.Errorf("errors %s", errs)
	}
	if f, _ := json.Marshal(got[1]["findings"]); !strings.Contains(string(f), `"sentence":"ran"`) {
		t.Errorf("findings %s", f)
	}
}

// The clock measures the check's own work: a slow engine answer does not
// spend it, and a check that runs out says how long it waited on the
// engine.
func TestTheCheckClockPausesForEngineAnswers(t *testing.T) {
	old := CheckDeadline
	CheckDeadline = 100 * time.Millisecond
	defer func() { CheckDeadline = old }()
	r := &registry{}
	r.add("p", Check{ID: "patient", Tags: []string{"world"}, Run: func(repo Repo) []Finding {
		return []Finding{{Path: ".", Sentence: strings.Join(repo.Files(), ",")}}
	}})
	r.add("p", Check{ID: "busy", Tags: []string{"world"}, Run: func(repo Repo) []Finding {
		_ = repo.Tracked()
		time.Sleep(time.Hour)
		return nil
	}})
	slow := func(string, json.RawMessage) (any, error) {
		time.Sleep(300 * time.Millisecond)
		return []string{"a.md"}, nil
	}
	got, _ := engineSide(t, r, []string{"tree.files", "tree.tracked"}, slow, `{"op":"run","tags":["world"],"repo":"/r"}`)
	errs, _ := json.Marshal(got[1]["errors"])
	if f, _ := json.Marshal(got[1]["findings"]); !strings.Contains(string(f), `"sentence":"a.md"`) {
		t.Errorf("a 300 ms engine answer spent a 100 ms clock: findings %s errors %s", f, errs)
	}
	if !regexp.MustCompile(`p/busy: deadline \(100ms\) passed in run \([3-9]\d\d ms waiting on the engine\)`).Match(errs) {
		t.Errorf("errors %s", errs)
	}
}

// Two engine calls in flight at once, from two goroutines of one check,
// still leave the clock running once both are answered.
func TestTheCheckClockSurvivesOverlappingEngineCalls(t *testing.T) {
	old := CheckDeadline
	CheckDeadline = 100 * time.Millisecond
	defer func() { CheckDeadline = old }()
	r := &registry{}
	r.add("p", Check{ID: "fanout", Tags: []string{"world"}, Run: func(repo Repo) []Finding {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _ = repo.Files() }()
		go func() { defer wg.Done(); _ = repo.Tracked() }()
		wg.Wait()
		time.Sleep(time.Hour)
		return nil
	}})
	slow := func(string, json.RawMessage) (any, error) {
		time.Sleep(50 * time.Millisecond)
		return []string{"a.md"}, nil
	}
	type result struct{ got []map[string]any }
	ch := make(chan result, 1)
	go func() {
		got, _ := engineSide(t, r, []string{"tree.files", "tree.tracked"}, slow, `{"op":"run","tags":["world"],"repo":"/r"}`)
		ch <- result{got}
	}()
	select {
	case res := <-ch:
		errs, _ := json.Marshal(res.got[1]["errors"])
		if !regexp.MustCompile(`p/fanout: deadline \(100ms\) passed in run`).Match(errs) {
			t.Errorf("errors %s", errs)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("after two overlapping engine calls the check's deadline never fired")
	}
}

// A check that keeps the engine busy cannot run past ten deadlines.
func TestTheCheckClockCapsTimeWaitingOnTheEngine(t *testing.T) {
	old := CheckDeadline
	CheckDeadline = 20 * time.Millisecond
	defer func() { CheckDeadline = old }()
	r := &registry{}
	r.add("p", Check{ID: "chatty", Tags: []string{"world"}, Run: func(repo Repo) []Finding {
		for i := 0; ; i++ {
			_ = repo.GrepTracked(fmt.Sprint(i))
		}
	}})
	slow := func(string, json.RawMessage) (any, error) {
		time.Sleep(10 * time.Millisecond)
		return []any{}, nil
	}
	ch := make(chan []byte, 1)
	go func() {
		got, _ := engineSide(t, r, []string{"change.grep"}, slow, `{"op":"run","tags":["world"],"repo":"/r"}`)
		errs, _ := json.Marshal(got[1]["errors"])
		ch <- errs
	}()
	select {
	case errs := <-ch:
		if !regexp.MustCompile(`p/chatty: deadline \(20ms\) passed in run \(\d+ ms waiting on the engine\)`).Match(errs) {
			t.Errorf("errors %s", errs)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a check calling the engine without end was never stopped")
	}
}

func TestRegisterRefusesBadOnFailAndSince(t *testing.T) {
	run := func(Repo) []Finding { return nil }
	for name, c := range map[string]Check{
		"on_fail": {ID: "a", Tags: []string{"world"}, Run: run, OnFail: "blocking"},
		"since":   {ID: "b", Tags: []string{"world"}, Run: run, Since: "2026-9-1"},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: registered", name)
				}
			}()
			(&registry{}).add("p", c)
		}()
	}
}

// A finding with no class takes its check's on_fail.
func TestFindingClassDefaultsToOnFail(t *testing.T) {
	r := &registry{}
	r.add("p", Check{ID: "adv", Tags: []string{"world"}, OnFail: "advise", Run: func(Repo) []Finding { return []Finding{{Path: "a"}, {Path: "b", Class: ClassFinding}} }})
	r.add("p", Check{ID: "blk", Tags: []string{"world"}, Run: func(Repo) []Finding { return []Finding{{Path: "c"}} }})
	got, _ := engineSide(t, r, []string{}, nil, `{"op":"run","tags":["world"],"repo":"/r"}`)
	f, _ := json.Marshal(got[1]["findings"])
	for _, want := range []string{`"class":"advisory","on_fail":"advise","path":"a"`, `"class":"finding","on_fail":"advise","path":"b"`, `"check":"p/blk","class":"finding","on_fail":"block","path":"c","sentence":""`} {
		if !strings.Contains(string(f), want) {
			t.Errorf("findings lack %s: %s", want, f)
		}
	}
}
