package settings

import (
	"regexp"
)

// LegacyCanaryPackage is the package the canary channel was published as
// before every release went to DefaultPackage under a dist-tag. A pin that
// still names it reads as the canary channel, and the next engine update
// moves it: the package line becomes the channel line.
//
// @legacy-tolerance advisory:engine-package retire:#97
const LegacyCanaryPackage = "@claudinite/cli-rc"

var legacyKey = regexp.MustCompile(`^([ \t\r\n]*"?)package("?[ \t]*[:=][ \t]*")[^"]*(")`)

// FromLegacyPackage rewrites engine.package "@claudinite/cli-rc" into
// engine.channel "canary" in place and changes no other byte; false when
// the pin names no legacy package.
//
// @legacy-tolerance advisory:engine-package retire:#97
func FromLegacyPackage(raw []byte, f Format) ([]byte, bool, error) {
	e, err := ReadEngine(raw, f)
	if err != nil || e.Package != LegacyCanaryPackage || e.HasChannel {
		return raw, false, err
	}
	spans, err := blockSpans(raw, f)
	if err != nil {
		return raw, false, err
	}
	present, _ := keyPatterns(f, "package")
	for _, s := range spans {
		if !present.Match(raw[s.start:s.end]) {
			continue
		}
		moved := legacyKey.ReplaceAll(raw[s.start:s.end], []byte("${1}channel${2}"+ChannelCanary+"${3}"))
		out := append(append(append([]byte{}, raw[:s.start]...), moved...), raw[s.end:]...)
		return out, true, nil
	}
	return raw, false, nil
}

// RetiredTasksPack is the pack whose entry config held the queue's
// settings before the tasks block did.
const RetiredTasksPack = "claudinite-tasks"

// TasksBlock is the tasks block; where the settings declare none, the
// retired claudinite-tasks entry's config spelled as one. legacy says the
// entry is still declared. A key the block does not know is carried, so
// the block's reader refuses it by name.
//
// @legacy-tolerance advisory:tasks-settings retire:#144
func (p Parsed) TasksBlock() (block map[string]any, legacy bool) {
	for _, e := range p.Packs.Entries {
		if e.Local || e.ID != RetiredTasksPack {
			continue
		}
		if p.Tasks != nil {
			return p.Tasks, true
		}
		block = map[string]any{}
		for k, v := range e.Config {
			switch k {
			case "agenticTaskInvocationEndpoints":
				block["routines"] = v
			case "dailyClaudiniteUpdatesRequirePrReview":
				if v == true {
					block["delivery"] = "review"
				}
			case "disabledTasks":
				block["disabled"] = v
			default:
				block[k] = v
			}
		}
		return block, true
	}
	return p.Tasks, false
}
