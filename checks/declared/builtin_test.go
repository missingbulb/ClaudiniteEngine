package declared

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/shared/transcript"
)

// probe is a pack-owned built-in that reports one blocking finding on
// every file named probe.txt and one advisory on every advise.txt.
func probe(id, pack, since string) Builtin {
	b := Builtin{ID: id, Pack: pack, OnFail: "block", Since: since, Tags: []string{"world", "builtin", pack}, Why: "probes"}
	b.Run = func(ctx *Ctx, _ *transcript.Session) []findings.Finding {
		var out []findings.Finding
		for _, f := range ctx.Files() {
			switch {
			case strings.HasSuffix(f, "probe.txt"):
				out = append(out, b.Finding(f, 0, "is a probe", "remove it"))
			case strings.HasSuffix(f, "advise.txt"):
				out = append(out, b.Advice(f, 0, "is advice", "read it"))
			}
		}
		return out
	}
	return b
}

func TestPackOwnedBuiltinsRunOnlyWhereTheirPackIsDeclared(t *testing.T) {
	dir := member(t, testSettings, "[]\n", map[string]string{"probe.txt": "x\n"})
	s, err := LoadSet(dir, "0.0.0", probe("on-acme", "acme-pack", ""), probe("on-other", "other-pack", ""), probe("on-none", "", ""))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, b := range s.Builtins {
		got[b.ID] = true
	}
	if !got["on-acme"] || !got["on-none"] || got["on-other"] {
		t.Fatalf("built-ins %v: want on-acme and on-none, not on-other", got)
	}
	fs, _ := s.Run(Selection{Tags: []string{"world"}}, time.Now(), nil)
	n := 0
	for _, f := range fs {
		if f.ID == "on-acme" {
			n++
			if f.Pack != "acme-pack" || f.Class != findings.Coded || f.Why != "probes" {
				t.Errorf("finding %+v: want pack acme-pack, blocking, its check's why", f)
			}
		}
		if f.ID == "on-other" {
			t.Errorf("a built-in of an undeclared pack ran: %+v", f)
		}
	}
	if n != 1 {
		t.Errorf("on-acme reported %d findings, want 1: %v", n, fs)
	}
	fs, _ = s.Run(Selection{Tags: []string{"world"}, Pack: "acme-pack"}, time.Now(), nil)
	for _, f := range fs {
		if f.ID == "on-none" {
			t.Errorf("--pack acme-pack ran a built-in of no pack: %+v", f)
		}
	}
}

func TestBuiltinFindingsTakeGraceAndConfig(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	dir := member(t, testSettings, "[]\n", map[string]string{"probe.txt": "x\n", "advise.txt": "y\n"})
	s, err := LoadSet(dir, "0.0.0", probe("fresh", "acme-pack", today), probe("old", "acme-pack", "2020-01-01"))
	if err != nil {
		t.Fatal(err)
	}
	fs, _ := s.Run(Selection{Tags: []string{"world"}}, time.Now(), nil)
	fs = ApplyConfig(fs, s.Config)
	byKey := map[string]findings.Finding{}
	for _, f := range fs {
		byKey[f.ID+" "+f.Path] = f
	}
	if f := byKey["fresh probe.txt"]; f.Class != findings.Advisory || !strings.Contains(f.Fix, "grace") {
		t.Errorf("a built-in inside its grace window: %+v, want advisory naming the grace", f)
	}
	if f := byKey["old probe.txt"]; f.Class != findings.Coded {
		t.Errorf("a built-in past its grace window: %+v, want blocking", f)
	}
	if f := byKey["old advise.txt"]; f.Class != findings.Advisory {
		t.Errorf("a built-in's advisory finding: %+v, want advisory", f)
	}
	for _, rules := range []string{"old: off", "acme-pack/old: off"} {
		cfg := testSettings + "checks:\n  rules:\n    " + rules + "\n"
		dir := member(t, cfg, "[]\n", map[string]string{"probe.txt": "x\n"})
		fs, _ := mustLoad(t, dir, probe("old", "acme-pack", "")).Run(Selection{Tags: []string{"world"}}, time.Now(), nil)
		for _, f := range fs {
			if f.ID == "old" {
				t.Errorf("rules %q left %+v", rules, f)
			}
		}
	}
}

