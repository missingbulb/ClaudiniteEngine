package dashboard

import (
	"encoding/json"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/descriptor"

	"github.com/missingbulb/ClaudiniteEngine/cn/fleet/roster"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/flatdecl"
)

// The page spells the engine's paths and the build's variable names
// itself, since it imports nothing from the engine; each copy must match
// the Go side it reads or is written by.
func TestThePageSpellsWhatTheEngineWrites(t *testing.T) {
	cases := []struct{ file, name, want string }{
		{"src/read/signin-vars.mjs", "clientId", ClientIDVar},
		{"src/read/signin-vars.mjs", "exchangeUrl", ExchangeURLVar},
		{"src/read/roster.mjs", "ROSTER_PATH", roster.RosterFile},
		{"src/read/member.mjs", "MEMBER_PATH", flatdecl.MemberFile},
		{"src/read/flat.mjs", "FLAT_TASKS_PATH", flatdecl.TasksFile},
	}
	for _, c := range cases {
		src, err := fs.ReadFile(site, "site/"+c.file)
		if err != nil {
			t.Fatal(err)
		}
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(c.name) + `\s*[:=]\s*'([^']*)'`)
		m := re.FindSubmatch(src)
		if m == nil || string(m[1]) != c.want {
			t.Errorf("%s %s = %q, want %q", c.file, c.name, m, c.want)
		}
	}
}

// Every module the page imports is in the embedded site.
func TestEveryRelativeImportResolvesInTheSite(t *testing.T) {
	imp := regexp.MustCompile(`(?m)^\s*(?:import|export)\b[^'"]*?from\s+'(\.[^']+)'`)
	n := 0
	err := fs.WalkDir(site, "site", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".mjs") {
			return err
		}
		src, err := fs.ReadFile(site, p)
		if err != nil {
			return err
		}
		dir := p[:strings.LastIndex(p, "/")]
		for _, m := range imp.FindAllSubmatch(src, -1) {
			n++
			target := cleanJoin(dir, string(m[1]))
			if _, err := fs.Stat(site, target); err != nil {
				t.Errorf("%s imports %s, not in the site", p, m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no imports found: the pattern matches nothing")
	}
}

func cleanJoin(dir, rel string) string {
	parts := strings.Split(dir, "/")
	for _, seg := range strings.Split(rel, "/") {
		switch seg {
		case ".":
		case "..":
			parts = parts[:len(parts)-1]
		default:
			parts = append(parts, seg)
		}
	}
	return strings.Join(parts, "/")
}

// The sample Pages workflow builds with the license-checked verb, uploads
// what it wrote, deploys it, and grants what deploying and the license
// gate's OIDC exchange need.
func TestThePagesWorkflowBuildsWithTheVerbAndDeploys(t *testing.T) {
	var wf struct {
		Permissions map[string]string `json:"permissions"`
		Jobs        map[string]struct {
			Permissions map[string]string `json:"permissions"`
			Steps       []struct {
				Uses string            `json:"uses"`
				Run  string            `json:"run"`
				With map[string]any    `json:"with"`
				Env  map[string]string `json:"env"`
			} `json:"steps"`
		} `json:"jobs"`
	}
	doc, err := descriptor.ParseDocument([]byte(PagesWorkflow), descriptor.YAML)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &wf); err != nil {
		t.Fatal(err)
	}
	var built, uploaded, deployed bool
	perms := map[string]string{}
	for k, v := range wf.Permissions {
		perms[k] = v
	}
	for _, job := range wf.Jobs {
		for k, v := range job.Permissions {
			perms[k] = v
		}
		for _, s := range job.Steps {
			switch {
			case strings.Contains(s.Run, "sh .claudinite/launch fleet create-dashboard-artifact --out _site"):
				built = s.Env[ClientIDVar] != "" && s.Env[ExchangeURLVar] != ""
			case strings.HasPrefix(s.Uses, "actions/upload-pages-artifact@"):
				uploaded = s.With["path"] == "_site"
			case strings.HasPrefix(s.Uses, "actions/deploy-pages@"):
				deployed = true
			}
		}
	}
	if !built || !uploaded || !deployed {
		t.Errorf("built %v (with the sign-in variables), uploaded _site %v, deployed %v", built, uploaded, deployed)
	}
	if perms["pages"] != "write" || perms["id-token"] != "write" {
		t.Errorf("permissions %v", perms)
	}
}
