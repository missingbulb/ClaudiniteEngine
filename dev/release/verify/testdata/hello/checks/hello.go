// Package checks is the hello pack's one coded check: a finding while a
// file named HELLO_FINDING sits at the repo root, so a member can watch
// the check run at Stop and in CI.
package checks

import "claudinite.com/checksdk"

func init() {
	checksdk.Register(checksdk.Check{
		ID:   "hello-check",
		Tags: []string{"work", "world"},
		Run: func(repo checksdk.Repo) []checksdk.Finding {
			if !repo.Exists("HELLO_FINDING") {
				return nil
			}
			return []checksdk.Finding{{
				Class:    checksdk.ClassFinding,
				Path:     "HELLO_FINDING",
				Sentence: "the hello pack's probe file is present; delete HELLO_FINDING",
			}}
		},
	})
}
