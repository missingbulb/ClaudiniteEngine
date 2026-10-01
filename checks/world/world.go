// Package world is cn check world. In this chunk the whole world run is the
// pin guard, which refuses a change to the engine pin or the launcher on a
// pull request the update bot did not open, or that moves anything but the
// pin, or whose new pin does not verify or is held, revoked or deprecated;
// then verify's findings and the packs' world-tagged checks. A change
// confined to .claudinite/shared/packs/ passes the guard whoever made it,
// the bot's pack PR and a person's adoption alike: verify's pack rules
// judge that tree. The caller injects the pin check and the findings, so
// this capability imports neither the updater nor verify.
package world

import (
	"fmt"
	"io"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
)

// Bot is the only author allowed to move the pin.
const Bot = "github-actions[bot]"

// Git is the repository reads the guard needs.
type Git interface {
	MergeBase(a, b string) (string, error)
	ChangedFiles(base, head string) ([]string, error)
	Show(ref, path string) ([]byte, bool, error)
}

// Input is one world run.
type Input struct {
	Repo     string
	PRAuthor string
	BaseRef  string
	Git      Git
	// CheckPin verifies a new pin as the updater does (signature, hashes)
	// and refuses one npm marks deprecated or the states list as held or
	// revoked.
	CheckPin func(settings.Engine) error
	Findings []findings.Finding
}

// Run prints the guard's verdict and the findings, and returns 1 on a
// refusal or a break, else 0.
func Run(w io.Writer, in Input) int {
	if err := guard(in); err != nil {
		fmt.Fprintf(w, "check world: refused (pin-guard): %v\n", err)
		findings.Print(w, in.Findings)
		return 1
	}
	findings.Print(w, in.Findings)
	if findings.AnyBreak(in.Findings) {
		return 1
	}
	return 0
}

func isPinFile(p string) bool {
	if p == ".claudinite/launch" {
		return true
	}
	for _, f := range settings.Formats {
		if p == settings.RelPath(f) {
			return true
		}
	}
	return false
}

func guard(in Input) error {
	base, err := in.Git.MergeBase(in.BaseRef, "HEAD")
	if err != nil {
		base = in.BaseRef
	}
	changed, err := in.Git.ChangedFiles(base, "HEAD")
	if err != nil {
		return err
	}
	var pinFiles []string
	for _, c := range changed {
		if isPinFile(c) {
			pinFiles = append(pinFiles, c)
		}
	}
	if len(pinFiles) == 0 {
		return nil
	}
	if in.PRAuthor != Bot {
		return fmt.Errorf("%s changes %s; only the update bot (%s) may move the engine pin or the launcher, through its update PR", in.PRAuthor, strings.Join(pinFiles, " and "), Bot)
	}
	for _, p := range pinFiles {
		if p == ".claudinite/launch" {
			return fmt.Errorf("the PR changes the launcher, .claudinite/launch; an engine update PR moves only engine.version and engine.manifest")
		}
	}
	if len(changed) != 1 {
		return fmt.Errorf("an update PR changes only engine.version and engine.manifest, but this one changes %s", strings.Join(changed, ", "))
	}
	path := pinFiles[0]
	format := settings.Format(strings.TrimPrefix(path, ".claudinite/settings."))
	old, inBase, err := in.Git.Show(base, path)
	if err != nil {
		return err
	}
	if !inBase {
		return fmt.Errorf("%s does not exist on %s; an update PR moves the pin in the settings file already there", path, in.BaseRef)
	}
	cur, _, err := in.Git.Show("HEAD", path)
	if err != nil {
		return err
	}
	if settings.PlanOnlyChange(old, cur, format) == nil {
		return nil
	}
	if err := settings.PinOnlyChange(old, cur, format); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	e, err := settings.ReadEngine(cur, format)
	if err != nil {
		return err
	}
	if err := in.CheckPin(e); err != nil {
		return fmt.Errorf("the new pin %s does not verify: %w", e.Version, err)
	}
	return nil
}
