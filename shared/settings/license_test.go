package settings

import (
	"strings"
	"testing"
)

const testManifest = pin1

const pinLines = "  version: \"1.60928.1\"\n  manifest: \"" + testManifest + "\"\n"

func licenseFixtures() map[Format]string {
	return map[Format]string{
		YAML: "# kept\nengine:\n" + pinLines + "license:\n  plan: \"public\"\npacks:\n  channel: \"stable\"\n",
		TOML: "# kept\n[engine]\nversion = \"1.60928.1\"\nmanifest = \"" + testManifest + "\"\n\n[license]\nplan = \"public\"\n",
		JSON: "{\n  \"engine\": {\"version\": \"1.60928.1\", \"manifest\": \"" + testManifest + "\"},\n  \"license\": {\"plan\": \"public\"}\n}\n",
	}
}

func noLicenseFixtures() map[Format]string {
	return map[Format]string{
		YAML: "# kept\nengine:\n" + pinLines,
		TOML: "[engine]\nversion = \"1.60928.1\"\nmanifest = \"" + testManifest + "\"\n",
		JSON: "{\n  \"engine\": {\"version\": \"1.60928.1\", \"manifest\": \"" + testManifest + "\"}\n}\n",
	}
}

// A file that still carries the retired block parses whatever its plan,
// and reads as carrying it.
func TestARetiredLicenseBlockStillParses(t *testing.T) {
	for f, raw := range licenseFixtures() {
		for _, plan := range []string{`"public"`, `"private-repo"`, `"gold"`} {
			r := []byte(strings.Replace(raw, `"public"`, plan, 1))
			if !HasRetiredLicense(r, f) {
				t.Errorf("%s %s: the block is not seen", f, plan)
			}
			if _, err := ReadEngine(r, f); err != nil {
				t.Errorf("%s %s: %v", f, plan, err)
			}
			if _, err := ParseFile(r, f); err != nil {
				t.Errorf("%s %s: %v", f, plan, err)
			}
		}
	}
	for f, raw := range noLicenseFixtures() {
		if HasRetiredLicense([]byte(raw), f) {
			t.Errorf("%s: a block where there is none", f)
		}
	}
}

func TestDropLicenseRemovesOnlyTheBlock(t *testing.T) {
	want := map[Format]string{
		YAML: "# kept\nengine:\n" + pinLines + "packs:\n  channel: \"stable\"\n",
		TOML: "# kept\n[engine]\nversion = \"1.60928.1\"\nmanifest = \"" + testManifest + "\"\n",
		JSON: "{\n  \"engine\": {\"version\": \"1.60928.1\", \"manifest\": \"" + testManifest + "\"}\n}\n",
	}
	for f, raw := range licenseFixtures() {
		got, err := DropLicense([]byte(raw), f)
		if err != nil || string(got) != want[f] {
			t.Errorf("%s: %v\n%s\nwant\n%s", f, err, got, want[f])
		}
	}
	for f, raw := range noLicenseFixtures() {
		if got, err := DropLicense([]byte(raw), f); err != nil || string(got) != raw {
			t.Errorf("%s without a block: %v\n%s", f, err, got)
		}
	}
	first := "{\"license\": {\"plan\": \"public\"},\n  \"engine\": {\"version\": \"1.60928.1\", \"manifest\": \"" + testManifest + "\"}}\n"
	if got, err := DropLicense([]byte(first), JSON); err != nil || string(got) != "{\"engine\": {\"version\": \"1.60928.1\", \"manifest\": \""+testManifest+"\"}}\n" {
		t.Errorf("first member: %v\n%s", err, got)
	}
	mid := "[engine]\nversion = \"1.60928.1\"\nmanifest = \"" + testManifest + "\"\n\n[license]\nplan = \"public\"\n\n[packs]\nchannel = \"stable\"\n"
	if got, err := DropLicense([]byte(mid), TOML); err != nil || string(got) != "[engine]\nversion = \"1.60928.1\"\nmanifest = \""+testManifest+"\"\n\n[packs]\nchannel = \"stable\"\n" {
		t.Errorf("a block between two: %v\n%s", err, got)
	}
}

// The engine update's pull request may drop the retired block beside the
// pin, and nothing else.
func TestAPinMoveMayDropTheRetiredBlock(t *testing.T) {
	for f, raw := range licenseFixtures() {
		moved, _ := SetPin([]byte(raw), f, "1.60929.1", testManifest)
		dropped, _ := DropLicense(moved, f)
		if err := PinOnlyChange([]byte(raw), dropped, f); err != nil {
			t.Errorf("%s: %v", f, err)
		}
		if err := PinOnlyChange([]byte(raw), []byte(strings.Replace(string(dropped), "1.60929.1", "1.60929.1", 1)+"\n"), f); err == nil {
			t.Errorf("%s: a drop with another change passed", f)
		}
	}
}
