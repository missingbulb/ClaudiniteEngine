package signals

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadLocalTellsAnUnreadableRetentionFromAnAbsentOne(t *testing.T) {
	cases := []struct {
		name       string
		config     map[string]map[string]any
		days       *float64
		unreadable bool
	}{
		{"absent", map[string]map[string]any{"acme-pack": {}}, nil, false},
		{"null", map[string]map[string]any{"acme-pack": {"retention_days": nil}}, nil, false},
		{"a number", map[string]map[string]any{"acme-pack": {"retention_days": 7.0}}, ptr(7), false},
		{"a string", map[string]map[string]any{"acme-pack": {"retention_days": "7"}}, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := ReadLocal(t.TempDir(), []string{"acme-pack"}, func(id string) map[string]any { return c.config[id] })
			if (l.RetentionDays == nil) != (c.days == nil) || (c.days != nil && *l.RetentionDays != *c.days) || l.RetentionUnreadable != c.unreadable {
				t.Fatalf("days %v, unreadable %v", l.RetentionDays, l.RetentionUnreadable)
			}
		})
	}
}

func ptr(v float64) *float64 { return &v }

func TestReadLocalReadsTheManifestReleaseConfigNames(t *testing.T) {
	put := func(t *testing.T, root, rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	none := func(string) map[string]any { return nil }
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"manifest_path names a nested manifest", map[string]string{
			".github/release.config":  "# c\nmanifest_path=extension/manifest.json\nsetup_command=npm ci\n",
			"extension/manifest.json": `{"version": "1.5.6"}`,
		}, "1.5.6"},
		{"manifest_path wins over a root manifest", map[string]string{
			".github/release.config":  "manifest_path=extension/manifest.json\n",
			"extension/manifest.json": `{"version": "2.0.0"}`,
			"manifest.json":           `{"version": "0.1.0"}`,
		}, "2.0.0"},
		{"no release config falls back to the usual places", map[string]string{
			"src/manifest.json": `{"version": "3.1.0"}`,
		}, "3.1.0"},
		{"manifest_path naming a missing file reads nothing", map[string]string{
			".github/release.config": "manifest_path=extension/manifest.json\n",
			"manifest.json":          `{"version": "0.1.0"}`,
		}, ""},
	}
	for _, c := range cases {
		root := t.TempDir()
		for rel, body := range c.files {
			put(t, root, rel, body)
		}
		l := ReadLocal(root, nil, none)
		got := ""
		if l.ManifestVersion != nil {
			got = *l.ManifestVersion
		}
		if got != c.want {
			t.Errorf("%s: manifest version %q, want %q", c.name, got, c.want)
		}
	}
}
