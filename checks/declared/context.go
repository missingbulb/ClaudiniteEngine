package declared

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/checks/declared/refs"
	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/shared/skilltriggers"
)

// SharedPrefix is the vendored mount, never part of the tree a check
// judges.
const SharedPrefix = ".claudinite/shared/"

// Ctx is the tree one run judges: the Node engine's buildContext set in
// whole-repo mode. Files are tracked plus untracked, minus the shared
// mount, minus anything that is not a regular file, minus what the
// attributes mark linguist-vendored or linguist-generated; AllFiles keeps
// the vendored ones; Tracked is the index unfiltered.
type Ctx struct {
	Root      string
	BaseRef   string
	Tracked   []string
	Untracked []string
	// Now is the clock the dated-section rules read.
	Now time.Time
	// Config is the member's checks configuration.
	Config Config
	// Triggers are the active packs' skills' force-load declarations.
	Triggers []skilltriggers.Trigger

	git gitcmd.Repo

	allOnce, filesOnce, mbOnce, diffOnce sync.Once
	allFiles, files                      []string
	mergeBase                            string
	vsBase, deleted                      []string
	reads                                map[string]*string
	baseReads                            map[string]*string
	refScan                              *refs.Scan
}

// refs is the run's one reference scan, shared by every barrier.
func (c *Ctx) refs() *refs.Scan {
	if c.refScan == nil {
		c.refScan = refs.NewScan(refsTree{c})
	}
	return c.refScan
}

// Config is what the member's settings say about checks.
type Config struct {
	// SettingsPath is the settings file, relative to the repo, where a
	// finding about the configuration is anchored.
	SettingsPath string
	Rules        map[string]string
	Accept       []Acceptance
	// PackConfig is each declared entry's config, by pack id.
	PackConfig map[string]map[string]any
	// Errors are faults in the settings that change what runs.
	Errors []string
	// Packs are the active packs, in the pack set's order.
	Packs []packset.Pack
}

// Acceptance is one accepted finding.
type Acceptance struct {
	Rule, Path, Reason, Pack string
}

// NewCtx reads the tree at root.
func NewCtx(root string, cfg Config) *Ctx {
	g := gitcmd.Repo{Dir: root, Faults: &gitcmd.Faults{}}
	c := &Ctx{Root: root, Config: cfg, Now: time.Now(), git: g, reads: map[string]*string{}, baseReads: map[string]*string{}}
	c.BaseRef = g.BaseRef()
	g.RefreshBaseRef(c.BaseRef)
	c.Tracked, c.Untracked = g.ListFiles()
	return c
}

// Spent is the git fault that spent the tree, nil while none has. Every
// read memoised after it holds whatever the failed command returned, so
// a check run over a spent Ctx would report silence: the caller stops
// there instead.
func (c *Ctx) Spent() error {
	if msg := c.git.Faults.First(); msg != "" {
		return errors.New(msg)
	}
	return nil
}

// GitFaults are the git commands that timed out since the last call.
func (c *Ctx) GitFaults() []string { return c.git.Faults.Take() }

// AllFiles is the scanned set before vendored files are dropped.
func (c *Ctx) AllFiles() []string {
	c.allOnce.Do(func() {
		for _, f := range append(append([]string{}, c.Tracked...), c.Untracked...) {
			if strings.HasPrefix(f, SharedPrefix) {
				continue
			}
			st, err := os.Stat(filepath.Join(c.Root, filepath.FromSlash(f)))
			if err != nil || !st.Mode().IsRegular() {
				continue
			}
			c.allFiles = append(c.allFiles, f)
		}
	})
	return c.allFiles
}

// Files is the scanned set.
func (c *Ctx) Files() []string {
	c.filesOnce.Do(func() {
		all := c.AllFiles()
		vendored := c.git.CheckAttr(all, "linguist-vendored", "linguist-generated")
		for _, f := range all {
			if !vendored[f] {
				c.files = append(c.files, f)
			}
		}
	})
	return c.files
}

// TrackedList is the index, for the refs engine.
func (c *Ctx) TrackedList() []string { return c.Tracked }

// Read is a file's text, false when it cannot be read.
func (c *Ctx) Read(path string) (string, bool) {
	if t, ok := c.reads[path]; ok {
		if t == nil {
			return "", false
		}
		return *t, true
	}
	b, err := os.ReadFile(filepath.Join(c.Root, filepath.FromSlash(path)))
	if err != nil {
		c.reads[path] = nil
		return "", false
	}
	s := validUTF8(b)
	c.reads[path] = &s
	return s, true
}

// read is Read with "" for an unreadable file, as `ctx.read(f) ?? ”`.
func (c *Ctx) read(path string) string {
	s, _ := c.Read(path)
	return s
}

// Exists reports whether path exists in the working tree.
func (c *Ctx) Exists(path string) bool {
	_, err := os.Stat(filepath.Join(c.Root, filepath.FromSlash(path)))
	return err == nil
}

