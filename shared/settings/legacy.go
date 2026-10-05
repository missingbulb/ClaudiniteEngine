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
