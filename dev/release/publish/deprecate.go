package publish

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/dev/release"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/version"
)

// DeprecateInput is a hold, revoke or release dispatch of promote.yml and what npm
// says exists: each CLI package's `npm view <pkg> versions --json`, keyed
// by package name, which is empty or an E404 error object when the package
// has no versions.
type DeprecateInput struct {
	Action, Version, Reason string
	Versions                map[string]string
}

// Deprecation is what the deprecate job does: the npm deprecate commands
// for every package holding the version, or, when @claudinite/cli has no
// such version, a notice and nothing.
type Deprecation struct {
	Exists   bool
	Commands []string
	Notice   string
}

func npmVersions(raw string) (map[string]bool, error) {
	out := map[string]bool{}
	s := strings.TrimSpace(raw)
	if s == "" {
		return out, nil
	}
	var e struct {
		Error struct {
			Code    string `json:"code"`
			Summary string `json:"summary"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(s), &e) == nil && e.Error.Code != "" {
		if e.Error.Code == "E404" {
			return out, nil
		}
		return nil, fmt.Errorf("npm view failed: %s %s", e.Error.Code, e.Error.Summary)
	}
	var list []string
	if err := json.Unmarshal([]byte(s), &list); err != nil {
		var one string
		if err := json.Unmarshal([]byte(s), &one); err != nil {
			return nil, fmt.Errorf("npm view's answer is neither a version list nor a version: %.80s", s)
		}
		list = []string{one}
	}
	for _, v := range list {
		out[v] = true
	}
	return out, nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// DeprecateCommands renders the hold or revoke of a version as the held:
// or revoked: deprecation every reader of npm's deprecated field
// understands, or its release as an empty deprecation, on @claudinite/cli
// and each platform package holding the version: a staging build has
// linux-x64 alone.
func DeprecateCommands(in DeprecateInput) (Deprecation, error) {
	prefix, ok := map[string]string{"hold": "held: ", "revoke": "revoked: ", "release": ""}[in.Action]
	switch {
	case !ok:
		return Deprecation{}, fmt.Errorf("action %q is not hold, revoke or release", in.Action)
	case in.Action == "release" && in.Reason != "":
		return Deprecation{}, errors.New("release lifts a hold or revocation and takes no reason")
	case in.Action != "release" && strings.TrimSpace(in.Reason) == "":
		return Deprecation{}, errors.New("a hold or revoke needs a reason")
	case strings.ContainsAny(in.Reason, "\r\n"):
		return Deprecation{}, errors.New("the reason must be one line")
	}
	if _, err := version.Parse(in.Version); err != nil {
		return Deprecation{}, err
	}
	held, missing, err := holding(in.Version, in.Versions)
	if err != nil {
		return Deprecation{}, err
	}
	if len(held) == 0 {
		return Deprecation{Notice: fmt.Sprintf("%s has no version %s; nothing to %s", release.CLI, in.Version, in.Action)}, nil
	}
	d := Deprecation{Exists: true}
	msg := "''"
	if in.Action != "release" {
		msg = shellQuote(prefix + strings.TrimSpace(in.Reason))
	}
	for _, n := range held {
		d.Commands = append(d.Commands, "npm deprecate "+shellQuote(n+"@"+in.Version)+" "+msg)
	}
	if len(missing) > 0 {
		d.Notice = fmt.Sprintf("%s has no version %s; left out", strings.Join(missing, ", "), in.Version)
	}
	return d, nil
}

// holding splits CLIPackages into those whose npm view answer in versions
// lists ver and those that lack it. It returns none held when CLI itself
// lacks ver, which then has nothing to act on.
func holding(ver string, versions map[string]string) (held, missing []string, err error) {
	for _, n := range release.CLIPackages() {
		vs, err := npmVersions(versions[n])
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", n, err)
		}
		if vs[ver] {
			held = append(held, n)
		} else {
			missing = append(missing, n)
		}
	}
	if len(held) == 0 || held[0] != release.CLI {
		return nil, nil, nil
	}
	return held, missing, nil
}
