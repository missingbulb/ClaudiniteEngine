package settings

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/report"
)

// Command is `cn settings`.
func Command(args []string, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "config" {
		return configCommand(args[1:], stdout)
	}
	return report.New(report.Usage, "settings takes config")
}

// configCommand is `cn settings config <pack>`: the declared entry's
// config as JSON, null where the entry carries none, for a pack's own
// script that reads its config without the engine's code. A pack the
// settings do not declare is an error, never an empty config.
func configCommand(args []string, stdout io.Writer) error {
	var token string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		token, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("settings config", flag.ContinueOnError)
	repo := fs.String("repo", ".", "")
	if err := report.ParseFlags(fs, args); err != nil {
		return err
	}
	if token == "" {
		return report.New(report.Usage, "settings config needs a pack id (local/<name> for a local pack)")
	}
	path, f, err := Find(*repo)
	if err != nil {
		return report.Wrap(report.Verify, "settings config", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return report.Wrap(report.IO, "settings config", err)
	}
	packs, err := ReadPacks(raw, f)
	if err != nil {
		return report.Wrap(report.Verify, "settings config", err)
	}
	id, local := strings.CutPrefix(token, LocalPrefix)
	e, ok := packs.Entry(id, local)
	if !ok {
		return report.New(report.Verify, fmt.Sprintf("settings config: %s does not declare %s", RelPath(f), token))
	}
	var config map[string]any
	if e.Config != nil {
		config = map[string]any{}
		for k, v := range e.Config {
			config[k] = Plain(v)
		}
	}
	out, err := json.Marshal(config)
	if err != nil {
		return report.Wrap(report.IO, "settings config", err)
	}
	fmt.Fprintln(stdout, string(out))
	return nil
}
