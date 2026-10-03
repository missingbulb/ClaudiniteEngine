package roster

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/fleet"
)

// RosterFile is the manager's published roster: one fleet.Verdict per
// repository the sweep enumerated, for a fleet page to read freshness
// from instead of pricing each member itself.
const RosterFile = ".claudinite/fleet/roster.GENERATED.json"

// ArtifactVersion is the roster file's shape.
const ArtifactVersion = 1

// Artifact is the roster file.
type Artifact struct {
	Version   int             `json:"version"`
	Generated string          `json:"generated"`
	Owner     string          `json:"owner"`
	Members   []fleet.Verdict `json:"members"`
}

// Render is the roster file's text: the verdicts sorted by repository,
// two-space JSON and a trailing newline.
func Render(owner string, verdicts []fleet.Verdict, generated string) (string, error) {
	members := append([]fleet.Verdict{}, verdicts...)
	sort.SliceStable(members, func(i, j int) bool {
		a, b := strings.ToLower(members[i].Repo), strings.ToLower(members[j].Repo)
		if a != b {
			return a < b
		}
		return members[i].Repo < members[j].Repo
	})
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(Artifact{Version: ArtifactVersion, Generated: generated, Owner: owner, Members: members}); err != nil {
		return "", err
	}
	return b.String(), nil
}

// Verdicts are the roster's rows.
func Verdicts(roster []Entry) []fleet.Verdict {
	out := make([]fleet.Verdict, 0, len(roster))
	for _, e := range roster {
		out = append(out, e.Verdict)
	}
	return out
}

// WriteArtifact writes the roster file into the checkout at root when
// anything but its generated stamp changed, reporting whether it did.
func WriteArtifact(root, owner string, verdicts []fleet.Verdict, generated string) (bool, error) {
	p := filepath.Join(root, filepath.FromSlash(RosterFile))
	if old, err := os.ReadFile(p); err == nil {
		var prev struct {
			Generated string `json:"generated"`
		}
		if json.Unmarshal(old, &prev) == nil {
			same, err := Render(owner, verdicts, prev.Generated)
			if err != nil {
				return false, err
			}
			if same == string(old) {
				return false, nil
			}
		}
	}
	text, err := Render(owner, verdicts, generated)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(p, []byte(text), 0o644)
}
