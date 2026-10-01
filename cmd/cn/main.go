// Command cn is the Claudinite engine.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/missingbulb/ClaudiniteEngine/lifecycle"
)

const usage = `usage: cn <command> [arguments]

commands:
  hook <event>   answer a Claude Code hook
  version        print the engine version
  selftest       check this machine can run the engine
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		lifecycle.PrintVersion(stdout)
		return 0
	default:
		fmt.Fprint(stderr, usage)
		return 2
	}
}
