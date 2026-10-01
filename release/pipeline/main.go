// Command pipeline prints what the release workflows need from package
// release: package names, the publish mode, and the bodies of the comments
// and issues they post.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/missingbulb/ClaudiniteEngine/release"
)

const usage = `usage:
  pipeline names [--channel rc|stable]
  pipeline bootstrap-comment
  pipeline blocker-issue --version V --leg PLATFORM --run-url URL --log FILE
  pipeline publish-mode --channel rc|stable --signing release|dev --dry-run true|false --npm-versions FILE [--stable-test pass|fail]
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
	case "publish-mode":
		return publishMode(args[1:], stdout, stderr)
	case "blocker-issue":
		return blockerIssue(args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "pipeline: unknown command %q\n%s", args[0], usage)
	return 2
}

// publishMode prints mode=<mode> for $GITHUB_OUTPUT and the reason as a
// workflow notice.
func publishMode(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("publish-mode", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var in release.ModeInput
	fs.StringVar(&in.Channel, "channel", "", "")
	fs.StringVar(&in.Signing, "signing", "", "")
	dry := fs.String("dry-run", "", "")
	versions := fs.String("npm-versions", "", "")
	fs.StringVar(&in.StableTest, "stable-test", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || (*dry != "true" && *dry != "false") || *versions == "" {
		fmt.Fprint(stderr, usage)
		return 2
	}
	in.DryRunInput = *dry == "true"
	raw, err := os.ReadFile(*versions)
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(stderr, "pipeline: %v\n", err)
		return 1
	}
	in.NpmVersions = string(raw)
	m := release.PublishMode(in)
	if m.Notice != "" {
		fmt.Fprintf(stderr, "::notice::publish %s: %s\n", m.Name, m.Notice)
	}
	fmt.Fprintf(stdout, "mode=%s\n", m.Name)
	return 0
}

// blockerIssue prints the issue's title, a blank line, then its body.
func blockerIssue(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("blocker-issue", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	ver := fs.String("version", "", "")
	leg := fs.String("leg", "", "")
	runURL := fs.String("run-url", "", "")
	logPath := fs.String("log", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *ver == "" || *leg == "" || *runURL == "" || *logPath == "" {
		fmt.Fprint(stderr, usage)
		return 2
	}
	raw, err := os.ReadFile(*logPath)
	if err != nil {
		raw = []byte("(no log: " + err.Error() + ")")
	}
	title, body := release.BlockerIssue(*ver, *leg, *runURL, string(raw))
	fmt.Fprintf(stdout, "%s\n\n%s", title, body)
	return 0
}
