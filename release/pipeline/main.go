// Command pipeline prints what the release workflows need from package
// release: package names, the publish mode, and the bodies of the comments
// and issues they post.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/missingbulb/ClaudiniteEngine/release"
)

const usage = `usage:
  pipeline names [--channel rc|stable]
  pipeline bootstrap-comment
  pipeline blocker-issue --version V --leg PLATFORM --run-url URL --log FILE
  pipeline blocker-issue --gate live-packs --version V --run-url URL --log FILE
  pipeline publish-mode --channel rc|stable --signing release|dev --dry-run true|false --npm-versions FILE [--stable-test pass|fail]
  pipeline deprecate-commands --action hold|revoke|release --version V [--reason R] --rc-versions FILE --stable-versions FILE
  pipeline unpublish-commands --version V --versions-dir DIR
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
	case "deprecate-commands":
		return deprecateCommands(args[1:], stdout, stderr)
	case "unpublish-commands":
		return unpublishCommands(args[1:], stdout, stderr)
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
	gate := fs.String("gate", "smoke-published", "")
	leg := fs.String("leg", "", "")
	runURL := fs.String("run-url", "", "")
	logPath := fs.String("log", "", "")
	err := fs.Parse(args)
	switch {
	case err != nil, fs.NArg() != 0, *ver == "", *runURL == "", *logPath == "",
		*gate == "smoke-published" && *leg == "",
		*gate == "live-packs" && *leg != "",
		*gate != "smoke-published" && *gate != "live-packs":
		fmt.Fprint(stderr, usage)
		return 2
	}
	raw, err := os.ReadFile(*logPath)
	if err != nil {
		raw = []byte("(no log: " + err.Error() + ")")
	}
	title, body := release.BlockerIssue(*ver, *leg, *runURL, string(raw))
	if *gate == "live-packs" {
		title, body = release.LivePacksBlockerIssue(*ver, *runURL, string(raw))
	}
	fmt.Fprintf(stdout, "%s\n\n%s", title, body)
	return 0
}

// deprecateCommands prints one npm deprecate command per line, or nothing
// and a notice when npm has no such version.
func deprecateCommands(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("deprecate-commands", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var in release.DeprecateInput
	fs.StringVar(&in.Action, "action", "", "")
	fs.StringVar(&in.Version, "version", "", "")
	fs.StringVar(&in.Reason, "reason", "", "")
	rc := fs.String("rc-versions", "", "")
	stable := fs.String("stable-versions", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *rc == "" || *stable == "" {
		fmt.Fprint(stderr, usage)
		return 2
	}
	for _, f := range []struct {
		path string
		dst  *string
	}{{*rc, &in.RCVersions}, {*stable, &in.StableVersions}} {
		raw, err := os.ReadFile(f.path)
		if err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(stderr, "pipeline: %v\n", err)
			return 1
		}
		*f.dst = string(raw)
	}
	d, err := release.DeprecateCommands(in)
	if err != nil {
		fmt.Fprintf(stderr, "pipeline: %v\n", err)
		return 1
	}
	if d.Notice != "" {
		fmt.Fprintf(stderr, "::notice::%s\n", d.Notice)
	}
	for _, c := range d.Commands {
		fmt.Fprintln(stdout, c)
	}
	return 0
}

// unpublishCommands prints one npm unpublish command per line, or nothing
// and a notice when npm has no such version. DIR holds each rc package's
// `npm view <pkg> versions --json` at <DIR>/<package name>.json; a refusal
// exits 1 with nothing on stdout.
func unpublishCommands(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("unpublish-commands", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	in := release.UnpublishInput{Versions: map[string]string{}}
	fs.StringVar(&in.Version, "version", "", "")
	dir := fs.String("versions-dir", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *dir == "" {
		fmt.Fprint(stderr, usage)
		return 2
	}
	for _, n := range release.UnpublishPackages() {
		raw, err := os.ReadFile(filepath.Join(*dir, n+".json"))
		if err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(stderr, "pipeline: %v\n", err)
			return 1
		}
		in.Versions[n] = string(raw)
	}
	u, err := release.UnpublishCommands(in)
	if err != nil {
		fmt.Fprintf(stderr, "::error::%v\n", err)
		return 1
	}
	if u.Notice != "" {
		fmt.Fprintf(stderr, "::notice::%s\n", u.Notice)
	}
	for _, c := range u.Commands {
		fmt.Fprintln(stdout, c)
	}
	return 0
}
