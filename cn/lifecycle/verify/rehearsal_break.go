//go:build rehearsal_break

package verify

import "github.com/missingbulb/ClaudiniteEngine/cn/shared/findings"

// The rehearsal's update mode builds a release whose verify refuses every
// member, to prove the updater opens no PR for it. dev/release/gobuild.sh
// accepts this tag only with REHEARSAL=1, so no published binary has it.
func init() {
	rules = append(rules, rule{"rehearsal", func(Input) []findings.Finding {
		return []findings.Finding{brk("rehearsal", ".", "this build's verify refuses every repo, for the rehearsal")}
	}})
}
