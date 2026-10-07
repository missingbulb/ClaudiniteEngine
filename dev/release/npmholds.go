package release

import (
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// HoldsInput is what NPMHolds checks: the dist a release published, and the
// registry it published to. Channel and Repo name the from-npm.yml dispatch
// a run that gives up prints.
type HoldsInput struct {
	Dist     string
	Version  string
	Channel  string
	Repo     string
	Registry string
	HTTP     *http.Client
	Timeout  time.Duration
	Every    time.Duration
	Log      io.Writer
}

// NPMHolds proves the registry holds exactly the tarballs in Dist/tarballs:
// for each CLI package Dist has a tarball of, it polls the registry's
// version document until it names that tarball's integrity, looking at
// every package not yet matched in each round. A document naming other
// bytes fails at once. When Timeout runs out the packages are already on
// npm, which refuses a second publish, so the error names the from-npm.yml
// dispatch that carries the release on.
func NPMHolds(in HoldsInput) error {
	start := time.Now()
	want := map[string]string{}
	var pending []string
	for i, pkg := range CLIPackages() {
		file := filepath.Join(in.Dist, "tarballs", strings.TrimPrefix(pkg, "@claudinite/")+"-"+in.Version+".tgz")
		raw, err := os.ReadFile(file)
		if errors.Is(err, os.ErrNotExist) && i > 0 {
			continue
		}
		if err != nil {
			return err
		}
		sum := sha512.Sum512(raw)
		want[pkg] = "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
		pending = append(pending, pkg)
	}
	checked := len(pending)
	for {
		var still, why []string
		for _, pkg := range pending {
			got, status, err := registryIntegrity(in, pkg)
			elapsed := time.Since(start).Round(time.Second)
			switch {
			case err == nil && got == want[pkg]:
				fmt.Fprintf(in.Log, "npm-holds: %s %s matches (+%s)\n", pkg, in.Version, elapsed)
				continue
			case err == nil:
				return fmt.Errorf("npm-holds: %s %s on the registry is %s, but the dist built %s", pkg, in.Version, got, want[pkg])
			}
			fmt.Fprintf(in.Log, "npm-holds: %s %s +%s %s\n", pkg, in.Version, elapsed, status)
			still = append(still, pkg)
			why = append(why, fmt.Sprintf("%s %s (%v)", pkg, in.Version, err))
		}
		if len(still) == 0 {
			fmt.Fprintf(in.Log, "npm-holds: %d packages of %s match the dist after %s\n", checked, in.Version, time.Since(start).Round(time.Second))
			return nil
		}
		if time.Since(start) >= in.Timeout {
			return fmt.Errorf("npm-holds: after %s the registry still does not list %s.\n"+
				"npm took every package of %s, so re-running this job is refused. Carry the release on with\n  %s",
				in.Timeout, strings.Join(why, ", "), in.Version, wayOn(in))
		}
		pending = still
		time.Sleep(in.Every)
	}
}

// wayOn is the from-npm.yml dispatch release.yml's from-npm job runs once
// npm-holds passes.
func wayOn(in HoldsInput) string {
	raw, err := os.ReadFile(filepath.Join(in.Dist, "manifest.integrity"))
	if err != nil {
		return fmt.Sprintf("the from-npm.yml dispatch, though its integrity is unknown: %v", err)
	}
	return FromNPMDispatch(in.Repo, in.Version, strings.TrimSpace(string(raw)), in.Channel)
}

// FromNPMDispatch is the command that starts from-npm.yml on one release.
func FromNPMDispatch(repo, version, integrity, channel string) string {
	return fmt.Sprintf("gh workflow run from-npm.yml --repo %s --ref v%s -f version=%s -f integrity=%s -f channel=%s", repo, version, version, integrity, channel)
}

// registryIntegrity reads one version document through a URL no cache has seen.
func registryIntegrity(in HoldsInput, pkg string) (string, string, error) {
	u := in.Registry + "/" + pkg + "/" + in.Version + "?npm-holds=" + strconv.FormatInt(time.Now().UnixNano(), 10)
	resp, err := in.HTTP.Get(u)
	if err != nil {
		return "", "error", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", strconv.Itoa(resp.StatusCode), fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var doc struct {
		Dist struct {
			Integrity string `json:"integrity"`
		} `json:"dist"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return "", "200", err
	}
	if doc.Dist.Integrity == "" {
		return "", "200", errors.New("no dist.integrity")
	}
	return doc.Dist.Integrity, "200", nil
}
