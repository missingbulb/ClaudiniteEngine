package provenance

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// WriteIO is a tree a verb reads and writes over repo-relative,
// slash-separated paths.
type WriteIO interface {
	Exists(p string) bool
	Read(p string) (string, bool)
	ListDir(p string) ([]string, bool)
	Write(p, text string) error
}

// Checkout is the WriteIO over a working tree at Root.
type Checkout struct{ Root string }

func (c Checkout) abs(p string) string { return filepath.Join(c.Root, filepath.FromSlash(p)) }

// Exists probes p.
func (c Checkout) Exists(p string) bool {
	_, err := os.Stat(c.abs(p))
	return err == nil
}

// Read reads p.
func (c Checkout) Read(p string) (string, bool) {
	raw, err := os.ReadFile(c.abs(p))
	if err != nil {
		return "", false
	}
	return string(raw), true
}

// ListDir names p's entries, false where p is no directory.
func (c Checkout) ListDir(p string) ([]string, bool) {
	entries, err := os.ReadDir(c.abs(p))
	if err != nil {
		return nil, false
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out, true
}

// Write writes p, making its directories.
func (c Checkout) Write(p, text string) error {
	if err := os.MkdirAll(filepath.Dir(c.abs(p)), 0o755); err != nil {
		return err
	}
	return os.WriteFile(c.abs(p), []byte(text), 0o644)
}

// Overlay is a copy-on-write WriteIO over another, which a dry run writes
// to so its report is the real one and the tree is untouched.
type Overlay struct {
	Base    WriteIO
	written map[string]string
}

// NewOverlay is an Overlay over base.
func NewOverlay(base WriteIO) *Overlay { return &Overlay{Base: base, written: map[string]string{}} }

// Exists probes p.
func (o *Overlay) Exists(p string) bool {
	_, ok := o.written[p]
	return ok || o.Base.Exists(p)
}

// Read reads p.
func (o *Overlay) Read(p string) (string, bool) {
	if t, ok := o.written[p]; ok {
		return t, true
	}
	return o.Base.Read(p)
}

// ListDir merges what the overlay wrote below p.
func (o *Overlay) ListDir(p string) ([]string, bool) {
	if _, ok := o.written[p]; ok {
		return nil, false
	}
	base, any := o.Base.ListDir(p)
	seen := map[string]bool{}
	var names []string
	for _, n := range base {
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	for w := range o.written {
		if strings.HasPrefix(w, p+"/") {
			n := strings.SplitN(w[len(p)+1:], "/", 2)[0]
			any = true
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	if !any {
		return nil, false
	}
	sort.Strings(names)
	return names, true
}

// Write records p.
func (o *Overlay) Write(p, text string) error {
	o.written[p] = text
	return nil
}
