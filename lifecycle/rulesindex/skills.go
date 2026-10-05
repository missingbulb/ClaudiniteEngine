package rulesindex

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/missingbulb/ClaudiniteEngine/shared/flatdecl"
	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/shared/skillfm"
)

// SkillsFile is the skills index: a catalog for a reader of every skill
// the active packs bundle and what loads it. CLAUDE.md does not import
// it; the harness already carries each mounted skill's description. The
// bytes are the Node engine's generate-skills-index.mjs's.
const SkillsFile = flatdecl.Dir + "/claudinite-skills.GENERATED.md"

// SkillRow is one mounted skill.
type SkillRow struct {
	Skill, Pack, Description string
	Paths                    []string
}

// SkillRows are the active packs' skills in mount order: canon before
// local, the first pack bundling a name winning, a skill counted only
// when its SKILL.md is there.
func SkillRows(s packset.Set) []SkillRow {
	var rows []SkillRow
	seen := map[string]bool{}
	for _, p := range s.Packs {
		if p.Kind == packset.Temp {
			continue
		}
		for _, name := range p.Skills {
			if seen[name] {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(p.Dir, "skills", name, "SKILL.md"))
			if err != nil {
				continue
			}
			seen[name] = true
			m := skillfm.Read(string(raw))
			rows = append(rows, SkillRow{Skill: name, Pack: p.ID, Description: m.Description, Paths: m.ForceLoadPaths})
		}
	}
	return rows
}

func cell(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ReplaceAll(s, "|", `\|`), isJSSpace), " ")
}

func isJSSpace(r rune) bool { return unicode.IsSpace(r) || r == 0xFEFF }

// RenderSkills is the index text, or "" when no skill is mounted:
// path-scoped skills first, then the rest, each group in the order
// JavaScript's localeCompare gives their names.
func RenderSkills(rows []SkillRow) string {
	if len(rows) == 0 {
		return ""
	}
	var scoped, rest []SkillRow
	for _, r := range rows {
		if len(r.Paths) > 0 {
			scoped = append(scoped, r)
		} else {
			rest = append(rest, r)
		}
	}
	for _, g := range [][]SkillRow{scoped, rest} {
		sort.SliceStable(g, func(i, j int) bool { return localeLess(g[i].Skill, g[j].Skill) })
	}
	lines := []string{
		"<!-- GENERATED — do not hand-edit; every update rewrites it. Edit a skill's SKILL.md frontmatter. -->",
		"# Skills mounted here, and what loads each one",
		"",
		"A skill loads when the session's activity matches its description. A skill that names files",
		"under `force-load-on-file-edits-paths` is also forced for them: a file tool aimed there is held",
		"by the PreToolUse guard until the skill is loaded (the Skill tool, or a Read of its SKILL.md),",
		"and an edit made another way is caught at Stop.",
		"",
	}
	if len(scoped) > 0 {
		lines = append(lines, "## Before editing these files", "", "| Files | Skill | Pack | Loads when |", "|---|---|---|---|")
		for _, r := range scoped {
			var ps []string
			for _, p := range r.Paths {
				ps = append(ps, "`"+cell(p)+"`")
			}
			lines = append(lines, "| "+strings.Join(ps, ", ")+" | `"+r.Skill+"` | "+r.Pack+" | "+cell(r.Description)+" |")
		}
		lines = append(lines, "")
	}
	if len(rest) > 0 {
		lines = append(lines, "## By activity", "", "| Skill | Pack | Loads when |", "|---|---|---|")
		for _, r := range rest {
			lines = append(lines, "| `"+r.Skill+"` | "+r.Pack+" | "+cell(r.Description)+" |")
		}
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// localeLess approximates ICU's root collation for skill names: letters
// compare case-blind and after digits, digits after punctuation, '_'
// before '-'; ties fall to lower case first, then to the bytes.
func localeLess(a, b string) bool {
	ka, kb := collationKey(a), collationKey(b)
	for i := 0; i < len(ka) && i < len(kb); i++ {
		if ka[i] != kb[i] {
			return ka[i] < kb[i]
		}
	}
	if len(ka) != len(kb) {
		return len(ka) < len(kb)
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			ua, ub := unicode.IsUpper(rune(a[i])), unicode.IsUpper(rune(b[i]))
			if ua != ub {
				return ub
			}
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

func collationKey(s string) []int {
	var k []int
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			k = append(k, 0)
		case r == '_':
			k = append(k, 1)
		case r == '-':
			k = append(k, 2)
		case unicode.IsDigit(r):
			k = append(k, 1000+int(r))
		case unicode.IsLetter(r):
			k = append(k, 2000+int(unicode.ToLower(r)))
		default:
			k = append(k, 10+int(r))
		}
	}
	return k
}

// SkillsContent is the skills index the declaration produces, or "".
func SkillsContent(repo, engine string) (string, error) {
	s, err := packset.Load(repo, engine, false)
	if err != nil {
		return "", err
	}
	return RenderSkills(SkillRows(s)), nil
}

// WriteSkills writes the skills index when it changed and removes one
// when no skill is mounted, reporting whether the file moved.
func WriteSkills(repo, engine string) (bool, error) {
	want, err := SkillsContent(repo, engine)
	if err != nil {
		return false, err
	}
	path := filepath.Join(repo, filepath.FromSlash(SkillsFile))
	have, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	exists := err == nil
	if want == "" {
		if !exists {
			return false, nil
		}
		return true, os.Remove(path)
	}
	if exists && string(have) == want {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(want), 0o644)
}