// MergeBase is the change's merge base with the base ref, or "".
func (c *Ctx) MergeBase() string {
	c.mbOnce.Do(func() {
		if c.BaseRef == "" {
			return
		}
		mb, err := c.git.MergeBase("HEAD", c.BaseRef)
		if err == nil {
			c.mergeBase = strings.TrimSpace(mb)
		}
	})
	return c.mergeBase
}

func (c *Ctx) diffBase() string {
	if mb := c.MergeBase(); mb != "" {
		return mb
	}
	return "HEAD"
}

func (c *Ctx) diff() {
	c.diffOnce.Do(func() { c.vsBase, c.deleted = c.git.DiffLists(c.diffBase(), c.MergeBase()) })
}

// ChangedFiles are the scanned files the change touched: changed since
// the merge base, or untracked.
func (c *Ctx) ChangedFiles() []string {
	c.diff()
	changed := map[string]bool{}
	for _, f := range c.vsBase {
		changed[f] = true
	}
	for _, f := range c.Untracked {
		changed[f] = true
	}
	var out []string
	for _, f := range c.Files() {
		if changed[f] {
			out = append(out, f)
		}
	}
	return out
}

func (c *Ctx) isTracked(file string) bool {
	for _, t := range c.Tracked {
		if t == file {
			return true
		}
	}
	return false
}

// AddedLines are a file's added lines; every line of an untracked file.
func (c *Ctx) AddedLines(file string) []gitcmd.Line {
	if !c.isTracked(file) {
		t, ok := c.Read(file)
		if !ok {
			return nil
		}
		var out []gitcmd.Line
		for i, l := range strings.Split(t, "\n") {
			out = append(out, gitcmd.Line{Line: i + 1, Text: l})
		}
		return out
	}
	return c.git.AddedLines(c.diffBase(), file)
}

// RemovedLines are a tracked file's removed lines.
func (c *Ctx) RemovedLines(file string) []gitcmd.Line {
	if c.MergeBase() == "" || !c.isTracked(file) {
		return nil
	}
	return c.git.RemovedLines(c.diffBase(), file)
}

// ReadBase is a file's text at the merge base.
func (c *Ctx) ReadBase(path string) (string, bool) {
	mb := c.MergeBase()
	if mb == "" {
		return "", false
	}
	if t, ok := c.baseReads[path]; ok {
		if t == nil {
			return "", false
		}
		return *t, true
	}
	s, ok := c.git.ShowText(mb, path)
	if !ok {
		c.baseReads[path] = nil
		return "", false
	}
	c.baseReads[path] = &s
	return s, true
}

// Commits are the change's commit messages.
func (c *Ctx) Commits() []string {
	if c.MergeBase() == "" {
		return nil
	}
	return c.git.CommitMessages(c.MergeBase())
}

// Branch is the checked-out branch.
func (c *Ctx) Branch() string { return c.git.AbbrevHead() }

// IntroducedMerges are the change's merge commits not on the base branch.
func (c *Ctx) IntroducedMerges() []gitcmd.Merge {
	if c.MergeBase() == "" {
		return nil
	}
	var out []gitcmd.Merge
	for _, m := range c.git.Merges(c.MergeBase()) {
		if c.BaseRef != "" && c.git.IsAncestor(m.Sha, c.BaseRef) {
			continue
		}
		out = append(out, m)
	}
	return out
}

// refsTree adapts the context to the refs engine.
type refsTree struct{ c *Ctx }

func (t refsTree) Files() []string                 { return t.c.Files() }
func (t refsTree) Tracked() []string               { return t.c.Tracked }
func (t refsTree) AllFiles() []string              { return t.c.AllFiles() }
func (t refsTree) Read(path string) (string, bool) { return t.c.Read(path) }

// Deleted are the paths the change deletes; none without a merge base.
func (c *Ctx) Deleted() []string {
	c.diff()
	return c.deleted
}

// UntrackedList are the files git does not track and does not ignore.
func (c *Ctx) UntrackedList() []string { return c.Untracked }

// BaseRefName is the ref the change is measured against, "" for none.
func (c *Ctx) BaseRefName() string { return c.BaseRef }

// ListBase is every path at the merge base.
func (c *Ctx) ListBase() []string {
	if c.MergeBase() == "" {
		return nil
	}
	return c.git.LsTree(c.MergeBase())
}

// CommitsWithFiles are the change's commits, oldest first, merges
// excluded, each with the files it changed.
func (c *Ctx) CommitsWithFiles() []gitcmd.Commit {
	if c.MergeBase() == "" {
		return nil
	}
	return c.git.CommitsWithFiles(c.MergeBase())
}

// GrepTracked is every tracked line containing needle, the shared mount
// excluded.
func (c *Ctx) GrepTracked(needle string) []gitcmd.Hit { return c.git.GrepTracked(needle, SharedPrefix) }
