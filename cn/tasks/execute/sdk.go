package execute

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/world"
)

// GitHubActions are the named GitHub actions the SDK answers. A pack
// declares the ones its scripts take in its manifest; the member may
// narrow that grant in the pack's settings entry.
var GitHubActions = []string{"openPr", "createComment", "readFile", "listIssues", "dispatchWorkflow", "findOrCreateTracker", "writeTracker"}

// GrantedActions is a pack's grant: what it declared, narrowed to the
// member's own list when the member wrote one.
func GrantedActions(declared []string, memberConfig map[string]any) []string {
	out := []string{}
	raw, narrowed := memberConfig["githubActions"]
	allowed := map[string]bool{}
	if narrowed {
		list, _ := raw.([]any)
		for _, v := range list {
			if s, ok := v.(string); ok {
				allowed[s] = true
			}
		}
	}
	for _, a := range declared {
		if !narrowed || allowed[a] {
			out = append(out, a)
		}
	}
	return out
}

// SDKGitHub is the repository the SDK's GitHub actions reach, under the
// job's token.
type SDKGitHub interface {
	world.Issues
	CreatePull(title, body, head, base string) (world.Pull, error)
	DispatchWorkflow(name, ref string) error
	// FileAt is a file's text at a ref; world.ErrGone when it has none.
	FileAt(path, ref string) (string, error)
}

// PackInfo is one declared pack as packs() answers it.
type PackInfo struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

// SDK answers one task script's calls back to the engine. Opened are the
// pull requests the script opened, the run's delivery.
type SDK struct {
	Pack, Task    string
	Granted       []string
	Git           func(args ...string) (gitcmd.Ran, error)
	GitHub        SDKGitHub
	DefaultBranch string
	Config        func(pack string) map[string]any
	Packs         []PackInfo
	Log           func(string)
	Opened        []world.Pull
}

// Methods announces every call the SDK knows, so a call outside the grant
// reaches Handle and is refused there, logged, rather than turned away
// unannounced.
func (s *SDK) Methods() []string {
	out := []string{"git", "config", "packs"}
	for _, a := range GitHubActions {
		out = append(out, "github."+a)
	}
	return out
}

func (s *SDK) log(line string) {
	if s.Log != nil {
		s.Log("sdk " + s.Pack + "/" + s.Task + ": " + line)
	}
}

func (s *SDK) granted(action string) bool {
	for _, a := range s.Granted {
		if a == action {
			return true
		}
	}
	return false
}

func decode[T any](args json.RawMessage) (T, error) {
	var v T
	if len(args) == 0 {
		return v, nil
	}
	if err := json.Unmarshal(args, &v); err != nil {
		return v, fmt.Errorf("its arguments do not read: %v", err)
	}
	return v, nil
}

// Handle answers one call.
func (s *SDK) Handle(method string, args json.RawMessage) (any, error) {
	switch method {
	case "git":
		return s.git(args)
	case "config":
		a, err := decode[struct {
			Pack string `json:"pack"`
		}](args)
		if err != nil {
			return nil, err
		}
		cfg := map[string]any{}
		if s.Config != nil {
			if c := s.Config(a.Pack); c != nil {
				cfg = c
			}
		}
		return cfg, nil
	case "packs":
		if s.Packs == nil {
			return []PackInfo{}, nil
		}
		return s.Packs, nil
	}
	action, ok := strings.CutPrefix(method, "github.")
	if !ok {
		return nil, fmt.Errorf("%s is not a call this engine answers", method)
	}
	known := false
	for _, a := range GitHubActions {
		known = known || a == action
	}
	if !known {
		return nil, fmt.Errorf("github.%s is not a GitHub action this engine knows", action)
	}
	if !s.granted(action) {
		s.log("refused github." + action + " — the pack " + s.Pack + " is not granted it")
		return nil, fmt.Errorf("github.%s is not granted to the pack %s: declare it in the pack's githubActions", action, s.Pack)
	}
	if s.GitHub == nil {
		return nil, errors.New("this run has no GitHub to reach")
	}
	out, err := s.github(action, args)
	if err != nil {
		s.log("github." + action + " failed: " + err.Error())
		return nil, err
	}
	return out, nil
}

