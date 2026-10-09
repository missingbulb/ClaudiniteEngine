package main

import (
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/rules/rulesindex"
)

// hookIndex gives the hooks the rules index writer.
type hookIndex struct{}

// Write refreshes the generated files where the member holds them and
// moves nothing: a hook leaves no CLAUDE.md edit in the session's tree.
func (hookIndex) Write(repo, engine string) (bool, error) {
	written, err := rulesindex.Refresh(repo, engine)
	return len(written) > 0, err
}

func (hookIndex) HasImport(repo string) bool { return rulesindex.ImportsHeldIndex(repo) }

func (hookIndex) HasRules(repo, engine string) bool {
	st, _, err := rulesindex.Check(repo, engine)
	return err == nil && st != rulesindex.Empty
}
