package release

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// DeprecateInput is a hold or revoke dispatch of promote.yml and what npm
// says exists: each channel's `npm view <pkg> versions --json`, which is
// empty or an E404 error object when the package has no versions.
type DeprecateInput struct {
	Action, Version, Reason    string
	RCVersions, StableVersions string
}

// Deprecation is what the deprecate job does: the npm deprecate commands
// for every package of the version, or, when npm has no such version, a
// notice and nothing.
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
// understands, on the channel package and its platform packages, and on
// the stable ones when the version was promoted.
func DeprecateCommands(in DeprecateInput) (Deprecation, error) {
	prefix := map[string]string{"hold": "held: ", "revoke": "revoked: "}[in.Action]
	switch {
	case prefix == "":
		return Deprecation{}, fmt.Errorf("action %q is not hold or revoke", in.Action)
	case strings.TrimSpace(in.Reason) == "":
		return Deprecation{}, errors.New("a hold or revoke needs a reason")
	case strings.ContainsAny(in.Reason, "\r\n"):
		return Deprecation{}, errors.New("the reason must be one line")
	}
	if _, err := version.Parse(in.Version); err != nil {
		return Deprecation{}, err
	}
	rc, err := npmVersions(in.RCVersions)
	if err != nil {
		return Deprecation{}, err
	}
	stable, err := npmVersions(in.StableVersions)
	if err != nil {
		return Deprecation{}, err
	}
	if !rc[in.Version] {
		return Deprecation{Notice: fmt.Sprintf("@claudinite/cli-rc has no version %s; nothing to %s", in.Version, in.Action)}, nil
	}
	d := Deprecation{Exists: true}
	bases := []string{"@claudinite/cli-rc"}
	if stable[in.Version] {
		bases = append(bases, "@claudinite/cli")
	}
	msg := shellQuote(prefix + strings.TrimSpace(in.Reason))
	for _, base := range bases {
		names := []string{base}
		for _, p := range version.Platforms {
			names = append(names, base+"-"+p)
		}
		for _, n := range names {
			d.Commands = append(d.Commands, "npm deprecate "+shellQuote(n+"@"+in.Version)+" "+msg)
		}
	}
	return d, nil
}
