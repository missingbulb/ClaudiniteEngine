package checksdk

import _ "embed"

// Source is checksdk.go, the whole SDK, which cn embeds and unpacks as the
// module claudinite.com/checksdk for the checks build. This file is not
// part of that copy.
//
//go:embed checksdk.go
var Source []byte
