package githubapi

import (
	"os"
	"os/exec"
	"strings"
)

// RepoFullName is the checkout's owner/name: GITHUB_REPOSITORY, else the
// executor's CLAUDINITE_REPO, else the checkout's origin remote; "" when
// none names one.
func RepoFullName(root string) string {
	for _, k := range []string{"GITHUB_REPOSITORY", "CLAUDINITE_REPO"} {
		if r := os.Getenv(k); strings.Contains(r, "/") {
			return r
		}
	}
	out, err := exec.Command("git", "-C", root, "remote", "get-url", "origin").Output()
	if err == nil {
		if r, ok := ParseRemote(strings.TrimSpace(string(out))); ok {
			return r
		}
	}
	return ""
}
