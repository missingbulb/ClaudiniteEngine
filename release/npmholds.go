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
// registry it published to.
type HoldsInput struct {
	Dist     string
	Version  string
	Registry string
	HTTP     *http.Client
	Timeout  time.Duration
	Every    time.Duration
	Log      io.Writer
}

// NPMHolds proves the registry holds exactly the tarballs in Dist: for each
// CLI package Dist has a tarball of, it polls the registry's version
// document, which npm serves within seconds of a publish while the tarball
// itself can answer 404 for minutes, until it names that tarball's
// integrity. A document naming other bytes fails at once.
func NPMHolds(in HoldsInput) error {
	start := time.Now()
	checked := 0
	for i, pkg := range CLIPackages() {
		file := filepath.Join(in.Dist, strings.TrimPrefix(pkg, "@claudinite/")+"-"+in.Version+".tgz")
		raw, err := os.ReadFile(file)
		if errors.Is(err, os.ErrNotExist) && i > 0 {
			continue
		}
		if err != nil {
			return err
		}
		sum := sha512.Sum512(raw)
		want := "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
		if err := holds(in, pkg, want, start); err != nil {
			return err
		}
		checked++
	}
	fmt.Fprintf(in.Log, "npm-holds: %d packages of %s match the dist after %s\n", checked, in.Version, time.Since(start).Round(time.Second))
	return nil
}

func holds(in HoldsInput, pkg, want string, start time.Time) error {
	for {
		got, status, err := registryIntegrity(in, pkg)
		elapsed := time.Since(start).Round(time.Second)
		switch {
		case err == nil && got == want:
			fmt.Fprintf(in.Log, "npm-holds: %s %s matches (+%s)\n", pkg, in.Version, elapsed)
			return nil
		case err == nil:
			return fmt.Errorf("npm-holds: %s %s on the registry is %s, but the dist built %s", pkg, in.Version, got, want)
		}
		fmt.Fprintf(in.Log, "npm-holds: %s %s +%s %s\n", pkg, in.Version, elapsed, status)
		if time.Since(start) >= in.Timeout {
			return fmt.Errorf("npm-holds: the registry still does not list %s %s after %s: %v", pkg, in.Version, in.Timeout, err)
		}
		time.Sleep(in.Every)
	}
}

// registryIntegrity reads one version document through a URL no cache has seen.
func registryIntegrity(in HoldsInput, pkg string) (string, string, error) {
	u := in.Registry + "/" + pkg + "/" + in.Version + "?npm-holds=" + strconv.FormatInt(time.Now().UnixNano(), 10)
	resp, err := in.HTTP.Get(u)
	if err != nil {
		return "", "error", err
	}
	defer resp.Body.Close()
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
