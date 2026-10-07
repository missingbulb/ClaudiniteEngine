package packindex

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/jsjson"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/jsregex"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/version"
)

// The catalog is the shelf as one signed file, catalog.json beside the
// pack indexes: for every pack, its newest stable and its newest canary
// version that is not revoked, with the manifest fields a pack suggester reads
// and the per-pack index does not carry (its fingerprint, what it is for,
// its adoption questions). The pack release writes it from the vendored
// branch on every publish, promote and revoke; catalog.sig.json is its
// signature by a packs key (shared/sign.VerifyPackCatalog), checked before
// DecodeCatalog. Like an index, unknown top-level and entry fields are
// ignored and only an unknown "v" is refused.

// MaxSearchTerms is how many code-search terms one fingerprint may name:
// GitHub's code search joins at most six with OR.
const MaxSearchTerms = 6

// Pattern is one fingerprint pattern, as `new RegExp(source, flags)`.
type Pattern struct {
	Source string `json:"source"`
	Flags  string `json:"flags"`
	re     *jsregex.Regex
}

// Test reports whether s matches.
func (p Pattern) Test(s string) bool { return p.re.Test(s) }

// Detector is a pack's relevanceDetector: which tracked paths suggest the
// pack, and optionally what one of those files must contain. It only
// suspects a pack is wanted; declaring one is the project's call.
type Detector struct {
	About  string    `json:"about"`
	Paths  Pattern   `json:"paths"`
	Text   []Pattern `json:"text,omitempty"`
	Search []string  `json:"search,omitempty"`
}

// Candidates is every tracked path the detector's paths pattern matches.
func (d *Detector) Candidates(tracked []string) []string {
	var out []string
	for _, f := range tracked {
		if d.Paths.Test(f) {
			out = append(out, f)
		}
	}
	return out
}

// Question is one adoption-interview question a pack asks.
type Question struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
}

// CatalogPack is one pack version the catalog offers.
type CatalogPack struct {
	ID                string     `json:"id"`
	Version           string     `json:"version"`
	Channel           string     `json:"channel"`
	MinEngineVersion  string     `json:"minEngineVersion"`
	Requires          []string   `json:"requires"`
	RelevanceDetector *Detector  `json:"relevanceDetector"`
	Belongs           string     `json:"belongs,omitempty"`
	Questions         []Question `json:"questions,omitempty"`
}

// Catalog is a decoded catalog.json.
type Catalog struct {
	V      int
	Serial int64
	Packs  []CatalogPack
}

// DecodeCatalog reads catalog.json, refusing a "v" other than 1, a serial
// that is not a positive integer, an entry without an id or a version
// ComparePack reads, a channel outside canary and stable, two entries for
// one pack and channel, and a fingerprint ValidateDetector refuses or
// whose patterns do not compile.
func DecodeCatalog(raw []byte) (Catalog, error) {
	var top struct {
		V      json.RawMessage   `json:"v"`
		Serial json.RawMessage   `json:"serial"`
		Packs  []json.RawMessage `json:"packs"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return Catalog{}, fmt.Errorf("catalog.json: %w", err)
	}
	if string(top.V) != "1" {
		return Catalog{}, fmt.Errorf("catalog.json format v%s is not supported", orMissing(top.V))
	}
	var serial int64
	if err := json.Unmarshal(top.Serial, &serial); err != nil || serial < 1 {
		return Catalog{}, fmt.Errorf("catalog.json serial %s is not a positive integer", orMissing(top.Serial))
	}
	c := Catalog{V: 1, Serial: serial}
	seen := map[string]bool{}
	var problems []string
	for i, rawPack := range top.Packs {
		p, err := decodeCatalogPack(rawPack)
		if err != nil {
			problems = append(problems, fmt.Sprintf("entry %d: %v", i, err))
			continue
		}
		key := p.ID + "@" + p.Channel
		if seen[key] {
			problems = append(problems, fmt.Sprintf("%s lists two %s versions", p.ID, p.Channel))
			continue
		}
		seen[key] = true
		c.Packs = append(c.Packs, p)
	}
	if len(problems) > 0 {
		return Catalog{}, errors.New("catalog.json: " + strings.Join(problems, "; "))
	}
	return c, nil
}

func decodeCatalogPack(raw json.RawMessage) (CatalogPack, error) {
	var p struct {
		CatalogPack
		RelevanceDetector json.RawMessage `json:"relevanceDetector"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return CatalogPack{}, err
	}
	out := p.CatalogPack
	if out.ID == "" {
		return out, errors.New("no id")
	}
	if _, err := version.ComparePack(out.Version, out.Version); err != nil {
		return out, fmt.Errorf("%s: version %q: %v", out.ID, out.Version, err)
	}
	if out.Channel != Stable && out.Channel != Canary {
		return out, fmt.Errorf("%s %s: channel %q is neither canary nor stable", out.ID, out.Version, out.Channel)
	}
	if out.Requires == nil {
		out.Requires = []string{}
	}
	d, err := DecodeDetector(p.RelevanceDetector)
	if err != nil {
		return out, fmt.Errorf("%s %s: %v", out.ID, out.Version, err)
	}
	out.RelevanceDetector = d
	return out, nil
}

