package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/report"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/flatdecl"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/version"
)

// listTasks prints every task the repo's active packs and the engine
// contribute, one per line as `<pack>/<task> <trigger>`, then each dropped
// task's reason on stderr's channel, the report; a dropped task fails the
// command.
func listTasks(repo string, stdout io.Writer) error {
	set, err := packset.Load(repo, version.Version(), false)
	if err != nil {
		return report.New(report.Verify, err.Error())
	}
	tasks, errs := taskspec.Discover(repo, set.Packs)
	for _, t := range tasks {
		trigger, _ := t.Decl.Str("trigger")
		fmt.Fprintf(stdout, "%s %s\n", t.Path(), trigger)
	}
	if len(errs) > 0 {
		lines := make([]string, len(errs))
		for i, e := range errs {
			lines[i] = e.What + "; " + e.Fix
		}
		return report.New(report.Verify, strings.Join(lines, "\n"))
	}
	return nil
}

// cmdTasksFlat is `cn tasks flat --paths [--json]`, internal: where the
// flat files live, for a reader's drift test. Session start and cn adopt
// write the files themselves.
func cmdTasksFlat(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("tasks flat", flag.ContinueOnError)
	paths := fs.Bool("paths", false, "")
	asJSON := fs.Bool("json", false, "")
	if err := flags(fs, args); err != nil {
		return err
	}
	if !*paths {
		return report.New(report.Usage, "tasks flat takes --paths [--json]; cn adopt writes the flat files")
	}
	return printFlatPaths(stdout, *asJSON)
}

// printFlatPaths is `cn tasks flat --paths [--json]`.
func printFlatPaths(stdout io.Writer, asJSON bool) error {
	if !asJSON {
		for _, f := range flatdecl.Files {
			fmt.Fprintln(stdout, f)
		}
		return nil
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]string{"tasks": flatdecl.TasksFile, "member": flatdecl.MemberFile})
}

func cmdTasks(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return report.New(report.Usage, "tasks needs a subcommand")
	}
	if args[0] == "flat" {
		return cmdTasksFlat(args[1:], stdout)
	}
	return report.New(report.Usage, fmt.Sprintf("unknown tasks subcommand %q", args[0]))
}
