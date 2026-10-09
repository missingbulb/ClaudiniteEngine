package capture

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/breadcrumb"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/gitcmd"
)

// Request is one capture: its key, and what discovery may be told.
type Request struct {
	Key Key
	// Transcript and Session are set when given; "" given is a value.
	Transcript, Session *string
	Branch              string
	// Dir is where the repository is found.
	Dir string
}

// UsageText is the sentence a request with no single valid key gets.
const UsageText = "required: exactly one of --pr <n> (the pull request the merge landed) or --issue <n> (the issue an unmerged capture is about, or 0 for none)"

var (
	prArg    = regexp.MustCompile(`^[1-9]\d*$`)
	issueArg = regexp.MustCompile(`^(0|[1-9]\d*)$`)
)

// ParseArgs reads `(--pr N | --issue N) [--transcript P] [--session ID]
// [--branch B] [--repo DIR]` as the Node tool read its flags: each --key
// takes the next argument, a flag with nothing after it is absent, and a
// bare word is skipped. False when there is not exactly one valid key.
func ParseArgs(args []string, dir string) (Request, bool) {
	vals := map[string]string{}
	for i := 0; i < len(args); i++ {
		if !strings.HasPrefix(args[i], "--") {
			continue
		}
		if i+1 < len(args) {
			vals[args[i][2:]] = args[i+1]
		} else {
			delete(vals, args[i][2:])
		}
		i++
	}
	pr, hasPr := vals["pr"]
	issue, hasIssue := vals["issue"]
	valid := hasPr && prArg.MatchString(pr) || hasIssue && issueArg.MatchString(issue)
	if hasPr == hasIssue || !valid {
		return Request{}, false
	}
	req := Request{Branch: DefaultBranch, Dir: dir}
	if hasPr {
		n, err := strconv.Atoi(pr)
		if err != nil {
			return Request{}, false
		}
		req.Key.PR = &n
	} else {
		n, err := strconv.Atoi(issue)
		if err != nil {
			return Request{}, false
		}
		req.Key.Issue = &n
	}
	if t, ok := vals["transcript"]; ok {
		req.Transcript = &t
	}
	if s, ok := vals["session"]; ok {
		req.Session = &s
	}
	if b, ok := vals["branch"]; ok {
		req.Branch = b
	}
	if d, ok := vals["repo"]; ok {
		req.Dir = d
	}
	return req, true
}

// Env is what a capture reads from its process.
type Env struct {
	Getenv  func(string) string
	Environ []string
	Home    string
	Now     func() time.Time
	// Backoff scales the wait between attempts (attempt N waits N times
	// it).
	Backoff time.Duration
}

// FromProcess is the capture's environment from this process, the
// backoff from CLAUDINITE_CAPTURE_BACKOFF_MS where a test sets it.
func FromProcess() Env {
	home, _ := os.UserHomeDir()
	e := Env{Getenv: os.Getenv, Environ: os.Environ(), Home: home, Now: time.Now, Backoff: 2 * time.Second}
	if ms, err := strconv.Atoi(os.Getenv("CLAUDINITE_CAPTURE_BACKOFF_MS")); err == nil && ms >= 0 {
		e.Backoff = time.Duration(ms) * time.Millisecond
	}
	return e
}

// Outcome is how a capture ended: OK wrote a file, Skip had nothing new,
// Error did not capture.
type Outcome string

const (
	OK    Outcome = "ok"
	Skip  Outcome = "skip"
	Error Outcome = "error"
)

// Crumb is the outcome as the growth capture's breadcrumb reports it.
func (o Outcome) Crumb() breadcrumb.Outcome {
	switch o {
	case OK:
		return breadcrumb.OK
	case Skip:
		return breadcrumb.Skip
	}
	return breadcrumb.Error
}

// Result is a capture's outcome, its exit code and the sentences it said.
type Result struct {
	Outcome Outcome
	Code    int
}

