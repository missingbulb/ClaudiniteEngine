// Package promotescope is the growth promote stage's write-surface gate:
// every path a promote branch touches since its merge base with the base
// branch lies under the canon's corpus roots - the packs/ shelf, plus
// each path the claudinite-canon-curation entry's config.write_paths
// declares. The promote agent never runs it; the canon's CI runs it on
// the promote pull request, keyed on its branch, since nothing in a diff
// marks it as a promote run. Prose is the request; this is the guarantee.
package promotescope

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
)

// Pack is the entry whose config may widen the corpus.
const Pack = "claudinite-canon-curation"

// Shelf is the corpus root no config removes: a canon with no shelf is
// not a canon.
const Shelf = "packs"

// ErrNoMergeBase refuses to certify a branch whose diff cannot be scoped.
var ErrNoMergeBase = errors.New("no merge-base with the base branch — cannot scope the diff; refusing to certify")

// Roots are the corpus roots an entry's config names, the shelf first,
// each with its trailing slash so a prefix cannot match a sibling file.
func Roots(config map[string]any) []string {
	out := []string{Shelf + "/"}
	declared, _ := config["write_paths"].([]any)
	for _, d := range declared {
		s, _ := d.(string)
		s = strings.TrimRight(strings.TrimPrefix(strings.TrimSpace(s), "./"), "/")
		if s == "" {
			continue
		}
		if r := s + "/"; !has(out, r) {
			out = append(out, r)
		}
	}
	return out
}

func has(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// RootsOf reads the corpus roots from the repository's settings, the
// shelf alone where the pack's entry carries no write_paths or the repo
// has no settings file.
func RootsOf(repo string) ([]string, error) {
	path, f, err := settings.Find(repo)
	if err != nil {
		for _, ff := range settings.Formats {
			if _, serr := os.Stat(filepath.Join(repo, filepath.FromSlash(settings.RelPath(ff)))); serr == nil {
				return nil, err
			}
		}
		return Roots(nil), nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	packs, err := settings.ReadPacks(raw, f)
	if err != nil {
		return nil, err
	}
	e, ok := packs.Entry(Pack, false)
	if !ok {
		e, _ = packs.Entry(Pack, true)
	}
	return Roots(e.Config), nil
}

// Result is the gate's answer: the roots, and every touched path outside
// them in byte order.
type Result struct {
	Roots, Stray []string
}

// Check scopes the branch at repo against base: what the working tree
// changed since the merge base, the paths deleted since it and the
// untracked files.
func Check(repo, base string) (Result, error) {
	git := gitcmd.Repo{Dir: repo}
	r, err := git.Run("merge-base", "HEAD", base)
	if err != nil {
		return Result{}, err
	}
	mergeBase := strings.TrimSpace(r.Stdout)
	if r.Code != 0 || mergeBase == "" {
		return Result{}, ErrNoMergeBase
	}
	roots, err := RootsOf(repo)
	if err != nil {
		return Result{}, err
	}
	changed, deleted := git.DiffLists(mergeBase, mergeBase)
	_, untracked := git.ListFiles()
	seen := map[string]bool{}
	res := Result{Roots: roots}
	for _, p := range append(append(changed, deleted...), untracked...) {
		if seen[p] {
			continue
		}
		seen[p] = true
		inside := false
		for _, root := range roots {
			inside = inside || strings.HasPrefix(p, root)
		}
		if !inside {
			res.Stray = append(res.Stray, p)
		}
	}
	sort.Strings(res.Stray)
	return res, nil
}
