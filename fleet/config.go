package fleet

import (
	"errors"
	"fmt"
	"strings"
)

// PackID is the pack whose entry carries the fleet's config.
const PackID = "claudinite-fleet-sheepdog"

// KindUser is the one fleet kind: the repositories a user owns.
const KindUser = "user"

// CanonRepoNote is the one line a config still naming canonRepo earns.
const CanonRepoNote = "[cn] fleet: canonRepo is not read; current is judged against the published engine and pack versions"

// Seed is one pack declaration the fleet wants in every member.
type Seed struct {
	ID     string         `json:"id"`
	Config map[string]any `json:"config,omitempty"`
}

// Config is the manager's own claudinite-fleet-sheepdog entry: who to
// cover, who to leave out, and the seeds.
type Config struct {
	Owner     string
	Exclude   []string
	PackSeeds []Seed
	// CanonRepoNamed says the entry still carries canonRepo, which cn
	// reads and ignores.
	CanonRepoNamed bool
}

// Excluded reports whether repo (lowercased owner/name) is on the list.
func (c Config) Excluded(repo string) bool {
	for _, e := range c.Exclude {
		if e == repo {
			return true
		}
	}
	return false
}

// ParseConfig reads the entry's config, present false when the entry
// carries none; home is the manager's owner/name. An absent config is
// refused: absence is not consent to cover everything.
func ParseConfig(raw any, present bool, home string) (Config, error) {
	sd, ok := raw.(map[string]any)
	if !present || !ok {
		return Config{}, fmt.Errorf("the fleet-enforcer repo %s declares no %s config { owner, exclude } on its pack entry - nothing to cover", home, PackID)
	}
	var c Config
	if v, ok := sd["owner"]; ok && v != nil {
		c.Owner = strings.ToLower(jsString(v))
	} else {
		c.Owner = strings.ToLower(strings.SplitN(home, "/", 2)[0])
	}
	if k, ok := sd["kind"]; ok && k != nil && k != KindUser {
		return Config{}, fmt.Errorf("the %s config's kind is %s; a fleet is the repositories a user owns (kind %q), the only kind cn sweeps", PackID, jsString(k), KindUser)
	}
	if ex, ok := sd["exclude"].([]any); ok {
		seen := map[string]bool{}
		for _, e := range ex {
			s := strings.ToLower(jsString(e))
			if !seen[s] {
				seen[s] = true
				c.Exclude = append(c.Exclude, s)
			}
		}
	}
	_, c.CanonRepoNamed = sd["canonRepo"]
	if seeds, ok := sd["packSeeds"].([]any); ok {
		for _, s := range seeds {
			o, ok := s.(map[string]any)
			if !ok {
				continue
			}
			id, ok := o["id"].(string)
			if !ok || strings.TrimSpace(id) == "" {
				continue
			}
			seed := Seed{ID: strings.TrimSpace(id)}
			if cfg, ok := o["config"].(map[string]any); ok {
				seed.Config = cfg
			}
			c.PackSeeds = append(c.PackSeeds, seed)
		}
	}
	return c, nil
}

// jsString is String(v) for the JSON values a config holds.
func jsString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return "null"
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%f", t), "0"), ".")
	}
	return fmt.Sprint(v)
}

// ErrNoOwnedRepos is an enumeration that found nothing under the owner:
// a wrong token user or scope, never an empty fleet.
var ErrNoOwnedRepos = errors.New("no owned repos")
