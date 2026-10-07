// Package packs fetches and verifies published pack versions and puts them
// in a member's tree: the two sources (the CDN and ClaudinitePacks'
// vendored branch), the signed index pair, the archive, the unpack and the
// tree compare. The engine never filters a pack: it copies a published set
// unchanged.
package packs

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packindex"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/sign"
)

// Default locations, overridable for the rehearsal by CLAUDINITE_PACKS_CDN
// and CLAUDINITE_PACKS_REPO.
const (
	DefaultCDN    = "https://packs.claudinite.com"
	DefaultRepo   = "https://github.com/missingbulb/ClaudinitePacks"
	VendoredRef   = packindex.VendoredBranch
	defaultMaxCDN = 64 << 20
)

// Source is one place published packs are read from.
type Source interface {
	Name() string
	// Index returns a pack's index.json and index.sig.json bytes.
	Index(id string) (index, sig []byte, err error)
	// Archive returns <id>/<version>.tar.gz.
	Archive(id, version string) ([]byte, error)
}

// CDN reads https://packs.claudinite.com/packs/<id>/..., HTTPS only and
// under a size cap.
type CDN struct {
	Base     string
	HTTP     *http.Client
	MaxBytes int64
}

// Name is "cdn".
func (CDN) Name() string { return "cdn" }

func (c CDN) get(rel string) ([]byte, error) {
	u, err := url.Parse(strings.TrimRight(c.Base, "/") + "/packs/" + rel)
	if err != nil || u.Scheme != "https" {
		return nil, fmt.Errorf("the pack CDN is read over HTTPS only, not %s", c.Base)
	}
	resp, err := httpsOnly(c.HTTP).Get(u.String())
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", u, resp.Status)
	}
	max := c.MaxBytes
	if max <= 0 {
		max = defaultMaxCDN
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", u, err)
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("GET %s: larger than the %d-byte cap", u, max)
	}
	return data, nil
}

// httpsOnly is client, refusing any redirect hop off HTTPS, on top of its
// own redirect policy.
func httpsOnly(client *http.Client) *http.Client {
	cl := *client
	own := client.CheckRedirect
	cl.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("the pack CDN is read over HTTPS only; a redirect went to %s", req.URL.Redacted())
		}
		if own != nil {
			return own(req, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return &cl
}

// Index reads the pair.
func (c CDN) Index(id string) ([]byte, []byte, error) {
	ix, err := c.get(packindex.IndexFile(id))
	if err != nil {
		return nil, nil, err
	}
	sig, err := c.get(packindex.IndexSigFile(id))
	if err != nil {
		return nil, nil, err
	}
	return ix, sig, nil
}

// Archive reads one version's archive.
func (c CDN) Archive(id, version string) ([]byte, error) {
	return c.get(packindex.ArchiveFile(id, version))
}

// Branch reads the vendored branch of ClaudinitePacks (or the repository
// Repo names, a URL or a path), cloned shallow and blobless into a
// temporary folder once per run with no credential. Close removes it.
type Branch struct {
	Repo string
	// Label names the source in logs and verdicts; "" is "branch", the
	// shelf's own.
	Label string
	once  sync.Once
	dir   string
	err   error
}

// Name is the label, "branch" when there is none.
func (b *Branch) Name() string {
	if b.Label != "" {
		return b.Label
	}
	return "branch"
}

func (b *Branch) clone() error {
	b.once.Do(func() {
		parent, err := os.MkdirTemp("", "claudinite-packs-*")
		if err != nil {
			b.err = err
			return
		}
		b.dir = filepath.Join(parent, "vendored")
		if err := gitcmd.Clone(b.Repo, VendoredRef, b.dir); err != nil {
			_ = os.RemoveAll(parent)
			b.dir, b.err = "", err
		}
	})
	return b.err
}

func (b *Branch) show(rel string) ([]byte, error) {
	if err := b.clone(); err != nil {
		return nil, err
	}
	data, ok, err := gitcmd.Repo{Dir: b.dir}.Show("HEAD", rel)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%s holds no %s on %s", b.Repo, rel, VendoredRef)
	}
	return data, nil
}

