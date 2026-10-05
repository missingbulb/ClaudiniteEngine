// Package flatdecl writes and reads the flat declarations: every active
// pack's task declarations and dashboard descriptor, one file each under
// .claudinite/cache/, so the dashboard reading a member over the API and a
// session asking what runs here read one file rather than every task
// folder; and, beside them, the member file a cn member states its
// declaration, pin and held versions in. Each entry carries the source's parsed value as written (no
// defaults, no normalisation) and the path it was read from; a file that
// does not parse carries its text. Session-copied packs are left out. The
// bytes match the Node engine's generate-flat-declarations.mjs at
// missingbulb/Claudinite@057841ac for the same packs.
package flatdecl

import (
	"bytes"
	"errors"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/descriptor"
	"github.com/missingbulb/ClaudiniteEngine/shared/jsjson"
	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
)

// Dir holds every file cn generates for a member: the rules and skills
// indexes, the flat declarations and the member file.
const Dir = ".claudinite/cache"

// LegacyDir is where an engine before Dir wrote those files. A member
// whose engine update lands before its next converge still holds them
// there, so they are read and rewritten where they are until a converge
// moves them.
//
// @legacy-tolerance advisory:rules-index-current retire:#94
const LegacyDir = ".claudinite/flat"

// The two files, relative to the repo root.
const (
	TasksFile     = Dir + "/tasks.GENERATED.json"
	DashboardFile = Dir + "/dashboard.GENERATED.json"
	Version       = 1
)

// LegacyPath is rel, a path under Dir, under LegacyDir; any other path is
// itself.
func LegacyPath(rel string) string {
	if rest, ok := strings.CutPrefix(rel, Dir+"/"); ok {
		return LegacyDir + "/" + rest
	}
	return rel
}

// Canonical is rel, a path under LegacyDir, under Dir; any other path is
// itself.
func Canonical(rel string) string {
	if rest, ok := strings.CutPrefix(rel, LegacyDir+"/"); ok {
		return Dir + "/" + rest
	}
	return rel
}

// Held is where the repo keeps rel, a path under Dir: rel itself, or its
// LegacyPath when only that exists, or when neither does and the repo
// holds LegacyDir and no Dir, so a file a legacy member gains joins the
// others until they move together.
func Held(rel string, exists func(string) bool) string {
	switch {
	case exists(rel):
		return rel
	case exists(LegacyPath(rel)), exists(LegacyDir) && !exists(Dir):
		return LegacyPath(rel)
	}
	return rel
}

// HeldIn is Held over the repo's working tree.
func HeldIn(repo, rel string) string {
	return Held(rel, func(r string) bool {
		_, err := os.Stat(filepath.Join(repo, filepath.FromSlash(r)))
		return err == nil
	})
}

// TaskDescriptor is the task declaration's descriptor name, and
// DashboardDescriptor the dashboard descriptor's file.
const (
	TaskDescriptor      = "task"
	DashboardDescriptor = "dashboard.json"
)

// Key is a pack as the declaration spells it: local/<id> for a local pack.
func Key(p packset.Pack) string {
	if p.Kind == packset.Local {
		return "local/" + p.ID
	}
	return p.ID
}

// Entry is one source: the parsed value, or the text when it does not
// parse.
func Entry(rel string, raw []byte) jsjson.Value {
	vals := map[string]jsjson.Value{"path": {Kind: jsjson.String, Str: rel}}
	if v, ok := Parse(rel, raw); ok {
		vals["declaration"] = v
		return jsjson.NewObject([]string{"path", "declaration"}, vals)
	}
	vals["text"] = jsjson.Value{Kind: jsjson.String, Str: string(raw)}
	return jsjson.NewObject([]string{"path", "text"}, vals)
}

