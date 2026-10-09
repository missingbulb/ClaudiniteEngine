package workflows

import (
	"flag"
	"fmt"
	"io"

	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/report"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/githubapi"
)

// Command is `cn workflows diff`, the patch to the workflows this version
// expects, and `cn workflows stage`, which writes them into the staging
// directory an engine update PR carries. Both read the repo's owner/name
// from --name, else githubapi.RepoFullName; a scheduler whose cron needs it and
// finds none fails rather than takes the template's placeholder.
func Command(args []string, stdout io.Writer) error {
	if len(args) == 0 || (args[0] != "diff" && args[0] != "stage") {
		return report.New(report.Usage, "workflows takes diff or stage")
	}
	fs := flag.NewFlagSet("workflows "+args[0], flag.ContinueOnError)
	repo := fs.String("repo", ".", "")
	name := fs.String("name", "", "")
	if err := report.ParseFlags(fs, args[1:]); err != nil {
		return err
	}
	full := *name
	if full == "" {
		full = githubapi.RepoFullName(*repo)
	}
	if args[0] == "stage" {
		staged, err := Stage(*repo, full)
		if err != nil {
			return report.Wrap(report.IO, "workflows stage", err)
		}
		for _, s := range staged {
			fmt.Fprintln(stdout, s)
		}
		return nil
	}
	d, err := Diff(*repo, full)
	if err != nil {
		return report.Wrap(report.IO, "workflows diff", err)
	}
	fmt.Fprint(stdout, d)
	return nil
}
