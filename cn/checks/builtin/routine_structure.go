package builtin

import (
	"path"
	"regexp"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/transcript"
)

// A routine, or a task folder, is an entry point (routine.md or task.md)
// beside the scripts it invokes; this asserts the prose-to-script wiring
// the unattended-agents skill mandates. It is inert in a repo with no
// entry point: every loop runs over an empty set.
var routineStructure = declared.Builtin{
	ID:     "routine-structure",
	Pack:   "claudinite-growth",
	OnFail: "block",
	Tags:   []string{"world", "builtin", "claudinite-growth"},
	Doc:    "packs/claudinite-growth/skills/unattended-agents/SKILL.md",
	Why:    "a routine is prose (read) + scripts (executed); a dangling invocation or an orphan/entry-less script means the agent runs the wrong thing or the job hides where it is never read",
}

func init() { register(&routineStructure, runRoutineStructure) }

var (
	entryNames   = []string{"routine.md", "task.md"}
	phaseScripts = []string{"preconditions.sh", "postconditions.sh"}
	invocation   = regexp.MustCompile(`\b(?:bash|sh)\s+(\S+\.sh)\b`)
)

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// children are the files directly in dir whose name pred takes.
func children(files []string, dir string, pred func(string) bool) []string {
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	var out []string
	for _, f := range files {
		if !strings.HasPrefix(f, prefix) {
			continue
		}
		rel := f[len(prefix):]
		if rel != "" && !strings.Contains(rel, "/") && pred(rel) {
			out = append(out, f)
		}
	}
	return out
}

func runRoutineStructure(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
	files := ctx.Files()
	var dirs []string
	entryOf := map[string]string{}
	for _, f := range files {
		if !contains(entryNames, path.Base(f)) {
			continue
		}
		if d := path.Dir(f); entryOf[d] == "" {
			entryOf[d] = path.Base(f)
			dirs = append(dirs, d)
		}
	}
	var out []findings.Finding
	for _, dir := range dirs {
		entry := path.Join(dir, entryOf[dir])
		text, ok := ctx.Read(entry)
		if !ok {
			continue
		}
		scripts := children(files, dir, func(rel string) bool { return strings.HasSuffix(rel, ".sh") })
		for i, ln := range strings.Split(text, "\n") {
			m := invocation.FindStringSubmatch(ln)
			if m == nil || ctx.Exists(m[1]) || ctx.Exists(path.Join(dir, m[1])) {
				continue
			}
			out = append(out, routineStructure.Finding(entry, i+1, "invokes "+m[1]+", which does not exist",
				"correct the path or restore the script; a routine must not tell the agent to run a missing file"))
		}
		for _, s := range scripts {
			if strings.Contains(text, path.Base(s)) {
				continue
			}
			out = append(out, routineStructure.Advice(s, 0, "is never invoked by "+entry,
				"invoke it from "+entryOf[dir]+", or remove it — a script the routine never runs does not earn its place"))
		}
		for _, s := range scripts {
			body, ok := ctx.Read(s)
			if !ok || strings.HasPrefix(body, "#!") {
				continue
			}
			out = append(out, routineStructure.Advice(s, 1, "has no shebang line",
				"start the script with a shebang (e.g. #!/usr/bin/env bash) so it runs standalone"))
		}
	}
	var phaseDirs []string
	for _, f := range files {
		if d := path.Dir(f); contains(phaseScripts, path.Base(f)) && !contains(phaseDirs, d) {
			phaseDirs = append(phaseDirs, d)
		}
	}
	names := strings.Join(entryNames, " / ")
	for _, dir := range phaseDirs {
		if entryOf[dir] != "" {
			continue
		}
		at := phaseScripts[0]
		for _, n := range phaseScripts {
			if ctx.Exists(path.Join(dir, n)) {
				at = n
				break
			}
		}
		out = append(out, routineStructure.Finding(path.Join(dir, at), 0, "sits in a routine folder with no "+names+" entry point",
			"add "+strings.Join(entryNames, " or ")+" as the folder's entry point (it invokes these scripts), or move the scripts out"))
	}
	return out
}
