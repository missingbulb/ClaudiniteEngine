package adopt

import (
	"path/filepath"
	"strings"
)

// FullNameOf is the repo's "owner/name" from its origin URL, in any form a
// clone carries (https, ssh, a proxy's path), or dir's base name when the
// URL names none.
func FullNameOf(originURL, dir string) string {
	u := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(originURL), "/"), ".git")
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	} else if at := strings.Index(u, "@"); at >= 0 {
		u = strings.Replace(u[at+1:], ":", "/", 1)
	}
	parts := strings.Split(u, "/")
	if len(parts) >= 3 && parts[len(parts)-2] != "" && parts[len(parts)-1] != "" {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return filepath.Base(dir)
}
