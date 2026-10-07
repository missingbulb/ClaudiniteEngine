package mirror_test

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/fleet"
	"github.com/missingbulb/ClaudiniteEngine/cn/fleet/mirror"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packindex"
)

type index struct {
	pair mirror.Signed
	ix   packindex.Index
}

// shelf answers verified copies from memory, counting archive reads.
type shelf struct {
	catalog  mirror.Signed
	listed   []string
	indexes  map[string]index
	archives map[string][]byte
	reads    []string
}

func (s *shelf) Catalog() (mirror.Signed, []string, error) { return s.catalog, s.listed, nil }

func (s *shelf) Index(id string) (mirror.Signed, packindex.Index, error) {
	v, ok := s.indexes[id]
	if !ok {
		return mirror.Signed{}, packindex.Index{}, fmt.Errorf("pack index %s: no source answered", id)
	}
	return v.pair, v.ix, nil
}

func (s *shelf) Archive(id string, e packindex.Entry) ([]byte, error) {
	s.reads = append(s.reads, id+"@"+e.Version)
	a, ok := s.archives[id+"@"+e.Version]
	if !ok {
		return nil, errors.New("no archive")
	}
	return a, nil
}

func newShelf() *shelf {
	return &shelf{
		catalog: mirror.Signed{Raw: []byte("CAT"), Sig: []byte("CATSIG")},
		listed:  []string{"hello", "hello"},
		indexes: map[string]index{
			"hello":     {mirror.Signed{Raw: []byte("IX-hello"), Sig: []byte("SIG-hello")}, packindex.Index{Pack: "hello", Versions: []packindex.Entry{{Version: "1.61004.1"}, {Version: "1.61005.1"}}}},
			"acme-pack": {mirror.Signed{Raw: []byte("IX-acme"), Sig: []byte("SIG-acme")}, packindex.Index{Pack: "acme-pack", Versions: []packindex.Entry{{Version: "1.61005.1"}}}},
		},
		archives: map[string][]byte{"hello@1.61004.1": []byte("TGZ-1"), "hello@1.61005.1": []byte("TGZ-2"), "acme-pack@1.61005.1": []byte("TGZ-a")},
	}
}

func blobSHA(b []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(b))
	h.Write(b)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// api fakes the git data API over one branch's tree.
type api struct {
	tree    map[string]string // path -> blob sha on the branch; nil: no branch
	blobs   map[string][]byte
	calls   []string
	written map[string]string // the new tree's entries
	base    any               // the new tree's base_tree
	refBody map[string]any
}

func (a *api) gh(method, path string, body any) (fleet.Response, error) {
	a.calls = append(a.calls, method+" "+path)
	ok := func(v any) (fleet.Response, error) {
		raw, _ := json.Marshal(v)
		return fleet.Response{Status: 200, JSON: raw}, nil
	}
	created := func(v any) (fleet.Response, error) {
		raw, _ := json.Marshal(v)
		return fleet.Response{Status: 201, JSON: raw}, nil
	}
	switch {
	case method == "GET" && path == "/repos/acme/fleet/git/ref/heads/vendored":
		if a.tree == nil {
			return fleet.Response{Status: 404}, nil
		}
		return ok(map[string]any{"object": map[string]string{"sha": "c0"}})
	case method == "GET" && path == "/repos/acme/fleet/git/commits/c0":
		return ok(map[string]any{"tree": map[string]string{"sha": "t0"}})
	case method == "GET" && path == "/repos/acme/fleet/git/trees/t0?recursive=1":
		var entries []map[string]string
		for p, sha := range a.tree {
			entries = append(entries, map[string]string{"path": p, "type": "blob", "sha": sha})
		}
		return ok(map[string]any{"tree": entries, "truncated": false})
	case method == "POST" && path == "/repos/acme/fleet/git/blobs":
		b := body.(map[string]string)
		raw, _ := base64.StdEncoding.DecodeString(b["content"])
		sha := blobSHA(raw)
		if a.blobs == nil {
			a.blobs = map[string][]byte{}
		}
		a.blobs[sha] = raw
		return created(map[string]string{"sha": sha})
	case method == "POST" && path == "/repos/acme/fleet/git/trees":
		a.written, a.base = map[string]string{}, body.(map[string]any)["base_tree"]
		for _, e := range body.(map[string]any)["tree"].([]map[string]string) {
			a.written[e["path"]] = e["sha"]
		}
		return created(map[string]string{"sha": "t1"})
	case method == "POST" && path == "/repos/acme/fleet/git/commits":
		return created(map[string]string{"sha": "c1"})
	case method == "PATCH" && path == "/repos/acme/fleet/git/refs/heads/vendored", method == "POST" && path == "/repos/acme/fleet/git/refs":
		a.refBody = body.(map[string]any)
		if method == "POST" {
			return created(nil)
		}
		return ok(nil)
	}
	return fleet.Response{Status: 500}, nil
}

