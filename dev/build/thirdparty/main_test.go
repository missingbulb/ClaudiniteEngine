package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// The committed notices are what the modules linked into cn ship today.
func TestTheNoticesMatchTheModulesLinkedIntoCn(t *testing.T) {
	mods, err := linked("../../..", "./cn/cli")
	if err != nil {
		t.Fatal(err)
	}
	if len(mods) == 0 {
		t.Fatal("no third-party modules found; the scope is wrong")
	}
	want, err := render(mods)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../THIRD_PARTY_LICENSES")
	if err != nil || !bytes.Equal(got, want) {
		t.Errorf("dev/build/THIRD_PARTY_LICENSES is stale (%v); run: go run ./dev/build/thirdparty > dev/build/THIRD_PARTY_LICENSES", err)
	}
	for _, m := range []string{"github.com/BurntSushi/toml", "go.yaml.in/yaml/v3", "github.com/dlclark/regexp2"} {
		if !strings.Contains(string(want), "== "+m+" ") {
			t.Errorf("%s missing", m)
		}
	}
	if !strings.Contains(string(want), "--- NOTICE ---") {
		t.Error("yaml's NOTICE missing")
	}
}
