package builtin

import (
	"regexp"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/transcript"
)

// The task folder's three cross-artifact invariants, each the way discovery
// fails soft rather than red: a declaration whose id disagrees with its
// folder is dropped and never runs; a dangling agent_instructions hands
// the executor a worker doc that is not there; a task.md beside an
// agentless declaration is prose no session opens, read by the rest of the
// corpus as "an agent runs here"; and a local shell worker that commits or
// pushes from whatever branch the shared checkout was left on.

const growthPack = "claudinite-growth"
const writingTasksDoc = "packs/claudinite-growth/skills/writing-tasks/SKILL.md"

var taskDeclarationMatchesFolder = declared.Builtin{
	ID:     "task-declaration-matches-folder",
	Pack:   growthPack,
	OnFail: "block",
	Tags:   []string{"world", "builtin", growthPack},
	Doc:    writingTasksDoc,
	Why:    "task discovery is fail-soft per task — a declaration that disagrees with its folder is dropped into errors and the task silently never runs, while every scheduler run keeps reporting healthy",
}

var taskMdOnlyWhenAgentic = declared.Builtin{
	ID:     "task-md-only-when-agentic",
	Pack:   growthPack,
	OnFail: "block",
	Tags:   []string{"world", "builtin", growthPack},
	Doc:    writingTasksDoc,
	Why:    `task.md is the spec a task's session follows, and the corpus reads its presence as "an agent runs here" — on an agentless task it is prose no session will ever open, judged by the routine contract and named by every work item as the file the run is about`,
}

var taskWorkerRestoresMain = declared.Builtin{
	ID:     "task-worker-restores-main",
	Pack:   growthPack,
	OnFail: "block",
	Since:  "2026-09-06",
	Tags:   []string{"world", "builtin", growthPack},
	Doc:    writingTasksDoc,
	Why: "the Claudinite scheduler runs every due task in ONE checkout, so a worker ordered after " +
		"anything that leaves the tree on another branch (a maintenance flow's `git checkout -B`, " +
		"never switched back) inherits it silently — a bare `git push origin HEAD:main` from an " +
		"upstream-less branch aborts with exit 128, and code that only pushes on success can leave " +
		"that failure unnoticed for days",
}

func init() {
	register(&taskDeclarationMatchesFolder, runTaskDeclarationMatchesFolder)
	register(&taskMdOnlyWhenAgentic, runTaskMdOnlyWhenAgentic)
	register(&taskWorkerRestoresMain, runTaskWorkerRestoresMain)
}

// taskDecls yields each scanned task declaration with its folder (trailing
// slash) and parsed fields, nil when the text does not parse.
func taskDecls(ctx *declared.Ctx, each func(file, dirName, taskDir string, d taskspec.Decl)) {
	for _, file := range ctx.Files() {
		m := taskDeclarationPath.FindStringSubmatch(file)
		if m == nil {
			continue
		}
		text, ok := ctx.Read(file)
		if !ok {
			continue
		}
		d, _ := taskspec.ParseText(file, []byte(text))
		each(file, m[2], file[:strings.LastIndex(file, "/")+1], d)
	}
}

func runTaskDeclarationMatchesFolder(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
	var out []findings.Finding
	taskDecls(ctx, func(file, dirName, taskDir string, d taskspec.Decl) {
		flag := func(what, fix string) { out = append(out, taskDeclarationMatchesFolder.Finding(file, 0, what, fix)) }
		if id, ok := d.Str("id"); ok && id != dirName {
			flag(`declares id "`+id+`" but its directory is "`+dirName+`"`,
				`rename the directory to "`+id+`", or set the id to "`+dirName+`" — the two must match`)
		}
		worker, _ := d.Str("agent_instructions")
		if worker == "" {
			return
		}
		if strings.HasPrefix(worker, "/") || inList(strings.Split(worker, "/"), "..") {
			flag(`declares agent_instructions "`+worker+`", which reaches outside the task directory`,
				`keep the worker doc beside the declaration (conventionally "task.md") — a task folder is self-contained`)
		} else if !ctx.Exists(taskDir + worker) {
			flag(`declares agent_instructions "`+worker+`", which does not exist in `+taskDir,
				"add "+taskDir+worker+`, or point agent_instructions at the worker doc that is there (conventionally "task.md")`)
		}
	})
	return out
}

func runTaskMdOnlyWhenAgentic(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
	var out []findings.Finding
	taskDecls(ctx, func(file, _, taskDir string, d taskspec.Decl) {
		if model, ok := d.Str("agent_model"); ok && model != "none" {
			return
		}
		if !ctx.Exists(taskDir + "task.md") {
			return
		}
		out = append(out, taskMdOnlyWhenAgentic.Finding(taskDir+"task.md", 0,
			"sits beside a task declaration that runs no agent (agent_model 'none'), so no session will ever read it",
			"rename it to "+taskDir+"README.md — an agentless task's doc is the human-facing record of what its worker does"))
	})
	return out
}

var (
	localTaskWorker = regexp.MustCompile(`^\.claudinite/local/packs/[^/]+/tasks/[^/]+/worker\.sh$`)
	workerWrites    = regexp.MustCompile(`(?m)^[^#\n]*\bgit\s+(?:commit|push)\b`)
	workerRestores  = regexp.MustCompile(`(?m)^[^#\n]*\bgit\s+(?:checkout|switch)\s+main\b`)
)

func runTaskWorkerRestoresMain(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
	var out []findings.Finding
	for _, file := range ctx.Files() {
		if !localTaskWorker.MatchString(file) {
			continue
		}
		src, ok := ctx.Read(file)
		if !ok {
			continue
		}
		write := workerWrites.FindStringIndex(src)
		if write == nil {
			continue
		}
		fix := "before " + file + " writes anything, read `git rev-parse --abbrev-ref HEAD` and " +
			"`git checkout main` when it is anything else. Do not reach for `git push origin HEAD:main` " +
			"instead — from a polluted checkout that pushes an unreviewed prior converge straight to `main`."
		restore := workerRestores.FindStringIndex(src)
		switch {
		case restore == nil:
			out = append(out, taskWorkerRestoresMain.Finding(file, 0, file+" commits or pushes without ever returning the checkout to `main`", fix))
		case restore[0] > write[0]:
			out = append(out, taskWorkerRestoresMain.Finding(file, 0, file+" returns the checkout to `main` only after it has already committed or pushed", fix))
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
