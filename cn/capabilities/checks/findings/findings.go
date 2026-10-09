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
	Class Class
	ID    string
	Path  string
	// Line is the 1-based line the finding is about, or 0 for the whole
	// path.
	Line     int
	Sentence string
	// Why and Fix are a declared check's reason and remedy; empty for a
	// finding whose sentence carries its own fix.
	Why, Fix string
	// Pack is the pack whose check reported it; empty for the engine's own.
	Pack string
}

// Name is the check as a finding line names it: <pack>/<id>, or the bare
// id for the engine's own rules.
func (f Finding) Name() string {
	if f.Pack != "" {
		return f.Pack + "/" + f.ID
	}
	return f.ID
}

// Location is the path, with the line when there is one.
func (f Finding) Location() string {
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d", f.Path, f.Line)
	}
	return f.Path
}

// String is the finding's first line.
func (f Finding) String() string {
	return fmt.Sprintf("%s %s %s: %s", f.Class, f.Name(), f.Location(), f.Sentence)
}

// Print writes each finding's line, then its why and fix lines when it
// has them.
func Print(w io.Writer, fs []Finding) {
	for _, f := range fs {
		fmt.Fprintln(w, f.String())
		if f.Why != "" {
			fmt.Fprintln(w, "  why: "+f.Why)
		}
		if f.Fix != "" {
			fmt.Fprintln(w, "  fix: "+f.Fix)
		}
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
