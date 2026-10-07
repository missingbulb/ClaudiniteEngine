package githubapi

// A GitHub remote URL.

import (
	"regexp"
	"strings"
)

var (
	remoteRe = []*regexp.Regexp{
		regexp.MustCompile(`^https://(?:[^@/]+@)?github\.com/([^/]+)/([^/]+?)(?:\.git)?/?$`),
		regexp.MustCompile(`^git@github\.com:([^/]+)/([^/]+?)(?:\.git)?$`),
		regexp.MustCompile(`^ssh://git@github\.com/([^/]+)/([^/]+?)(?:\.git)?/?$`),
		regexp.MustCompile(`^git://github\.com/([^/]+)/([^/]+?)(?:\.git)?/?$`),
	}
	ownerRe = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
	nameRe  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
)

// ParseRemote is owner/name for a GitHub remote URL in any of its four
// shapes (https, scp-like ssh, ssh and git), and false for anything else.
func ParseRemote(remote string) (string, bool) {
	remote = strings.TrimSpace(remote)
	for _, re := range remoteRe {
		if m := re.FindStringSubmatch(remote); m != nil && ownerRe.MatchString(m[1]) && nameRe.MatchString(m[2]) {
			return m[1] + "/" + m[2], true
		}
	}
	return "", false
}
