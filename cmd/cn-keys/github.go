package main

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// secretStore is where the ceremony's keys go: GitHub Actions secrets, set
// with the owner's token. env "" means the repository itself.
type secretStore interface {
	CheckAuth() error
	EnvironmentExists(repo, env string) (bool, error)
	CreateEnvironment(repo, env string) error
	SecretNames(repo, env string) ([]string, error)
	SetSecret(repo, env, name string, value []byte) error
}

// ghCLI drives the gh command line, authenticated by GH_TOKEN.
type ghCLI struct{}

var errNotFound = errors.New("not found")

func (ghCLI) gh(stdin []byte, args ...string) (string, error) {
	cmd := exec.Command("gh", args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if strings.Contains(msg, "HTTP 404") {
			return "", fmt.Errorf("gh %s %s: %w: %s", args[0], args[1], errNotFound, msg)
		}
		return "", fmt.Errorf("gh %s %s: %v: %s", args[0], args[1], err, msg)
	}
	return stdout.String(), nil
}

func (g ghCLI) CheckAuth() error {
	if _, err := exec.LookPath("gh"); err != nil {
		return errors.New("the GitHub CLI `gh` is not on PATH")
	}
	if _, err := g.gh(nil, "auth", "status"); err != nil {
		return fmt.Errorf("gh is not authenticated; is CEREMONY_TOKEN set in this environment? (%v)", err)
	}
	return nil
}

func apiPath(repo, env string) string {
	if env == "" {
		return "repos/" + repo
	}
	return "repos/" + repo + "/environments/" + env
}

func (g ghCLI) EnvironmentExists(repo, env string) (bool, error) {
	_, err := g.gh(nil, "api", "--silent", apiPath(repo, env))
	if errors.Is(err, errNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (g ghCLI) CreateEnvironment(repo, env string) error {
	_, err := g.gh(nil, "api", "--silent", "-X", "PUT", apiPath(repo, env))
	return err
}

func (g ghCLI) SecretNames(repo, env string) ([]string, error) {
	out, err := g.gh(nil, "api", "--paginate", apiPath(repo, env)+"/secrets", "--jq", ".secrets[].name")
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

// SetSecret feeds the value on stdin so it never appears in a process list.
func (g ghCLI) SetSecret(repo, env, name string, value []byte) error {
	args := []string{"secret", "set", name, "--repo", repo}
	if env != "" {
		args = append(args, "--env", env)
	}
	_, err := g.gh(value, args...)
	return err
}
