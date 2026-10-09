package packset

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func tokens(s Set) string {
	var out []string
	for _, p := range s.Packs {
		out = append(out, p.Token())
	}
	return strings.Join(out, ",")
}

func notLoaded(s Set) string {
	var out []string
	for _, n := range s.NotLoaded {
		out = append(out, n.Token+": "+n.Why)
	}
	return strings.Join(out, "\n")
}

func member(t *testing.T, declared string) string {
	t.Helper()
	repo := t.TempDir()
	write(t, filepath.Join(repo, ".claudinite/settings.yaml"), "engine:\n  version: \"1.1.0\"\npacks:\n  declared:\n"+declared)
	return repo
}

// Canon packs come first by directory name, then local packs, then (in a
// session) every temp pack present, as the Node registry orders them.
func TestLoadOrderAndKinds(t *testing.T) {
	repo := member(t, "    - zeta\n    - local/mine\n    - alpha\n")
	write(t, filepath.Join(Tree(repo, "zeta"), "pack.json"), `{"version": "1.0"}`)
	write(t, filepath.Join(Tree(repo, "zeta"), "RULES.md"), "- z\n")
	write(t, filepath.Join(Tree(repo, "alpha"), "pack.yaml"), "version: \"1.0\"\n")
	write(t, filepath.Join(Tree(repo, "undeclared"), "pack.json"), `{"version": "1.0"}`)
	write(t, filepath.Join(repo, LocalDir, "mine", "pack.toml"), "prose = \"NOTES.md\"\n")
	write(t, filepath.Join(repo, LocalDir, "mine", "NOTES.md"), "- n\n")
	write(t, filepath.Join(repo, TempDir, "current_user", "pack.json"), `{}`)
	write(t, filepath.Join(repo, TempDir, "current_user", "RULES.md"), "- u\n")
	write(t, filepath.Join(repo, TempDir, "no-manifest", "RULES.md"), "- x\n")

	s, err := Load(repo, "0.0.0", true)
	if err != nil {
		t.Fatal(err)
	}
	if got := tokens(s); got != "alpha,zeta,local/mine,temp/current_user" {
		t.Fatalf("order %s\n%s", got, notLoaded(s))
	}
	if s.Packs[0].Prose != "" || s.Packs[1].Prose != "RULES.md" || s.Packs[2].Prose != "NOTES.md" || s.Packs[2].Kind != Local {
		t.Errorf("prose %+v", s.Packs)
	}
	if s.Packs[1].ProsePath() == "" || s.Packs[0].ProsePath() != "" {
		t.Errorf("prose paths")
	}
	if s, _ := Load(repo, "0.0.0", false); tokens(s) != "alpha,zeta,local/mine" {
		t.Errorf("outside a session: %s", tokens(s))
	}
}

// The manifest's prose: null and skills subset override the tree.
func TestLoadConventionsOverride(t *testing.T) {
	repo := member(t, "    - p\n    - q\n")
	write(t, filepath.Join(Tree(repo, "p"), "pack.json"), `{"version": "1.0", "prose": null, "skills": ["b"]}`)
	write(t, filepath.Join(Tree(repo, "p"), "RULES.md"), "- doc\n")
	write(t, filepath.Join(Tree(repo, "p"), "skills/a/SKILL.md"), "a")
	write(t, filepath.Join(Tree(repo, "p"), "skills/b/SKILL.md"), "b")
	write(t, filepath.Join(Tree(repo, "q"), "pack.json"), `{"version": "1.0"}`)
	write(t, filepath.Join(Tree(repo, "q"), "skills/c/SKILL.md"), "c")
	write(t, filepath.Join(Tree(repo, "q"), "skills/d/checks.mjs"), "")
	s, err := Load(repo, "0.0.0", false)
	if err != nil || len(s.Packs) != 2 {
		t.Fatalf("%+v %v\n%s", s, err, notLoaded(s))
	}
	if s.Packs[0].Prose != "" || strings.Join(s.Packs[0].Skills, ",") != "b" {
		t.Errorf("p %+v", s.Packs[0])
	}
	if strings.Join(s.Packs[1].Skills, ",") != "c,d" {
		t.Errorf("q %+v", s.Packs[1])
	}
}

