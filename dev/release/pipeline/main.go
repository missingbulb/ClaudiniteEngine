// Command pipeline prints what the release workflows need from package
// release: package names, the release kind, the publish mode, the bodies
// of the comments and issues they post, whether npm holds what a release
// published, and a staging release's release.json.
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/ghrelease"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/dev/release"
	"github.com/missingbulb/ClaudiniteEngine/dev/release/publish"
	"github.com/missingbulb/ClaudiniteEngine/dev/release/verify"
)

const usage = `usage:
  pipeline names
  pipeline release-kind --kind full|staging
  pipeline blocker-issue --version V --leg PLATFORM --run-url URL --log FILE
  pipeline blocker-issue --gate live-packs --version V --run-url URL --log FILE
  pipeline publish-mode --tag rc|staging --signing release --dry-run true|false --npm-versions FILE
  pipeline deprecate-commands --action hold|revoke|release --version V [--reason R] --versions-dir DIR
  pipeline unpublish-commands --version V --versions-dir DIR --dist-tags FILE
  pipeline npm-holds --dist DIR --version V --channel C --repo OWNER/NAME [--registry URL] [--timeout DURATION]
  pipeline release-json --version V --manifest INTEGRITY --commit SHA

A --versions-dir holds each CLI package's ` + "`npm view <pkg> versions --json`" + ` at
<DIR>/<package name>.json.
`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "names":
		if len(args) != 1 {
			fmt.Fprint(stderr, usage)
			return 2
		}
		for _, n := range release.CLIPackages() {
			fmt.Fprintln(stdout, n)
		}
		return 0
	case "release-kind":
		if len(args) != 3 || args[1] != "--kind" {
			fmt.Fprint(stderr, usage)
			return 2
		}
		k, err := release.ReleaseKindOf(args[2])
		if err != nil {
			fmt.Fprintf(stderr, "pipeline: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "tag=%s\nchannel=%s\nplatforms=%s\n", k.Tag, k.Channel, strings.Join(k.Platforms, " "))
		return 0
	case "publish-mode":
		return publishMode(args[1:], stdout, stderr)
	case "blocker-issue":
		return blockerIssue(args[1:], stdout, stderr)
	case "deprecate-commands":
		return deprecateCommands(args[1:], stdout, stderr)
	case "unpublish-commands":
		return unpublishCommands(args[1:], stdout, stderr)
	case "npm-holds":
		return npmHolds(args[1:], stdout, stderr)
	case "release-json":
		return releaseJSON(args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "pipeline: unknown command %q\n%s", args[0], usage)
	return 2
}

// publishMode prints mode=<mode> for $GITHUB_OUTPUT and the reason as a
// workflow notice.
func publishMode(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("publish-mode", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var in publish.ModeInput
	fs.StringVar(&in.Tag, "tag", "", "")
	fs.StringVar(&in.Signing, "signing", "", "")
	dry := fs.String("dry-run", "", "")
	versions := fs.String("npm-versions", "", "")
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
	m := publish.PublishMode(in)
	if m.Notice != "" {
		fmt.Fprintf(stderr, "::notice::publish %s: %s\n", m.Name, m.Notice)
	}
	fmt.Fprintf(stdout, "mode=%s\n", m.Name)
	return 0
}

// npmHolds exits 0 once the registry names the integrity of every
// tarball the dist holds.
func npmHolds(args []string, stdout, stderr io.Writer) int {
	in, ok := holdsFlags(args)
	if !ok {
		fmt.Fprint(stderr, usage)
		return 2
	}
	in.Log = stdout
	if err := publish.NPMHolds(in); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// holdsFlags reads npm-holds' flags; it waits as long as cn update does.
func holdsFlags(args []string) (publish.HoldsInput, bool) {
	fs := flag.NewFlagSet("npm-holds", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	in := publish.HoldsInput{HTTP: &http.Client{Timeout: 30 * time.Second}, Every: 5 * time.Second}
	fs.StringVar(&in.Dist, "dist", "", "")
	fs.StringVar(&in.Version, "version", "", "")
	fs.StringVar(&in.Channel, "channel", "", "")
	fs.StringVar(&in.Repo, "repo", "", "")
	fs.StringVar(&in.Registry, "registry", npmreg.DefaultRegistry, "")
	fs.DurationVar(&in.Timeout, "timeout", npmreg.ServeWait, "")
	err := fs.Parse(args)
	return in, err == nil && fs.NArg() == 0 && in.Dist != "" && in.Version != "" && in.Channel != "" && in.Repo != ""
}

// releaseJSON prints the release.json a staging release carries.
func releaseJSON(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("release-json", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var l ghrelease.Latest
	fs.StringVar(&l.Version, "version", "", "")
	fs.StringVar(&l.Manifest, "manifest", "", "")
	fs.StringVar(&l.Commit, "commit", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	raw, err := ghrelease.FormatLatest(l)
	if err != nil {
		fmt.Fprintf(stderr, "pipeline: %v\n", err)
		return 1
	}
	_, _ = stdout.Write(raw)
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
	title, body := verify.BlockerIssue(*ver, *leg, *runURL, string(raw))
	if *gate == "live-packs" {
		title, body = verify.LivePacksBlockerIssue(*ver, *runURL, string(raw))
	}
	fmt.Fprintf(stdout, "%s\n\n%s", title, body)
	return 0
}

// readVersionsDir reads each CLI package's npm view answer from DIR, an
// absent file reading as empty.
func readVersionsDir(dir string) (map[string]string, error) {
	out := map[string]string{}
	for _, n := range release.CLIPackages() {
		raw, err := os.ReadFile(filepath.Join(dir, n+".json"))
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		out[n] = string(raw)
	}
	return out, nil
}

// deprecateCommands prints one npm deprecate command per line, or nothing
// and a notice when npm has no such version.
func deprecateCommands(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("deprecate-commands", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var in publish.DeprecateInput
	fs.StringVar(&in.Action, "action", "", "")
	fs.StringVar(&in.Version, "version", "", "")
	fs.StringVar(&in.Reason, "reason", "", "")
	dir := fs.String("versions-dir", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *dir == "" {
		fmt.Fprint(stderr, usage)
		return 2
	}
	var err error
	if in.Versions, err = readVersionsDir(*dir); err != nil {
		fmt.Fprintf(stderr, "pipeline: %v\n", err)
		return 1
	}
	d, err := publish.DeprecateCommands(in)
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
// and a notice when npm has no such version; a refusal exits 1 with
// nothing on stdout.
func unpublishCommands(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("unpublish-commands", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var in publish.UnpublishInput
	fs.StringVar(&in.Version, "version", "", "")
	dir := fs.String("versions-dir", "", "")
	tags := fs.String("dist-tags", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *dir == "" || *tags == "" {
		fmt.Fprint(stderr, usage)
		return 2
	}
	var err error
	if in.Versions, err = readVersionsDir(*dir); err != nil {
		fmt.Fprintf(stderr, "pipeline: %v\n", err)
		return 1
	}
	raw, err := os.ReadFile(*tags)
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(stderr, "pipeline: %v\n", err)
		return 1
	}
	in.DistTags = string(raw)
	u, err := publish.UnpublishCommands(in)
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
