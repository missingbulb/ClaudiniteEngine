// Package scaffold creates the local pack a repo's own lessons land in:
// the manifest, the rule file and the pack's provenance file, declared
// as local/<name> so it is active from the next session.
package scaffold

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/growth/provenance"
	prov "github.com/missingbulb/ClaudiniteEngine/cn/shared/provenance"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
)

// Placeholder routing guidance, for the first rule that lands to replace.
const (
	PlaceholderBelongs  = "this repo's own lessons; the first rule that lands here replaces this line"
	PlaceholderExcludes = "anything a declared canon pack already owns"
)

// Request is one new local pack.
type Request struct {
	Repo, Name        string
	Belongs, Excludes string
	Now               time.Time
}

// Made is what a scaffold wrote: the files, relative to the repo, and the
// settings file the declaration went into.
type Made struct {
	Files    []string
	Settings string
	Token    string
}

func manifest(r Request) ([]byte, error) {
	belongs, excludes := r.Belongs, r.Excludes
	if belongs == "" {
		belongs = PlaceholderBelongs
	}
	if excludes == "" {
		excludes = PlaceholderExcludes
	}
	type guidance struct {
		Belongs  string `json:"belongs"`
		Excludes string `json:"excludes"`
	}
	raw, err := json.MarshalIndent(struct {
		Version  string   `json:"version"`
		Guidance guidance `json:"ruleRoutingGuidance"`
	}{"1", guidance{belongs, excludes}}, "", "  ")
	return append(raw, '\n'), err
}

func born(r Request) string {
	reason := "a home for the lessons this repo keeps for itself"
	if r.Belongs != "" {
		reason += ": " + r.Belongs
	}
	return provenance.Render(provenance.Entry{Date: r.Now.UTC().Format("2006-01-02"), Kind: "born", Title: "the local pack " + r.Name,
		Fields: []provenance.Field{
			{Name: "Reason", Value: reason + "."},
			{Name: "Actor", Value: "whoever ran `cn pack new " + r.Name + "`."},
			{Name: "Mechanism", Value: "the pack manifest."},
		}})
}

// New writes the pack and declares it, refusing a name already declared
// (as a local pack or a canon one, which a local pack may not shadow) or
// a directory already present. Nothing is written when it refuses.
func New(r Request) (Made, error) {
	if !settings.PackIDPattern.MatchString(r.Name) {
		return Made{}, fmt.Errorf("%q is not a pack name (lowercase letters, digits and dashes)", r.Name)
	}
	token := provenance.LocalPrefix + r.Name
	path, f, err := settings.Find(r.Repo)
	if err != nil {
		return Made{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Made{}, err
	}
	p, err := settings.ReadPacks(raw, f)
	if err != nil {
		return Made{}, err
	}
	for _, e := range p.Entries {
		if e.Token() == token || e.Token() == r.Name {
			return Made{}, fmt.Errorf("%s is already declared", e.Token())
		}
	}
	rel := prov.PackRoots[1] + "/" + r.Name
	dir := filepath.Join(r.Repo, filepath.FromSlash(rel))
	if _, err := os.Stat(dir); err == nil {
		return Made{}, fmt.Errorf("%s already exists", rel)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Made{}, err
	}
	declared, err := settings.AddDeclared(raw, f, token)
	if err != nil {
		return Made{}, err
	}
	m, err := manifest(r)
	if err != nil {
		return Made{}, err
	}
	files := []struct {
		rel  string
		body string
	}{
		{rel + "/pack.json", string(m)},
		{rel + "/RULES.md", "# " + r.Name + "\n"},
		{rel + "/" + prov.Dir + "/" + prov.FileOfID(prov.PackElement), born(r)},
	}
	made := Made{Token: token}
	for _, w := range files {
		abs := filepath.Join(r.Repo, filepath.FromSlash(w.rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return made, err
		}
		if err := os.WriteFile(abs, []byte(w.body), 0o644); err != nil {
			return made, err
		}
		made.Files = append(made.Files, w.rel)
	}
	if err := os.WriteFile(path, declared, 0o644); err != nil {
		return made, err
	}
	made.Settings, _ = filepath.Rel(r.Repo, path)
	made.Settings = filepath.ToSlash(made.Settings)
	return made, nil
}
