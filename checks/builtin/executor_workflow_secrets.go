package builtin

import (
	"os"
	"regexp"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/shared/transcript"
	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
)

// A secret reaches a task only when the executor workflow names it
// statically, and that file is scaffolded once and lands only through a
// person's merge, so a task added later can declare a secret the executor
// never carries: the item is picked and only then does the worker read the
// variable as empty. The expectation is the active packs' task
// declarations, parsed but not validated (a declaration another check
// rejects still says which secret it needs). Extra names in the workflow
// are not a finding. Advisory: the remedy is a human-merged change to
// .github/workflows/, which a member's own machinery cannot make.
var executorWorkflowSecrets = declared.Builtin{
	ID:     "executor-workflow-secrets",
	Pack:   workitem.TasksPackID,
	OnFail: "advise",
	Tags:   []string{"world", "builtin", workitem.TasksPackID},
	Doc:    "packs/claudinite-tasks/README.md",
	Why:    "a secret the executor does not name statically never reaches the job, and the task fails only once the queue has already picked its item up",
}

func init() { register(&executorWorkflowSecrets, runExecutorWorkflowSecrets) }

// ExecutorWorkflow is the member's executor workflow.
const executorWorkflow = ".github/workflows/" + workitem.ExecutorWorkflowFile

var (
	secretsMarker = regexp.MustCompile(`(?m)^[ \t]*# claudinite:secrets\b`)
	secretName    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// SecretEnvLine is the executor env line that passes one secret.
func secretEnvLine(name string) string {
	return "          " + name + ": ${{ secrets." + name + " }}"
}

func passesSecret(text, name string) bool {
	if !secretName.MatchString(name) {
		return false
	}
	return regexp.MustCompile(`(?m)^[ \t]*` + name + `:[ \t]*\$\{\{[ \t]*secrets\.` + name + `[ \t]*\}\}[ \t]*$`).MatchString(text)
}

func runExecutorWorkflowSecrets(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
	found, _ := taskspec.DeclarationFiles(ctx.Config.Packs)
	var decls []taskspec.Decl
	for _, f := range found {
		raw, err := os.ReadFile(f.File)
		if err != nil {
			continue
		}
		if d, err := taskspec.ParseText(f.File, raw); err == nil {
			decls = append(decls, d)
		}
	}
	expected := taskspec.SecretNames(decls)
	if len(expected) == 0 {
		return nil
	}
	text, ok := ctx.Read(executorWorkflow)
	missing := expected
	if ok {
		missing = nil
		for _, name := range expected {
			if !passesSecret(text, name) {
				missing = append(missing, name)
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	lines := make([]string, len(missing))
	for i, n := range missing {
		lines[i] = "`" + strings.TrimSpace(secretEnvLine(n)) + "`"
	}
	what := "does not pass " + strings.Join(missing, ", ") + ", which the tasks of this repo's packs declare"
	line := 0
	if !ok {
		what = "is missing, so nothing passes " + strings.Join(missing, ", ") + " to the executor"
	} else if at := secretsMarker.FindStringIndex(text); at != nil {
		line = strings.Count(text[:at[0]], "\n") + 1
	}
	return []findings.Finding{executorWorkflowSecrets.Finding(executorWorkflow, line, what,
		"add "+strings.Join(lines, " and ")+" to the executor's env, beneath its `# claudinite:secrets` marker — `cn adopt` stamps a pack's secrets there when it adopts the pack — and get that PR merged: a converge cannot push to .github/workflows/. Setting the repository secret itself is the other half")}
}
