package workflows

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// StagingDir holds the workflow files an engine update computed for a
// member but cannot push: the job token that pushes the update branch may
// not write .github/workflows/, and GitHub refuses the whole push for
// trying. An agent stage, whose credential may, moves each file from here
// to .github/workflows/ unedited.
const StagingDir = ".claudinite/cache/pending-workflows"

// StagedPath is where name waits, repo-relative.
func StagedPath(name string) string { return StagingDir + "/" + name }

// Pending is each workflow whose expected content (Expected) differs from
// repo's copy, by file name, with that content.
func Pending(repo, fullName string) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, name := range Names {
		rel := ".github/workflows/" + name
		have, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(rel)))
		if errors.Is(err, os.ErrNotExist) {
			have = nil
		} else if err != nil {
			return nil, err
		}
		want, err := Expected(name, have, fullName)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		if have == nil || !bytes.Equal(have, want) {
			out[name] = want
		}
	}
	return out, nil
}

// Stage writes Pending into repo's staging directory in place of whatever
// was there, and returns the staged paths, repo-relative and sorted; none
// when the workflows are already what this binary expects.
func Stage(repo, fullName string) ([]string, error) {
	pending, err := Pending(repo, fullName)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(repo, filepath.FromSlash(StagingDir))
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	var staged []string
	for name, data := range pending {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return nil, err
		}
		staged = append(staged, StagedPath(name))
	}
	sort.Strings(staged)
	return staged, nil
}
