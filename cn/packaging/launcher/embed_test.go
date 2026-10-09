package launcher

import (
	"os"
	"testing"
)

func TestScriptIsTheLauncher(t *testing.T) {
	raw, err := os.ReadFile("launch")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(Script) {
		t.Error("the embedded launcher differs from cn/packaging/launcher/launch")
	}
	for _, h := range Shipped() {
		if len(h) != 64 {
			t.Errorf("shipped.sha256 entry %q is not a SHA-256", h)
		}
	}
}
