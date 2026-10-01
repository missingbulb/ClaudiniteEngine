// Package packset reads what a member holds of its packs: the declaration
// in its settings and the vendored trees under .claudinite/shared/packs/.
// Hooks, verify, the checks build and the lifecycle commands share it, so
// they agree on what "declared" and "held" mean.
package packset

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
)

// Dir is where a member's vendored packs live, relative to its root.
const Dir = ".claudinite/shared/packs"

// TreeRel is pack id's vendored tree, relative to the repo root, with
// forward slashes.
func TreeRel(id string) string { return Dir + "/" + id }

// Tree is pack id's vendored tree under repo.
func Tree(repo, id string) string { return filepath.Join(repo, filepath.FromSlash(TreeRel(id))) }

// Manifest is what the engine reads of a pack's pack.json.
type Manifest struct {
	Version          string   `json:"version"`
	MinEngineVersion string   `json:"minEngineVersion"`
	Requires         []string `json:"requires"`
}

// ErrNoManifest is returned when a tree holds no pack manifest at all.
var ErrNoManifest = errors.New("holds no pack.json")

// ReadManifest reads dir/pack.json. Only JSON is read this chunk: a
// pack.yaml or pack.toml is refused naming phase 6, which brings the
// parsers.
func ReadManifest(dir string) (Manifest, error) {
	for _, alt := range []string{"pack.yaml", "pack.toml"} {
		if _, err := os.Stat(filepath.Join(dir, alt)); err == nil {
			return Manifest{}, fmt.Errorf("holds %s; this engine reads only pack.json until the descriptor parsers arrive (phase 6)", alt)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "pack.json"))
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{}, ErrNoManifest
	}
	if err != nil {
		return Manifest{}, err
	}
	return ParseManifest(raw)
}

// ParseManifest reads pack.json's bytes.
func ParseManifest(raw []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, fmt.Errorf("pack.json: %w", err)
	}
	if m.Version == "" {
		return Manifest{}, errors.New("pack.json has no version")
	}
	return m, nil
}

// Declared reads the repo's settings file and its packs block.
func Declared(repo string) (settings.Packs, error) {
	path, f, err := settings.Find(repo)
	if err != nil {
		return settings.Packs{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return settings.Packs{}, err
	}
	p, err := settings.ReadPacks(raw, f)
	if err != nil {
		return settings.Packs{}, fmt.Errorf("%s: %w", settings.RelPath(f), err)
	}
	return p, nil
}

// Vendored lists the ids of the trees under Dir, sorted.
func Vendored(repo string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(repo, filepath.FromSlash(Dir)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}
