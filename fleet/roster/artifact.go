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
// anything but its generated stamp changed from prior, reporting whether
// it did. prior is the file as the branch it lands on holds it, nil to
// read the checkout's own; a stamp-only recompute leaves prior's bytes in
// the checkout, so the tree handed on is the one already landed.
func WriteArtifact(root, owner string, verdicts []fleet.Verdict, generated string, prior []byte) (bool, error) {
	p := filepath.Join(root, filepath.FromSlash(RosterFile))
	current, err := os.ReadFile(p)
	if err != nil {
		current = nil
	}
	if prior == nil {
		prior = current
	}
	if prior != nil {
		var prev struct {
			Generated string `json:"generated"`
		}
		if json.Unmarshal(prior, &prev) == nil {
			same, err := Render(owner, verdicts, prev.Generated)
			if err != nil {
				return false, err
			}
			if same == string(prior) {
				if string(current) == same {
					return false, nil
				}
				return false, write(p, same)
			}
		}
	}
	text, err := Render(owner, verdicts, generated)
	if err != nil {
		return false, err
	}
	return true, write(p, text)
}

func write(p, text string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(text), 0o644)
}
