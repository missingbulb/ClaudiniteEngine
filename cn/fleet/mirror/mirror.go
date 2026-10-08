// Package mirror keeps a fleet manager's copy of the shelf: every pack's
// signed index pair, every archive the index lists and the signed
// catalog, byte for byte, on the manager's own vendored branch, which its
// members read as their one pack source. The copy needs no key of its
// own: a member verifies it against the embedded roots as it would the
// shelf.
//
// A run commits through the git data API over the fleet token, a blob for
// each file that moved, a tree on the branch's last, a commit and a ref
// move that never forces, in commits of at most a hundred files, so a run
// cut short keeps what it wrote. An archive the branch already holds
// is never read again, since a published version is never rewritten.
// Blob writes are spaced a second apart, under GitHub's 80 content
// writes a minute, since a first mirror writes every archive on the shelf
// (https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api#about-secondary-rate-limits).
package mirror

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/fleet"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packindex"
)

// Branch is the manager's mirror branch, the layout ClaudinitePacks'
// vendored branch keeps.
const Branch = packindex.VendoredBranch

// writeGap is the pause before each blob write after the first.
var writeGap = time.Second

// Signed is a signed file as read, with its detached signature.
type Signed struct{ Raw, Sig []byte }

// Shelf is what the mirror copies, each answer already verified against
// the embedded roots.
type Shelf interface {
	// Catalog is the catalog pair and the ids it lists.
	Catalog() (Signed, []string, error)
	// Index is pack id's index pair, decoded.
	Index(id string) (Signed, packindex.Index, error)
	// Archive is one version's archive, checked against its entry.
	Archive(id string, e packindex.Entry) ([]byte, error)
}

// Result is one run: the last commit it made ("" when the branch already
// matched), how many files moved, and each pack only a member declares
// that the shelf did not answer for, with why.
type Result struct {
	Commit  string
	Changed int
	Packs   int
	Missing []string
	// Remaining is the files left for the next run, 0 once the branch is
	// level with the shelf.
	Remaining int
}

// Summary is the run in one line.
func (r Result) Summary(home string) string {
	s := fmt.Sprintf("Mirror: %s's %s branch holds %d packs from the shelf", home, Branch, r.Packs)
	if r.Commit != "" {
		s += fmt.Sprintf("; this run wrote %d files (commit %s)", r.Changed, r.Commit)
	} else {
		s += "; nothing moved"
	}
	if r.Remaining > 0 {
		s += fmt.Sprintf("; %d files are left for the next run, and no member is pointed at it until none are", r.Remaining)
	}
	if len(r.Missing) > 0 {
		s += fmt.Sprintf("; not on the shelf: %v", r.Missing)
	}
	return s
}

// blobSHA is the git blob id of b, what the branch's tree names.
func blobSHA(b []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(b))
	h.Write(b)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// Mirror brings home's vendored branch level with the shelf for every
// catalog pack and every id in declared, the packs the fleet's members
// carry. A catalog pack the shelf cannot answer for fails the run; a
// declared one only it names is reported in Missing.
func Mirror(gh fleet.GH, home string, shelf Shelf, declared []string) (Result, error) {
	cat, listed, err := shelf.Catalog()
	if err != nil {
		return Result{}, err
	}
	inCatalog := map[string]bool{}
	for _, id := range listed {
		inCatalog[id] = true
	}
	ids := map[string]bool{}
	for id := range inCatalog {
		ids[id] = true
	}
	for _, id := range declared {
		ids[id] = true
	}
	sorted := make([]string, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)

	parent, baseTree, tree, err := readBranch(gh, home)
	if err != nil {
		return Result{}, err
	}
	var r Result
	// Units land whole, archives first, then each index pair, then the
	// catalog pair, so a member reading mid-run never meets an index
	// naming an archive the branch lacks, nor half a signed pair.
	var archives, pairs []unit
	signed := func(rawPath, sigPath string, pair Signed) {
		if tree[rawPath] != blobSHA(pair.Raw) || tree[sigPath] != blobSHA(pair.Sig) {
			pairs = append(pairs, unit{{path: rawPath, data: pair.Raw}, {path: sigPath, data: pair.Sig}})
		}
	}
	for _, id := range sorted {
		pair, ix, err := shelf.Index(id)
		if err != nil {
			if inCatalog[id] {
				return Result{}, err
			}
			r.Missing = append(r.Missing, id+" ("+err.Error()+")")
			continue
		}
		r.Packs++
		for _, e := range ix.Versions {
			path := packindex.ArchiveFile(id, e.Version)
			if _, held := tree[path]; held {
				continue
			}
			archives = append(archives, unit{{path: path, load: func() ([]byte, error) { return shelf.Archive(id, e) }}})
		}
		signed(packindex.IndexFile(id), packindex.IndexSigFile(id), pair)
	}
	signed(packindex.CatalogFile, packindex.CatalogSigFile, cat)
	units := append(archives, pairs...)
	files := 0
	for i, u := range units {
		if files+len(u) > perRun {
			for _, rest := range units[i:] {
				r.Remaining += len(rest)
			}
			units = units[:i]
			break
		}
		files += len(u)
	}
	for len(units) > 0 {
		n, size := 0, 0
		for n < len(units) && (n == 0 || size+len(units[n]) <= perCommit) {
			size += len(units[n])
			n++
		}
		var chunk []file
		for _, u := range units[:n] {
			chunk = append(chunk, u...)
		}
		units = units[n:]
		sha, treeSHA, err := commit(gh, home, parent, baseTree, chunk)
		if err != nil {
			return r, err
		}
		r.Commit, r.Changed = sha, r.Changed+len(chunk)
		parent, baseTree = sha, treeSHA
	}
	return r, nil
}

