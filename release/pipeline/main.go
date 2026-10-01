// Command pipeline prints what the release workflows need from package
// release: package names, the publish mode, and the bodies of the comments
// and issues they post.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/missingbulb/ClaudiniteEngine/release"
)

const usage = `usage:
  pipeline names [--channel rc|stable]
  pipeline bootstrap-comment
`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "names":
		channel := ""
		if len(args) == 3 && args[1] == "--channel" {
			channel = args[2]
		} else if len(args) != 1 {
			fmt.Fprint(stderr, usage)
			return 2
		}
		for _, p := range release.Placeholders() {
			if channel == "" || p.Channel == channel {
				fmt.Fprintln(stdout, p.Name)
			}
		}
		return 0
	case "bootstrap-comment":
		fmt.Fprint(stdout, release.BootstrapComment())
		return 0
	}
	fmt.Fprintf(stderr, "pipeline: unknown command %q\n%s", args[0], usage)
	return 2
}
