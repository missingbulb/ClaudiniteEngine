// Package rulesindex writes the channel pack prose reaches a session by: a
// generated file of nothing but @ imports, one per active pack's prose, which
// the member's CLAUDE.md imports in turn. Claude Code loads CLAUDE.md and its
// imports whole, where a SessionStart hook's output is previewed at about
// 2 KB. The bytes match the Node engine's generate-rules-index.mjs for the
// same declaration, so a member moving between the engines keeps its file.
package rulesindex

import (
	"bytes"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/flatdecl"
	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
)

// File is the index, relative to the repo root.
const File = ".claudinite/flat/claudinite-rules.GENERATED.md"

// Import is the line the member's CLAUDE.md carries.
const Import = "@" + File

// ClaudeMD is the member's memory file.
const ClaudeMD = "CLAUDE.md"

// prepareStep is the file a pack ships when it copies a pack into the
// session; a member with one imports the person's copied prose.
const prepareStep = "session-prepare.mjs"

// sessionUserProse is the copied person's prose as the index addresses it:
// a literal, because the copy happens at session start, after the index is
// written.
const sessionUserProse = "../temp/packs/current_user/RULES.md"

// Imports are the index's import paths for an active pack set, relative to
// the index's directory, in the set's order.
func Imports(s packset.Set) []string {
	var out []string
	prepare := false
	for _, p := range s.Packs {
		if st, err := os.Stat(filepath.Join(p.Dir, prepareStep)); err == nil && st.Mode().IsRegular() {
			prepare = true
		}
		if p.Kind == packset.Temp || p.ProsePath() == "" {
			continue
		}
		rel, err := filepath.Rel(".claudinite/flat", filepath.FromSlash(path.Join(p.Rel, p.Prose)))
		if err != nil {
			continue
		}
		out = append(out, filepath.ToSlash(rel))
	}
	if prepare {
		out = append(out, sessionUserProse)
	}
	return out
}

// Render is the index text, or "" when there is nothing to import.
func Render(imports []string) string {
	if len(imports) == 0 {
		return ""
	}
	return "@" + strings.Join(imports, "\n@") + "\n"
}

// Content is the index the repo's declaration produces, or "".
func Content(repo, engine string) (string, error) {
	s, err := packset.Load(repo, engine, false)
	if err != nil {
		return "", err
	}
	return Render(Imports(s)), nil
}

// State is how the index on disk compares with the declaration's.
type State string

const (
	Current State = "current"
	Stale   State = "stale"
	Absent  State = "absent"
	// Empty is a declaration with nothing to import, whatever is on disk.
	Empty State = "empty"
)

// Check compares the index on disk with what the declaration produces.
func Check(repo, engine string) (State, string, error) {
	want, err := Content(repo, engine)
	if err != nil {
		return "", "", err
	}
	if want == "" {
		return Empty, want, nil
	}
	have, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(File)))
	if errors.Is(err, os.ErrNotExist) {
		return Absent, want, nil
	}
	if err != nil {
		return "", want, err
	}
	if string(have) != want {
		return Stale, want, nil
	}
	return Current, want, nil
}

// Write writes the index when it changed, reporting whether it did. A
// declaration with nothing to import leaves any file alone, as the Node
// engine does: the packs may simply not be vendored yet.
func Write(repo, engine string) (bool, error) {
	st, want, err := Check(repo, engine)
	if err != nil || st == Current || st == Empty {
		return false, err
	}
	path := filepath.Join(repo, filepath.FromSlash(File))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(want), 0o644)
}

// Converge writes the index and the flat declarations beside it,
// returning the paths written: the same pack set produces both, so every
// writer of one writes the other.
func Converge(repo, engine string) ([]string, error) {
	var written []string
	changed, err := Write(repo, engine)
	if err != nil {
		return nil, err
	}
	if changed {
		written = append(written, File)
	}
	s, err := packset.Load(repo, engine, false)
	if err != nil {
		return written, err
	}
	flat, err := flatdecl.Write(repo, s.Packs)
	return append(written, flat...), err
}

// HasImport reports whether the repo's CLAUDE.md carries the import line on
// a line of its own.
func HasImport(repo string) bool {
	raw, err := os.ReadFile(filepath.Join(repo, ClaudeMD))
	return err == nil && HasImportIn(raw)
}

// HasImportIn reports whether CLAUDE.md's bytes carry the import line on a
// line of its own.
func HasImportIn(raw []byte) bool {
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(l) == Import {
			return true
		}
	}
	return false
}

// EnsureImport creates CLAUDE.md with the import line, or appends the line
// to it, reporting whether the file changed.
func EnsureImport(repo string) (bool, error) {
	if HasImport(repo) {
		return false, nil
	}
	path := filepath.Join(repo, ClaudeMD)
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return true, os.WriteFile(path, WithImport(raw), 0o644)
}

// WithImport is a CLAUDE.md's bytes with the import line appended, as
// EnsureImport writes it, in the file's own line endings (CRLF when it
// already uses them).
func WithImport(raw []byte) []byte {
	eol := "\n"
	if bytes.Contains(raw, []byte("\r\n")) {
		eol = "\r\n"
	}
	out := append([]byte{}, raw...)
	if len(out) > 0 && !bytes.HasSuffix(out, []byte("\n")) {
		out = append(out, eol...)
	}
	return append(out, Import+eol...)
}
