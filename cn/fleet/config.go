package fleet

import (
	"errors"
	"fmt"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packseed"
)

// Block names the settings block that makes a repo a fleet manager and
// carries its config, as a finding or a refusal names it to a person.
const Block = "the fleet block of .claudinite/settings"

// KindUser is the one fleet kind: the repositories a user owns.
const KindUser = "user"

// Seed is one pack declaration the fleet wants in every member.
type Seed = packseed.Seed

// Config is the manager's fleet block: who to cover, who to leave out,
// and the seeds.
type Config struct {
	Owner     string
	Exclude   []string
	PackSeeds []Seed
}

// Owns reports whether repo (lowercased owner/name) is under the fleet's
// owner.
func (c Config) Owns(repo string) bool {
	owner, _, _ := strings.Cut(repo, "/")
	return owner == c.Owner
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

// ParseConfig reads the fleet block, nil when the settings carry none;
// home is the manager's owner/name. An absent block is refused: absence
// is not consent to cover everything.
func ParseConfig(sd map[string]any, home string) (Config, error) {
	if sd == nil {
		return Config{}, fmt.Errorf("%s declares no fleet block { owner, exclude } in .claudinite/settings - nothing to cover", home)
	}
	var c Config
	if v, ok := sd["owner"]; ok && v != nil {
		c.Owner = strings.ToLower(jsString(v))
	} else {
		c.Owner = strings.ToLower(strings.SplitN(home, "/", 2)[0])
	}
	if k, ok := sd["kind"]; ok && k != nil && k != KindUser {
		return Config{}, fmt.Errorf("the fleet block's kind is %s; a fleet is the repositories a user owns (kind %q), the only kind cn sweeps", jsString(k), KindUser)
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
	c.PackSeeds = packseed.Parse(sd)
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
