// Package world is cn check world: the pin guard, which refuses a change
// to the engine pin or the launcher on a pull request the update bot did
// not open (but for the first pin an adoption writes), or that moves
// anything but the pin, its member file and the managed workflows exactly
// as the new engine expects them, or whose new pin does not verify or is held,
// revoked or deprecated; then the findings the caller
// gathered, verify's and every world-tagged check's, declared checks
// included. A change confined to .claudinite/shared/packs/ passes the
// guard whoever made it, the bot's pack PR and a person's adoption alike:
// verify's pack rules judge that tree. The caller injects the pin check,
// the expected workflows and the findings, so this capability imports
// neither the updater, verify, the workflows nor the check engine.
package world

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/flatdecl"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
)

// Bot is the only author allowed to move the pin.
const Bot = "github-actions[bot]"

// Git is the repository reads the guard needs.
type Git interface {
	MergeBase(a, b string) (string, error)
	ChangedFiles(base, head string) ([]string, error)
	Show(ref, path string) ([]byte, bool, error)
	Regular(ref, path string) (bool, error)
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
	// Workflows are the file names under .github/workflows/ the engine
	// manages, and ExpectedWorkflow what this engine expects one to hold
	// given the base's copy (nil when absent): an update PR may carry each
	// exactly so. A nil ExpectedWorkflow accepts none.
	Workflows        []string
	ExpectedWorkflow func(name string, base []byte) ([]byte, error)
	// Launcher is the launcher this engine ships: an update PR may carry
	// it, byte for byte, and no other.
	Launcher []byte
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

// Report prints the findings without the pin guard, for a run with no
// pull request to judge, and returns 1 on a break, else 0.
func Report(w io.Writer, in Input) int {
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
		// A directory never lists as a changed path; a symlink or submodule
		// there would carry pin files git never shows.
		if c == ".claudinite" {
			return fmt.Errorf(".claudinite is not a directory")
		}
		if isPinFile(c) {
			pinFiles = append(pinFiles, c)
		}
	}
	if len(pinFiles) == 0 {
		return nil
	}
	// The launcher follows a symlink that Show reads as its target's path.
	for _, p := range pinFiles {
		_, there, err := in.Git.Show("HEAD", p)
		if err != nil {
			return err
		}
		regular, err := in.Git.Regular("HEAD", p)
		if err != nil {
			return err
		}
		if there && !regular {
			return fmt.Errorf("%s is not a regular file", p)
		}
	}
	if in.PRAuthor != Bot {
		return personGuard(in, base, pinFiles)
	}
	var settingsFiles []string
	for _, p := range pinFiles {
		if p != ".claudinite/launch" {
			settingsFiles = append(settingsFiles, p)
			continue
		}
		have, _, err := in.Git.Show("HEAD", p)
		if err != nil {
			return err
		}
		if _, held, err := in.Git.Show(base, p); err != nil {
			return err
		} else if !held {
			return fmt.Errorf("the PR adds the launcher, .claudinite/launch, which only cn init writes")
		}
		if len(in.Launcher) == 0 || !bytes.Equal(have, in.Launcher) {
			return fmt.Errorf("the PR changes the launcher, .claudinite/launch, to one this engine does not ship")
		}
	}
	if len(settingsFiles) == 0 {
		return fmt.Errorf("an update PR moves engine.version and engine.manifest, but this one changes only the launcher")
	}
	pinFiles = settingsFiles
	// The member file restates the pin, so the engine update PR carries it,
	// with the launcher the new engine ships and the workflows it expects,
	// which its agent stage moved in.
	others := 0
	for _, c := range changed {
		if c == flatdecl.MemberFile || c == flatdecl.LegacyPath(flatdecl.MemberFile) || c == ".claudinite/launch" {
			continue
		}
		accepted, err := expectedWorkflow(in, base, c)
		if err != nil {
			return err
		}
		if !accepted {
			others++
		}
	}
	if others != 1 {
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

// expectedWorkflow reports whether path is a workflow the engine manages
// that HEAD holds, as a regular file, exactly as this engine expects it
// over base's copy. Any other path is not accepted, and is no error.
func expectedWorkflow(in Input, base, path string) (bool, error) {
	name, ok := strings.CutPrefix(path, ".github/workflows/")
	if !ok || in.ExpectedWorkflow == nil {
		return false, nil
	}
	managed := false
	for _, n := range in.Workflows {
		managed = managed || n == name
	}
	if !managed {
		return false, nil
	}
	have, there, err := in.Git.Show("HEAD", path)
	if err != nil || !there {
		return false, err
	}
	if regular, err := in.Git.Regular("HEAD", path); err != nil || !regular {
		return false, err
	}
	old, _, err := in.Git.Show(base, path)
	if err != nil {
		return false, err
	}
	want, err := in.ExpectedWorkflow(name, old)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	return string(have) == string(want), nil
}

// personGuard judges a PR a person opened that touches the settings file or
// the launcher. On a repo whose base holds neither, it is the adoption (or
// the move from the Node engine) writing the first pin, which must verify.
// Otherwise the person may change anything in the settings file but its
// engine block, save engine.channel, and never the launcher.
func personGuard(in Input, base string, pinFiles []string) error {
	refuse := func() error {
		return fmt.Errorf("%s changes %s; only the update bot (%s) may move the engine pin or the launcher, through its update PR", in.PRAuthor, strings.Join(pinFiles, " and "), Bot)
	}
	adopted := false
	for _, p := range append([]string{".claudinite/launch"}, settingsPaths()...) {
		_, ok, err := in.Git.Show(base, p)
		if err != nil {
			return err
		}
		adopted = adopted || ok
	}
	if !adopted {
		written := 0
		for _, p := range settingsPaths() {
			_, ok, err := in.Git.Show("HEAD", p)
			if err != nil {
				return err
			}
			if ok {
				written++
			}
		}
		if written > 1 {
			return fmt.Errorf("the adoption writes %d settings files; the launcher reads one settings file", written)
		}
		for _, p := range pinFiles {
			if p == ".claudinite/launch" {
				continue
			}
			format := settings.Format(strings.TrimPrefix(p, ".claudinite/settings."))
			cur, _, err := in.Git.Show("HEAD", p)
			if err != nil {
				return err
			}
			e, err := settings.ReadEngine(cur, format)
			if err != nil {
				return err
			}
			if err := in.CheckPin(e); err != nil {
				return fmt.Errorf("the first pin %s does not verify: %w", e.Version, err)
			}
			return nil
		}
		return refuse()
	}
	for _, p := range pinFiles {
		if p == ".claudinite/launch" {
			return refuse()
		}
		format := settings.Format(strings.TrimPrefix(p, ".claudinite/settings."))
		old, inBase, err := in.Git.Show(base, p)
		if err != nil {
			return err
		}
		cur, inHead, err := in.Git.Show("HEAD", p)
		if err != nil {
			return err
		}
		if !inBase || !inHead {
			return refuse()
		}
		was, err := settings.ReadEngine(old, format)
		if err != nil {
			return err
		}
		now, err := settings.ReadEngine(cur, format)
		if err != nil {
			return refuse()
		}
		// The channel and the releases repository are the person's choice of
		// which releases to take and from where; the update bot still moves
		// the pin.
		now.Channel, now.HasChannel = was.Channel, was.HasChannel
		now.Releases = was.Releases
		if now != was {
			return refuse()
		}
	}
	return nil
}

func settingsPaths() []string {
	var out []string
	for _, f := range settings.Formats {
		out = append(out, settings.RelPath(f))
	}
	return out
}