// file is one path to write, its bytes or how to read them.
type file struct {
	path string
	data []byte
	load func() ([]byte, error)
}

// unit is files that land in one commit or not at all.
type unit []file

// perCommit and perRun bound one commit's files and one run's: a run that
// stops short has committed what it wrote, and the next goes on from
// there, under GitHub's 500 content writes an hour.
var (
	perCommit = 100
	perRun    = 400
)

// readBranch is the branch's head commit, its tree and the tree's blobs
// by path, "" and none when the branch does not exist yet.
func readBranch(gh fleet.GH, home string) (string, string, map[string]string, error) {
	base := "/repos/" + home + "/git/"
	ref, err := gh.Get(base + "ref/heads/" + Branch)
	if err != nil {
		return "", "", nil, err
	}
	if ref.Status == 404 {
		return "", "", map[string]string{}, nil
	}
	var head struct {
		Object struct{ SHA string } `json:"object"`
	}
	if err := decode(ref, base+"ref/heads/"+Branch, &head); err != nil {
		return "", "", nil, err
	}
	c, err := gh.Get(base + "commits/" + head.Object.SHA)
	if err != nil {
		return "", "", nil, err
	}
	var cm struct {
		Tree struct{ SHA string } `json:"tree"`
	}
	if err := decode(c, base+"commits/", &cm); err != nil {
		return "", "", nil, err
	}
	t, err := gh.Get(base + "trees/" + cm.Tree.SHA + "?recursive=1")
	if err != nil {
		return "", "", nil, err
	}
	var tr struct {
		Tree []struct {
			Path, Type, SHA string
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	if err := decode(t, base+"trees/", &tr); err != nil {
		return "", "", nil, err
	}
	if tr.Truncated {
		return "", "", nil, fmt.Errorf("%s's %s tree is too large for one listing", home, Branch)
	}
	blobs := map[string]string{}
	for _, e := range tr.Tree {
		if e.Type == "blob" {
			blobs[e.Path] = e.SHA
		}
	}
	return head.Object.SHA, cm.Tree.SHA, blobs, nil
}

func decode(r fleet.Response, path string, v any) error {
	if r.Status == 403 {
		return &fleet.GrantError{Msg: fmt.Sprintf("GET %s returned 403%s", path, fleet.ForbiddenHint(path))}
	}
	if r.Status != 200 || json.Unmarshal(r.JSON, v) != nil {
		return fmt.Errorf("GET %s returned %d", path, r.Status)
	}
	return nil
}

// commit writes files as one commit on parent ("" for a new branch),
// over its tree, and moves the branch to it, never forcing; it returns the
// commit and its tree.
func commit(gh fleet.GH, home, parent, baseTree string, files []file) (string, string, error) {
	base := "/repos/" + home + "/git/"
	var entries []map[string]string
	for i, f := range files {
		data := f.data
		if f.load != nil {
			var err error
			if data, err = f.load(); err != nil {
				return "", "", err
			}
		}
		if i > 0 {
			time.Sleep(writeGap)
		}
		r, err := fleet.Expect(gh, "POST", base+"blobs", map[string]string{"content": base64.StdEncoding.EncodeToString(data), "encoding": "base64"}, 201)
		if err != nil {
			return "", "", err
		}
		sha, err := shaOf(r)
		if err != nil {
			return "", "", err
		}
		entries = append(entries, map[string]string{"path": f.path, "mode": "100644", "type": "blob", "sha": sha})
	}
	treeBody := map[string]any{"tree": entries}
	parents := []string{}
	if parent != "" {
		treeBody["base_tree"] = baseTree
		parents = append(parents, parent)
	}
	r, err := fleet.Expect(gh, "POST", base+"trees", treeBody, 201)
	if err != nil {
		return "", "", err
	}
	tree, err := shaOf(r)
	if err != nil {
		return "", "", err
	}
	r, err = fleet.Expect(gh, "POST", base+"commits", map[string]any{
		"message": "Mirror the shelf's packs for this fleet\n\nWritten by the claudinite-fleet-sheepdog pack's fleet-pack-seeds sweep.",
		"tree":    tree, "parents": parents,
	}, 201)
	if err != nil {
		return "", "", err
	}
	sha, err := shaOf(r)
	if err != nil {
		return "", "", err
	}
	if parent == "" {
		_, err = fleet.Expect(gh, "POST", base+"refs", map[string]any{"ref": "refs/heads/" + Branch, "sha": sha}, 201)
	} else {
		_, err = fleet.Expect(gh, "PATCH", base+"refs/heads/"+Branch, map[string]any{"sha": sha, "force": false}, 200)
	}
	return sha, tree, err
}

func shaOf(r fleet.Response) (string, error) {
	var v struct{ SHA string }
	if json.Unmarshal(r.JSON, &v) != nil || v.SHA == "" {
		return "", errors.New("the git data API answered no sha")
	}
	return v.SHA, nil
}
