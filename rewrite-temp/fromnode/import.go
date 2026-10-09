package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/settings"
	"github.com/missingbulb/ClaudiniteEngine/rewrite-temp/fromnode/node"
)

// cmdImport reads the Node declaration into the pinned settings file's
// packs and checks blocks, once; a refused key writes nothing.
func cmdImport(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	repo := fs.String("repo", ".", "")
	from := fs.String("from", node.File, "")
	toStdout := fs.Bool("stdout", false, "")
	if err := parse(fs, args); err != nil {
		return err
	}
	src := *from
	if !filepath.IsAbs(src) {
		src = filepath.Join(*repo, filepath.FromSlash(src))
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	decl, rep, err := node.Read(raw, localTree(*repo))
	if err != nil {
		return err
	}
	if *toStdout {
		if !rep.Refused() {
			text, err := settings.RenderBlocks(settings.YAML, decl.Blocks())
			if err != nil {
				return err
			}
			fmt.Fprint(stdout, text)
		}
		for _, l := range rep {
			fmt.Fprintln(stdout, "# "+l.String())
		}
		if rep.Refused() {
			return errors.New("refused a key; nothing was printed to write")
		}
		return nil
	}
	fmt.Fprint(stdout, rep.String())
	if rep.Refused() {
		return errors.New("refused a key; the settings are unchanged")
	}
	path, f, err := settings.Find(*repo)
	if err != nil {
		return errors.New("the import writes into the pinned settings file: " + err.Error() + "; pin the engine first, or read the declaration with --stdout")
	}
	have, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	top, err := settings.ParseBytesTop(have, f)
	if err != nil {
		return err
	}
	for _, b := range []string{"packs", "checks"} {
		if _, ok := top[b]; ok {
			return fmt.Errorf("%s already holds a %s block; the import writes it whole, once", settings.RelPath(f), b)
		}
	}
	out, err := settings.SpliceBlocks(have, f, decl.Blocks())
	if err != nil {
		return err
	}
	if _, err := settings.ParseFile(out, f); err != nil {
		return fmt.Errorf("the import wrote settings it cannot read back: %w", err)
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	return os.WriteFile(path, out, mode)
}