// Parse reads a source as JSON.parse does (key order kept), or as its
// descriptor format for a .yaml or .toml task.
func Parse(rel string, raw []byte) (jsjson.Value, bool) {
	switch descriptor.FormatOf(rel) {
	case descriptor.YAML, descriptor.TOML:
		v, err := descriptor.ParseBytes(raw, descriptor.FormatOf(rel))
		if err != nil {
			return jsjson.Value{}, false
		}
		return jsjson.FromAny(v), true
	}
	v, err := jsjson.Decode(raw)
	return v, err == nil
}

// TaskFile is the one declaration a task folder holds, or "".
func TaskFile(dir string) string {
	file, _, err := descriptor.Find(dir, TaskDescriptor)
	if err != nil {
		return ""
	}
	return filepath.Base(file)
}

// Sources are the files the flat declarations copy, keyed as the files
// key them, each with its repo-relative path.
func Sources(packs []packset.Pack) (tasks, dashboards map[string]string) {
	tasks, dashboards = map[string]string{}, map[string]string{}
	for _, p := range packs {
		if p.Kind == packset.Temp {
			continue
		}
		key := Key(p)
		names, _ := os.ReadDir(filepath.Join(p.Dir, "tasks"))
		for _, n := range names {
			if !n.IsDir() {
				continue
			}
			if f := TaskFile(filepath.Join(p.Dir, "tasks", n.Name())); f != "" {
				tasks[key+"/"+n.Name()] = path.Join(p.Rel, "tasks", n.Name(), f)
			}
		}
		if st, err := os.Stat(filepath.Join(p.Dir, DashboardDescriptor)); err == nil && st.Mode().IsRegular() {
			dashboards[key] = path.Join(p.Rel, DashboardDescriptor)
		}
	}
	return tasks, dashboards
}

func render(repo, key string, sources map[string]string) (string, error) {
	names := make([]string, 0, len(sources))
	for k := range sources {
		names = append(names, k)
	}
	sort.Strings(names)
	entries := map[string]jsjson.Value{}
	for _, k := range names {
		raw, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(sources[k])))
		if err != nil {
			return "", err
		}
		entries[k] = Entry(sources[k], raw)
	}
	doc := jsjson.NewObject([]string{"version", key}, map[string]jsjson.Value{
		"version": {Kind: jsjson.Number, Num: Version},
		key:       jsjson.NewObject(names, entries),
	})
	return jsjson.StringifyIndent(doc, "  ") + "\n", nil
}

// Files are the flat files in the order they are written and reported.
var Files = []string{TasksFile, DashboardFile, MemberFile}

// Content is the flat files' text for the active packs, keyed by file:
// the task and dashboard declarations, and the member file where the repo
// keeps a .claudinite/settings.*; nil when no pack is active, so an
// unloadable declaration leaves the files on disk as they are.
func Content(repo string, packs []packset.Pack) (map[string]string, error) {
	if len(packs) == 0 {
		return nil, nil
	}
	tasks, dashboards := Sources(packs)
	t, err := render(repo, "tasks", tasks)
	if err != nil {
		return nil, err
	}
	d, err := render(repo, "dashboards", dashboards)
	if err != nil {
		return nil, err
	}
	out := map[string]string{TasksFile: t, DashboardFile: d}
	m, ok, err := ReadMember(repo, packs)
	if err != nil {
		return nil, err
	}
	if ok {
		if out[MemberFile], err = MemberContent(m); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Write writes whichever file changed where the repo holds it (Held) and
// returns the paths written.
func Write(repo string, packs []packset.Pack) ([]string, error) {
	content, err := Content(repo, packs)
	if err != nil || content == nil {
		return nil, err
	}
	var written []string
	for _, f := range Files {
		if _, ok := content[f]; !ok {
			continue
		}
		rel := HeldIn(repo, f)
		p := filepath.Join(repo, filepath.FromSlash(rel))
		if old, err := os.ReadFile(p); err == nil && bytes.Equal(old, []byte(content[f])) {
			continue
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return written, err
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return written, err
		}
		if err := os.WriteFile(p, []byte(content[f]), 0o644); err != nil {
			return written, err
		}
		written = append(written, rel)
	}
	return written, nil
}
