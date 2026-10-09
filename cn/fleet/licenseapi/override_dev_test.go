//go:build devroots

package licenseapi

import "testing"

// A development build, the rehearsal's, takes the stub's base from the
// environment.
func TestADevelopmentBuildTakesTheBaseOverride(t *testing.T) {
	t.Setenv("CLAUDINITE_LICENSE_API", "https://127.0.0.1:9")
	c, err := FromEnv()
	if err != nil || c.Base != "https://127.0.0.1:9" {
		t.Fatalf("%+v %v", c, err)
	}
}
