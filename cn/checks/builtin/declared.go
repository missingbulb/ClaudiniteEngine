package builtin

import (
	"embed"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/descriptor"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/provenance"
)

// declarations are the declared checks the engine carries for a pack,
// one file per pack, named for its id.
//
//go:embed declared/*.json
var declarations embed.FS

func init() {
	entries, err := declarations.ReadDir("declared")
	if err != nil {
		panic(err)
	}
	for _, e := range entries {
		pack := strings.TrimSuffix(e.Name(), ".json")
		raw, err := declarations.ReadFile("declared/" + e.Name())
		if err != nil {
			panic(err)
		}
		decls, err := declared.Declarations(raw, descriptor.JSON)
		if err != nil {
			panic("the engine's declared checks for " + pack + " do not parse: " + err.Error())
		}
		declared.RegisterEngineDeclarations(pack, raw)
		for _, d := range decls {
			if id, ok := d["id"].(string); ok {
				provenance.RegisterEngineCheck(pack, id)
			}
		}
	}
}
