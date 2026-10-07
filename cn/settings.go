package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/adopt"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings/node"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/version"
)

func cmdSettings(args []string, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "answer" {
		return cmdSettingsAnswer(args[1:], stdout)
	}
	if len(args) > 0 && args[0] == "config" {
		return cmdSettingsConfig(args[1:], stdout)
	}
	if len(args) == 0 || args[0] != "import" {
		return report.New(report.Usage, "settings takes import, answer or config")
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
	decl, rep, err := node.Read(raw, adopt.LocalTree(*repo))
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

// cmdSettingsAnswer is cn settings answer <pack>/<question> [--] <text>.
// The two positionals come first; after a `--` the next ones are taken as
// they are, so a text may start with a dash.
func cmdSettingsAnswer(args []string, stdout io.Writer) error {
	var pos []string
	for len(args) > 0 && len(pos) < 2 {
		if args[0] == "--" {
			args = args[1:]
			for len(args) > 0 && len(pos) < 2 {
				pos, args = append(pos, args[0]), args[1:]
			}
			break
		}
		if strings.HasPrefix(args[0], "-") {
			break
		}
		pos, args = append(pos, args[0]), args[1:]
	}
	fs := flag.NewFlagSet("settings answer", flag.ContinueOnError)
	repo := fs.String("repo", ".", "")
	if err := flags(fs, args); err != nil {
		return err
	}
	if len(pos) != 2 {
		return report.New(report.Usage, "settings answer takes <pack>/<question> [--] <text>")
	}
	file, err := adopt.Answer(*repo, version.Version(), pos[0], pos[1])
	if err != nil {
		return report.Wrap(report.Verify, "settings answer", err)
	}
	fmt.Fprintf(stdout, "answered %s in %s\n", pos[0], file)
	return nil
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

// cmdSettingsConfig is `cn settings config <pack>`: the declared entry's
// config as JSON, null where the entry carries none, for a pack's own
// script that reads its config without the engine's code. A pack the
// settings do not declare is an error, never an empty config.
func cmdSettingsConfig(args []string, stdout io.Writer) error {
	var token string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		token, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("settings config", flag.ContinueOnError)
	repo := fs.String("repo", ".", "")
	if err := flags(fs, args); err != nil {
		return err
	}
	if token == "" {
		return report.New(report.Usage, "settings config needs a pack id (local/<name> for a local pack)")
	}
	path, f, err := settings.Find(*repo)
	if err != nil {
		return report.Wrap(report.Verify, "settings config", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return report.Wrap(report.IO, "settings config", err)
	}
	packs, err := settings.ReadPacks(raw, f)
	if err != nil {
		return report.Wrap(report.Verify, "settings config", err)
	}
	id, local := strings.CutPrefix(token, settings.LocalPrefix)
	e, ok := packs.Entry(id, local)
	if !ok {
		return report.New(report.Verify, fmt.Sprintf("settings config: %s does not declare %s", settings.RelPath(f), token))
	}
	var config map[string]any
	if e.Config != nil {
		config = map[string]any{}
		for k, v := range e.Config {
			config[k] = settings.Plain(v)
		}
	}
	out, err := json.Marshal(config)
	if err != nil {
		return report.Wrap(report.IO, "settings config", err)
	}
	fmt.Fprintln(stdout, string(out))
	return nil
}
