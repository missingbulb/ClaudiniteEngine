package checksdk

import (
	"embed"
	"io/fs"
)

// sdk is the whole SDK, which cn embeds and unpacks as the module
// claudinite.com/checksdk for the checks build and for a pack repo's
// tests. This file is not part of that copy.
//
//go:embed checksdk.go fake.go markdown.go pipe.go repo.go scan.go session.go text.go
var sdk embed.FS

// Sources maps each SDK file's name to its content.
func Sources() map[string][]byte {
	out := map[string][]byte{}
	entries, _ := fs.ReadDir(sdk, ".")
	for _, e := range entries {
		b, _ := fs.ReadFile(sdk, e.Name())
		out[e.Name()] = b
	}
	return out
}
