package checksdk

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Fake is an engine for a pack's unit tests: it answers a Repo's calls
// from its fields and from the files under the Repo's root, so a check
// runs without cn. A nil list is derived where it can be: Files, Tracked
// and AllFiles walk the root (.git skipped), ChangedFiles is empty,
// AddedLines reads every line of a changed file the Base does not hold.
type Fake struct {
	Files, Tracked, AllFiles, Untracked []string
	ChangedFiles, Deleted               []string
	Branch, BaseRef, MergeBase          string
	// Base is each file's text at the merge base.
	Base    map[string]string
	Added   []Line
	Removed []Line
	Commits []Commit
	// Messages default to the Commits' subjects.
	Messages     []string
	Merges       []Merge
	Turns        []Turn
	ToolCalls    []ToolCall
	SkillLoads   []string
	ReplyClasses []string
	// PackConfig is each pack entry's config.
	PackConfig map[string]any
	Checks     ChecksConfig
	// Parsed answers doc.parse; a path it lacks parses as JSON from disk.
	Parsed map[string]any
}

// Repo is a Repo at root that f answers.
func (f *Fake) Repo(root string) Repo {
	return NewRepo(root, func(method string, args json.RawMessage) (any, error) { return f.answer(root, method, args) })
}

func (f *Fake) walk(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.Type().IsRegular() {
			rel, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func or(list []string, fallback func() []string) []string {
	if list != nil {
		return list
	}
	if fallback == nil {
		return []string{}
	}
	return fallback()
}

func (f *Fake) answer(root, method string, raw json.RawMessage) (any, error) {
	var args struct {
		Path   string   `json:"path"`
		ID     string   `json:"id"`
		Needle string   `json:"needle"`
		Files  []string `json:"files"`
	}
	_ = json.Unmarshal(raw, &args)
	walk := func() []string { return f.walk(root) }
	switch method {
	case "tree.files":
		return or(f.Files, walk), nil
	case "tree.tracked":
		return or(f.Tracked, walk), nil
	case "tree.all":
		return or(f.AllFiles, func() []string { return or(f.Files, walk) }), nil
	case "tree.untracked":
		return or(f.Untracked, nil), nil
	case "change.files":
		return or(f.ChangedFiles, nil), nil
	case "change.deleted":
		return or(f.Deleted, nil), nil
	case "change.base":
		branch := f.Branch
		if branch == "" {
			branch = "change"
		}
		return base{Branch: branch, BaseRef: f.BaseRef, MergeBase: f.MergeBase}, nil
	case "change.readBase":
		t, ok := f.Base[args.Path]
		return map[string]any{"text": t, "ok": ok}, nil
	case "change.listBase":
		var out []string
		for p := range f.Base {
			out = append(out, p)
		}
		sort.Strings(out)
		return or(out, nil), nil
	case "change.added":
		if f.Added != nil {
			return filterLines(f.Added, args.Files), nil
		}
		files := args.Files
		if files == nil {
			files = f.ChangedFiles
		}
		out := []Line{}
		for _, p := range files {
			if _, inBase := f.Base[p]; inBase {
				continue
			}
			b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
			if err != nil {
				continue
			}
			for i, l := range strings.Split(string(b), "\n") {
				out = append(out, Line{Path: p, Line: i + 1, Text: l})
			}
		}
		return out, nil
	case "change.removed":
		return filterLines(f.Removed, args.Files), nil
	case "change.commits":
		if f.Commits == nil {
			return []Commit{}, nil
		}
		return f.Commits, nil
	case "change.messages":
		if f.Messages != nil {
			return f.Messages, nil
		}
		out := []string{}
		for _, c := range f.Commits {
			out = append(out, c.Subject)
		}
		return out, nil
	case "change.merges":
		if f.Merges == nil {
			return []Merge{}, nil
		}
		return f.Merges, nil
	case "change.grep":
		out := []Line{}
		for _, p := range or(f.Tracked, walk) {
			b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
			if err != nil {
				continue
			}
			for i, l := range strings.Split(string(b), "\n") {
				if strings.Contains(l, args.Needle) {
					out = append(out, Line{Path: p, Line: i + 1, Text: l})
				}
			}
		}
		return out, nil
	case "session.ownerTurns":
		if f.Turns == nil {
			return []Turn{}, nil
		}
		return f.Turns, nil
	case "session.replyClasses":
		return or(f.ReplyClasses, nil), nil
	case "session.toolCalls":
		if f.ToolCalls == nil {
			return []ToolCall{}, nil
		}
		return f.ToolCalls, nil
	case "session.skillLoads":
		return or(f.SkillLoads, nil), nil
	case "config.pack":
		return f.PackConfig[args.ID], nil
	case "config.checks":
		return f.Checks, nil
	case "doc.parse":
		if v, ok := f.Parsed[args.Path]; ok {
			return v, nil
		}
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(args.Path)))
		if err != nil {
			return nil, err
		}
		var v any
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, fmt.Errorf("the fake engine parses JSON only (set Fake.Parsed for %s): %v", args.Path, err)
		}
		return v, nil
	}
	return nil, fmt.Errorf("unknown method %s", method)
}

func filterLines(ls []Line, files []string) []Line {
	out := []Line{}
	for _, l := range ls {
		keep := files == nil
		for _, f := range files {
			keep = keep || f == l.Path
		}
		if keep {
			out = append(out, l)
		}
	}
	return out
}
