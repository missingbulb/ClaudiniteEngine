package curation

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
)

// PromoteBranch is what a promote run's branch name carries. The growth
// promote stage's write surface is the canon's corpus roots: the shelf,
// plus each path the fleet block's writePaths declares. The boundary
// holds for promote alone - an ordinary change legitimately edits
// anything - and nothing in a diff marks it as a promote run, so the gate
// keys on the branch. Prose is the request; this is the guarantee.
const PromoteBranch = "growth-promote"

// WritePathsKey is the fleet block's key naming corpus roots beside the
// shelf.
const WritePathsKey = "writePaths"

// ErrNoMergeBase refuses to certify a branch whose diff cannot be scoped.
var ErrNoMergeBase = errors.New("no merge-base with the base branch — cannot scope the diff; refusing to certify")

// Roots are the corpus roots a fleet block names, the shelf first, each
// with its trailing slash so a prefix cannot match a sibling file.
func Roots(fleet map[string]any) []string {
	out := []string{Shelf + "/"}
	declared, _ := fleet[WritePathsKey].([]any)
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

// RootsOf reads the corpus roots from the repository's settings: the
// shelf alone where its fleet block carries no writePaths or the repo has
// no settings file.
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
	p, err := settings.ParseFile(raw, f)
	if err != nil {
		return nil, err
	}
	return Roots(p.Fleet), nil
}

// Stray are the paths outside every root, each once, in byte order.
func Stray(roots, paths []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range paths {
		if seen[p] {
			continue
		}
		seen[p] = true
		inside := false
		for _, r := range roots {
			inside = inside || strings.HasPrefix(p, r)
		}
		if !inside {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
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
	return Result{Roots: roots, Stray: Stray(roots, append(append(changed, deleted...), untracked...))}, nil
}
