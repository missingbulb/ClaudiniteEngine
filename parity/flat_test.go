package parity

import "testing"

// cn's flat output compares with Node's once the member file, which Node
// never writes, is left out; the other documents keep their bytes.
func TestSplitMemberFile(t *testing.T) {
	tasks := "{\n  \"version\": 1,\n  \"tasks\": {}\n}\n"
	dashboards := "{\n  \"version\": 1,\n  \"dashboards\": {}\n}\n"
	member := "{\n  \"version\": 1,\n  \"settings\": {\n    \"path\": \".claudinite/settings.yaml\"\n  }\n}\n"
	rest, got, err := splitMemberFile(tasks + dashboards + member)
	if err != nil || rest != tasks+dashboards || got != member {
		t.Errorf("got %q and %q, %v", rest, got, err)
	}
	if rest, got, err := splitMemberFile(tasks + dashboards); err != nil || rest != tasks+dashboards || got != "" {
		t.Errorf("without a member file: %q and %q, %v", rest, got, err)
	}
	if _, _, err := splitMemberFile(tasks + "{"); err == nil {
		t.Error("a truncated document passed")
	}
}
