package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/fleet/dashboard"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/report"
)

// fleetCreateDashboardArtifact is `cn fleet create-dashboard-artifact`:
// the fleet dashboard's site, configured from the manager's fleet block
// and its sign-in variables, written into --out for a Pages workflow to
// upload. It reads no member and needs no fleet token: the page reads the
// fleet itself, as its viewer.
func fleetCreateDashboardArtifact(args []string, stdout, stderr io.Writer, start time.Time) error {
	const name = "fleet create-dashboard-artifact"
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	repo := fs.String("repo", "", "")
	out := fs.String("out", "", "")
	if err := flags(fs, args); err != nil {
		return err
	}
	crumb := func(outcome string, n int) {
		fmt.Fprintf(stderr, "[cn] fleet create-dashboard-artifact %s %d/%d %dms\n", outcome, n, n, time.Since(start).Milliseconds())
	}
	failed := func(err error) error {
		fmt.Fprintf(stderr, "%s failed: %s\n", name, err.Error())
		crumb("error", 0)
		return report.Said(report.IO)
	}
	root, err := fleetRoot(*repo)
	if err != nil {
		return failed(err)
	}
	home, err := fleetHome(root)
	if err != nil {
		return failed(err)
	}
	cfg, err := fleetConfig(root, home)
	if err != nil {
		return failed(err)
	}
	if *out == "" {
		*out = filepath.Join(root, "_site")
	}
	site := dashboard.Config{
		Owner:          cfg.Owner,
		Exclude:        cfg.Exclude,
		DeploymentRepo: home,
		ClientID:       os.Getenv(dashboard.ClientIDVar),
		ExchangeURL:    os.Getenv(dashboard.ExchangeURLVar),
	}
	n, err := dashboard.Build(*out, site)
	if err != nil {
		return failed(err)
	}
	signIn := "configured"
	var unset []string
	for _, v := range [][2]string{{dashboard.ClientIDVar, site.ClientID}, {dashboard.ExchangeURLVar, site.ExchangeURL}} {
		if strings.TrimSpace(v[1]) == "" {
			unset = append(unset, v[0])
		}
	}
	if len(unset) > 0 {
		signIn = "NOT configured, so nobody can read the site: set the repository variable(s) " + strings.Join(unset, ", ")
	}
	covers := "every repository under " + cfg.Owner + " the viewer can read"
	if len(cfg.Exclude) > 0 {
		covers += fmt.Sprintf(", %d excluded", len(cfg.Exclude))
	}
	emit(stdout, fmt.Sprintf("Built the fleet dashboard of %s into %s\n  covers: %s\n  sign-in: %s", home, *out, covers, signIn))
	crumb("ok", n)
	return nil
}
