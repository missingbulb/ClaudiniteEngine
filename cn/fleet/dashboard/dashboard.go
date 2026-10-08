// Package dashboard is the fleet dashboard: a static page that enumerates
// the fleet's owner's repositories in the viewer's browser, as the
// viewer, and reads each over the API, so nothing it shows is baked in at
// build time but the deployment's config. Build lays the page out as the
// site a Pages deployment serves; the page's own sources are under site/
// and carry their own account of what they read.
package dashboard

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

//go:embed site
var site embed.FS

// PagesWorkflow is a fleet manager's Pages workflow: build the site with
// `cn fleet create-dashboard-artifact` and deploy what it wrote.
//
//go:embed pages-workflow.yml
var PagesWorkflow string

// Home is where the page is served under the site root: the path a
// deployment's sign-in callback and its readers' links already name. The
// root redirects to it.
const Home = "packs/claudinite-dashboard"

// The repository variables the sign-in pair are read from, as the page's
// sign-in gate names them to a viewer when they are unset.
const (
	ClientIDVar    = "CLAUDINITE_DASHBOARD_CLIENT_ID"
	ExchangeURLVar = "CLAUDINITE_DASHBOARD_EXCHANGE_URL"
)

// pageAt is the page's file among the sources; it is served from Home, so
// its relative links resolve against Home rather than src/.
const pageAt = "src/index.html"

// published are the sources the browser loads.
var published = []string{"src", "favicon.svg"}

// Config is what the page is told about its deployment.
type Config struct {
	// Owner is whose repositories the page enumerates, as the viewer.
	Owner string
	// Exclude are the repositories the fleet leaves out, drawn greyed.
	Exclude []string
	// DeploymentRepo is the manager's owner/name, where the page reads the
	// fleet roster from.
	DeploymentRepo string
	// ClientID and ExchangeURL are the sign-in pair; "" is unset.
	ClientID, ExchangeURL string
}

// pageConfig is dashboard.config.json, in the page's key order. A nil
// pointer is null: a key the page reads as unset.
type pageConfig struct {
	Mode           string          `json:"mode"`
	ClientID       *string         `json:"clientId"`
	ExchangeURL    *string         `json:"exchangeUrl"`
	RedirectURI    *string         `json:"redirectUri"`
	Owner          string          `json:"owner"`
	Exclude        []string        `json:"exclude"`
	DefaultRepo    *string         `json:"defaultRepo"`
	DeploymentRepo string          `json:"deploymentRepo"`
	Rates          json.RawMessage `json:"rates"`
}

func orNull(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Build writes the site into out, replacing whatever out held. The site is
// staged in a directory beside out and renamed into place, so out is the
// previous site or the whole new one, never a mix; the stage is removed
// whatever happens. It returns the number of files written.
func Build(out string, c Config) (int, error) {
	out, err := filepath.Abs(out)
	if err != nil {
		return 0, err
	}
	parent := filepath.Dir(out)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return 0, err
	}
	stage, err := os.MkdirTemp(parent, ".cn-dashboard-*")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(stage)
	n, err := stageSite(stage, c)
	if err != nil {
		return 0, err
	}
	if err := os.Chmod(stage, 0o755); err != nil {
		return 0, err
	}
	if err := os.RemoveAll(out); err != nil {
		return 0, err
	}
	return n, os.Rename(stage, out)
}

func stageSite(dir string, c Config) (int, error) {
	sources, err := fs.Sub(site, "site")
	if err != nil {
		return 0, err
	}
	n := 0
	write := func(rel string, data []byte) error {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		n++
		return os.WriteFile(p, data, 0o644)
	}
	for _, root := range published {
		err := fs.WalkDir(sources, root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := fs.ReadFile(sources, p)
			if err != nil {
				return err
			}
			if p == pageAt {
				p = "index.html"
			}
			return write(path.Join(Home, p), data)
		})
		if err != nil {
			return 0, err
		}
	}
	cfg, err := configJSON(c)
	if err != nil {
		return 0, err
	}
	if err := write(path.Join(Home, "dashboard.config.json"), cfg); err != nil {
		return 0, err
	}
	if err := write("index.html", []byte(redirect)); err != nil {
		return 0, err
	}
	// Pages runs an uploaded site through Jekyll unless told otherwise, and
	// Jekyll drops paths that begin with an underscore.
	if err := write(".nojekyll", nil); err != nil {
		return 0, err
	}
	return n, nil
}

func configJSON(c Config) ([]byte, error) {
	exclude := c.Exclude
	if exclude == nil {
		exclude = []string{}
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	err := enc.Encode(pageConfig{
		Mode:           "fleet",
		ClientID:       orNull(strings.TrimSpace(c.ClientID)),
		ExchangeURL:    orNull(strings.TrimSpace(c.ExchangeURL)),
		Owner:          c.Owner,
		Exclude:        exclude,
		DeploymentRepo: c.DeploymentRepo,
		Rates:          json.RawMessage("null"),
	})
	return b.Bytes(), err
}

var redirect = fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Claudinite fleet</title>
<link rel="icon" href="./%[1]s/favicon.svg" type="image/svg+xml">
<meta http-equiv="refresh" content="0; url=./%[1]s/">
<link rel="canonical" href="./%[1]s/">
</head>
<body><p>Continue to the <a href="./%[1]s/">fleet dashboard</a>.</p></body>
</html>
`, Home)
