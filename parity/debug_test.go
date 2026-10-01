package parity

import (
	"fmt"
	"os"
	"testing"
)

// TestDebugMaterialize keeps one scenario's two repos for a look by hand.
func TestDebugMaterialize(t *testing.T) {
	name := os.Getenv("CLAUDINITE_PARITY_DEBUG")
	if name == "" {
		t.Skip()
	}
	ss, _ := LoadScenarios("testdata/scenarios")
	for _, s := range ss {
		if s.Group+"/"+s.Name != name {
			continue
		}
		out, _ := os.MkdirTemp("", "parity-")
		for _, e := range []Engine{Node{}, Cn{}} {
			d, err := s.Materialize(out, os.Getenv(nodeEnv)+"/packs", e)
			fmt.Println(d, err)
		}
	}
}
