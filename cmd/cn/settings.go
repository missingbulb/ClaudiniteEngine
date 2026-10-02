package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings/node"
)

func cmdSettings(args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "import" {
		return report.New(report.Usage, "settings takes import")
	}
	fs := flag.NewFlagSet("settings import", flag.ContinueOnError)
	repo := fs.String("repo", ".", "")
	from := fs.String("from", node.File, "")
	toStdout := fs.Bool("stdout", false, "")
	if err := flags(fs, args[1:]); err != nil {
		return err
	}
	src := *from
	if !filepath.IsAbs(src) {
		src = filepath.Join(*repo, filepath.FromSlash(src))
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		return report.Wrap(report.IO, "settings import", err)
	}
	decl, rep, err := node.Read(raw, localTree(*repo))
	if err != nil {
		return report.Wrap(report.IO, "settings import", err)
	}
	if *toStdout {
		if !rep.Refused() {
			text, err := settings.RenderBlocks(settings.YAML, decl.Blocks())
			if err != nil {
				return report.Wrap(report.IO, "settings import", err)
			}
			fmt.Fprint(stdout, text)
		}
		for _, l := range rep {
			fmt.Fprintln(stdout, "# "+l.String())
		}
		if rep.Refused() {
			return report.New(report.Verify, "settings import refused a key; nothing was printed to write")
		}
		return nil
	}
	fmt.Fprint(stdout, rep.String())
	if rep.Refused() {
		return report.New(report.Verify, "settings import refused a key; the settings are unchanged")
	}
	path, f, err := settings.Find(*repo)
	if err != nil {
		return report.New(report.IO, "settings import writes into the pinned settings file: "+err.Error()+"; pin the engine first, or read the declaration with --stdout")
	}
	have, err := os.ReadFile(path)
	if err != nil {
		return report.Wrap(report.IO, "settings import", err)
	}
	top, err := settings.ParseBytesTop(have, f)
	if err != nil {
		return report.Wrap(report.IO, "settings import", err)
	}
	for _, b := range []string{"packs", "checks"} {
		if _, ok := top[b]; ok {
			return report.New(report.IO, fmt.Sprintf("%s already holds a %s block; the import writes it whole, once", settings.RelPath(f), b))
		}
	}
	out, err := settings.SpliceBlocks(have, f, decl.Blocks())
	if err != nil {
		return report.Wrap(report.IO, "settings import", err)
	}
	if _, err := settings.ParseFile(out, f); err != nil {
		return report.Wrap(report.Internal, "settings import wrote settings it cannot read back", err)
	}
	return writeKeepingMode(path, out)
}

func writeKeepingMode(path string, raw []byte) error {
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.WriteFile(path, raw, mode); err != nil {
		return report.Wrap(report.IO, "settings import", err)
	}
	return nil
}

// localTree answers the import's question about the member's own packs.
type localTree string

func (t localTree) HasLocal(name string) bool {
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return false
	}
	st, err := os.Stat(filepath.Join(string(t), filepath.FromSlash(packset.LocalDir), name))
	return err == nil && st.IsDir()
}