// Index reads the pair.
func (b *Branch) Index(id string) ([]byte, []byte, error) {
	ix, err := b.show(packindex.IndexFile(id))
	if err != nil {
		return nil, nil, err
	}
	sig, err := b.show(packindex.IndexSigFile(id))
	if err != nil {
		return nil, nil, err
	}
	return ix, sig, nil
}

// Archive reads one version's archive.
func (b *Branch) Archive(id, version string) ([]byte, error) {
	return b.show(packindex.ArchiveFile(id, version))
}

// Close removes the clone.
func (b *Branch) Close() {
	if b.dir != "" {
		_ = os.RemoveAll(filepath.Dir(b.dir))
		b.dir = ""
	}
}

// Reader reads verified indexes and archives from its sources, in order
// (the CDN, then the branch), for one run.
type Reader struct {
	Sources []Source
	Roots   []ed25519.PublicKey
	Now     func() time.Time
	// Log receives one line per index and archive saying which source
	// answered.
	Log io.Writer

	seen map[string]int64
}

// Verified is an index whose signature, format and serial checked out.
type Verified struct {
	Index packindex.Index
	// From names the source whose copy is used.
	From string
	// KeyID is the packs key that signed it.
	KeyID string
	// Raw and Sig are index.json and index.sig.json as read, which a
	// mirror copies byte for byte.
	Raw, Sig []byte
}

func (r *Reader) logf(format string, args ...any) {
	if r.Log != nil {
		fmt.Fprintf(r.Log, "packs: "+format+"\n", args...)
	}
}

// verifyPair checks the signature by a packs key the roots certify and
// decodes the index.
func (r *Reader) verifyPair(index, sig []byte) (Verified, error) {
	var s sign.SignedPackIndex
	if err := json.Unmarshal(sig, &s); err != nil {
		return Verified{}, fmt.Errorf("index.sig.json: %w", err)
	}
	body, err := sign.VerifyPackIndex(s, index, r.Roots, r.Now())
	if err != nil {
		return Verified{}, err
	}
	ix, err := packindex.Decode(index)
	if err != nil {
		return Verified{}, err
	}
	return Verified{Index: ix, KeyID: body.KeyID}, nil
}

// fromSource reads one source's pair and verifies it, asking once more
// when the pair does not verify, since the writer rewrites index.json and
// index.sig.json one after the other.
func (r *Reader) fromSource(src Source, id string) (Verified, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		index, sig, err := src.Index(id)
		if err != nil {
			return Verified{}, errUnreachable{err}
		}
		v, err := r.verifyPair(index, sig)
		if err == nil {
			if v.Index.Pack != id {
				return Verified{}, fmt.Errorf("the index names pack %q, not %q", v.Index.Pack, id)
			}
			v.From, v.Raw, v.Sig = src.Name(), index, sig
			return v, nil
		}
		lastErr = err
	}
	return Verified{}, lastErr
}

type errUnreachable struct{ error }

// SourceSerial is the serial one source's index carried.
type SourceSerial struct {
	Source string
	Serial int64
}

// SourcesDisagree is a read in which a later source's index is older than
// an earlier one's: the window while the CDN is ahead of the vendored
// branch (or the branch has regressed). Nothing reads either until they
// agree.
type SourcesDisagree struct{ Serials []SourceSerial }

func (e *SourcesDisagree) Error() string {
	var parts []string
	for _, s := range e.Serials {
		parts = append(parts, fmt.Sprintf("%s serial %d", s.Source, s.Serial))
	}
	return "pack index sources disagree (" + strings.Join(parts, ", ") + ")"
}

