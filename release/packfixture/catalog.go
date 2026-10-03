package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// CatalogPattern is a fingerprint pattern in the catalog's one form.
type CatalogPattern struct {
	Source string `json:"source"`
	Flags  string `json:"flags"`
}

// CatalogDetector is a relevanceDetector with every pattern in that form
// and text always a list.
type CatalogDetector struct {
	About  string           `json:"about"`
	Paths  CatalogPattern   `json:"paths"`
	Text   []CatalogPattern `json:"text,omitempty"`
	Search []string         `json:"search,omitempty"`
}

// CatalogQuestion is one adoption question, id and prompt.
type CatalogQuestion struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
}

// CatalogEntry is one pack version, in docs/release.md's key order.
type CatalogEntry struct {
	ID                string            `json:"id"`
	Version           string            `json:"version"`
	Channel           string            `json:"channel"`
	MinEngineVersion  string            `json:"minEngineVersion"`
	Requires          []string          `json:"requires"`
	RelevanceDetector *CatalogDetector  `json:"relevanceDetector"`
	Belongs           string            `json:"belongs,omitempty"`
	Questions         []CatalogQuestion `json:"questions,omitempty"`
}

// Catalog is catalog.json.
type Catalog struct {
	V      int            `json:"v"`
	Serial int            `json:"serial"`
	Packs  []CatalogEntry `json:"packs"`
}

// pattern is a manifest pattern, a string or {source, flags}, in the
// catalog's form.
func pattern(v any) CatalogPattern {
	switch p := v.(type) {
	case string:
		return CatalogPattern{Source: p}
	case map[string]any:
		s, _ := p["source"].(string)
		f, _ := p["flags"].(string)
		return CatalogPattern{Source: s, Flags: f}
	}
	return CatalogPattern{}
}

func detector(raw json.RawMessage) *CatalogDetector {
	var d struct {
		About  string          `json:"about"`
		Paths  any             `json:"paths"`
		Text   json.RawMessage `json:"text"`
		Search []string        `json:"search"`
	}
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &d) != nil {
		return nil
	}
	out := &CatalogDetector{About: d.About, Paths: pattern(d.Paths), Search: d.Search}
	var text any
	_ = json.Unmarshal(d.Text, &text)
	switch t := text.(type) {
	case nil:
	case []any:
		for _, p := range t {
			out.Text = append(out.Text, pattern(p))
		}
	default:
		out.Text = []CatalogPattern{pattern(t)}
	}
	return out
}

// manifestOf is the pack.json inside a published archive.
func manifestOf(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err != nil {
			return nil, fmt.Errorf("no pack.json: %w", err)
		}
		if h.Name == "pack.json" {
			return io.ReadAll(tr)
		}
	}
}

// entry is the catalog entry for one published version.
func entry(tree, pack string, e Entry) (CatalogEntry, error) {
	archive, err := os.ReadFile(filepath.Join(tree, pack, e.Version+".tar.gz"))
	if err != nil {
		return CatalogEntry{}, err
	}
	raw, err := manifestOf(archive)
	if err != nil {
		return CatalogEntry{}, fmt.Errorf("%s %s: %w", pack, e.Version, err)
	}
	var pj struct {
		RelevanceDetector json.RawMessage `json:"relevanceDetector"`
		RuleRouting       struct {
			Belongs string `json:"belongs"`
		} `json:"ruleRoutingGuidance"`
		Questions []CatalogQuestion `json:"questions"`
	}
	if err := json.Unmarshal(raw, &pj); err != nil {
		return CatalogEntry{}, fmt.Errorf("%s %s: pack.json: %w", pack, e.Version, err)
	}
	return CatalogEntry{ID: pack, Version: e.Version, Channel: e.Channel, MinEngineVersion: e.MinEngineVersion,
		Requires: e.Requires, RelevanceDetector: detector(pj.RelevanceDetector), Belongs: pj.RuleRouting.Belongs, Questions: pj.Questions}, nil
}

// writeCatalog writes catalog.json from every index in tree, as the pack
// release does: per pack, its newest stable and its newest canary version
// that is not revoked, the serial the sum of the indexes', signed beside
// it as catalog.sig.json.
func writeCatalog(tree string, priv ed25519.PrivateKey, c sign.Certificate) error {
	indexes, err := filepath.Glob(filepath.Join(tree, "*", "index.json"))
	if err != nil {
		return err
	}
	sort.Strings(indexes)
	cat := Catalog{V: 1, Packs: []CatalogEntry{}}
	for _, p := range indexes {
		ix, err := readIndex(tree, filepath.Base(filepath.Dir(p)))
		if err != nil {
			return err
		}
		cat.Serial += ix.Serial
		newest := map[string]Entry{}
		for _, e := range ix.Versions {
			if e.Revoked {
				continue
			}
			if prior, ok := newest[e.Channel]; ok {
				if n, err := version.ComparePack(e.Version, prior.Version); err != nil || n <= 0 {
					continue
				}
			}
			newest[e.Channel] = e
		}
		for _, ch := range []string{"stable", "canary"} {
			e, ok := newest[ch]
			if !ok {
				continue
			}
			ce, err := entry(tree, ix.Pack, e)
			if err != nil {
				return err
			}
			cat.Packs = append(cat.Packs, ce)
		}
	}
	if cat.Serial < 1 {
		return nil
	}
	raw, err := json.MarshalIndent(cat, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	sig, err := json.MarshalIndent(sign.SignPackCatalog(priv, c, raw), "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tree, "catalog.json"), raw, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(tree, "catalog.sig.json"), append(sig, '\n'), 0o644)
}
