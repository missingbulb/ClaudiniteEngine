package update

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/proc"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/workflows"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/selftest"
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
	cmd := proc.CommandContext(ctx, binary, args...)
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

// SelftestFailed is a candidate whose selftest over the member failed a
// probe: the update stops there, forced or not.
type SelftestFailed struct {
	Version string
	Probes  []string
	Report  string
}

func (e *SelftestFailed) Error() string {
	return "selftest of " + e.Version + " failed (" + strings.Join(e.Probes, ", ") + ")"
}

// Selftest runs `<binary> selftest --repo DIR`, or the machine probes
// alone when repo is "", and refuses a first line
// other than "version <want>". A non-zero exit naming failed probes is a
// *SelftestFailed; any other non-zero exit is an error.
func Selftest(binary, want, repo string, timeout time.Duration) (string, error) {
	args := []string{"selftest"}
	if repo != "" {
		args = append(args, "--repo", repo)
	}
	out, errOut, code, err := child(binary, timeout, args...)
	if err != nil {
		return out, fmt.Errorf("selftest: %w", err)
	}
	first, _, _ := strings.Cut(out, "\n")
	if strings.TrimSpace(first) != "version "+want {
		return out, fmt.Errorf("selftest reports %q, not version %s: %s", first, want, strings.TrimSpace(errOut))
	}
	if code != 0 {
		if failed := selftest.Failed(out); len(failed) > 0 {
			return out, &SelftestFailed{Version: want, Probes: failed, Report: out}
		}
		return out, fmt.Errorf("selftest of %s exited %d: %s", want, code, strings.TrimSpace(out+errOut))
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

// StageWorkflows runs `<binary> workflows stage --repo DIR --name NAME`,
// which writes into DIR's staging directory each workflow that binary
// expects of the member name and DIR does not carry, and returns the
// staged paths, repo-relative; none when the workflows already match.
func StageWorkflows(binary, repo, name string, timeout time.Duration) ([]string, error) {
	if name == "" {
		return nil, errors.New("workflows stage: this repo's owner/name is unknown (GITHUB_REPOSITORY is not set)")
	}
	out, errOut, code, err := child(binary, timeout, "workflows", "stage", "--repo", repo, "--name", name)
	if err != nil {
		return nil, fmt.Errorf("workflows stage: %w", err)
	}
	if code != 0 {
		return nil, fmt.Errorf("workflows stage exited %d: %s", code, strings.TrimSpace(out+errOut))
	}
	var staged []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if !strings.HasPrefix(l, workflows.StagingDir+"/") || strings.Contains(strings.TrimPrefix(l, workflows.StagingDir+"/"), "/") {
				return nil, fmt.Errorf("workflows stage named %q, not a file in %s", l, workflows.StagingDir)
			}
			staged = append(staged, l)
		}
	}
	return staged, nil
}
