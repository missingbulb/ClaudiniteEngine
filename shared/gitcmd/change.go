package gitcmd

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The reads a check run makes of a working tree and its change: which
// files there are, which the attributes mark vendored or generated, the
// base the change is measured against, and the change's lines, commits
// and merges. A failing read answers empty, as a check over a repo with no
// base or no history reads nothing rather than failing.

// BaseRefCandidates are tried in order for the ref a change is measured
// against.
var BaseRefCandidates = []string{"origin/main", "origin/master", "main", "master"}

// Base-ref refresh: at most one fetch per window, bounded by a timeout,
// recorded in a marker in the git dir.
const (
	FetchTimeout = 8 * time.Second
	FetchWindow  = 5 * time.Minute
	refreshFile  = "claudinite-base-refresh.json"
)

// NoFetchEnv, set to 1, skips the base-ref refresh.
const NoFetchEnv = "CLAUDINITE_CHECKS_NO_FETCH"

func (r Repo) try(args ...string) (string, bool) {
	out, err := r.run(args...)
	return string(out), err == nil
}

func (r Repo) input(stdin string, args ...string) (string, bool) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	cmd.Env = childEnv()
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	return string(out), err == nil
}

func nonEmpty(s, sep string) []string {
	var out []string
	for _, l := range strings.Split(s, sep) {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// ListFiles lists the tracked files (the index, including ones deleted
// from the working tree) and the untracked files git does not ignore.
func (r Repo) ListFiles() (tracked, untracked []string) {
	out, _ := r.try("ls-files", "-c", "-o", "--exclude-standard", "-t", "-z")
	for _, rec := range nonEmpty(out, "\x00") {
		if len(rec) < 3 {
			continue
		}
		if strings.HasPrefix(rec, "? ") {
			untracked = append(untracked, rec[2:])
		} else {
			tracked = append(tracked, rec[2:])
		}
	}
	return tracked, untracked
}

// CheckAttr returns the files any of attrs is set on.
func (r Repo) CheckAttr(files []string, attrs ...string) map[string]bool {
	set := map[string]bool{}
	if len(files) == 0 {
		return set
	}
	out, _ := r.input(strings.Join(files, "\x00")+"\x00", append([]string{"check-attr", "-z", "--stdin"}, attrs...)...)
	f := strings.Split(out, "\x00")
	for i := 0; i+2 < len(f); i += 3 {
		if f[i+2] == "set" {
			set[f[i]] = true
		}
	}
	return set
}

var commitAnswer = regexp.MustCompile(`^[0-9a-f]+ commit `)

// BaseRef is the first of BaseRefCandidates naming a commit, or "".
func (r Repo) BaseRef() string {
	var q strings.Builder
	for _, c := range BaseRefCandidates {
		q.WriteString(c + "^{commit}\n")
	}
	out, _ := r.input(q.String(), "cat-file", "--batch-check")
	answers := strings.Split(out, "\n")
	for i, c := range BaseRefCandidates {
		if i < len(answers) && commitAnswer.MatchString(answers[i]) {
			return c
		}
	}
	return ""
}

// RefreshBaseRef fetches a remote-tracking ref once per FetchWindow,
// best effort, unless NoFetchEnv is 1. A local branch is left as it is.
func (r Repo) RefreshBaseRef(ref string) {
	if ref == "" || os.Getenv(NoFetchEnv) == "1" {
		return
	}
	remote, branch, ok := strings.Cut(ref, "/")
	if !ok || remote == "" || branch == "" {
		return
	}
	markerRel, _ := r.try("rev-parse", "--git-path", refreshFile)
	markerRel = strings.TrimSpace(markerRel)
	marker := ""
	if markerRel != "" {
		marker = markerRel
		if !filepath.IsAbs(marker) {
			marker = filepath.Join(r.Dir, marker)
		}
		var last struct {
			Ref string `json:"ref"`
			At  int64  `json:"at"`
		}
		if b, err := os.ReadFile(marker); err == nil && json.Unmarshal(b, &last) == nil {
			if last.Ref == ref && time.Since(time.UnixMilli(last.At)) < FetchWindow {
				return
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), FetchTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "fetch", "--quiet", "--no-tags", remote, "+refs/heads/"+branch+":refs/remotes/"+remote+"/"+branch)
	cmd.Dir = r.Dir
	cmd.Env = childEnv()
	if cmd.Run() == nil && marker != "" {
		b, _ := json.Marshal(map[string]any{"ref": ref, "at": time.Now().UnixMilli()})
		_ = os.WriteFile(marker, b, 0o644)
	}
}

// IsAncestor reports whether a is an ancestor of b.
func (r Repo) IsAncestor(a, b string) bool {
	_, ok := r.try("merge-base", "--is-ancestor", a, b)
	return ok
}

// AbbrevHead is the checked-out branch's short name, "HEAD" when
// detached, "" when unreadable.
func (r Repo) AbbrevHead() string {
	out, _ := r.try("rev-parse", "--abbrev-ref", "HEAD")
	return strings.TrimSpace(out)
}

// DiffLists lists the paths the working tree changed since diffBase
// (present ones) and, when mergeBase is set, the paths deleted since it.
func (r Repo) DiffLists(diffBase, mergeBase string) (vsBase, deleted []string) {
	out, ok := r.try("diff", "--name-status", "-z", diffBase)
	if ok {
		f := nonEmpty(out, "\x00")
		renames := false
		type entry struct {
			status string
			paths  []string
		}
		var entries []entry
		for i := 0; i < len(f); {
			st := f[i]
			n := 1
			if strings.HasPrefix(st, "R") || strings.HasPrefix(st, "C") {
				n, renames = 2, true
			}
			if i+n >= len(f) {
				break
			}
			entries = append(entries, entry{st, f[i+1 : i+1+n]})
			i += 1 + n
		}
		if !renames {
			for _, e := range entries {
				if strings.HasPrefix(e.status, "D") {
					if mergeBase != "" {
						deleted = append(deleted, e.paths[0])
					}
					continue
				}
				vsBase = append(vsBase, e.paths[len(e.paths)-1])
			}
			return vsBase, deleted
		}
	}
	o1, _ := r.try("diff", "--name-only", "-z", "--diff-filter=d", diffBase)
	vsBase = nonEmpty(o1, "\x00")
	if mergeBase != "" {
		o2, _ := r.try("diff", "--name-only", "-z", "--diff-filter=D", mergeBase)
		deleted = nonEmpty(o2, "\x00")
	}
	return vsBase, deleted
}

// Line is one line of a change.
type Line struct {
	Line int
	Text string
}

var (
	addHunk = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
	delHunk = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+\d+(?:,\d+)? @@`)
)

// AddedLines are the lines the working tree's file added since base.
func (r Repo) AddedLines(base, file string) []Line {
	return r.hunkLines(base, file, addHunk, "+", "-")
}

// RemovedLines are the lines the working tree's file removed since base.
func (r Repo) RemovedLines(base, file string) []Line {
	return r.hunkLines(base, file, delHunk, "-", "+")
}

func (r Repo) hunkLines(base, file string, hunk *regexp.Regexp, mark, other string) []Line {
	out, _ := r.try("diff", "-U0", base, "--", file)
	var lines []Line
	n := 0
	for _, l := range strings.Split(out, "\n") {
		if m := hunk.FindStringSubmatch(l); m != nil {
			n, _ = strconv.Atoi(m[1])
			continue
		}
		switch {
		case strings.HasPrefix(l, mark) && !strings.HasPrefix(l, mark+mark+mark):
			lines = append(lines, Line{n, l[1:]})
			n++
		case !strings.HasPrefix(l, other):
			if l != "" {
				n++
			}
		}
	}
	return lines
}

// CommitMessages are the subjects and bodies of the commits since base.
func (r Repo) CommitMessages(base string) []string {
	out, _ := r.try("log", "--format=%s%n%b%x00", base+"..HEAD")
	var msgs []string
	for _, m := range strings.Split(out, "\x00") {
		if m = strings.TrimSpace(m); m != "" {
			msgs = append(msgs, m)
		}
	}
	return msgs
}

// Merge is one merge commit.
type Merge struct{ Sha, Subject string }

// Merges are the first-parent merge commits since base.
func (r Repo) Merges(base string) []Merge {
	out, _ := r.try("log", "--merges", "--first-parent", "--format=%h %s", base+"..HEAD")
	var ms []Merge
	for _, l := range nonEmpty(out, "\n") {
		sha, subject, _ := strings.Cut(l, " ")
		ms = append(ms, Merge{sha, subject})
	}
	return ms
}

// ShowText is path's content at ref, or false.
func (r Repo) ShowText(ref, path string) (string, bool) {
	out, ok := r.try("show", ref+":"+path)
	return out, ok
}

// Commit is one commit of a change, with the files it changed.
type Commit struct {
	Sha, Date, Subject string
	Files              []string
}

// CommitsWithFiles are the commits since base, oldest first, merges
// excluded (their content arrives through their parents), each with its
// committer date and the files it changed.
func (r Repo) CommitsWithFiles(base string) []Commit {
	out, _ := r.try("log", "--reverse", "--no-merges", "--name-only", "--format=%x00%H%x1f%cI%x1f%s", base+"..HEAD")
	var cs []Commit
	for _, block := range strings.Split(out, "\x00") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		lines := strings.Split(block, "\n")
		head := strings.SplitN(lines[0], "\x1f", 3)
		for len(head) < 3 {
			head = append(head, "")
		}
		c := Commit{Sha: head[0], Date: head[1], Subject: head[2]}
		for _, f := range lines[1:] {
			if f = strings.TrimSpace(f); f != "" {
				c.Files = append(c.Files, f)
			}
		}
		cs = append(cs, c)
	}
	return cs
}

// LsTree is every path in ref's tree.
func (r Repo) LsTree(ref string) []string {
	out, _ := r.try("ls-tree", "-r", "--name-only", ref)
	return nonEmpty(out, "\n")
}

// Hit is one line a search found.
type Hit struct {
	Path string
	Line int
	Text string
}

var grepLine = regexp.MustCompile(`^([^:]+):(\d+):(.*)$`)

// GrepTracked is every tracked line containing needle, a fixed string,
// with the tree under exclude (a folder prefix, "" for none) left out.
func (r Repo) GrepTracked(needle, exclude string) []Hit {
	args := []string{"grep", "-n", "-F", "-e", needle, "--", "."}
	if exclude != "" {
		args = append(args, ":(exclude)"+exclude)
	}
	out, _ := r.try(args...)
	var hits []Hit
	for _, l := range nonEmpty(out, "\n") {
		m := grepLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[2])
		hits = append(hits, Hit{Path: m[1], Line: n, Text: m[3]})
	}
	return hits
}
