package checks

import (
	"strings"

	"claudinite.com/checksdk"
)

// hello-change reads the change through the SDK: an advisory on the first
// added line of every changed file under HELLO_CHANGED/.
func init() {
	checksdk.Register(checksdk.Check{
		ID:     "hello-change",
		Tags:   []string{"work"},
		OnFail: "advise",
		Run: func(repo checksdk.Repo) []checksdk.Finding {
			var files []string
			for _, f := range repo.ChangedFiles() {
				if strings.HasPrefix(f, "HELLO_CHANGED/") {
					files = append(files, f)
				}
			}
			if len(files) == 0 {
				return nil
			}
			seen := map[string]bool{}
			var out []checksdk.Finding
			for _, l := range repo.AddedLines(files) {
				if seen[l.Path] {
					continue
				}
				seen[l.Path] = true
				out = append(out, checksdk.Finding{
					Path: l.Path, Line: l.Line,
					Sentence: "the change adds a file under HELLO_CHANGED/, the hello pack's change probe",
				})
			}
			return out
		},
	})
}