func TestAFirstMirrorCopiesTheShelfByteForByte(t *testing.T) {
	a, s := &api{}, newShelf()
	r, err := mirror.Mirror(a.gh, "acme/fleet", s, []string{"acme-pack"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"catalog.json": "CAT", "catalog.sig.json": "CATSIG",
		"hello/index.json": "IX-hello", "hello/index.sig.json": "SIG-hello",
		"hello/1.61004.1.tar.gz": "TGZ-1", "hello/1.61005.1.tar.gz": "TGZ-2",
		"acme-pack/index.json": "IX-acme", "acme-pack/index.sig.json": "SIG-acme", "acme-pack/1.61005.1.tar.gz": "TGZ-a",
	}
	if len(a.written) != len(want) {
		t.Errorf("wrote %v", a.written)
	}
	for p, body := range want {
		if got := string(a.blobs[a.written[p]]); got != body {
			t.Errorf("%s holds %q, want %q", p, got, body)
		}
	}
	if a.refBody["ref"] != "refs/heads/vendored" || a.refBody["sha"] != "c1" || r.Commit != "c1" {
		t.Errorf("branch not created at the commit: %v %+v", a.refBody, r)
	}
}

func TestAMirrorWritesOnlyWhatMoved(t *testing.T) {
	s := newShelf()
	a := &api{tree: map[string]string{
		"catalog.json": blobSHA([]byte("CAT")), "catalog.sig.json": blobSHA([]byte("CATSIG")),
		"hello/index.json": blobSHA([]byte("IX-old")), "hello/index.sig.json": blobSHA([]byte("SIG-old")),
		"hello/1.61004.1.tar.gz": blobSHA([]byte("TGZ-1")),
	}}
	r, err := mirror.Mirror(a.gh, "acme/fleet", s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(s.reads, ",") != "hello@1.61005.1" {
		t.Errorf("read archives %v; a version the branch holds is never read again", s.reads)
	}
	if len(a.written) != 3 || a.written["hello/index.json"] == "" || a.written["hello/1.61005.1.tar.gz"] == "" || a.written["hello/index.sig.json"] == "" {
		t.Errorf("wrote %v", a.written)
	}
	if a.refBody["sha"] != "c1" || a.refBody["force"] != false || r.Commit != "c1" || a.base != "t0" {
		t.Errorf("ref %v over tree %v", a.refBody, a.base)
	}
}

func TestAMirrorThatMatchesWritesNothing(t *testing.T) {
	s := newShelf()
	a := &api{tree: map[string]string{
		"catalog.json": blobSHA([]byte("CAT")), "catalog.sig.json": blobSHA([]byte("CATSIG")),
		"hello/index.json": blobSHA([]byte("IX-hello")), "hello/index.sig.json": blobSHA([]byte("SIG-hello")),
		"hello/1.61004.1.tar.gz": "x", "hello/1.61005.1.tar.gz": "y",
	}}
	r, err := mirror.Mirror(a.gh, "acme/fleet", s, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range a.calls {
		if !strings.HasPrefix(c, "GET ") {
			t.Errorf("wrote %s on a mirror that matched", c)
		}
	}
	if r.Commit != "" || r.Changed != 0 {
		t.Errorf("%+v", r)
	}
}

// A pack only a member declares, which the shelf cannot answer for, is
// named and skipped: the member's own update would fail on it from the
// shelf too. A catalog pack the shelf cannot answer for fails the mirror.
func TestAMirrorSkipsAMembersPackTheShelfLacks(t *testing.T) {
	a, s := &api{}, newShelf()
	r, err := mirror.Mirror(a.gh, "acme/fleet", s, []string{"gone-pack"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Missing) != 1 || !strings.HasPrefix(r.Missing[0], "gone-pack") {
		t.Errorf("missing %v", r.Missing)
	}
	delete(s.indexes, "hello")
	if _, err := mirror.Mirror((&api{}).gh, "acme/fleet", s, nil); err == nil {
		t.Error("mirrored a shelf missing a catalog pack's index")
	}
}

func TestAMirrorTheTokenCannotWriteIsAGrantError(t *testing.T) {
	s := newShelf()
	a := &api{}
	gh := func(method, path string, body any) (fleet.Response, error) {
		if method == "POST" {
			return fleet.Response{Status: 403}, nil
		}
		return a.gh(method, path, body)
	}
	if _, err := mirror.Mirror(gh, "acme/fleet", s, nil); !fleet.IsGrant(err) {
		t.Errorf("%v", err)
	}
}

// A run past its cap commits whole units in small commits, archives
// first, and leaves the rest, every index pair among it, for the next.
func TestAMirrorPastItsCapKeepsWhatItWrote(t *testing.T) {
	defer mirror.SetLimits(2, 4)()
	a, s := &api{}, newShelf()
	var trees []map[string]string
	var refs []string
	gh := func(method, path string, body any) (fleet.Response, error) {
		r, err := a.gh(method, path, body)
		switch {
		case strings.HasSuffix(path, "/git/trees") && method == "POST":
			trees = append(trees, a.written)
		case strings.Contains(path, "/git/refs"):
			refs = append(refs, method)
		}
		return r, err
	}
	r, err := mirror.Mirror(gh, "acme/fleet", s, []string{"acme-pack"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Changed != 3 || r.Remaining != 6 || r.Commit == "" {
		t.Errorf("%+v", r)
	}
	if len(trees) != 2 || len(trees[0]) != 2 || len(trees[1]) != 1 || strings.Join(refs, ",") != "POST,PATCH" {
		t.Errorf("trees %v refs %v", trees, refs)
	}
	for _, tr := range trees {
		for p := range tr {
			if !strings.HasSuffix(p, ".tar.gz") {
				t.Errorf("wrote %s before every archive was in", p)
			}
		}
	}
	if !strings.Contains(r.Summary("acme/fleet"), "6 files are left") {
		t.Error(r.Summary("acme/fleet"))
	}
}
