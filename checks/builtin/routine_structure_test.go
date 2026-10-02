package builtin

import "testing"

func TestRoutineStructure(t *testing.T) {
	r := repo{base: map[string]string{
		"tasks/x/task.md":           "# x\n\nRun `bash tasks/x/x.sh` first,\nthen `sh missing.sh`.\n",
		"tasks/x/x.sh":              "#!/bin/sh\necho x\n",
		"tasks/x/orphan.sh":         "echo never run\n",
		"dev/routines/r/routine.md": "Run `bash dev/routines/r/a.sh`.\n",
		"dev/routines/r/a.sh":       "#!/usr/bin/env bash\n",
		"lost/preconditions.sh":     "#!/bin/sh\n",
		"lost/postconditions.sh":    "#!/bin/sh\n",
	}}
	expect(t, r.run(t, "routine-structure"),
		want{path: "lost/preconditions.sh", what: `^sits in a routine folder with no routine\.md / task\.md entry point$`},
		want{path: "tasks/x/orphan.sh", what: `^is never invoked by tasks/x/task\.md$`, advise: true},
		want{path: "tasks/x/orphan.sh", line: 1, what: `^has no shebang line$`, advise: true},
		want{path: "tasks/x/task.md", line: 4, what: `^invokes missing\.sh, which does not exist$`, fix: "missing file"})
	clean := repo{base: map[string]string{
		"tasks/x/task.md": "# x\n\nRun `bash tasks/x/x.sh` first, or `sh x.sh` from the folder.\n",
		"tasks/x/x.sh":    "#!/bin/sh\necho x\n",
	}}
	expect(t, clean.run(t, "routine-structure"))
	expect(t, repo{base: map[string]string{"a.sh": "echo\n"}}.run(t, "routine-structure"))
}
