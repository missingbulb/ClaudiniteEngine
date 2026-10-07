package builtin

import (
	"regexp"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/checksdk"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/transcript"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/workitem"
)

// A task's code may read only the CLAUDINITE_* variables code work is
// handed. Any other is a variable nobody sets: it reads as undefined, the
// worker's parse of it yields an empty result, and the run goes green
// having done something other than what it was asked. The legal set is the
// contract's own list, so a variable added to or removed from it changes
// what this accepts with no edit here. Tests are out: a test naming a
// retired variable names it as a fixture. Comments strip both ways, since a
// note explaining why a task stopped reading one names it too.
var taskCodeWorkEnv = declared.Builtin{
	ID:     "task-code-work-env",
	Pack:   workitem.TasksPackID,
	OnFail: "block",
	Tags:   []string{"world", "builtin", workitem.TasksPackID},
	Doc:    "packs/claudinite-tasks/README.md",
	Why:    "a variable nothing sets reads as undefined and the run still goes green — a parameter channel that has stopped being delivered leaves the operation in its unscoped, unguarded mode with no signal at all",
}

func init() { register(&taskCodeWorkEnv, runTaskCodeWorkEnv) }

var (
	taskCodeFile  = regexp.MustCompile(`(^|/)tasks/[^/]+/.+\.mjs$`)
	codeWorkReads = regexp.MustCompile(`\bCLAUDINITE_[A-Z0-9_]+\b`)
)

func runTaskCodeWorkEnv(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
	var out []findings.Finding
	legal := strings.Join(taskspec.CodeWorkEnvVars, ", ")
	for _, file := range ctx.Files() {
		if !taskCodeFile.MatchString(file) || strings.HasSuffix(file, ".test.mjs") {
			continue
		}
		text, ok := ctx.Read(file)
		if !ok {
			continue
		}
		seen := unique(codeWorkReads.FindAllString(checksdk.StripComments(text), -1))
		sort.Strings(seen)
		for _, name := range seen {
			if inList(taskspec.CodeWorkEnvVars, name) {
				continue
			}
			out = append(out, taskCodeWorkEnv.Finding(file, 0,
				"reads `"+name+"`, which code_work never sets",
				"take the value from the item's Context (`CLAUDINITE_CONTEXT`, one line per bullet, set by `create-work-item --context`) — the code_work contract sets "+legal+" and nothing else"))
		}
	}
	return out
}
