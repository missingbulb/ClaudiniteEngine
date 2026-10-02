// Package sdkserver answers the SDK calls a pack's checks binary makes
// back to cn over its pipe while it answers a request: the walk, the
// change, the session, the member's settings and parsed documents. Every
// answer is read from the run's own context, the tree the declared run
// already walked and the session it was handed, never the environment,
// and every answer is JSON.
package sdkserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/descriptor"
	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/shared/transcript"
)

// Tree is the walk and the change one run reads, which the declared
// checks' context provides.
type Tree interface {
	Files() []string
	TrackedList() []string
	AllFiles() []string
	UntrackedList() []string
	ChangedFiles() []string
	Deleted() []string
	Branch() string
	BaseRefName() string
	MergeBase() string
	ReadBase(path string) (string, bool)
	ListBase() []string
	AddedLines(file string) []gitcmd.Line
	RemovedLines(file string) []gitcmd.Line
	CommitsWithFiles() []gitcmd.Commit
	Commits() []string
	IntroducedMerges() []gitcmd.Merge
	GrepTracked(needle string) []gitcmd.Hit
}

// Config is what the member's settings say that a check may read.
type Config struct {
	// PackConfig is each declared entry's config, by pack id (a local
	// pack's without its local/ prefix).
	PackConfig map[string]map[string]any
	Rules      map[string]string
	Accept     []Acceptance
	// Packs is the run's pack set, in the set's order.
	Packs []packset.Pack
}

// pack is one entry of packs.list.
type pack struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Dir       string   `json:"dir"`
	Version   string   `json:"version"`
	MinEngine string   `json:"minEngineVersion"`
	Prose     string   `json:"prose"`
	Skills    []string `json:"skills"`
	Requires  []string `json:"requires"`
}

// Acceptance is one accepted finding.
type Acceptance struct {
	Rule   string `json:"rule"`
	Path   string `json:"path,omitempty"`
	Reason string `json:"reason,omitempty"`
	Pack   string `json:"pack,omitempty"`
}

// Server answers one run's calls.
type Server struct {
	root    string
	tree    Tree
	session *transcript.Session
	cfg     Config
}

// Serve is the server for one run over root: tree is the run's walk and
// change, session its transcript (nil for none).
func Serve(root string, tree Tree, session *transcript.Session, cfg Config) *Server {
	return &Server{root: root, tree: tree, session: session, cfg: cfg}
}

type handler func(s *Server, a args) (any, error)

type args struct {
	Path   string    `json:"path"`
	ID     string    `json:"id"`
	Needle string    `json:"needle"`
	Files  *[]string `json:"files"`
}

var methods = map[string]handler{
	"tree.files":      func(s *Server, _ args) (any, error) { return list(s.tree.Files()), nil },
	"tree.tracked":    func(s *Server, _ args) (any, error) { return list(s.tree.TrackedList()), nil },
	"tree.all":        func(s *Server, _ args) (any, error) { return list(s.tree.AllFiles()), nil },
	"tree.untracked":  func(s *Server, _ args) (any, error) { return list(s.tree.UntrackedList()), nil },
	"change.files":    func(s *Server, _ args) (any, error) { return list(s.tree.ChangedFiles()), nil },
	"change.deleted":  func(s *Server, _ args) (any, error) { return list(s.tree.Deleted()), nil },
	"change.listBase": func(s *Server, _ args) (any, error) { return list(s.tree.ListBase()), nil },
	"change.messages": func(s *Server, _ args) (any, error) { return list(s.tree.Commits()), nil },
	"change.base": func(s *Server, _ args) (any, error) {
		return struct {
			Branch    string `json:"branch"`
			BaseRef   string `json:"baseRef"`
			MergeBase string `json:"mergeBase"`
		}{s.tree.Branch(), s.tree.BaseRefName(), s.tree.MergeBase()}, nil
	},
	"change.readBase": func(s *Server, a args) (any, error) {
		rel, err := inRepo(a.Path)
		if err != nil {
			return nil, err
		}
		text, ok := s.tree.ReadBase(rel)
		return struct {
			Text string `json:"text"`
			OK   bool   `json:"ok"`
		}{text, ok}, nil
	},
	"change.added":   func(s *Server, a args) (any, error) { return s.lines(a, s.tree.AddedLines), nil },
	"change.removed": func(s *Server, a args) (any, error) { return s.lines(a, s.tree.RemovedLines), nil },
	"change.commits": func(s *Server, _ args) (any, error) {
		out := []commit{}
		for _, c := range s.tree.CommitsWithFiles() {
			out = append(out, commit{c.Sha, c.Date, c.Subject, list(c.Files)})
		}
		return out, nil
	},
	"change.merges": func(s *Server, _ args) (any, error) {
		out := []merge{}
		for _, m := range s.tree.IntroducedMerges() {
			out = append(out, merge{m.Sha, m.Subject})
		}
		return out, nil
	},
	"change.grep": func(s *Server, a args) (any, error) {
		out := []line{}
		if a.Needle == "" {
			return nil, errors.New("change.grep needs a needle")
		}
		for _, h := range s.tree.GrepTracked(a.Needle) {
			out = append(out, line{h.Path, h.Line, h.Text})
		}
		return out, nil
	},
	"session.ownerTurns":   func(s *Server, _ args) (any, error) { return s.ownerTurns(), nil },
	"session.replyClasses": func(s *Server, _ args) (any, error) { return sorted(s.session.ReplyClasses()), nil },
	"session.toolCalls":    func(s *Server, _ args) (any, error) { return s.toolCalls(), nil },
	"session.skillLoads":   func(s *Server, _ args) (any, error) { return sorted(s.session.Loaded()), nil },
	"config.pack": func(s *Server, a args) (any, error) {
		if c, ok := s.cfg.PackConfig[strings.TrimPrefix(a.ID, "local/")]; ok && c != nil {
			return c, nil
		}
		return nil, nil
	},
	"config.checks": func(s *Server, _ args) (any, error) {
		rules := s.cfg.Rules
		if rules == nil {
			rules = map[string]string{}
		}
		accept := s.cfg.Accept
		if accept == nil {
			accept = []Acceptance{}
		}
		return struct {
			Rules  map[string]string `json:"rules"`
			Accept []Acceptance      `json:"accept"`
		}{rules, accept}, nil
	},
	"packs.list": func(s *Server, _ args) (any, error) {
		out := []pack{}
		for _, p := range s.cfg.Packs {
			out = append(out, pack{p.ID, string(p.Kind), p.Rel, p.Version, p.MinEngine, p.Prose, list(p.Skills), list(p.Requires)})
		}
		return out, nil
	},
	"doc.parse": func(s *Server, a args) (any, error) {
		rel, err := inRepo(a.Path)
		if err != nil {
			return nil, err
		}
		f := descriptor.FormatOf(rel)
		if f == "" {
			return nil, fmt.Errorf("%s is not JSON, YAML or TOML by its extension", rel)
		}
		raw, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("%s cannot be read", rel)
		}
		v, err := descriptor.ParseDocument(raw, f)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", rel, err)
		}
		return v, nil
	},
}

