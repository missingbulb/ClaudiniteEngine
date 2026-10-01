package lifecycle

import (
	"fmt"
	"io"
	"runtime"

	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// PrintVersion writes the version on the first line, then the commit,
// platform and Go version.
func PrintVersion(w io.Writer) {
	fmt.Fprintln(w, version.Version())
	fmt.Fprintf(w, "commit %s %s %s\n", version.Commit(), version.Platform(), runtime.Version())
}
