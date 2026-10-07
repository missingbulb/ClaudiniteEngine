package main

import (
	"io"
	"os"
	"path/filepath"

	"github.com/missingbulb/ClaudiniteEngine/cn/growth/provenance"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/report"
)

// cmdProvenance is `cn provenance`, the provenance verbs over the
// repository --repo names, else CLAUDE_PROJECT_DIR, else the working
// directory.
func cmdProvenance(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
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
	switch provenance.Main(args, abs, stdin, stdout, stderr) {
	case 0:
		return nil
	case 2:
		return report.Said(report.Usage)
	}
	return report.Said(report.Verify)
}
