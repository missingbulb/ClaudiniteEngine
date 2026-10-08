package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/flatdecl"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/version"
)

// cmdTasksList prints every task the repo's active packs and the engine
// contribute, one per line as `<pack>/<task> <trigger>`, then each dropped
// task's reason on stderr's channel, the report; a dropped task fails the
// command.
func cmdTasksList(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("tasks list", flag.ContinueOnError)
	repo := fs.String("repo", ".", "")
	if err := flags(fs, args); err != nil {
		return err
	}
	set, err := packset.Load(*repo, version.Version(), false)
	if err != nil {
		return report.New(report.Verify, err.Error())
	}
	tasks, errs := taskspec.Discover(*repo, set.Packs)
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

// cmdTasksFlat prints the flat declarations the active packs produce,
// writes them (--write) or compares them with the files on disk (--check),
// naming each stale file and failing; --paths prints where the three
// files live, for a reader's drift test.
func cmdTasksFlat(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("tasks flat", flag.ContinueOnError)
	repo := fs.String("repo", ".", "")
	write := fs.Bool("write", false, "")
	check := fs.Bool("check", false, "")
	paths := fs.Bool("paths", false, "")
	asJSON := fs.Bool("json", false, "")
	if err := flags(fs, args); err != nil {
		return err
	}
	if *write && *check || *paths && (*write || *check) {
		return report.New(report.Usage, "tasks flat takes one of --write, --check and --paths")
	}
	if *asJSON && !*paths {
		return report.New(report.Usage, "tasks flat takes --json with --paths alone")
	}
	if *paths {
		return printFlatPaths(stdout, *asJSON)
	}
	set, err := packset.Load(*repo, version.Version(), false)
	if err != nil {
		return report.New(report.Verify, err.Error())
	}
	if *write {
		written, err := flatdecl.Write(*repo, set.Packs)
		if err != nil {
			return report.New(report.IO, err.Error())
		}
		for _, f := range written {
			fmt.Fprintf(stdout, "wrote %s\n", f)
		}
		return nil
	}
	content, err := flatdecl.Content(*repo, set.Packs)
	if err != nil {
		return report.New(report.IO, err.Error())
	}
	if !*check {
		for _, f := range flatdecl.Files {
			fmt.Fprint(stdout, content[f])
		}
		return nil
	}
	var stale []string
	for _, f := range flatdecl.Files {
		if _, ok := content[f]; !ok {
			continue
		}
		have, err := os.ReadFile(filepath.Join(*repo, filepath.FromSlash(flatdecl.HeldIn(*repo, f))))
		if err != nil || string(have) != content[f] {
			stale = append(stale, f)
		}
	}
	if len(stale) > 0 {
		return report.New(report.Verify, strings.Join(stale, ", ")+" not what the declared packs produce; run cn tasks flat --write")
	}
	return nil
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
	switch args[0] {
	case "list":
		return cmdTasksList(args[1:], stdout)
	case "flat":
		return cmdTasksFlat(args[1:], stdout)
	}
	return report.New(report.Usage, fmt.Sprintf("unknown tasks subcommand %q", args[0]))
}
