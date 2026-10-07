package checks

import (
	"encoding/json"
	"strings"

	"claudinite.com/checksdk"
)

// hello-judge is the pack's coded hook judge: a Bash command naming
// HELLO_JUDGE is blocked before it runs.
func init() {
	checksdk.Register(checksdk.Check{
		ID:   "hello-judge",
		Tags: []string{"pre-tool-use"},
		Judge: func(_ checksdk.Repo, call checksdk.Call) []checksdk.Finding {
			var in struct {
				Command string `json:"command"`
			}
			if call.Tool != "Bash" || json.Unmarshal(call.Input, &in) != nil || !strings.Contains(in.Command, "HELLO_JUDGE") {
				return nil
			}
			return []checksdk.Finding{{
				Class:    checksdk.ClassFinding,
				Path:     "(tool call)",
				Sentence: "the command names HELLO_JUDGE, the hello pack's judge probe; drop it from the command",
			}}
		},
	})
}
