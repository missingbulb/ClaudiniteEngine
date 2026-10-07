package workflows

import (
	"reflect"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/mergepolicy"
)

// The engine/update task's automerge covers exactly the workflows this
// package manages: mergepolicy keeps its own copy of the list, since a
// shared package imports no lifecycle one.
func TestTheUpdatePolicyCoversExactlyTheManagedWorkflows(t *testing.T) {
	if !reflect.DeepEqual(mergepolicy.EngineWorkflows, Names) {
		t.Fatalf("mergepolicy.EngineWorkflows %v, Names %v", mergepolicy.EngineWorkflows, Names)
	}
	body := "on: {}\n"
	for _, n := range append(append([]string{}, Names...), Superseded) {
		v := mergepolicy.Judge([]any{"engine-update-files"}, []mergepolicy.Entry{{File: ".github/workflows/" + n, After: &body}}, mergepolicy.Declared{})
		if v.Mergeable != (n != Superseded) {
			t.Errorf("%s: mergeable %v: %s", n, v.Mergeable, v.Why)
		}
	}
}
