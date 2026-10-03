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

func TestReadLicense(t *testing.T) {
	for f, raw := range licenseFixtures() {
		l, err := ReadLicense([]byte(raw), f)
		if err != nil || l.Plan != "public" || !l.Present {
			t.Errorf("%s: %+v %v", f, l, err)
		}
	}
	for f, raw := range noLicenseFixtures() {
		l, err := ReadLicense([]byte(raw), f)
		if err != nil || l.Plan != "" || l.Present {
			t.Errorf("%s without a block: %+v %v", f, l, err)
		}
	}
}

func TestReadLicenseRefusesAPlanOutsideTheFive(t *testing.T) {
	for f, raw := range licenseFixtures() {
		bad := strings.Replace(raw, `"public"`, `"gold"`, 1)
		if _, err := ReadLicense([]byte(bad), f); err == nil || !strings.Contains(err.Error(), "gold") {
			t.Errorf("%s: %v", f, err)
		}
	}
}

func TestSetPlanIsALineEdit(t *testing.T) {
	for f, raw := range licenseFixtures() {
		got, err := SetPlan([]byte(raw), f, "personal")
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if want := strings.Replace(raw, `"public"`, `"personal"`, 1); string(got) != want {
			t.Errorf("%s:\n%s\nwant\n%s", f, got, want)
		}
	}
}

func TestSetPlanAddsTheBlock(t *testing.T) {
	for f, raw := range noLicenseFixtures() {
		got, err := SetPlan([]byte(raw), f, "public")
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if !strings.HasPrefix(string(got), strings.TrimSuffix(raw, "}\n")[:len(raw)/2]) {
			t.Errorf("%s: the head of the file moved:\n%s", f, got)
		}
		l, err := ReadLicense(got, f)
		if err != nil || l.Plan != "public" {
			t.Errorf("%s: %+v %v\n%s", f, l, err, got)
		}
		if _, err := ReadEngine(got, f); err != nil {
			t.Errorf("%s: the pin no longer reads: %v\n%s", f, err, got)
		}
	}
}

func TestSetPlanRefusesAnUnknownPlan(t *testing.T) {
	if _, err := SetPlan([]byte(licenseFixtures()[YAML]), YAML, "gold"); err == nil {
		t.Fatal("set gold")
	}
}

func TestPlanOnlyChange(t *testing.T) {
	for f, raw := range licenseFixtures() {
		moved, _ := SetPlan([]byte(raw), f, "organization")
		if err := PlanOnlyChange([]byte(raw), moved, f); err != nil {
			t.Errorf("%s: %v", f, err)
		}
		pin, _ := SetPin(moved, f, "1.60929.1", testManifest)
		if err := PlanOnlyChange([]byte(raw), pin, f); err == nil {
			t.Errorf("%s: a pin move passed as plan-only", f)
		}
		if err := PlanOnlyChange([]byte(raw), []byte(raw), f); err == nil {
			t.Errorf("%s: no change passed as a plan change", f)
		}
	}
	for f, raw := range noLicenseFixtures() {
		added, _ := SetPlan([]byte(raw), f, "public")
		if err := PlanOnlyChange([]byte(raw), added, f); err != nil {
			t.Errorf("%s adding the block: %v", f, err)
		}
	}
}