// Run captures the session the request names onto its branch, printing
// Node's sentences: the capture on stdout, a failure on stderr.
func Run(req Request, env Env, stdout, stderr io.Writer) Result {
	fail := func(code int, msg string) Result {
		fmt.Fprintln(stderr, msg)
		return Result{Error, code}
	}
	git := gitcmd.Repo{Dir: req.Dir}
	top, err := git.Run("rev-parse", "--show-toplevel")
	if err != nil {
		return fail(1, err.Error())
	}
	if top.Code != 0 {
		return fail(1, "git rev-parse --show-toplevel failed: "+strings.TrimSpace(firstNonEmpty(top.Stderr, top.Stdout)))
	}
	root := strings.TrimSpace(top.Stdout)
	projects := ProjectsRoot(env.Getenv, env.Home)
	session, hasSession := "", false
	if req.Session != nil {
		session, hasSession = *req.Session, true
	} else if v, ok := lookup(env.Environ, "CLAUDE_CODE_SESSION_ID"); ok {
		session, hasSession = v, true
	}
	transcript := ""
	if req.Transcript != nil {
		transcript = *req.Transcript
	} else {
		transcript = FindTranscript(root, session, projects)
	}
	if transcript == "" || !exists(transcript) {
		if req.Transcript != nil && *req.Transcript != "" {
			return fail(1, "no session transcript found at "+*req.Transcript)
		}
		shown := "(unknown)"
		if hasSession {
			shown = session
		}
		return fail(1, fmt.Sprintf("no session transcript found for session %s under %s", shown, projects))
	}
	resolved := strings.TrimSuffix(filepath.Base(transcript), ".jsonl")
	if filepath.Base(transcript) == ".jsonl" {
		resolved = ".jsonl"
	}
	if req.Session != nil {
		resolved = *req.Session
	}
	files := append([]string{transcript}, Sidechains(transcript, resolved)...)
	var streams [][]Line
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return fail(1, err.Error())
		}
		streams = append(streams, ParseLines(validText(raw)))
	}
	bundled := Bundle(streams)
	if len(bundled) == 0 {
		return fail(1, fmt.Sprintf("transcript %s holds no parseable entries", transcript))
	}
	extra := credentialStore(env)
	got, err := Push(Target{Root: root, Branch: req.Branch, Session: resolved, Key: req.Key, Backoff: env.Backoff},
		bundled, env.Now(), Redactions(env.Environ, extra))
	if err != nil {
		return fail(1, err.Error())
	}
	if got.Name == "" {
		fmt.Fprintf(stdout, "nothing new to capture — session %s already captured through %s\n", resolved, got.LastTs)
		return Result{Skip, 0}
	}
	delta := ""
	if got.LastTs != "" {
		delta = " (delta since " + got.LastTs + ")"
	}
	fmt.Fprintf(stdout, "captured %d entries%s → %s on %s\n", got.Entries, delta, got.Name, req.Branch)
	return Result{OK, 0}
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func lookup(environ []string, key string) (string, bool) {
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			return v, true
		}
	}
	return "", false
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// credentialStore is what Claude Code's credential store holds, read
// under CLAUDE_CONFIG_DIR and under ~/.claude where the two differ: a
// value redacted once too often costs nothing.
func credentialStore(env Env) []Value {
	var out []Value
	seen := map[string]bool{}
	for _, dir := range []string{ConfigDir(env.Getenv, env.Home), filepath.Join(env.Home, ".claude")} {
		p := filepath.Join(dir, ".credentials.json")
		if seen[p] {
			continue
		}
		seen[p] = true
		if raw, err := os.ReadFile(p); err == nil {
			out = append(out, CredentialValues(raw)...)
		}
	}
	return out
}

// Target is where a capture lands.
type Target struct {
	Root, Branch, Session string
	Key                   Key
	Backoff               time.Duration
}

// Pushed is a capture's write: the file's name ("" when there was no
// delta), the latest timestamp already captured for the session ("" for
// none) and how many lines the file holds.
type Pushed struct {
	Name, LastTs string
	Entries      int
}

// BranchReadme is the README a branch's first capture writes.
const BranchReadme = `# conversation-logs

Captured working-session conversations (one JSONL per merge), pushed by the
claudinite-growth pack's capture step and consumed by the conversation half of
its growth-extract scheduled task. An orphan work-queue branch: never merged, files
deleted by the retention prune once extracted and aged out. See the pack README
(packs/claudinite-growth/README.md in the Claudinite canon) for the standard.
`

// Attempts is how many times a capture tries the remote.
const Attempts = 3

// gitError is the first line a failed command said.
func gitError(r gitcmd.Ran) string {
	s := strings.TrimSpace(firstNonEmpty(r.Stderr, r.Stdout))
	if s == "" {
		return fmt.Sprintf("exit %d", r.Code)
	}
	return strings.SplitN(s, "\n", 2)[0]
}

// must runs a plumbing command that has no reason to fail; a failure is a
// bug, not weather, and ends the capture.
func must(git gitcmd.Repo, stdin string, args ...string) (string, error) {
	r, err := git.RunInput(stdin, args...)
	if err != nil {
		return "", err
	}
	if r.Code != 0 {
		return "", fmt.Errorf("git %s failed: %s", strings.Join(args, " "), strings.TrimSpace(firstNonEmpty(r.Stderr, r.Stdout)))
	}
	return r.Stdout, nil
}

// identity is the commit identity: the one configured, else a fixed
// capture identity so a bare automation environment still commits. The
// configured one is read past the bot identity every git child is given
// on its command line.
func identity(git gitcmd.Repo) []string {
	email := configured(git, "user.email")
	if email == "" {
		return []string{"-c", "user.name=claudinite-capture", "-c", "user.email=capture@claudinite"}
	}
	return []string{"-c", "user.name=" + configured(git, "user.name"), "-c", "user.email=" + email}
}

