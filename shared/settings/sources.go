package settings

import (
	"errors"
	"fmt"
	"regexp"
)

// The packs block's sources name where the repo reads new pack versions,
// each later one a backup:
//
//	packs:                       [packs]                        "packs": {
//	  sources:                   sources = ["acme/fleet"]         "sources": ["acme/fleet"]
//	    - "acme/fleet"                                          }
//
// "<owner>/<name>" is that GitHub repo's vendored branch and
// "https://<host>" a CDN base. A block naming no sources reads the shelf.

var (
	sourceRepo = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+$`)
	sourceCDN  = regexp.MustCompile(`^https://[A-Za-z0-9.-]+(:[0-9]+)?(/[^\s]*)?$`)
)

// IsRepoSource reports whether a source names a GitHub repo.
func IsRepoSource(s string) bool { return sourceRepo.MatchString(s) }

// IsCDNSource reports whether a source names a CDN base.
func IsCDNSource(s string) bool { return sourceCDN.MatchString(s) }

func checkSources(list []string) error {
	if len(list) == 0 {
		return errors.New("packs.sources must name at least one source; leave it out to read the shelf")
	}
	seen := map[string]bool{}
	for _, s := range list {
		if !IsRepoSource(s) && !IsCDNSource(s) {
			return fmt.Errorf("packs.sources: %q is neither <owner>/<name> nor https://<host>", s)
		}
		if seen[s] {
			return fmt.Errorf("packs.sources names %s twice", s)
		}
		seen[s] = true
	}
	return nil
}

func parseSources(raw any) ([]string, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, errors.New("packs.sources must be a list")
	}
	list := make([]string, 0, len(items))
	for _, it := range items {
		s, ok := it.(string)
		if !ok {
			return nil, fmt.Errorf("packs.sources holds %v; each source is a string", it)
		}
		list = append(list, s)
	}
	if err := checkSources(list); err != nil {
		return nil, err
	}
	return list, nil
}

// SetSources sets packs.sources, rewriting the packs block in its place.
func SetSources(raw []byte, f Format, list []string) ([]byte, error) {
	if err := checkSources(list); err != nil {
		return nil, err
	}
	packs, err := orderedPacks(raw, f)
	if err != nil {
		return nil, err
	}
	items := make([]any, len(list))
	for i, s := range list {
		items[i] = s
	}
	packs.Set("sources", items)
	return replacePacks(raw, f, packs)
}
