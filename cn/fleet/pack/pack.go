// Package pack is the fleet pack the engine carries: rules, skills,
// checks and tasks a fleet manager runs, turned on by the settings' fleet
// block and written for each run by packset.
package pack

import (
	"embed"
	"io/fs"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/settings"
)

//go:embed all:files
var files embed.FS

func init() {
	sub, err := fs.Sub(files, "files")
	if err != nil {
		panic(err)
	}
	packset.RegisterEmbedded(packset.Embedded{
		ID:     packset.FleetPack,
		Files:  sub,
		Active: func(p settings.Parsed) bool { return p.Fleet != nil },
	})
}