func mustLoad(t *testing.T, dir string, extra ...Builtin) *Set {
	t.Helper()
	s, err := LoadSet(dir, "0.0.0", extra...)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestListShowsABuiltinWithItsPackAndSince(t *testing.T) {
	dir := member(t, testSettings, "[]\n", nil)
	s := mustLoad(t, dir, probe("on-acme", "acme-pack", "2026-09-20"))
	for _, l := range s.List() {
		if l.ID != "on-acme" {
			continue
		}
		if l.Kind != "builtin" || l.Pack != "acme-pack" || l.OnFail != "block" || l.Since != "2026-09-20" || strings.Join(l.Tags, ",") != "world,builtin,acme-pack" {
			t.Errorf("listed %+v", l)
		}
		return
	}
	t.Fatal("on-acme is not listed")
}

func TestABuiltinThatPanicsIsABreak(t *testing.T) {
	dir := member(t, testSettings, "[]\n", nil)
	b := Builtin{ID: "boom", Tags: []string{"world", "builtin"}, OnFail: "block", Run: func(*Ctx, *transcript.Session) []findings.Finding { panic("kaboom") }}
	fs, _ := mustLoad(t, dir, b).Run(Selection{Tags: []string{"world"}}, time.Now(), nil)
	for _, f := range fs {
		if f.ID == "checks-run" && strings.Contains(f.Sentence, "boom") && strings.Contains(f.Sentence, "kaboom") {
			return
		}
	}
	t.Fatalf("no checks-run break for the panicking built-in: %v", fs)
}

// A git that never answers is a checks-run break naming the command, not
// a hang and not a silent empty read.
func TestAHungGitIsACheckRunBreak(t *testing.T) {
	dir := member(t, testSettings, "[]\n", map[string]string{"probe.txt": "x\n"})
	s, err := LoadSet(dir, "0.0.0", probe("on-acme", "acme-pack", ""))
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CLAUDINITE_CHECKS_NO_FETCH", "1")
	old := gitcmd.CommandTimeout
	gitcmd.CommandTimeout = 100 * time.Millisecond
	defer func() { gitcmd.CommandTimeout = old }()
	fs, _ := s.Run(Selection{Tags: []string{"world"}}, time.Now(), nil)
	var breaks []string
	for _, f := range fs {
		if f.ID == "checks-run" && f.Class == findings.Break {
			breaks = append(breaks, f.Sentence)
		}
	}
	if len(breaks) == 0 || !strings.Contains(strings.Join(breaks, "\n"), "timed out after 100ms") {
		t.Errorf("breaks %q in %v", breaks, fs)
	}
}

// A git fault spends the run's tree: the checks that would have read it
// are named as skipped, none runs silently over an empty read, and the
// fault is one checks-run break.
func TestAGitFaultSpendsTheTree(t *testing.T) {
	dir := member(t, testSettings, markerCheck, map[string]string{"probe.txt": "x\n", "notes.md": "ACME_MARKER\n"})
	s, err := LoadSet(dir, "0.0.0", probe("on-acme", "acme-pack", ""))
	if err != nil {
		t.Fatal(err)
	}
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	mark := filepath.Join(bin, "hung-once")
	script := "#!/bin/sh\nif [ ! -e " + mark + " ]; then : >" + mark + "; exec sleep 30; fi\nexec " + real + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	old := gitcmd.CommandTimeout
	gitcmd.CommandTimeout = 100 * time.Millisecond
	defer func() { gitcmd.CommandTimeout = old }()
	fs, _ := s.Run(Selection{Tags: []string{"world"}}, time.Now(), nil)
	breaks := 0
	for _, f := range fs {
		if f.ID != "checks-run" {
			t.Errorf("a check ran over the faulted tree: %+v", f)
			continue
		}
		breaks++
	}
	if breaks != 1 {
		t.Errorf("%d checks-run breaks, want 1: %v", breaks, fs)
	}
	want := []string{"acme-pack/acme-check", "declared-check-spec-keys", "acme-pack/on-acme"}
	if strings.Join(s.Skipped, ",") != strings.Join(want, ",") {
		t.Errorf("skipped %q, want %q", s.Skipped, want)
	}
}