func (s *SDK) git(args json.RawMessage) (any, error) {
	a, err := decode[struct {
		Args []string `json:"args"`
	}](args)
	if err != nil {
		return nil, err
	}
	if s.Git == nil {
		return nil, errors.New("this run has no git to reach")
	}
	if why := pushRefusal(a.Args, s.DefaultBranch); why != "" {
		s.log("refused git push — " + why)
		return nil, errors.New("git push refused: " + why)
	}
	ran, err := s.Git(a.Args...)
	if err != nil {
		return nil, err
	}
	return map[string]any{"code": ran.Code, "stdout": ran.Stdout, "stderr": ran.Stderr}, nil
}

// gitGlobalWithValue are git's options before the subcommand that take
// the next argument as their value.
var gitGlobalWithValue = map[string]bool{"-c": true, "-C": true, "--git-dir": true, "--work-tree": true, "--namespace": true, "--exec-path": true, "--config-env": true}

// pushWithValue are push's options that take the next argument as their
// value, so it is no repository or refspec.
var pushWithValue = map[string]bool{"-o": true, "--push-option": true, "--receive-pack": true, "--exec": true, "--repo": true}

// pushRefusal is why a script's git call may not run, "" when it may: a
// push may not rewrite or delete the default branch, nor push every ref
// at once, whatever refs the task otherwise pushes. A forced or deleting
// refspec must name its destination plainly, since git resolves HEAD,
// heads/<b> and revision syntax to refs the script never spelled.
func pushRefusal(args []string, defaultBranch string) string {
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		opt, val, joined := strings.Cut(args[i], "=")
		if gitGlobalWithValue[opt] && !joined && i+1 < len(args) {
			val = args[i+1]
			i++
		}
		if (opt == "-c" || opt == "--config-env") && strings.HasPrefix(strings.ToLower(val), "alias.") {
			return "a git alias set on the command line hides which command runs"
		}
		i++
	}
	if i >= len(args) {
		return ""
	}
	switch args[i] {
	case "send-pack", "receive-pack":
		return args[i] + " updates remote refs past the push checks"
	case "push":
	default:
		return ""
	}
	force, deletes := false, false
	var positional []string
	rest := args[i+1:]
	for j := 0; j < len(rest); j++ {
		a := rest[j]
		switch {
		case a == "--mirror" || a == "--all" || a == "--prune":
			return a + " pushes refs beyond the task's own"
		case a == "--force" || strings.HasPrefix(a, "--force-with-lease") || a == "--force-if-includes":
			force = true
		case a == "--delete":
			deletes = true
		case pushWithValue[a]:
			j++
		case strings.HasPrefix(a, "--"):
		case strings.HasPrefix(a, "-"):
			force = force || strings.Contains(a, "f")
			deletes = deletes || strings.Contains(a, "d")
		default:
			positional = append(positional, a)
		}
	}
	if len(positional) < 2 {
		if force || deletes {
			return "a forced or deleting push names its refspec, so it can be seen to spare " + defaultBranch
		}
		return ""
	}
	for _, spec := range positional[1:] {
		plus := strings.HasPrefix(spec, "+")
		spec = strings.TrimPrefix(spec, "+")
		src, dst, paired := strings.Cut(spec, ":")
		if !paired {
			dst = src
		}
		deleting := deletes || (paired && src == "")
		if !force && !plus && !deleting {
			continue
		}
		branch, ok := plainBranch(dst)
		switch {
		case !ok:
			return "a forced or deleting push names its refspec, so it can be seen to spare " + defaultBranch + ": " + dst + " is no plain branch"
		case branch != defaultBranch:
		case deleting:
			return "it deletes the default branch " + defaultBranch
		default:
			return "it force-pushes the default branch " + defaultBranch
		}
	}
	return ""
}

