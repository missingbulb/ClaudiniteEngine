package version

import (
	"fmt"
	"io"
	"runtime"
)

// Print writes the version on the first line, then the commit,
// platform and Go version.
func Print(w io.Writer) {
	fmt.Fprintln(w, Version())
	fmt.Fprintf(w, "commit %s %s %s\n", Commit(), Platform(), runtime.Version())
}
