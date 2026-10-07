package parity

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
)

// Deferred reads parity/deferred.txt: each Node coded check of a ported
// pack that cn does not run yet, by rule id, with the slice that ports it.
func Deferred() (map[string]string, error) {
	_, self, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(self), "deferred.txt"))
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for i, l := range strings.Split(string(raw), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		f := strings.Fields(l)
		if len(f) != 2 {
			return nil, fmt.Errorf("deferred.txt:%d: a line is <rule id> <slice>, not %q", i+1, l)
		}
		if _, dup := out[f[0]]; dup {
			return nil, fmt.Errorf("deferred.txt:%d: %s is named twice", i+1, f[0])
		}
		out[f[0]] = f[1]
	}
	return out, nil
}

// Answered are the Node coded checks whose question cn verify answers
// instead, each a directory of testdata/answered, by rule id.
func Answered() (map[string]string, error) {
	_, self, _, _ := runtime.Caller(0)
	dirs, err := os.ReadDir(filepath.Join(filepath.Dir(self), "testdata", "answered"))
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, d := range dirs {
		if d.IsDir() {
			out[d.Name()] = "answered"
		}
	}
	return out, nil
}

// Explained are the subtractions cn knows of: the deferred rules and the
// answered ones; a rule in both is an error.
func Explained() (map[string]string, error) {
	deferred, err := Deferred()
	if err != nil {
		return nil, err
	}
	answered, err := Answered()
	if err != nil {
		return nil, err
	}
	for id, v := range answered {
		if deferred[id] != "" {
			return nil, fmt.Errorf("%s is both deferred and answered", id)
		}
		deferred[id] = v
	}
	return deferred, nil
}

var shelfID = regexp.MustCompile(`\bid\s*:\s*['"]([^'"]+)['"]`)

// ShelfChecks are the coded checks of the frozen shelf under root, by rule
// id, with the pack that carries each: the id: literals of every
// packs/<pack>/worldRules/*.mjs, workRules/*.mjs and skills/*/*.mjs.
func ShelfChecks(root string) (map[string]string, error) {
	out := map[string]string{}
	packs := filepath.Join(root, "packs")
	err := filepath.WalkDir(packs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".mjs") || strings.HasSuffix(p, ".test.mjs") {
			return nil
		}
		rel, _ := filepath.Rel(packs, p)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		coded := len(parts) == 3 && (parts[1] == "worldRules" || parts[1] == "workRules") ||
			len(parts) == 4 && parts[1] == "skills"
		if !coded {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range shelfID.FindAllStringSubmatch(string(raw), -1) {
			if _, ok := out[m[1]]; !ok {
				out[m[1]] = parts[0]
			}
		}
		return nil
	})
	return out, err
}

// StrayDeferrals are the deferred rules the shelf does not carry, sorted.
func StrayDeferrals(deferred, shelf map[string]string) []string {
	var out []string
	for id := range deferred {
		if _, ok := shelf[id]; !ok {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// unexplained says why a rule subtracted from Node's findings is not one
// cn knows it does not run here, or "" when it is: a Node coded check of
// a pack not yet ported, or a rule deferred.txt names.
func unexplained(rule string, listed map[string]Listed, declared []string, shelf, deferred map[string]string, ported map[string]bool) string {
	l, ok := listed[rule]
	switch {
	case ok && l.Kind != "coded":
		return fmt.Sprintf("subtracted %s, which cn lists as %s %v", rule, l.Kind, l.Tags)
	case ok && ported[l.Pack]:
		return fmt.Sprintf("subtracted %s, a check of ported pack %s that cn runs at another moment %v", rule, l.Pack, l.Tags)
	case !ok && slices.Contains(declared, rule):
		return fmt.Sprintf("subtracted %s, a declared check cn does not list: it did not load", rule)
	}
	if deferred[rule] != "" {
		return ""
	}
	pack, onShelf := shelf[rule]
	switch {
	case onShelf && !ported[pack]:
		return ""
	case onShelf:
		return fmt.Sprintf("subtracted %s, a coded check of ported pack %s, which deferred.txt does not name", rule, pack)
	}
	return fmt.Sprintf("subtracted %s, neither a coded check of an unported pack nor a line of deferred.txt", rule)
}
