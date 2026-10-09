package parity

import "testing"

// The engines' flat output compares once the member file, which only cn
// writes, and the dashboards, which only Node writes, are left out; the
// other documents keep their bytes.
func TestSplitDoc(t *testing.T) {
	tasks := "{\n  \"version\": 1,\n  \"tasks\": {}\n}\n"
	dashboards := "{\n  \"version\": 1,\n  \"dashboards\": {}\n}\n"
	member := "{\n  \"version\": 1,\n  \"settings\": {\n    \"path\": \".claudinite/settings.yaml\"\n  }\n}\n"
	rest, got, err := splitDoc(tasks+member, "settings")
	if err != nil || rest != tasks || got != member {
		t.Errorf("got %q and %q, %v", rest, got, err)
	}
	if rest, got, err := splitDoc(tasks+dashboards, "dashboards"); err != nil || rest != tasks || got != dashboards {
		t.Errorf("Node's dashboards: %q and %q, %v", rest, got, err)
	}
	if rest, got, err := splitDoc(tasks, "settings"); err != nil || rest != tasks || got != "" {
		t.Errorf("without a member file: %q and %q, %v", rest, got, err)
	}
	if _, _, err := splitDoc(tasks+"{", "settings"); err == nil {
		t.Error("a truncated document passed")
	}
}
