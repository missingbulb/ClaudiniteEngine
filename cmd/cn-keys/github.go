package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// secretStore is where the ceremony's keys go: GitHub Actions secrets, set
// with the owner's token. env "" means the repository itself.
type secretStore interface {
	CheckAuth() error
	// Environment returns nil, nil when the repository or environment does
	// not exist.
	Environment(repo, env string) (*environment, error)
	SecretNames(repo, env string) ([]string, error)
	SetSecret(repo, env, name string, value []byte) error
}

// environment is what the ceremony reads of an environment's protection.
type environment struct {
	RequiredReviewers bool
	BranchPolicy      bool
}

// ghCLI drives the gh command line, authenticated by GH_TOKEN.
type ghCLI struct{}

var (
	errNotFound  = errors.New("not found")
	errForbidden = errors.New("forbidden")
)

// ghError names what a failed gh call means: a 404 is absence, a 403 a
// token missing a permission, anything else is passed on.
func ghError(args []string, err error, stderr string) error {
	msg := strings.TrimSpace(stderr)
	what := strings.Join(args[:min(2, len(args))], " ")
	switch {
	case strings.Contains(msg, "HTTP 404"):
		return fmt.Errorf("gh %s: %w: %s", what, errNotFound, msg)
	case strings.Contains(msg, "HTTP 403"):
		return fmt.Errorf("gh %s: %w: the token lacks a permission (Secrets: read and write, Environments: read and write, Administration: read): %s", what, errForbidden, msg)
	}
	return fmt.Errorf("gh %s: %v: %s", what, err, msg)
}

func (ghCLI) gh(stdin []byte, args ...string) (string, error) {
	cmd := exec.Command("gh", args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", ghError(args, err, stderr.String())
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

func (g ghCLI) Environment(repo, env string) (*environment, error) {
	out, err := g.gh(nil, "api", apiPath(repo, env))
	if errors.Is(err, errNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if env == "" {
		return &environment{}, nil
	}
	return parseEnvironment([]byte(out))
}

func parseEnvironment(raw []byte) (*environment, error) {
	var e struct {
		ProtectionRules []struct {
			Type string `json:"type"`
		} `json:"protection_rules"`
		DeploymentBranchPolicy json.RawMessage `json:"deployment_branch_policy"`
	}
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, fmt.Errorf("reading an environment: %w", err)
	}
	policy := strings.TrimSpace(string(e.DeploymentBranchPolicy))
	out := &environment{BranchPolicy: policy != "" && policy != "null"}
	for _, r := range e.ProtectionRules {
		out.RequiredReviewers = out.RequiredReviewers || r.Type == "required_reviewers"
	}
	return out, nil
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
