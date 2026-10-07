package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/dashdesc"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/report"
)

func cmdDashboard(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return report.New(report.Usage, "dashboard takes descriptor")
	}
	switch args[0] {
	case "descriptor":
		return dashboardDescriptor(args[1:], stdout)
	}
	return report.New(report.Usage, fmt.Sprintf("unknown dashboard command %q", args[0]))
}

// verdict is one descriptor as `cn dashboard descriptor --json` prints
// it: the page reader's answer and the check's problems.
type verdict struct {
	File     string             `json:"file"`
	Pack     string             `json:"pack"`
	Widgets  []dashdesc.Widget  `json:"widgets"`
	Repo     []string           `json:"repo"`
	Fleet    *dashdesc.Fleet    `json:"fleet"`
	Fault    *string            `json:"fault"`
	Problems []dashdesc.Problem `json:"problems"`
}

func verdictOf(file string, text []byte) verdict {
	pack := filepath.Base(filepath.Dir(file))
	d := dashdesc.Parse(text, pack)
	v := verdict{File: file, Pack: pack, Problems: dashdesc.Problems(text, pack)}
	if v.Problems == nil {
		v.Problems = []dashdesc.Problem{}
	}
	if d.Fault != "" {
		v.Fault = &d.Fault
		return v
	}
	v.Widgets, v.Repo, v.Fleet = d.Widgets, d.Repo, &d.Fleet
	return v
}

// dashboardDescriptor is `cn dashboard descriptor FILE… [--json]`: each
// descriptor as the page's reader and the descriptor-usable check see it,
// the pack being the file's directory name; exit 1 on any problem.
func dashboardDescriptor(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("dashboard descriptor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "")
	var files []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return report.Wrap(report.Usage, fs.Name(), err)
		}
		args = fs.Args()
		if len(args) > 0 {
			files = append(files, args[0])
			args = args[1:]
		}
	}
	if len(files) == 0 {
		return report.New(report.Usage, "dashboard descriptor takes one or more descriptor files")
	}
	var out []verdict
	bad := 0
	for _, f := range files {
		text, err := os.ReadFile(f)
		if err != nil {
			return report.Wrap(report.IO, "dashboard descriptor", err)
		}
		v := verdictOf(f, text)
		if len(v.Problems) > 0 {
			bad++
		}
		out = append(out, v)
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return err
		}
	} else {
		for _, v := range out {
			if len(v.Problems) == 0 {
				fmt.Fprintf(stdout, "%s: ok\n", v.File)
			}
			for _, p := range v.Problems {
				fmt.Fprintf(stdout, "%s: %s\n  fix: %s\n", v.File, p.What, p.Fix)
			}
		}
	}
	if bad > 0 {
		return report.Said(report.Verify)
	}
	return nil
}