// Methods are the calls this engine answers, sorted.
func (s *Server) Methods() []string { return Methods() }

// Methods are the calls this engine answers, sorted.
func Methods() []string {
	out := make([]string, 0, len(methods))
	for m := range methods {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// Handle answers one call.
func (s *Server) Handle(method string, raw json.RawMessage) (json.RawMessage, error) {
	h, ok := methods[method]
	if !ok {
		return nil, fmt.Errorf("unknown method %s", method)
	}
	var a args
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, fmt.Errorf("%s: malformed arguments: %v", method, err)
		}
	}
	v, err := h(s, a)
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

type line struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type commit struct {
	Sha     string   `json:"sha"`
	Date    string   `json:"date"`
	Subject string   `json:"subject"`
	Files   []string `json:"files"`
}

type merge struct {
	Sha     string `json:"sha"`
	Subject string `json:"subject"`
}

func (s *Server) lines(a args, of func(string) []gitcmd.Line) []line {
	files := s.tree.ChangedFiles()
	if a.Files != nil {
		files = *a.Files
	}
	out := []line{}
	for _, f := range files {
		for _, l := range of(f) {
			out = append(out, line{f, l.Line, l.Text})
		}
	}
	return out
}

type turn struct {
	Index     int      `json:"index"`
	Timestamp string   `json:"timestamp"`
	Text      string   `json:"text"`
	Reply     string   `json:"reply"`
	ClassLine string   `json:"classLine"`
	Classes   []string `json:"classes"`
}

// ownerTurns are read from the session file alone, where the owner's
// turns are.
func (s *Server) ownerTurns() []turn {
	out := []turn{}
	if !s.session.Present() {
		return out
	}
	es := transcript.Entries(s.session.Path)
	for _, t := range transcript.OwnerTurns(es) {
		reply := transcript.AssistantTextAfter(es, t.Index)
		cl := transcript.ClassificationLine(reply)
		out = append(out, turn{t.Index, t.Timestamp, t.Text, reply, cl, sorted(transcript.ClassesIn(cl))})
	}
	return out
}

type toolCall struct {
	Tool      string          `json:"tool"`
	Input     json.RawMessage `json:"input"`
	Sidechain bool            `json:"sidechain"`
	DeniedBy  []string        `json:"deniedBy"`
}

func (s *Server) toolCalls() []toolCall {
	out := []toolCall{}
	for _, c := range s.session.Calls() {
		in := c.InputJSON()
		if in == "" {
			in = "null"
		}
		out = append(out, toolCall{c.Name, json.RawMessage(in), c.Sidechain, list(c.DeniedBy)})
	}
	return out
}

func list(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

func sorted(m map[string]bool) []string {
	out := []string{}
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// inRepo is a repo-relative path, refused when it leaves the repo.
func inRepo(p string) (string, error) {
	if p == "" || strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		return "", fmt.Errorf("%q is not a path in the repo", p)
	}
	c := path.Clean(filepath.ToSlash(p))
	if c == ".." || strings.HasPrefix(c, "../") {
		return "", fmt.Errorf("%q is outside the repo", p)
	}
	return c, nil
}
