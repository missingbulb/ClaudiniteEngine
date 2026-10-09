package declared

import (
	"sort"
	"sync"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/descriptor"
)

// EngineDeclarationFile stands for the file of a declared check the
// engine carries, where a pack's own check names its declared-checks
// file.
func EngineDeclarationFile(pack string) string { return "(engine) " + pack + "/declared-checks.json" }

var (
	engineMu   sync.Mutex
	engineDecl = map[string][]byte{}
)

// RegisterEngineDeclarations adds the declared checks the engine carries
// under pack; LoadSet loads EnginePack's on every member, and an id a
// pack's own file also declares runs as the engine's.
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

// outranked is own less every check whose id the engine carries in
// engine, which runs in its place.
func outranked(own, engine []*Check) []*Check {
	ids := map[string]bool{}
	for _, c := range engine {
		ids[c.ID] = true
	}
	var out []*Check
	for _, c := range own {
		if !ids[c.ID] {
			out = append(out, c)
		}
	}
	return out
}
