package parity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// The growth face's scenarios run each engine's real command over one
// tree and compare what lands. A scenario is a directory holding
// case.json and the files it names; expect.json is the Node engine's
// answer, written by CLAUDINITE_PARITY_RECORD=1 before the Go side
// existed.
//
//   - growth-capture/<case>: a member with a bare origin (or none, or one
//     that cannot be reached), each step's transcript laid under a fake
//     CLAUDE_CONFIG_DIR; Node runs packs/claudinite-growth/capture-log.mjs,
//     cn runs cn growth capture, with the same flags.
//   - session-end/<case>: Node runs engine/hooks/session-end-command.mjs
//     over a Node declaration, cn runs cn hook session-end over the same
//     packs declared in .claudinite/settings.yaml, the payload on stdin.
//   - provenance/<case>: a repo built from case.json's commits; Node runs
//     packs/claudinite-growth/provenance.mjs, cn runs cn provenance.
//
// Each run has a fixed environment: a fresh HOME whose .claude is
// CLAUDE_CONFIG_DIR, no system git config, and PARITY_SECRET, a value
// the transcripts plant so the scrub is exercised end to end.

const paritySecret = "parity-secret-value-42"

// GrowthStep is one command of a capture scenario.
type GrowthStep struct {
	// Transcript is the session transcript's parts, concatenated, laid
	// before the step runs.
	Transcript []string `json:"transcript,omitempty"`
	// Sidechains are files under the transcript's directory, each the
	// concatenation of its parts.
	Sidechains map[string][]string `json:"sidechains,omitempty"`
	Args       []string            `json:"args"`
}

