package mirror

func init() { writeGap = 0 }

// SetLimits sets the per-commit and per-run file caps, returning the undo.
func SetLimits(commit, run int) func() {
	c, r := perCommit, perRun
	perCommit, perRun = commit, run
	return func() { perCommit, perRun = c, r }
}
