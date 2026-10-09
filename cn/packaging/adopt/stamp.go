package adopt

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/workflows"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/packset"
)

// ExecutorWorkflow is the member's executor workflow.
const ExecutorWorkflow = ".github/workflows/" + workitem.ExecutorWorkflowFile

// packSecrets are the secrets the tasks of the packs in ids declare.
func packSecrets(set packset.Set, ids map[string]bool) []string {
	var packs []packset.Pack
	for _, p := range set.Packs {
		if p.Kind == packset.Canon && ids[p.ID] {
			packs = append(packs, p)
		}
	}
	found, _ := taskspec.DeclarationFiles(packs)
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
	return taskspec.SecretNames(decls)
}

// stampExecutor adds secrets to the executor's stamped lines, keeping the
// lines already there, and prints one line per name it added. A missing
// executor or marker is said, not repaired: the workflow is the member's.
func stampExecutor(repo string, secrets []string, out io.Writer) error {
	if len(secrets) == 0 {
		return nil
	}
	path := filepath.Join(repo, filepath.FromSlash(ExecutorWorkflow))
	have, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(out, "%s is absent, so nothing passes %v to the executor\n", ExecutorWorkflow, secrets)
		return nil
	}
	if err != nil {
		return err
	}
	before := workflows.StampedSecrets(have)
	stamped, ok := workflows.Stamp(have, append(append([]string{}, before...), secrets...))
	if !ok {
		fmt.Fprintf(out, "%s carries no %q line; pass %v in its env by hand\n", ExecutorWorkflow, workflows.SecretsMarker, secrets)
		return nil
	}
	if string(stamped) == string(have) {
		return nil
	}
	if err := os.WriteFile(path, stamped, 0o644); err != nil {
		return err
	}
	held := map[string]bool{}
	for _, s := range before {
		held[s] = true
	}
	for _, s := range workflows.StampedSecrets(stamped) {
		if !held[s] {
			fmt.Fprintf(out, "stamped %s into %s\n", s, ExecutorWorkflow)
		}
	}
	return nil
}
