package declared

import (
	"sort"
	"sync"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/descriptor"
)

// EngineDeclarationFile stands for the file of a declared check the
// engine carries for a pack, where a pack's own check names its
// declared-checks file.
func EngineDeclarationFile(pack string) string { return "(engine) " + pack + "/declared-checks.json" }

var (
	engineMu   sync.Mutex
	engineDecl = map[string][]byte{}
)

// RegisterEngineDeclarations adds the declared checks the engine carries
// for pack: they load wherever that pack is active, as the pack's own
// declared-checks file does, and an id the pack's file also declares runs
// as the engine's.
func RegisterEngineDeclarations(pack string, raw []byte) {
	engineMu.Lock()
	defer engineMu.Unlock()
	engineDecl[pack] = raw
}

// EngineDeclarationPacks are the packs the engine carries declared checks
// for, sorted.
func EngineDeclarationPacks() []string {
	engineMu.Lock()
	defer engineMu.Unlock()
	out := make([]string, 0, len(engineDecl))
	for p := range engineDecl {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// LoadEngine compiles the declared checks the engine carries for pack,
// none when it carries none.
func LoadEngine(pack string) ([]*Check, error) {
	engineMu.Lock()
	raw, ok := engineDecl[pack]
	engineMu.Unlock()
	if !ok {
		return nil, nil
	}
	file := EngineDeclarationFile(pack)
	decls, err := Declarations(raw, descriptor.JSON)
	if err != nil {
		return nil, &LoadError{file, err}
	}
	var out []*Check
	for _, d := range decls {
		c, err := Compile(d, nil)
		if err != nil {
			return nil, &LoadError{file, err}
		}
		c.Pack, c.File = pack, file
		c.Tags = []string{c.Kind(), "declared", "builtin", pack}
		if c.Scope == "action" {
			c.Tags = []string{"action", "work", "pre-tool-use", "declared", "builtin", pack}
		}
		out = append(out, c)
	}
	return out, nil
}

// withEngine is the pack's own checks with the engine's for it, an id
// both declare taken from the engine.
func withEngine(own, engine []*Check) []*Check {
	if len(engine) == 0 {
		return own
	}
	ids := map[string]bool{}
	for _, c := range engine {
		ids[c.ID] = true
	}
	out := append([]*Check{}, engine...)
	for _, c := range own {
		if !ids[c.ID] {
			out = append(out, c)
		}
	}
	return out
}
