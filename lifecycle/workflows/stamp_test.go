package workflows

import (
	"reflect"
	"strings"
	"testing"
)

func TestStamp(t *testing.T) {
	exe := Templates()["claudinite-executor.yml"]
	once, ok := Stamp(exe, []string{"ZED_TOKEN", "HELLO_TOKEN", "CCR_ROUTINE_TOKEN", "HELLO_TOKEN"})
	if !ok {
		t.Fatal("the template has no marker")
	}
	if got := StampedSecrets(once); !reflect.DeepEqual(got, []string{"HELLO_TOKEN", "ZED_TOKEN"}) {
		t.Errorf("stamped %v; the template already passes CCR_ROUTINE_TOKEN above the marker", got)
	}
	twice, _ := Stamp(once, StampedSecrets(once))
	if string(twice) != string(once) {
		t.Errorf("a second stamp moved the file:\n%s", twice)
	}
	if string(Expected("claudinite-executor.yml", once)) != string(once) {
		t.Error("Expected does not keep the stamped lines")
	}
	cleared, _ := Stamp(once, nil)
	if string(cleared) != string(exe) {
		t.Error("stamping nothing does not give the template back")
	}
	if _, ok := Stamp([]byte("name: x\n"), []string{"A"}); ok {
		t.Error("stamped a file with no marker")
	}
	if strings.Count(string(once), "HELLO_TOKEN: ${{ secrets.HELLO_TOKEN }}") != 1 {
		t.Error("a repeated name stamped twice")
	}
}
