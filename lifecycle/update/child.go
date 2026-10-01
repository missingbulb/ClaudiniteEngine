package update

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// scrubbedEnv is the whole environment a new binary runs with: enough to
// find its cache, a temp dir and the Go toolchain's caches (check world
// builds the packs' checks), never a token.
func scrubbedEnv() []string {
	var env []string
	for _, k := range []string{"PATH", "HOME", "XDG_CACHE_HOME", "TMPDIR", "SYSTEMROOT", "USERPROFILE", "GOCACHE", "GOROOT", "GOPATH", "GOMODCACHE"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// child runs binary with args, a scrubbed environment and a timeout, and
// returns its stdout, stderr and exit code.
func child(binary string, timeout time.Duration, args ...string) (string, string, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = scrubbedEnv()
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return stdout.String(), stderr.String(), -1, fmt.Errorf("%s %s did not finish within %v", binary, strings.Join(args, " "), timeout)
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return stdout.String(), stderr.String(), exit.ExitCode(), nil
	}
	if err != nil {
		return stdout.String(), stderr.String(), -1, err
	}
	return stdout.String(), stderr.String(), 0, nil
}

// Selftest runs `<binary> selftest` and refuses a non-zero exit or a first
// line other than "version <want>".
func Selftest(binary, want string, timeout time.Duration) (string, error) {
	out, errOut, code, err := child(binary, timeout, "selftest")
	if err != nil {
		return out, fmt.Errorf("selftest: %w", err)
	}
	first, _, _ := strings.Cut(out, "\n")
	if code != 0 {
		return out, fmt.Errorf("selftest of %s exited %d: %s", want, code, strings.TrimSpace(out+errOut))
	}
	if strings.TrimSpace(first) != "version "+want {
		return out, fmt.Errorf("selftest reports %q, not version %s", first, want)
	}
	return out, nil
}

// RunVerify runs `<binary> verify --repo DIR`: exit 0 is no break, exit 1
// a break, anything else an error. Its findings come back verbatim.
func RunVerify(binary, repo string, timeout time.Duration) (string, bool, error) {
	out, errOut, code, err := child(binary, timeout, "verify", "--repo", repo)
	if err != nil {
		return out, false, fmt.Errorf("verify: %w", err)
	}
	switch code {
	case 0:
		return out, false, nil
	case 1:
		return out, true, nil
	}
	return out, false, fmt.Errorf("verify exited %d: %s", code, strings.TrimSpace(out+errOut))
}

// WorkflowsDiff runs `<binary> workflows diff --repo DIR` and returns the
// unified diff of the member's workflows against that binary's templates,
// empty when they match.
func WorkflowsDiff(binary, repo string, timeout time.Duration) (string, error) {
	out, errOut, code, err := child(binary, timeout, "workflows", "diff", "--repo", repo)
	if err != nil {
		return "", fmt.Errorf("workflows diff: %w", err)
	}
	if code != 0 {
		return "", fmt.Errorf("workflows diff exited %d: %s", code, strings.TrimSpace(out+errOut))
	}
	return out, nil
}