// configured is key's value from the repository's own configuration
// files, the last one read winning; "" for none.
func configured(git gitcmd.Repo, key string) string {
	r, err := git.Run("config", "--show-scope", "--get-all", key)
	if err != nil || r.Code != 0 {
		return ""
	}
	v := ""
	for _, l := range strings.Split(r.Stdout, "\n") {
		scope, val, ok := strings.Cut(l, "\t")
		if ok && scope != "command" {
			v = val
		}
	}
	return v
}

// tip is the remote branch's tip, fetched: "" for a branch that does not
// exist yet, and unreachable set when the remote did not answer, which
// costs the attempt like a lost push.
func tip(git gitcmd.Repo, branch string) (string, *gitcmd.Ran, error) {
	ls, err := git.Run("ls-remote", "--heads", "origin", branch)
	if err != nil {
		return "", nil, err
	}
	if ls.Code != 0 {
		return "", &ls, nil
	}
	if strings.TrimSpace(ls.Stdout) == "" {
		return "", nil, nil
	}
	f, err := git.Run("fetch", "--quiet", "origin", branch)
	if err != nil {
		return "", nil, err
	}
	if f.Code != 0 {
		return "", &f, nil
	}
	out, err := must(git, "", "rev-parse", "FETCH_HEAD")
	return strings.TrimSpace(out), nil, err
}

// Push writes the delta of bundled after everything already captured for
// the session as one new file on the branch. The whole read-build-push
// runs inside the attempt, so a lost race recomputes against the fresh
// tip instead of clobbering it; the checkout and its index are never
// touched.
func Push(t Target, bundled []Bundled, now time.Time, redactions []Redaction) (Pushed, error) {
	git := gitcmd.Repo{Dir: t.Root}
	lastErr := ""
	wait := func(attempt int) {
		if attempt < Attempts {
			time.Sleep(time.Duration(attempt) * t.Backoff)
		}
	}
	for attempt := 1; attempt <= Attempts; attempt++ {
		head, down, err := tip(git, t.Branch)
		if err != nil {
			return Pushed{}, err
		}
		if down != nil {
			lastErr = "could not reach origin: " + gitError(*down)
			wait(attempt)
			continue
		}
		var treeLines, names []string
		if head != "" {
			out, err := must(git, "", "ls-tree", head)
			if err != nil {
				return Pushed{}, err
			}
			for _, l := range strings.Split(out, "\n") {
				if l == "" {
					continue
				}
				treeLines = append(treeLines, l)
				name := ""
				if parts := strings.SplitN(l, "\t", 3); len(parts) > 1 {
					name = parts[1]
				}
				names = append(names, name)
			}
		}
		lastTs := ""
		for _, n := range names {
			if !strings.HasSuffix(n, "--"+t.Session+".jsonl") {
				continue
			}
			text, err := must(git, "", "show", head+":"+n)
			if err != nil {
				return Pushed{}, err
			}
			if m := MaxTimestamp(Bundle([][]Line{ParseLines(text)})); m != "" && m > lastTs {
				lastTs = m
			}
		}
		delta := SliceAfter(bundled, lastTs)
		if len(delta) == 0 {
			return Pushed{LastTs: lastTs}, nil
		}
		name := LogFilename(now, t.Key, t.Session)
		for k := 2; has(names, name); k++ {
			name = strings.Replace(LogFilename(now, t.Key, t.Session), "Z--", "Z-"+strconv.Itoa(k)+"--", 1)
		}
		var b strings.Builder
		for i, l := range delta {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(Scrub(l.Raw, redactions))
		}
		b.WriteString("\n")
		blob, err := must(git, b.String(), "hash-object", "-w", "--stdin")
		if err != nil {
			return Pushed{}, err
		}
		lines := append(append([]string{}, treeLines...), "100644 blob "+strings.TrimSpace(blob)+"\t"+name)
		if head == "" && !has(names, "README.md") {
			readme, err := must(git, BranchReadme, "hash-object", "-w", "--stdin")
			if err != nil {
				return Pushed{}, err
			}
			lines = append(lines, "100644 blob "+strings.TrimSpace(readme)+"\tREADME.md")
		}
		tree, err := must(git, strings.Join(lines, "\n")+"\n", "mktree")
		if err != nil {
			return Pushed{}, err
		}
		args := append(identity(git), "commit-tree", strings.TrimSpace(tree))
		if head != "" {
			args = append(args, "-p", head)
		}
		args = append(args, "-m", "capture conversation log "+name+" [skip ci]")
		commit, err := must(git, "", args...)
		if err != nil {
			return Pushed{}, err
		}
		push, err := git.Run("push", "--quiet", "origin", strings.TrimSpace(commit)+":refs/heads/"+t.Branch)
		if err != nil {
			return Pushed{}, err
		}
		if push.Code == 0 {
			return Pushed{Name: name, LastTs: lastTs, Entries: len(delta)}, nil
		}
		lastErr = "push rejected: " + gitError(push)
		wait(attempt)
	}
	return Pushed{}, errors.New("could not capture to " + t.Branch + " after 3 attempts — " + lastErr)
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
