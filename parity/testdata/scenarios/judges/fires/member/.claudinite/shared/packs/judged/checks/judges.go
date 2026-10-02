package checks

import (
	"encoding/json"
	"strings"

	"claudinite.com/checksdk"
)

func command(call checksdk.Call) string {
	var in struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(call.Input, &in)
	return in.Command
}

func init() {
	checksdk.Register(checksdk.Check{
		ID:   "no-rm-rf",
		Tags: []string{"pre-tool-use"},
		Judge: func(_ checksdk.Repo, call checksdk.Call) []checksdk.Finding {
			if call.Tool != "Bash" || !strings.Contains(command(call), "rm -rf") {
				return nil
			}
			return []checksdk.Finding{{Class: checksdk.ClassFinding, Path: "(tool call)", Sentence: "rm -rf is not run here"}}
		},
	})
	checksdk.Register(checksdk.Check{
		ID:   "failed-command",
		Tags: []string{"post-tool-use"},
		Judge: func(_ checksdk.Repo, call checksdk.Call) []checksdk.Finding {
			if call.Tool != "Bash" || !strings.Contains(string(call.Response), "FAILED") {
				return nil
			}
			return []checksdk.Finding{{Class: checksdk.ClassFinding, Path: "(tool call)", Sentence: "the command failed; read its output before going on"}}
		},
	})
}
