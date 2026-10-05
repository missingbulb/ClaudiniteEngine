package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// The updater picks a channel's candidate from @claudinite/cli's
// dist-tags, so the stub serves them: latest on the newest version as npm
// does on a plain publish, and each --tag beside it.
func TestPackumentServesDistTags(t *testing.T) {
	dist := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dist, "tarballs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"1.61004.1", "1.61005.1", "1.61005.2"} {
		if err := os.WriteFile(filepath.Join(dist, "tarballs", "cli-"+v+".tgz"), []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var tags tagList
	for _, f := range []string{"rc", "staging=1.61005.1", "old=9.9.9"} {
		if err := tags.Set(f); err != nil {
			t.Fatal(err)
		}
	}
	for _, bad := range []string{"", "RC", "rc=latest", "rc=1.2"} {
		if err := tags.Set(bad); err == nil {
			t.Errorf("--tag %q accepted", bad)
		}
	}
	rec := httptest.NewRecorder()
	servePackument(rec, httptest.NewRequest("GET", "/@claudinite%2fcli", nil), distList{dist}, "cli", "", tags)
	var p struct {
		DistTags map[string]string `json:"dist-tags"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("%v: %s", err, rec.Body)
	}
	want := map[string]string{"latest": "1.61005.2", "rc": "1.61005.2", "staging": "1.61005.1"}
	if len(p.DistTags) != len(want) {
		t.Errorf("dist-tags %v, want %v", p.DistTags, want)
	}
	for k, v := range want {
		if p.DistTags[k] != v {
			t.Errorf("dist-tags %v, want %v", p.DistTags, want)
		}
	}
}