// plainBranch is the branch a refspec destination names when it is
// refs/heads/<b> or a bare <b> git cannot resolve to anything else.
func plainBranch(dst string) (string, bool) {
	b, full := strings.CutPrefix(dst, "refs/heads/")
	if !full {
		for _, p := range []string{"refs/", "heads/", "tags/", "remotes/"} {
			if strings.HasPrefix(dst, p) {
				return "", false
			}
		}
	}
	if b == "" || b == "@" || strings.HasSuffix(b, "HEAD") || strings.ContainsAny(b, "^~*:?[\\ ") || strings.Contains(b, "@{") || strings.Contains(b, "..") {
		return "", false
	}
	return b, true
}

func (s *SDK) github(action string, args json.RawMessage) (any, error) {
	switch action {
	case "openPr":
		a, err := decode[struct {
			Title string `json:"title"`
			Body  string `json:"body"`
			Head  string `json:"head"`
			Base  string `json:"base"`
		}](args)
		if err != nil {
			return nil, err
		}
		if a.Title == "" || a.Head == "" {
			return nil, errors.New("openPr needs a title and a head branch")
		}
		if a.Base == "" {
			a.Base = s.DefaultBranch
		}
		p, err := s.GitHub.CreatePull(a.Title, a.Body, a.Head, a.Base)
		if err != nil {
			return nil, err
		}
		s.Opened = append(s.Opened, p)
		s.log(fmt.Sprintf("github.openPr #%d from %s", p.Number, a.Head))
		return map[string]any{"number": p.Number, "headRef": p.HeadRef, "nodeId": p.NodeID}, nil
	case "createComment":
		a, err := decode[struct {
			Issue int    `json:"issue"`
			Body  string `json:"body"`
		}](args)
		if err != nil {
			return nil, err
		}
		id, err := s.GitHub.Comment(a.Issue, a.Body)
		if err != nil {
			return nil, err
		}
		s.log(fmt.Sprintf("github.createComment on #%d", a.Issue))
		return map[string]any{"id": id}, nil
	case "readFile":
		a, err := decode[struct {
			Path string `json:"path"`
			Ref  string `json:"ref"`
		}](args)
		if err != nil {
			return nil, err
		}
		if a.Ref == "" {
			a.Ref = s.DefaultBranch
		}
		text, err := s.GitHub.FileAt(a.Path, a.Ref)
		if errors.Is(err, world.ErrGone) {
			s.log("github.readFile " + a.Path + " at " + a.Ref + ": absent")
			return map[string]any{"content": nil}, nil
		}
		if err != nil {
			return nil, err
		}
		s.log("github.readFile " + a.Path + " at " + a.Ref)
		return map[string]any{"content": text}, nil
	case "listIssues":
		a, err := decode[struct {
			State string `json:"state"`
			Label string `json:"label"`
		}](args)
		if err != nil {
			return nil, err
		}
		if a.State == "" {
			a.State = "open"
		}
		issues, err := s.listIssues(world.Query{State: a.State, Label: a.Label})
		if err != nil {
			return nil, err
		}
		out := make([]map[string]any, 0, len(issues))
		for _, i := range issues {
			labels := []string(i.Labels)
			if labels == nil {
				labels = []string{}
			}
			out = append(out, map[string]any{"number": i.Number, "title": i.Title, "state": i.State, "labels": labels, "body": i.Body})
		}
		s.log(fmt.Sprintf("github.listIssues: %d", len(out)))
		return out, nil
	case "dispatchWorkflow":
		a, err := decode[struct {
			Workflow string `json:"workflow"`
			Ref      string `json:"ref"`
		}](args)
		if err != nil {
			return nil, err
		}
		if a.Ref == "" {
			a.Ref = s.DefaultBranch
		}
		if err := s.GitHub.DispatchWorkflow(a.Workflow, a.Ref); err != nil {
			return nil, err
		}
		s.log("github.dispatchWorkflow " + a.Workflow + " on " + a.Ref)
		return map[string]any{"ok": true}, nil
	case "findOrCreateTracker":
		a, err := decode[struct {
			Title string `json:"title"`
		}](args)
		if err != nil {
			return nil, err
		}
		return s.findOrCreateTracker(strings.TrimSpace(a.Title))
	case "writeTracker":
		a, err := decode[struct {
			Number  int     `json:"number"`
			Body    *string `json:"body"`
			Comment string  `json:"comment"`
		}](args)
		if err != nil {
			return nil, err
		}
		if a.Body != nil {
			if err := s.GitHub.SetIssueBody(a.Number, *a.Body); err != nil {
				return nil, fmt.Errorf("could not refresh tracker #%d: %w", a.Number, err)
			}
		}
		if a.Comment != "" {
			if _, err := s.GitHub.Comment(a.Number, a.Comment); err != nil {
				return nil, fmt.Errorf("could not comment on tracker #%d: %w", a.Number, err)
			}
		}
		s.log(fmt.Sprintf("github.writeTracker #%d", a.Number))
		return map[string]any{"number": a.Number}, nil
	}
	return nil, fmt.Errorf("github.%s has no handler", action)
}

