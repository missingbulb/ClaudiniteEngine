package addpacks

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/fleet"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/packindex"
)

// DefaultReadBudget is how many file contents one pack's fingerprint may
// read in one repository: a cost ceiling on a sweep across every repo the
// owner has, not a correctness knob. Past it the fingerprint is undecided,
// never false.
const DefaultReadBudget = 24

// Verdict is one fingerprint's answer: Fit true or false, or nil for
// undecided, with Why saying why it could not be decided.
type Verdict struct {
	Fit *bool   `json:"verdict"`
	Why *string `json:"why"`
}

var (
	yes = true
	no  = false
)

func fit() Verdict   { return Verdict{Fit: &yes} }
func noFit() Verdict { return Verdict{Fit: &no} }
func undecided(why string) Verdict {
	return Verdict{Why: &why}
}

// Undecided is a fingerprint the sweep could not settle, and why.
type Undecided struct {
	ID  string `json:"id"`
	Why string `json:"why"`
}

// Fits is one member's answer: the packs its tree fingerprints and its
// declaration lacks, and the ones that could not be decided from outside.
type Fits struct {
	Fits      []string    `json:"fits"`
	Undecided []Undecided `json:"undecided"`
}

// DeclaredID is the id a declaration entry names, as the Node engine's
// packEntryId compares it: a local pack's bare id, a canon pack's id
// under its current name, "" for an entry naming none.
func DeclaredID(entry any) string {
	var raw string
	switch t := entry.(type) {
	case string:
		raw = t
	case map[string]any:
		raw, _ = t["id"].(string)
		if _, ok := t["id"].(string); !ok {
			return ""
		}
	default:
		return ""
	}
	if bare, ok := strings.CutPrefix(raw, "local/"); ok {
		return bare
	}
	return workitem.CanonicalPackID(raw)
}

// Candidates are the catalog packs a fit sweep may consider for a member
// declaring declared: those carrying a fingerprint that it does not
// already declare. A pack without one is declaration-authoritative, and
// its absence says nothing.
func Candidates(packs []packindex.CatalogPack, declared []string) []packindex.CatalogPack {
	var out []packindex.CatalogPack
	for _, p := range packs {
		if p.RelevanceDetector != nil && !contains(declared, p.ID) {
			out = append(out, p)
		}
	}
	return out
}

// Evaluate answers one pack's fingerprint; an error is undecided with its
// text as the reason, so a broken predicate cannot make a fleet look
// clean.
type Evaluate func(p packindex.CatalogPack) (Verdict, error)

// UndeclaredFits runs each candidate's fingerprint and splits the answers
// into fits, undecided and the quiet majority that did not match.
func UndeclaredFits(packs []packindex.CatalogPack, declared []string, evaluate Evaluate) Fits {
	out := Fits{Fits: []string{}, Undecided: []Undecided{}}
	for _, p := range Candidates(packs, declared) {
		v, err := evaluate(p)
		switch {
		case err != nil:
			out.Undecided = append(out.Undecided, Undecided{p.ID, "fingerprint threw: " + err.Error()})
		case v.Fit != nil && *v.Fit:
			out.Fits = append(out.Fits, p.ID)
		case v.Fit != nil:
		default:
			why := "the fingerprint could not be evaluated here"
			if v.Why != nil && *v.Why != "" {
				why = *v.Why
			}
			out.Undecided = append(out.Undecided, Undecided{p.ID, why})
		}
	}
	sort.Strings(out.Fits)
	sort.SliceStable(out.Undecided, func(i, j int) bool { return localeLess(out.Undecided[i].ID, out.Undecided[j].ID) })
	return out
}

// localeLess orders as JavaScript's localeCompare orders these names: case
// folded first, lowercase before uppercase on a tie.
func localeLess(a, b string) bool {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if la != lb {
		return la < lb
	}
	return a > b
}

// Tree is every tracked path on a ref, from one listing; Truncated is
// GitHub saying the listing is a subset, so a non-match over it is no
// evidence.
type Tree struct {
	Tracked   []string `json:"tracked"`
	Truncated bool     `json:"truncated"`
}

// FetchTree lists repo's tree at ref.
func FetchTree(gh fleet.GH, repo, ref string) (Tree, error) {
	r, err := gh.Get(fmt.Sprintf("/repos/%s/git/trees/%s?recursive=1", repo, fleet.EncodeURIComponent(ref)))
	if err != nil {
		return Tree{}, err
	}
	var body struct {
		Tree      []struct{ Path, Type string } `json:"tree"`
		Truncated *bool                         `json:"truncated"`
	}
	if r.Status != 200 || json.Unmarshal(r.JSON, &body) != nil || body.Tree == nil {
		msg := fmt.Sprintf("listing the tree of %s@%s returned %d", repo, ref, r.Status)
		if r.Status == 403 {
			return Tree{}, &fleet.GrantError{Msg: msg + fleet.ForbiddenHint("/repos/"+repo+"/git/trees/")}
		}
		return Tree{}, fmt.Errorf("%s", msg)
	}
	t := Tree{Tracked: []string{}, Truncated: body.Truncated != nil && *body.Truncated}
	for _, n := range body.Tree {
		if n.Type == "blob" {
			t.Tracked = append(t.Tracked, n.Path)
		}
	}
	return t, nil
}

// fetchBlob is one file's text, ok false when it cannot be read (absent,
// too large for the contents API, a submodule).
func fetchBlob(gh fleet.GH, repo, ref, path string) (string, bool, error) {
	segs := strings.Split(path, "/")
	for i, s := range segs {
		segs[i] = fleet.EncodeURIComponent(s)
	}
	r, err := gh.Get(fmt.Sprintf("/repos/%s/contents/%s?ref=%s", repo, strings.Join(segs, "/"), fleet.EncodeURIComponent(ref)))
	if err != nil {
		return "", false, err
	}
	var body struct {
		Content *string `json:"content"`
	}
	if r.Status != 200 || json.Unmarshal(r.JSON, &body) != nil || body.Content == nil {
		return "", false, nil
	}
	text, err := base64.StdEncoding.DecodeString(strings.NewReplacer("\n", "", "\r", "").Replace(*body.Content))
	if err != nil {
		return "", false, nil
	}
	return string(text), true, nil
}

// RemoteEvaluator judges a pack's fingerprint against a repository the
// sweep has not cloned: a path-only one by the listing alone, one with
// text by reading its candidate files, at most budget of them.
func RemoteEvaluator(gh fleet.GH, repo, ref string, t Tree, budget int) Evaluate {
	return func(p packindex.CatalogPack) (Verdict, error) {
		d := p.RelevanceDetector
		candidates := d.Candidates(t.Tracked)
		absent := noFit()
		if t.Truncated {
			absent = undecided("the tree listing was truncated - a non-match here is not evidence")
		}
		if len(d.Text) == 0 {
			if len(candidates) > 0 {
				return fit(), nil
			}
			return absent, nil
		}
		if len(candidates) > budget {
			return undecided(fmt.Sprintf("%d files could carry what it looks for (budget %d) - it greps source rather than probing paths", len(candidates), budget)), nil
		}
		for _, path := range candidates {
			body, ok, err := fetchBlob(gh, repo, ref, path)
			if err != nil {
				return Verdict{}, err
			}
			if ok && every(d.Text, body) {
				return fit(), nil
			}
		}
		return absent, nil
	}
}

func every(ps []packindex.Pattern, s string) bool {
	for _, p := range ps {
		if !p.Test(s) {
			return false
		}
	}
	return true
}
