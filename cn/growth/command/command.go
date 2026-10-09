// Package command is the growth segment's command line: cn growth and
// cn provenance.
package command

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/growth/capture"
	"github.com/missingbulb/ClaudiniteEngine/cn/growth/provenance/verbs"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/breadcrumb"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/report"
)

// Growth is `cn growth`.
func Growth(args []string, stdout, stderr io.Writer, start time.Time) error {
	if len(args) == 0 {
		return report.New(report.Usage, "growth takes capture")
	}
	if args[0] == "capture" {
		return captureCommand(args[1:], stdout, stderr, start)
	}
	return report.New(report.Usage, fmt.Sprintf("unknown growth command %q", args[0]))
}

// capture_ is `cn growth capture`: the merge-to-main skill's and a
// routine session's capture, and a person's.
func captureCommand(args []string, stdout, stderr io.Writer, start time.Time) error {
	wd, err := os.Getwd()
	if err != nil {
		return report.Wrap(report.IO, "growth capture", err)
	}
	crumb := func(o breadcrumb.Outcome) {
		fmt.Fprintln(stderr, breadcrumb.Line("growth", "capture", o, time.Since(start)))
	}
	req, ok := capture.ParseArgs(args, wd)
	if !ok {
		fmt.Fprintln(stderr, capture.UsageText)
		crumb(breadcrumb.Error)
		return report.Said(report.Usage)
	}
	res := capture.Run(req, capture.FromProcess(), stdout, stderr)
	crumb(res.Outcome.Crumb())
	switch res.Code {
	case 0:
		return nil
	case 2:
		return report.Said(report.Usage)
	}
	return report.Said(report.IO)
}

// Provenance is `cn provenance`, the provenance verbs over the
// repository --repo names, else CLAUDE_PROJECT_DIR, else the working
// directory.
func Provenance(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	root := os.Getenv("CLAUDE_PROJECT_DIR")
	for i, a := range args {
		if a == "--repo" && i+1 < len(args) {
			root = args[i+1]
		}
	}
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return report.Wrap(report.IO, "provenance", err)
		}
		root = wd
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return report.Wrap(report.IO, "provenance", err)
	}
	switch verbs.Main(args, abs, stdin, stdout, stderr) {
	case 0:
		return nil
	case 2:
		return report.Said(report.Usage)
	}
	return report.Said(report.Verify)
}