var detectorKeys = []string{"about", "paths", "text", "search"}

// DecodeDetector reads a relevanceDetector as data, nil for null or
// absent; every pattern is {source, flags} and is compiled as JavaScript
// compiles it. A detector ValidateDetector refuses is an error naming each
// problem.
func DecodeDetector(raw json.RawMessage) (*Detector, error) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || string(t) == "null" {
		return nil, nil
	}
	if errs := ValidateDetector(t); len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "; "))
	}
	var d Detector
	if err := json.Unmarshal(t, &d); err != nil {
		return nil, fmt.Errorf("relevanceDetector: %w", err)
	}
	compile := func(p *Pattern) error {
		re, err := jsregex.Compile(p.Source, p.Flags)
		if err != nil {
			return fmt.Errorf("relevanceDetector pattern /%s/%s does not compile: %v", p.Source, p.Flags, err)
		}
		p.re = re
		return nil
	}
	if err := compile(&d.Paths); err != nil {
		return nil, err
	}
	for i := range d.Text {
		if err := compile(&d.Text[i]); err != nil {
			return nil, err
		}
	}
	return &d, nil
}

// ValidateDetector is the Node engine's validateRelevanceDetector over the
// data form: each problem as a sentence, none for a well-formed detector
// or null. An undeclared key is named in the order the detector writes it.
func ValidateDetector(raw json.RawMessage) []string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return []string{"relevanceDetector does not parse: " + err.Error()}
	}
	if v == nil {
		return nil
	}
	o, ok := v.(map[string]any)
	if !ok {
		return []string{"relevanceDetector is an object or null"}
	}
	var errs []string
	ordered, _ := jsjson.Decode(raw)
	for _, k := range ordered.Keys {
		if !contains(detectorKeys, k) {
			errs = append(errs, fmt.Sprintf("relevanceDetector declares %q, which is not one of %s", k, strings.Join(detectorKeys, ", ")))
		}
	}
	if about, ok := o["about"].(string); !ok || strings.TrimSpace(about) == "" {
		errs = append(errs, "relevanceDetector.about names what is found, in words")
	}
	all := []map[string]any{}
	if p, ok := pattern(o["paths"]); ok {
		all = append(all, p)
	} else {
		errs = append(errs, "relevanceDetector.paths is a RegExp over tracked paths")
	}
	var text []any
	switch t := o["text"].(type) {
	case nil:
	case []any:
		text = t
	default:
		text = []any{t}
	}
	textOK := true
	for _, t := range text {
		if p, ok := pattern(t); ok {
			all = append(all, p)
		} else {
			textOK = false
		}
	}
	if !textOK {
		errs = append(errs, "relevanceDetector.text is a RegExp or a list of them")
	}
	for _, p := range all {
		if f, _ := p["flags"].(string); strings.ContainsAny(f, "gy") {
			errs = append(errs, "a relevanceDetector pattern carries the g or y flag, which makes .test stateful")
			break
		}
	}
	search, isList := o["search"].([]any)
	if len(text) > 0 {
		good := isList && len(search) > 0
		for _, s := range search {
			if str, ok := s.(string); !ok || strings.TrimSpace(str) == "" {
				good = false
			}
		}
		if !good {
			errs = append(errs, "relevanceDetector.search lists the code-search terms that find every file relevanceDetector.text matches")
		}
	}
	if isList && len(search) > MaxSearchTerms {
		errs = append(errs, fmt.Sprintf("relevanceDetector.search names %d terms; one code search joins at most six", len(search)))
	}
	return errs
}

// pattern is a {source, flags} object, flags optional.
func pattern(v any) (map[string]any, bool) {
	o, ok := v.(map[string]any)
	if !ok {
		return nil, false
	}
	if _, ok := o["source"].(string); !ok {
		return nil, false
	}
	if f, present := o["flags"]; present {
		if _, ok := f.(string); !ok {
			return nil, false
		}
	}
	return o, true
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// For is the catalog as a member on channel sees it: one entry per pack,
// in id order. A stable member reads the stable entry; a canary member
// reads the newer of the two, as its own update would.
func (c Catalog) For(channel string) []CatalogPack {
	byID := map[string]CatalogPack{}
	for _, p := range c.Packs {
		prior, ok := byID[p.ID]
		switch {
		case p.Channel == Canary && channel != Canary:
		case !ok:
			byID[p.ID] = p
		case newer(p.Version, prior.Version):
			byID[p.ID] = p
		}
	}
	out := make([]CatalogPack, 0, len(byID))
	for _, p := range byID {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
