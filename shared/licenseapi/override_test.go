//go:build !devroots

package licenseapi

import "testing"

// A release binary talks to the license server it was built for, whatever
// its environment names.
func TestAReleaseBuildIgnoresTheBaseOverride(t *testing.T) {
	t.Setenv("CLAUDINITE_LICENSE_API", "https://127.0.0.1:9")
	c, err := FromEnv()
	if err != nil || c.Base != DefaultBase {
		t.Fatalf("%+v %v", c, err)
	}
}
