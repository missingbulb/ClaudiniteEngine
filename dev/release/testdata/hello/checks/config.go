package checks

import (
	"encoding/json"

	"claudinite.com/checksdk"
)

// hello-config reads the member's settings through the SDK: a finding
// while the hello entry's config carries probe: true.
func init() {
	checksdk.Register(checksdk.Check{
		ID:   "hello-config",
		Tags: []string{"world"},
		Run: func(repo checksdk.Repo) []checksdk.Finding {
			var cfg struct {
				Probe bool `json:"probe"`
			}
			if raw := repo.PackConfig("hello"); len(raw) == 0 || json.Unmarshal(raw, &cfg) != nil || !cfg.Probe {
				return nil
			}
			return []checksdk.Finding{{
				Path:     "(settings)",
				Sentence: "the hello entry's config sets probe: true, the hello pack's config probe; remove it",
			}}
		},
	})
}
