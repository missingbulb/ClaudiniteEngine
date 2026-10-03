// Package packhistory reads a canon shelf's version history off git: for
// each pack under packs/, the commit that last moved its manifest's
// version, the shipping files changed since, and the pull requests each
// version carried, with the versions its provenance/VERSIONS.md has no
// row for. A pack's content reaches a member only on a new version, so a
// shipping change since the last move is a change no member has; the
// per-version commits are what a VERSIONS.md row is made of.
package packhistory

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	prov "github.com/missingbulb/ClaudiniteEngine/shared/provenance"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// Shelf is the tree a canon's packs live under.
const Shelf = "packs"

// VersionsFile is a pack's version record, under its provenance folder.
const VersionsFile = "VERSIONS.md"

// BumpTask stamped the commits that moved only version numbers, which
// therefore shipped nothing.
const BumpTask = "claudinite-canon-curation/pack-version-bump"

// droppedAtRoot are the folders a pack's vendored set leaves out at its
// root; it also leaves out the Go tests beside its checks.
var droppedAtRoot = map[string]bool{"test": true, "docs": true, Dir: true}

// Dir is the provenance folder's name.
const Dir = prov.Dir

// ErrShallow refuses a walk over a clone that does not hold the history
// it would answer from.
var ErrShallow = errors.New("the clone is shallow, so the version walk would answer from its horizon - fetch the full history first (git fetch --unshallow)")

// Bump is a commit that moved a pack's version.
type Bump struct {
	Sha     string `json:"sha"`
	Version string `json:"version"`
	Date    string `json:"date"`
}

// Commit is one first-parent commit a version shipped; PR is the pull
// request its squash subject names, nil for none.
type Commit struct {
	Sha     string `json:"sha"`
	Subject string `json:"subject"`
	PR      *int   `json:"pr"`
}

// Version is one version and the commits it shipped, oldest first.
type Version struct {
	Version string   `json:"version"`
	Date    string   `json:"date"`
	Commits []Commit `json:"commits"`
}

// Pack is one pack's history at a ref.
type Pack struct {
	ID       string `json:"id"`
	Manifest string `json:"manifest"`
	Version  string `json:"version"`
	// Record is the version record's path, and Missing the versions it
	// carries no row for, oldest first.
	Record  string   `json:"record"`
	Missing []string `json:"missing"`
	// LastBump is the commit that last moved the version, nil where the
	// history holds none.
	LastBump *Bump `json:"lastBump"`
	// ShippingSince are the shipping paths changed since LastBump.
	ShippingSince []string  `json:"shippingSince"`
	Versions      []Version `json:"versions"`
}

// IsShipping reports whether a repository path rides its pack's vendored
// set to a member.
func IsShipping(p string) bool {
	parts := strings.SplitN(p, "/", 4)
	if len(parts) < 3 || parts[0] != Shelf || parts[1] == "" {
		return false
	}
	if len(parts) > 3 && droppedAtRoot[parts[2]] {
		return false
	}
	checkTest := parts[2] == "checks" && len(parts) == 4 && !strings.Contains(parts[3], "/") && strings.HasSuffix(parts[3], "_test.go")
	return !checkTest
}

var (
	manifestVersion = regexp.MustCompile(`(?m)(?:^|[\s{,])"?version"?:\s*['"]?(\d+(?:\.\d+)*)['"]?`)
	rowRE           = regexp.MustCompile(`^\|\s*(\d+(?:\.\d+)*)\s*\|`)
	pullRE          = regexp.MustCompile(`\(#(\d+)\)\s*$`)
)

// DeclaredVersion is the version a manifest's text declares, "" for none.
func DeclaredVersion(text string) string {
	if m := manifestVersion.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return ""
}

