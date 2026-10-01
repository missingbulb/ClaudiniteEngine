// Package findings is the shape every member-file check reports in: a
// class, a rule id, the path it is about and the sentence a person fixes it
// from. cn verify and cn check world print them one per line.
package findings

import (
	"fmt"
	"io"
)

// Class is how much a finding matters to an update.
type Class string

const (
	// Break means the engine would not work on this repo as it stands.
	Break Class = "break"
	// Deprecation means an old shape still works and will not at the next
	// major.
	Deprecation Class = "deprecation"
	// Coded is a finding a pack's coded check reported: it blocks like a
	// break.
	Coded Class = "finding"
	// Advisory is an advisory a pack's coded check reported: it is shown
	// like a deprecation and blocks nothing.
	Advisory Class = "advisory"
)

// Finding is one problem in a member's files.
type Finding struct {
	Class    Class
	ID       string
	Path     string
	Sentence string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s %s %s: %s", f.Class, f.ID, f.Path, f.Sentence)
}

// Print writes one line per finding.
func Print(w io.Writer, fs []Finding) {
	for _, f := range fs {
		fmt.Fprintln(w, f.String())
	}
}

// AnyBreak reports whether any finding blocks: a break, or a coded
// check's finding.
func AnyBreak(fs []Finding) bool {
	for _, f := range fs {
		if f.Class == Break || f.Class == Coded {
			return true
		}
	}
	return false
}
