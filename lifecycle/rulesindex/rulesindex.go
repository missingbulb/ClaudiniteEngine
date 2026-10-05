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
	"slices"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/flatdecl"
	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
)

// File is the index, relative to the repo root.
const File = flatdecl.Dir + "/claudinite-rules.GENERATED.md"

// Import is the line the member's CLAUDE.md carries.
const Import = "@" + File

// LegacyImport is the line a CLAUDE.md carried while the index lived
// under flatdecl.LegacyDir; Move repoints it.
//
// @legacy-tolerance advisory:rules-index-current retire:#94
const LegacyImport = "@" + flatdecl.LegacyDir + "/claudinite-rules.GENERATED.md"

// ClaudeMD is the member's memory file.
const ClaudeMD = "CLAUDE.md"

// sessionUserProse is the copied person's prose as the index addresses it
// where the user-pack pack is declared: a literal, because the copy
// happens at session start, after the index is written.
const sessionUserProse = "../temp/packs/current_user/RULES.md"

// Imports are the index's import paths for an active pack set, relative to
// the index's directory, in the set's order.
func Imports(s packset.Set) []string {
	var out []string
	prepare := false
	for _, p := range s.Packs {
		if p.Kind == packset.Canon && p.ID == packset.UserPackID {
			prepare = true
		}
		if p.Kind == packset.Temp || p.ProsePath() == "" {
			continue
		}
		rel, err := filepath.Rel(filepath.FromSlash(flatdecl.Dir), filepath.FromSlash(path.Join(p.Rel, p.Prose)))
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

// Check compares the index on disk, where the repo holds it
// (flatdecl.HeldIn), with what the declaration produces.
func Check(repo, engine string) (State, string, error) {
	want, err := Content(repo, engine)
	if err != nil {
		return "", "", err
	}
	if want == "" {
		return Empty, want, nil
	}
	have, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(flatdecl.HeldIn(repo, File))))
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

// Write writes the index, where the repo holds it, when it changed,
// reporting whether it did. A declaration with nothing to import leaves
// any file alone, as the Node engine does: the packs may simply not be
// vendored yet.
func Write(repo, engine string) (bool, error) {
	st, want, err := Check(repo, engine)
	if err != nil || st == Current || st == Empty {
		return false, err
	}
	path := filepath.Join(repo, filepath.FromSlash(flatdecl.HeldIn(repo, File)))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(want), 0o644)
}

// Generated are the files cn generates for a member, every one under
// flatdecl.Dir.
func Generated() []string {
	return append([]string{File, SkillsFile}, flatdecl.Files...)
}

// Move moves the generated files a member still holds under
// flatdecl.LegacyDir to flatdecl.Dir, a file already there winning, and
// repoints CLAUDE.md's import at the moved index, returning every path it
// changed: each old path, each new one and CLAUDE.md.
func Move(repo string) ([]string, error) {
	var changed []string
	for _, f := range Generated() {
		old := flatdecl.LegacyPath(f)
		from := filepath.Join(repo, filepath.FromSlash(old))
		if _, err := os.Stat(from); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return changed, err
		}
		to := filepath.Join(repo, filepath.FromSlash(f))
		if _, err := os.Stat(to); err == nil {
			if err := os.Remove(from); err != nil {
				return changed, err
			}
			changed = append(changed, old)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return changed, err
		}
		if err := os.Rename(from, to); err != nil {
			return changed, err
		}
		changed = append(changed, old, f)
	}
	// Removes the directory only when the move emptied it.
	_ = os.Remove(filepath.Join(repo, filepath.FromSlash(flatdecl.LegacyDir)))
	md := filepath.Join(repo, ClaudeMD)
	raw, err := os.ReadFile(md)
	if errors.Is(err, os.ErrNotExist) {
		return changed, nil
	}
	if err != nil {
		return changed, err
	}
	if moved := RepointImport(raw); !bytes.Equal(moved, raw) {
		if err := os.WriteFile(md, moved, 0o644); err != nil {
			return changed, err
		}
		changed = append(changed, ClaudeMD)
	}
	return changed, nil
}

// NeedsMove reports whether Move would change anything: a generated
// file under flatdecl.LegacyDir, or a CLAUDE.md still carrying
// LegacyImport.
func NeedsMove(repo string) bool {
	for _, f := range Generated() {
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(flatdecl.LegacyPath(f)))); err == nil {
			return true
		}
	}
	raw, err := os.ReadFile(filepath.Join(repo, ClaudeMD))
	return err == nil && !bytes.Equal(RepointImport(raw), raw)
}

// RepointImport is a CLAUDE.md's bytes with each LegacyImport line
// replaced by the Import line, or dropped where the file already carries
// Import; every other byte stays.
func RepointImport(raw []byte) []byte {
	lines := strings.SplitAfter(string(raw), "\n")
	has := HasImportIn(raw)
	var b strings.Builder
	for _, l := range lines {
		if strings.TrimSpace(l) != LegacyImport {
			b.WriteString(l)
			continue
		}
		if !has {
			b.WriteString(strings.Replace(l, LegacyImport, Import, 1))
			has = true
		}
	}
	return []byte(b.String())
}

// Converge moves the generated files out of flatdecl.LegacyDir, then
// refreshes them (Refresh), returning the paths changed.
func Converge(repo, engine string) ([]string, error) {
	written, err := Move(repo)
	if err != nil {
		return written, err
	}
	refreshed, err := Refresh(repo, engine)
	for _, f := range refreshed {
		if !slices.Contains(written, f) {
			written = append(written, f)
		}
	}
	return written, err
}

// Refresh writes the index, the skills index and the flat declarations
// beside it where the repo holds them, moving nothing and leaving
// CLAUDE.md alone, returning the paths written: the same pack set
// produces both, so every writer of one writes the other.
func Refresh(repo, engine string) ([]string, error) {
	var written []string
	changed, err := Write(repo, engine)
	if err != nil {
		return nil, err
	}
	if changed {
		written = append(written, flatdecl.HeldIn(repo, File))
	}
	if changed, err = WriteSkills(repo, engine); err != nil {
		return written, err
	}
	if changed {
		written = append(written, flatdecl.HeldIn(repo, SkillsFile))
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

// ImportsHeldIndex reports whether the repo's CLAUDE.md imports the
// index where the repo holds it: the Import line, or LegacyImport while
// the index is still under flatdecl.LegacyDir.
func ImportsHeldIndex(repo string) bool {
	if flatdecl.HeldIn(repo, File) == File {
		return HasImport(repo)
	}
	raw, err := os.ReadFile(filepath.Join(repo, ClaudeMD))
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(l) == LegacyImport {
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