func (s *SDK) listIssues(q world.Query) ([]world.Issue, error) {
	var all []world.Issue
	for page := 1; ; page++ {
		got, err := s.GitHub.IssuesPage(q, page)
		if err != nil {
			return nil, err
		}
		for _, i := range got {
			if !i.PullRequest {
				all = append(all, i)
			}
		}
		if len(got) < world.PageSize {
			return all, nil
		}
	}
}

// TrackerSeedBody is what a fresh tracker says before its first run
// writes to it.
func TrackerSeedBody(title string) string {
	return strings.Join([]string{
		"Standing tracker for the Claudinite task that logs to **" + title + "**.",
		"",
		"This issue is a living document: the **body** is rewritten to the current picture each run,",
		"and each run adds a dated comment. Its closed state carries no meaning — nothing opens,",
		"closes or reopens it, and no run should be read as \"resolved\" because it is closed.",
		"",
		"_No run has written here yet._",
	}, "\n") + "\n"
}

// findOrCreateTracker is the issue titled exactly title, in any state,
// the lowest number winning, through the list API; a found tracker left
// open is closed on the way back, fail-soft. Absent, it is created and
// closed.
func (s *SDK) findOrCreateTracker(title string) (any, error) {
	if title == "" {
		return nil, errors.New("findOrCreateTracker needs a title")
	}
	issues, err := s.listIssues(world.Query{State: "all"})
	if err != nil {
		return nil, fmt.Errorf("could not look for the tracker %q: %w", title, err)
	}
	var exact []world.Issue
	for _, i := range issues {
		if strings.TrimSpace(i.Title) == title {
			exact = append(exact, i)
		}
	}
	if len(exact) > 0 {
		sort.Slice(exact, func(a, b int) bool { return exact[a].Number < exact[b].Number })
		found := exact[0]
		if found.State == "open" {
			if err := s.GitHub.CloseIssue(found.Number, "completed"); err != nil {
				s.log(fmt.Sprintf("could not close tracker #%d (%v)", found.Number, err))
			}
		}
		s.log(fmt.Sprintf("github.findOrCreateTracker found #%d", found.Number))
		return map[string]any{"number": found.Number, "created": false, "duplicates": len(exact) - 1}, nil
	}
	n, err := s.GitHub.CreateIssue(title, TrackerSeedBody(title), nil)
	if err != nil {
		return nil, fmt.Errorf("could not create the tracker %q: %w", title, err)
	}
	leftOpen := s.GitHub.CloseIssue(n, "completed") != nil
	s.log(fmt.Sprintf("github.findOrCreateTracker created #%d", n))
	return map[string]any{"number": n, "created": true, "duplicates": 0, "leftOpen": leftOpen}, nil
}