func TestLoadNotLoaded(t *testing.T) {
	repo := member(t, "    - needs\n    - absent\n    - newer\n    - local/gone\n    - local/needs\n    - badskill\n")
	write(t, filepath.Join(Tree(repo, "needs"), "pack.json"), `{"version": "1.0", "minEngineVersion": "1.60102.1", "requires": ["other"]}`)
	write(t, filepath.Join(Tree(repo, "newer"), "pack.json"), `{"version": "1.0", "minEngineVersion": "9.60101.1"}`)
	write(t, filepath.Join(Tree(repo, "badskill"), "pack.json"), `{"version": "1.0", "minEngineVersion": "1.60102.1", "skills": ["nope"]}`)
	write(t, filepath.Join(repo, LocalDir, "needs", "pack.json"), `{}`)
	s, err := Load(repo, "1.60102.1", false)
	if err != nil {
		t.Fatal(err)
	}
	got := notLoaded(s)
	for _, want := range []string{
		"absent: .claudinite/shared/packs/absent is missing",
		"newer: pack newer 1.0 needs engine 9.60101.1 or newer; this is 1.60102.1",
		"local/gone: .claudinite/local/packs/gone is missing",
		"local/needs: its id is the declared canon pack needs's",
		`badskill: pack.json names a skill "nope" with no skills/nope/ directory`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if len(s.Unmet) != 1 || s.Unmet[0].Token != "needs" || s.Unmet[0].Why != "requires other, which is not declared" {
		t.Errorf("unmet %+v", s.Unmet)
	}
	if tokens(s) != "needs" {
		t.Errorf("loaded %s; a pack whose requires is undeclared loads, as in the Node engine", tokens(s))
	}
}

// A floor that is no <major>.<day>.<n> version does not load: a Node
// engine's two-part floor no more than any other.
func TestLoadRefusesAnUnreadableFloor(t *testing.T) {
	repo := member(t, "    - old\n    - bad\n")
	write(t, filepath.Join(Tree(repo, "old"), "pack.json"), `{"version": "1.0", "minEngineVersion": "60928.1"}`)
	write(t, filepath.Join(Tree(repo, "bad"), "pack.json"), `{"version": "1.0", "minEngineVersion": "soon"}`)
	s, err := Load(repo, "1.61003.1", false)
	if err != nil {
		t.Fatal(err)
	}
	if tokens(s) != "" || !strings.Contains(notLoaded(s), `bad: minEngineVersion "soon"`) || !strings.Contains(notLoaded(s), `old: minEngineVersion "60928.1"`) {
		t.Errorf("loaded %s; not loaded:\n%s", tokens(s), notLoaded(s))
	}
}

// A malformed adoption declaration is the pack's load fault, as the Node
// engine's discovery reported it; a well-formed one is typed.
func TestAdoptionKeysAreValidatedAtLoad(t *testing.T) {
	repo := member(t, "    - asks\n    - local/bad\n    - local/seeds\n")
	write(t, filepath.Join(Tree(repo, "asks"), "pack.json"), `{"version": "1.0", "questions": [{"id": "goals", "prompt": "What for?", "distill": "config.goals"}],
		"adoptionHandover": [{"step": "Flip it", "breaks": "nothing ships", "done": "it ships"}]}`)
	write(t, filepath.Join(repo, LocalDir, "bad", "pack.json"), `{"questions": [{"id": "a", "prompt": "x"}, {"id": "a", "prompt": "y"}]}`)
	write(t, filepath.Join(repo, LocalDir, "seeds", "pack.json"), `{"seedOps": [{"template": "t.yml", "dest": ".claudinite/shared/x"}]}`)
	s, err := Load(repo, "0.0.0", false)
	if err != nil {
		t.Fatal(err)
	}
	if tokens(s) != "asks" {
		t.Fatalf("loaded %s; not loaded:\n%s", tokens(s), notLoaded(s))
	}
	m := s.Packs[0].Manifest
	if len(m.Questions) != 1 || m.Questions[0] != (Question{"goals", "What for?", "config.goals"}) || len(m.Handover) != 1 || m.Handover[0].Done != "it ships" {
		t.Errorf("manifest %+v", m)
	}
	nl := notLoaded(s)
	if !strings.Contains(nl, `questions names id "a" twice`) || !strings.Contains(nl, "under .claudinite/shared/") {
		t.Errorf("not loaded:\n%s", nl)
	}
	for _, bad := range []string{`[{"template": "../x", "dest": "y"}]`, `[{"template": "x", "dest": "/etc/y"}]`, `[{"template": "x"}]`} {
		if _, err := readSeedOps(mustJSON(t, bad)); err == nil {
			t.Errorf("seedOps %s read", bad)
		}
	}
	if _, err := readHandover(mustJSON(t, `[{"step": "x", "breaks": "y"}]`)); err == nil {
		t.Error("a handover step without done read")
	}
}

func mustJSON(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}