// GrowthCase is a scenario's case.json.
type GrowthCase struct {
	// Origin is bare, none or unreachable.
	Origin string `json:"origin,omitempty"`
	// Session names the transcript file under the member's slug directory.
	Session string            `json:"session,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Steps   []GrowthStep      `json:"steps,omitempty"`
	// SameMinute runs the steps inside one clock minute.
	SameMinute bool `json:"sameMinute,omitempty"`
	// Declared is session-end's declaration; absent is no settings file.
	Declared *[]string `json:"declared,omitempty"`
	// Payload is session-end's stdin; {transcript} names the laid
	// transcript.
	Payload string `json:"payload,omitempty"`
	// Commits build a provenance scenario's repo; Working is laid over
	// the last one uncommitted.
	Commits []GrowthCommit    `json:"commits,omitempty"`
	Working map[string]string `json:"working,omitempty"`
	Stdin   string            `json:"stdin,omitempty"`
	Why     string            `json:"why,omitempty"`
	// Remote is the origin URL the repo names, which nothing fetches.
	Remote string `json:"remote,omitempty"`
}

// GrowthCommit is one commit of a provenance scenario's repo.
type GrowthCommit struct {
	Message string            `json:"message"`
	Files   map[string]string `json:"files"`
	Remove  []string          `json:"remove,omitempty"`
	// Author is the commit's author email, the parity identity's when "".
	Author string `json:"author,omitempty"`
}

// GrowthRun is one command's answer.
type GrowthRun struct {
	Exit   int    `json:"exit"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

// GrowthExpect is a scenario's expect.json: every run, then the branch
// the captures wrote (absent when the scenario writes none), or the
// tree's files after a provenance command.
type GrowthExpect struct {
	Runs []GrowthRun `json:"runs"`
	// Branch is the conversation-logs branch by masked name, null when
	// no branch exists.
	Branch   map[string]string `json:"branch"`
	Subjects []string          `json:"subjects,omitempty"`
	Files    map[string]string `json:"files,omitempty"`
	// Divergence names the record row where cn decides otherwise, with
	// cn's answer in Cn.
	Divergence string        `json:"divergence,omitempty"`
	Cn         *GrowthExpect `json:"cn,omitempty"`
}

var (
	stampRE   = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{4}Z`)
	growthGit = []string{"-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}
)

type growthWorld struct {
	root, home, member, origin string
}

func (w growthWorld) env(c GrowthCase, extra ...string) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + w.home,
		"CLAUDE_CONFIG_DIR=" + filepath.Join(w.home, ".claude"),
		"GIT_CONFIG_NOSYSTEM=1",
		"PARITY_SECRET=" + paritySecret,
		"XDG_CACHE_HOME=" + filepath.Join(w.home, ".cache"),
		"CLAUDINITE_CAPTURE_BACKOFF_MS=1",
	}
	keys := make([]string, 0, len(c.Env))
	for k := range c.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+c.Env[k])
	}
	return append(env, extra...)
}

func (w growthWorld) mask(s string) string {
	s = strings.ReplaceAll(s, w.root, "{root}")
	s = strings.ReplaceAll(s, slug(w.root), "{rootslug}")
	s = stampRE.ReplaceAllString(s, "<stamp>")
	return s
}

func gitIn(dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command("git", append(append([]string{}, growthGit...), args...)...)
	cmd.Dir = dir
	cmd.Env = env
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, errb.String())
	}
	return out.String(), nil
}

func runEnv(dir string, env []string, stdin, name string, args ...string) (GrowthRun, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code, err = ee.ExitCode(), nil
	}
	return GrowthRun{Exit: code, Stdout: out.String(), Stderr: errb.String()}, err
}

// newGrowthWorld makes a member repository with one commit and the origin
// the case names.
func newGrowthWorld(t *testing.T, c GrowthCase) growthWorld {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := growthWorld{root: root, home: filepath.Join(root, "home"), member: filepath.Join(root, "member"), origin: filepath.Join(root, "origin.git")}
	for _, d := range []string{w.home, w.member} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env := w.env(c)
	must := func(_ string, err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(gitIn(w.member, env, "init", "-q"))
	if err := os.WriteFile(filepath.Join(w.member, "README.md"), []byte("member\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	must(gitIn(w.member, env, "add", "-A"))
	must(gitIn(w.member, env, "-c", "user.name=parity", "-c", "user.email=parity@example.com", "commit", "-q", "-m", "base"))
	switch c.Origin {
	case "", "bare":
		must(gitIn(w.root, env, "init", "-q", "--bare", w.origin))
		must(gitIn(w.member, env, "remote", "add", "origin", w.origin))
	case "unreachable":
		must(gitIn(w.member, env, "remote", "add", "origin", filepath.Join(w.root, "nowhere.git")))
	case "none":
	default:
		t.Fatalf("origin %q: want bare, none or unreachable", c.Origin)
	}
	return w
}

// slug is the projects directory Claude Code names for a launch path.
func slug(p string) string { return regexp.MustCompile(`[^A-Za-z0-9]`).ReplaceAllString(p, "-") }

func (w growthWorld) transcriptPath(session string) string {
	return filepath.Join(w.home, ".claude", "projects", slug(w.member), session+".jsonl")
}

func concat(dir string, parts []string) ([]byte, error) {
	var b bytes.Buffer
	for _, p := range parts {
		raw, err := os.ReadFile(filepath.Join(dir, p))
		if err != nil {
			return nil, err
		}
		b.Write(raw)
	}
	return b.Bytes(), nil
}

func (w growthWorld) lay(dir, session string, step GrowthStep) error {
	if len(step.Transcript) == 0 {
		return nil
	}
	tp := w.transcriptPath(session)
	raw, err := concat(dir, step.Transcript)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(tp), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tp, raw, 0o644); err != nil {
		return err
	}
	for rel, parts := range step.Sidechains {
		raw, err := concat(dir, parts)
		if err != nil {
			return err
		}
		p := filepath.Join(filepath.Dir(tp), rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, raw, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (w growthWorld) expand(s, session string) string {
	s = strings.ReplaceAll(s, "{transcript}", w.transcriptPath(session))
	return strings.ReplaceAll(s, "{root}", w.root)
}

// branch reads the conversation-logs branch off the origin: each file by
// masked name, and the commit subjects oldest first. A nil map is no
// branch.
func (w growthWorld) branch(c GrowthCase) (map[string]string, []string, error) {
	if c.Origin != "" && c.Origin != "bare" {
		return nil, nil, nil
	}
	env := w.env(c)
	heads, err := gitIn(w.origin, env, "for-each-ref", "--format=%(refname)", "refs/heads/conversation-logs")
	if err != nil || strings.TrimSpace(heads) == "" {
		return nil, nil, err
	}
	names, err := gitIn(w.origin, env, "ls-tree", "--name-only", "conversation-logs")
	if err != nil {
		return nil, nil, err
	}
	files := map[string]string{}
	for _, n := range strings.Split(strings.TrimSpace(names), "\n") {
		body, err := gitIn(w.origin, env, "show", "conversation-logs:"+n)
		if err != nil {
			return nil, nil, err
		}
		files[w.mask(n)] = w.mask(body)
	}
	log, err := gitIn(w.origin, env, "log", "--reverse", "--format=%s", "conversation-logs")
	if err != nil {
		return nil, nil, err
	}
	var subjects []string
	for _, s := range strings.Split(strings.TrimSpace(log), "\n") {
		subjects = append(subjects, w.mask(s))
	}
	return files, subjects, nil
}

// cleanRun masks a run and drops cn's own breadcrumb lines, which the
// Node engine never printed.
func (w growthWorld) cleanRun(r GrowthRun) GrowthRun {
	var errs []string
	for _, l := range strings.Split(r.Stderr, "\n") {
		if strings.HasPrefix(l, "[cn] ") || l == "" {
			continue
		}
		errs = append(errs, l)
	}
	r.Stdout, r.Stderr = w.mask(r.Stdout), w.mask(strings.Join(errs, "\n"))
	return r
}

// waitForFreshMinute keeps a same-minute case off a minute boundary.
func waitForFreshMinute() {
	if s := time.Now().Second(); s >= 50 {
		time.Sleep(time.Duration(61-s) * time.Second)
	}
}

func loadGrowthScenarios(t *testing.T, group string) []string {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join("testdata", "scenarios", group, "*", "case.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) == 0 {
		t.Fatalf("no scenarios under testdata/scenarios/%s", group)
	}
	for i := range dirs {
		dirs[i] = filepath.Dir(dirs[i])
	}
	sort.Strings(dirs)
	return dirs
}

func readGrowthCase(t *testing.T, dir string) (GrowthCase, *GrowthExpect) {
	t.Helper()
	var c GrowthCase
	raw, err := os.ReadFile(filepath.Join(dir, "case.json"))
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	raw, err = os.ReadFile(filepath.Join(dir, "expect.json"))
	if err != nil {
		return c, nil
	}
	var x GrowthExpect
	dec = json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&x); err != nil {
		t.Fatalf("%s expect.json: %v", dir, err)
	}
	if (x.Divergence == "") != (x.Cn == nil) {
		t.Fatalf("%s: a divergence names its record row and carries cn's answer, both or neither", dir)
	}
	if x.Divergence != "" && !divergenceForm.MatchString(x.Divergence) {
		t.Fatalf("%s: divergence %q is not record-<row>", dir, x.Divergence)
	}
	return c, &x
}

func recordGrowth(t *testing.T, dir string, got GrowthExpect, old *GrowthExpect) {
	t.Helper()
	if old != nil {
		got.Divergence, got.Cn = old.Divergence, old.Cn
	}
	raw, err := jsonIndent(got)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "expect.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func compareGrowth(t *testing.T, dir string, e Engine, got GrowthExpect, want *GrowthExpect) {
	t.Helper()
	if want == nil {
		t.Fatalf("%s has no expect.json; record it against the Node engine first (%s=1)", dir, recordEnv)
	}
	w := *want
	if e.Name() == "cn" && want.Divergence != "" {
		w = *want.Cn
		if node := stripDivergence(*want); reflect.DeepEqual(got, node) {
			t.Errorf("%s is marked %s but cn now gives the Node answer: drop the divergence", dir, want.Divergence)
		}
	}
	w = stripDivergence(w)
	var g, x any
	if err := json.Unmarshal(mustJSON(got), &g); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(mustJSON(w), &x); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g, x) {
		t.Errorf("%s disagrees:\n%s", e.Name(), strings.Join(jsonDiff("$", g, x, 30), "\n"))
	}
}

func stripDivergence(x GrowthExpect) GrowthExpect {
	x.Divergence, x.Cn = "", nil
	return x
}

func runGrowthFace(t *testing.T, group string, play func(t *testing.T, dir string, c GrowthCase, e Engine) GrowthExpect) {
	es := engines(t)
	recording := os.Getenv(recordEnv) == "1"
	for _, dir := range loadGrowthScenarios(t, group) {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			c, want := readGrowthCase(t, dir)
			for _, e := range es {
				got := play(t, dir, c, e)
				if recording && e.Name() == "node" {
					recordGrowth(t, dir, got, want)
					continue
				}
				compareGrowth(t, dir, e, got, want)
			}
		})
	}
}

func TestParityGrowthCapture(t *testing.T) {
	runGrowthFace(t, "growth-capture", func(t *testing.T, dir string, c GrowthCase, e Engine) GrowthExpect {
		w := newGrowthWorld(t, c)
		session := c.Session
		if session == "" {
			session = "sess-parity"
		}
		if c.SameMinute {
			waitForFreshMinute()
		}
		var x GrowthExpect
		for _, step := range c.Steps {
			if err := w.lay(dir, session, step); err != nil {
				t.Fatal(err)
			}
			args := make([]string, len(step.Args))
			for i, a := range step.Args {
				args[i] = w.expand(a, session)
			}
			var r GrowthRun
			var err error
			switch e := e.(type) {
			case Node:
				r, err = runEnv(w.member, w.env(c), "", "node", append([]string{filepath.Join(e.Root, "packs/claudinite-growth/capture-log.mjs")}, args...)...)
			case Cn:
				r, err = runEnv(w.member, w.env(c), "", e.Binary, append([]string{"growth", "capture"}, args...)...)
			}
			if err != nil {
				t.Fatal(err)
			}
			x.Runs = append(x.Runs, w.cleanRun(r))
		}
		files, subjects, err := w.branch(c)
		if err != nil {
			t.Fatal(err)
		}
		x.Branch, x.Subjects = files, subjects
		return x
	})
}

func TestParitySessionEnd(t *testing.T) {
	runGrowthFace(t, "session-end", func(t *testing.T, dir string, c GrowthCase, e Engine) GrowthExpect {
		w := newGrowthWorld(t, c)
		session := c.Session
		if session == "" {
			session = "sess-end"
		}
		for _, step := range c.Steps {
			if err := w.lay(dir, session, step); err != nil {
				t.Fatal(err)
			}
		}
		payload := w.expand(c.Payload, session)
		var r GrowthRun
		var err error
		env := w.env(c, "CLAUDE_PROJECT_DIR="+w.member)
		switch e := e.(type) {
		case Node:
			if c.Declared != nil {
				raw, _ := json.Marshal(map[string]any{"packs": *c.Declared})
				if err := os.WriteFile(filepath.Join(w.member, ".claudinite-settings.json"), raw, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			r, err = runEnv(w.member, env, payload, "node", filepath.Join(e.Root, "engine/hooks/session-end-command.mjs"))
		case Cn:
			if c.Declared != nil {
				var b strings.Builder
				fmt.Fprintf(&b, "engine:\n  version: \"0.0.0\"\n  manifest: %q\npacks:\n  declared:\n", DevManifest)
				for _, id := range *c.Declared {
					fmt.Fprintf(&b, "    - %s\n", id)
				}
				if len(*c.Declared) == 0 {
					b.Reset()
					fmt.Fprintf(&b, "engine:\n  version: \"0.0.0\"\n  manifest: %q\n", DevManifest)
				}
				if err := os.MkdirAll(filepath.Join(w.member, ".claudinite"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(w.member, ".claudinite/settings.yaml"), []byte(b.String()), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			r, err = runEnv(w.member, env, payload, e.Binary, "hook", "session-end")
			if err == nil && r.Stdout != "{}\n" {
				t.Errorf("cn hook session-end answered %q, want {}", r.Stdout)
			}
			// The hook's answer is Claude Code's protocol, which the
			// Node runner never printed; the exit code is compared.
			r.Stdout = ""
		}
		if err != nil {
			t.Fatal(err)
		}
		files, subjects, err := w.branch(c)
		if err != nil {
			t.Fatal(err)
		}
		// What a failing capture says is each runner's own log line;
		// the session must end either way.
		return GrowthExpect{Runs: []GrowthRun{{Exit: r.Exit}}, Branch: files, Subjects: subjects}
	})
}

// growthCommitEnv fixes a provenance scenario's commits, so both engines'
// repositories carry the same hashes and dates.
func growthCommitEnv(env []string, n int) []string {
	at := fmt.Sprintf("2026-07-%02dT12:00:00Z", n+1)
	return append(env, "GIT_AUTHOR_NAME=parity", "GIT_AUTHOR_EMAIL=parity@example.com", "GIT_COMMITTER_NAME=parity",
		"GIT_COMMITTER_EMAIL=parity@example.com", "GIT_AUTHOR_DATE="+at, "GIT_COMMITTER_DATE="+at)
}

func writeFiles(root string, files map[string]string) error {
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// treeFiles reads every file under root but .git.
func treeFiles(root string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out[filepath.ToSlash(rel)] = string(raw)
		return nil
	})
	return out, err
}

// growthRepo lays a provenance-shaped case's commits, each dated by its
// position, and its working files over the last.
func growthRepo(t *testing.T, c GrowthCase) (growthWorld, string, []string) {
	t.Helper()
	w := newGrowthWorld(t, GrowthCase{Origin: "none"})
	repo := filepath.Join(w.root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	env := w.env(c)
	if _, err := gitIn(repo, env, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	for i, cm := range c.Commits {
		if err := writeFiles(repo, cm.Files); err != nil {
			t.Fatal(err)
		}
		for _, rm := range cm.Remove {
			if err := os.RemoveAll(filepath.Join(repo, rm)); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := gitIn(repo, env, "add", "-A"); err != nil {
			t.Fatal(err)
		}
		cenv := growthCommitEnv(env, i)
		if cm.Author != "" {
			cenv = append(cenv, "GIT_AUTHOR_EMAIL="+cm.Author)
		}
		if _, err := gitIn(repo, cenv, "commit", "-q", "--allow-empty", "-m", cm.Message); err != nil {
			t.Fatal(err)
		}
	}
	if c.Remote != "" {
		if _, err := gitIn(repo, env, "remote", "add", "origin", c.Remote); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeFiles(repo, c.Working); err != nil {
		t.Fatal(err)
	}
	return w, repo, env
}

func TestParityProvenance(t *testing.T) {
	runGrowthFace(t, "provenance", func(t *testing.T, dir string, c GrowthCase, e Engine) GrowthExpect {
		w, repo, env := growthRepo(t, c)
		args := c.Steps[0].Args
		var r GrowthRun
		var err error
		switch e := e.(type) {
		case Node:
			r, err = runEnv(repo, append(env, "CLAUDE_PROJECT_DIR="+repo), c.Stdin, "node", append([]string{filepath.Join(e.Root, "packs/claudinite-growth/provenance.mjs")}, args...)...)
		case Cn:
			r, err = runEnv(repo, env, c.Stdin, e.Binary, append([]string{"provenance"}, args...)...)
		}
		if err != nil {
			t.Fatal(err)
		}
		files, err := treeFiles(repo)
		if err != nil {
			t.Fatal(err)
		}
		return GrowthExpect{Runs: []GrowthRun{w.cleanRun(r)}, Files: files}
	})
}