// VerifiedIndex reads pack id's index from every source that answers,
// verifies each pair, and refuses a serial lower than one an earlier read
// of that pack saw in this run; a source whose serial is lower than an
// earlier source's in this read is a *SourcesDisagree, so the CDN's and the
// branch's copies never regress each other. It returns the copy with the
// highest serial. A pair
// that does not verify refuses the read outright, naming the check. The
// member keeps no record of serials until phase 4 carries the last one in
// its key.
func (r *Reader) VerifiedIndex(id string) (Verified, error) {
	if r.seen == nil {
		r.seen = map[string]int64{}
	}
	var best *Verified
	var unreachable []string
	var answered []string
	var serials []SourceSerial
	disagree := false
	floor, seen := r.seen[id]
	for _, src := range r.Sources {
		v, err := r.fromSource(src, id)
		var u errUnreachable
		if errors.As(err, &u) {
			unreachable = append(unreachable, src.Name()+": "+u.Error())
			continue
		}
		if err != nil {
			return Verified{}, fmt.Errorf("pack index %s from %s refused: %w", id, src.Name(), err)
		}
		if seen && v.Index.Serial < floor {
			return Verified{}, fmt.Errorf("pack index %s from %s refused: serial %d is older than serial %d already read in this run", id, src.Name(), v.Index.Serial, floor)
		}
		if best != nil && v.Index.Serial < best.Index.Serial {
			disagree = true
		}
		if v.Index.Serial > r.seen[id] {
			r.seen[id] = v.Index.Serial
		}
		serials = append(serials, SourceSerial{src.Name(), v.Index.Serial})
		answered = append(answered, fmt.Sprintf("%s serial %d", src.Name(), v.Index.Serial))
		if best == nil || v.Index.Serial > best.Index.Serial {
			vv := v
			best = &vv
		}
	}
	for _, u := range unreachable {
		r.logf("%s: index unreachable from %s", id, u)
	}
	if best == nil {
		return Verified{}, fmt.Errorf("pack index %s: no source answered (%s)", id, strings.Join(unreachable, "; "))
	}
	if disagree {
		return Verified{}, &SourcesDisagree{Serials: serials}
	}
	r.logf("%s: index serial %d from %s (read: %s)", id, best.Index.Serial, best.From, strings.Join(answered, ", "))
	return *best, nil
}

// Archive fetches the archive of e from the first source that has it and
// checks it against the entry's SHA-256 and size.
func (r *Reader) Archive(id string, e packindex.Entry) ([]byte, error) {
	var errs []string
	for _, src := range r.Sources {
		data, err := src.Archive(id, e.Version)
		if err != nil {
			errs = append(errs, src.Name()+": "+err.Error())
			continue
		}
		if err := VerifyArchive(data, e); err != nil {
			return nil, fmt.Errorf("pack %s %s from %s refused: %w", id, e.Version, src.Name(), err)
		}
		r.logf("%s %s: archive from %s", id, e.Version, src.Name())
		return data, nil
	}
	return nil, fmt.Errorf("pack %s %s: no source has the archive (%s)", id, e.Version, strings.Join(errs, "; "))
}

// Sources builds the CDN and branch sources from the environment.
func Sources(httpClient *http.Client) (CDN, *Branch) {
	base := os.Getenv("CLAUDINITE_PACKS_CDN")
	if base == "" {
		base = DefaultCDN
	}
	repo := os.Getenv("CLAUDINITE_PACKS_REPO")
	if repo == "" {
		repo = DefaultRepo
	}
	return CDN{Base: base, HTTP: httpClient, MaxBytes: defaultMaxCDN}, &Branch{Repo: repo}
}

// GitHubBase is where a repo source's owner/name is cloned from.
const GitHubBase = "https://github.com"

// SourcesFor builds a repo's pack sources in order: an https:// entry is a
// CDN base, any other an owner/name whose vendored branch is read; none
// is the shelf. close removes every branch's clone.
func SourcesFor(list []string, httpClient *http.Client) ([]Source, func()) {
	if len(list) == 0 {
		cdn, branch := Sources(httpClient)
		return []Source{cdn, branch}, branch.Close
	}
	var srcs []Source
	var branches []*Branch
	for _, s := range list {
		if strings.HasPrefix(s, "https://") {
			srcs = append(srcs, CDN{Base: s, HTTP: httpClient, MaxBytes: defaultMaxCDN})
			continue
		}
		b := &Branch{Repo: GitHubBase + "/" + s, Label: s}
		branches = append(branches, b)
		srcs = append(srcs, b)
	}
	return srcs, func() {
		for _, b := range branches {
			b.Close()
		}
	}
}

