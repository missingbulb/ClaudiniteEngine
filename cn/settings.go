package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/adopt"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/version"
)

func cmdSettings(args []string, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "answer" {
		return cmdSettingsAnswer(args[1:], stdout)
	}
	if len(args) > 0 && args[0] == "config" {
		return cmdSettingsConfig(args[1:], stdout)
	}
	return report.New(report.Usage, "settings takes answer or config")
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