// RowVersions are the versions a record's table rows carry.
func RowVersions(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if m := rowRE.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

func equal(a, b string) bool {
	c, err := version.ComparePack(a, b)
	return err == nil && c == 0
}

// Walker reads a repository's shelf.
type Walker struct{ Git gitcmd.Repo }

func (w Walker) out(args ...string) (string, error) {
	r, err := w.Git.Run(args...)
	if err != nil {
		return "", err
	}
	if r.Code != 0 {
		return "", fmt.Errorf("git %s exited %d: %s", args[0], r.Code, strings.TrimSpace(r.Stderr))
	}
	return r.Stdout, nil
}

func (w Walker) fileAt(ref, p string) (string, bool) {
	t, err := w.out("show", ref+":"+p)
	return t, err == nil
}

func (w Walker) manifestAt(ref, id string) (string, string) {
	for _, f := range prov.ManifestFiles {
		p := Shelf + "/" + id + "/" + f
		if t, ok := w.fileAt(ref, p); ok {
			return p, t
		}
	}
	return "", ""
}

// Packs are the ids whose manifest exists at ref, read from the tree
// rather than the working directory.
func (w Walker) Packs(ref string) ([]string, error) {
	listed, err := w.out("ls-tree", "--name-only", "-r", ref, Shelf+"/")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var ids []string
	for _, p := range strings.Split(listed, "\n") {
		parts := strings.Split(p, "/")
		if len(parts) != 3 || seen[parts[1]] {
			continue
		}
		for _, f := range prov.ManifestFiles {
			if parts[2] == f {
				seen[parts[1]] = true
				ids = append(ids, parts[1])
			}
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// Bumps are the first-parent commits that moved id's version, newest
// first; a pack's first commit counts.
func (w Walker) Bumps(ref, id string) ([]Bump, error) {
	args := []string{"log", "--first-parent", "--format=%H %cs", ref, "--"}
	for _, f := range prov.ManifestFiles {
		args = append(args, Shelf+"/"+id+"/"+f)
	}
	log, err := w.out(args...)
	if err != nil {
		return nil, err
	}
	var out []Bump
	for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
		sha, date, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		_, text := w.manifestAt(sha, id)
		here := DeclaredVersion(text)
		if here == "" {
			continue
		}
		_, prev := w.manifestAt(sha+"^", id)
		if before := DeclaredVersion(prev); before == "" || !equal(here, before) {
			out = append(out, Bump{Sha: sha, Version: here, Date: date})
		}
	}
	return out, nil
}

func (w Walker) shippingChanges(from, to, id string) ([]string, error) {
	diff, err := w.out("diff", "--name-only", from, to, "--", Shelf+"/"+id+"/")
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, p := range strings.Split(diff, "\n") {
		if p != "" && IsShipping(p) {
			out = append(out, p)
		}
	}
	return out, nil
}

// versions are the commits each bump shipped, oldest version first.
func (w Walker) versions(bumps []Bump, id string) ([]Version, error) {
	out := []Version{}
	for i := len(bumps) - 1; i >= 0; i-- {
		rng := bumps[i].Sha
		if i < len(bumps)-1 {
			rng = bumps[i+1].Sha + ".." + bumps[i].Sha
		}
		raw, err := w.out("log", "--first-parent", "--name-only",
			"--format=%x01%H%x00%s%x00%(trailers:key=Claudinite-Task,valueonly)%x00", rng, "--", Shelf+"/"+id+"/")
		if err != nil {
			return nil, err
		}
		commits := []Commit{}
		for _, block := range strings.Split(raw, "\x01") {
			if block == "" {
				continue
			}
			f := strings.SplitN(block, "\x00", 4)
			for len(f) < 4 {
				f = append(f, "")
			}
			if strings.TrimSpace(f[2]) == BumpTask {
				continue
			}
			shipped := false
			for _, p := range strings.Split(f[3], "\n") {
				shipped = shipped || IsShipping(strings.TrimSpace(p))
			}
			if !shipped {
				continue
			}
			c := Commit{Sha: f[0], Subject: f[1]}
			if m := pullRE.FindStringSubmatch(f[1]); m != nil {
				if n, err := strconv.Atoi(m[1]); err == nil {
					c.PR = &n
				}
			}
			commits = append(commits, c)
		}
		for a, b := 0, len(commits)-1; a < b; a, b = a+1, b-1 {
			commits[a], commits[b] = commits[b], commits[a]
		}
		out = append(out, Version{Version: bumps[i].Version, Date: bumps[i].Date, Commits: commits})
	}
	return out, nil
}

// Read is one pack's history at ref.
func (w Walker) Read(ref, id string) (Pack, error) {
	manifest, text := w.manifestAt(ref, id)
	if manifest == "" {
		return Pack{}, fmt.Errorf("no manifest for pack %q under %s/ at %s", id, Shelf, ref)
	}
	p := Pack{ID: id, Manifest: manifest, Version: DeclaredVersion(text), Record: path.Join(Shelf, id, Dir, VersionsFile), Missing: []string{}, ShippingSince: []string{}}
	bumps, err := w.Bumps(ref, id)
	if err != nil {
		return Pack{}, err
	}
	if len(bumps) > 0 {
		p.LastBump = &bumps[0]
		if p.ShippingSince, err = w.shippingChanges(bumps[0].Sha, ref, id); err != nil {
			return Pack{}, err
		}
	}
	if p.Versions, err = w.versions(bumps, id); err != nil {
		return Pack{}, err
	}
	record, _ := w.fileAt(ref, p.Record)
	rows := RowVersions(record)
	for _, v := range p.Versions {
		found := false
		for _, r := range rows {
			found = found || equal(r, v.Version)
		}
		if !found {
			p.Missing = append(p.Missing, v.Version)
		}
	}
	return p, nil
}

// History reads every pack ids names at ref, or every pack on the shelf
// there when ids is empty, refusing a shallow clone.
func (w Walker) History(ref string, ids []string) ([]Pack, error) {
	shallow, err := w.out("rev-parse", "--is-shallow-repository")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(shallow) != "false" {
		return nil, ErrShallow
	}
	if len(ids) == 0 {
		if ids, err = w.Packs(ref); err != nil {
			return nil, err
		}
	}
	out := []Pack{}
	for _, id := range ids {
		p, err := w.Read(ref, id)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// Lines is the history as text: one line per pack, then a line per
// version, newest first.
func Lines(packs []Pack) []string {
	var out []string
	for _, p := range packs {
		head := p.ID + " " + p.Version
		switch {
		case p.LastBump == nil:
			head += ": no commit moved its version"
		case len(p.ShippingSince) == 0:
			head += fmt.Sprintf(": moved at %.10s on %s, nothing shipping changed since", p.LastBump.Sha, p.LastBump.Date)
		default:
			head += fmt.Sprintf(": moved at %.10s on %s, %d shipping %s changed since - no member has them until the version moves", p.LastBump.Sha, p.LastBump.Date, len(p.ShippingSince), plural(len(p.ShippingSince)))
		}
		out = append(out, head)
		for _, f := range p.ShippingSince {
			out = append(out, "  changed: "+f)
		}
		for i := len(p.Versions) - 1; i >= 0; i-- {
			v := p.Versions[i]
			what := "no pull request is attributed to this version"
			if len(v.Commits) > 0 {
				subjects := make([]string, len(v.Commits))
				for j, c := range v.Commits {
					subjects[j] = c.Subject
				}
				what = strings.Join(subjects, "; ")
			}
			norow := ""
			for _, m := range p.Missing {
				if m == v.Version {
					norow = " (no row in " + VersionsFile + ")"
				}
			}
			out = append(out, fmt.Sprintf("  %s %s%s: %s", v.Version, v.Date, norow, what))
		}
	}
	return out
}

func plural(n int) string {
	if n == 1 {
		return "file"
	}
	return "files"
}