// Catalog paths, beside the pack directories on both sources.
const (
	CatalogFile    = packindex.CatalogFile
	CatalogSigFile = packindex.CatalogSigFile
)

// CatalogSource is a source that also serves the shelf's catalog.
type CatalogSource interface {
	Source
	// Catalog returns catalog.json and catalog.sig.json bytes.
	Catalog() (catalog, sig []byte, err error)
}

// Catalog reads the pair.
func (c CDN) Catalog() ([]byte, []byte, error) {
	cat, err := c.get(CatalogFile)
	if err != nil {
		return nil, nil, err
	}
	sig, err := c.get(CatalogSigFile)
	if err != nil {
		return nil, nil, err
	}
	return cat, sig, nil
}

// Catalog reads the pair.
func (b *Branch) Catalog() ([]byte, []byte, error) {
	cat, err := b.show(CatalogFile)
	if err != nil {
		return nil, nil, err
	}
	sig, err := b.show(CatalogSigFile)
	if err != nil {
		return nil, nil, err
	}
	return cat, sig, nil
}

// VerifiedCatalog is a catalog whose signature, format and serial checked
// out, and the source whose copy it is.
type VerifiedCatalog struct {
	Catalog  packindex.Catalog
	From     string
	KeyID    string
	Raw, Sig []byte
}

// VerifiedCatalog reads the shelf's catalog from every source that serves
// one, under the rule VerifiedIndex keeps for an index: a pair that does
// not verify refuses the read, a later source older than an earlier one
// is a *SourcesDisagree, and the highest serial is used.
func (r *Reader) VerifiedCatalog() (VerifiedCatalog, error) {
	var best *VerifiedCatalog
	var unreachable, answered []string
	var serials []SourceSerial
	disagree := false
	for _, src := range r.Sources {
		cs, ok := src.(CatalogSource)
		if !ok {
			continue
		}
		v, err := r.catalogFrom(cs)
		var u errUnreachable
		if errors.As(err, &u) {
			unreachable = append(unreachable, src.Name()+": "+u.Error())
			continue
		}
		if err != nil {
			return VerifiedCatalog{}, fmt.Errorf("pack catalog from %s refused: %w", src.Name(), err)
		}
		if best != nil && v.Catalog.Serial < best.Catalog.Serial {
			disagree = true
		}
		serials = append(serials, SourceSerial{src.Name(), v.Catalog.Serial})
		answered = append(answered, fmt.Sprintf("%s serial %d", src.Name(), v.Catalog.Serial))
		if best == nil || v.Catalog.Serial > best.Catalog.Serial {
			vv := v
			best = &vv
		}
	}
	for _, u := range unreachable {
		r.logf("catalog unreachable from %s", u)
	}
	if best == nil {
		return VerifiedCatalog{}, fmt.Errorf("pack catalog: no source answered (%s)", strings.Join(unreachable, "; "))
	}
	if disagree {
		return VerifiedCatalog{}, &SourcesDisagree{Serials: serials}
	}
	r.logf("catalog serial %d from %s (read: %s)", best.Catalog.Serial, best.From, strings.Join(answered, ", "))
	return *best, nil
}

// catalogFrom reads one source's pair and verifies it, asking once more
// when it does not verify, since the writer rewrites the two files one
// after the other.
func (r *Reader) catalogFrom(src CatalogSource) (VerifiedCatalog, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		cat, sigRaw, err := src.Catalog()
		if err != nil {
			return VerifiedCatalog{}, errUnreachable{err}
		}
		var s sign.SignedPackCatalog
		if err := json.Unmarshal(sigRaw, &s); err != nil {
			lastErr = fmt.Errorf("%s: %w", CatalogSigFile, err)
			continue
		}
		body, err := sign.VerifyPackCatalog(s, cat, r.Roots, r.Now())
		if err != nil {
			lastErr = err
			continue
		}
		c, err := packindex.DecodeCatalog(cat)
		if err != nil {
			return VerifiedCatalog{}, err
		}
		return VerifiedCatalog{Catalog: c, From: src.Name(), KeyID: body.KeyID, Raw: cat, Sig: sigRaw}, nil
	}
	return VerifiedCatalog{}, lastErr
}
