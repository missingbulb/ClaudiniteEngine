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

// A name the env already passes outside the stamped block, above or below
// it, is left to that line; a line passing another secret under the name
// does not count.
func TestStampLeavesANamePassedOutsideTheBlock(t *testing.T) {
	exe := "jobs:\n  run:\n    steps:\n      - env:\n" +
		"          ABOVE_TOKEN: ${{ secrets.ABOVE_TOKEN }}\n" +
		"          " + SecretsMarker + "\n" +
		"          OLD_TOKEN: ${{ secrets.OLD_TOKEN }}\n" +
		"          GH_TOKEN: ${{ github.token }}\n" +
		"          BELOW_TOKEN: ${{ secrets.BELOW_TOKEN }}\n" +
		"          ALIAS_TOKEN: ${{ secrets.OTHER_TOKEN }}\n"
	got, ok := Stamp([]byte(exe), []string{"BELOW_TOKEN", "ABOVE_TOKEN", "ALIAS_TOKEN", "NEW_TOKEN"})
	if !ok {
		t.Fatal("no marker found")
	}
	if s := StampedSecrets(got); !reflect.DeepEqual(s, []string{"ALIAS_TOKEN", "NEW_TOKEN"}) {
		t.Errorf("stamped %v\n%s", s, got)
	}
	for _, n := range []string{"ABOVE_TOKEN", "BELOW_TOKEN"} {
		if c := strings.Count(string(got), n+": ${{ secrets."+n+" }}"); c != 1 {
			t.Errorf("%s passed %d times:\n%s", n, c, got)
		}
	}
	if strings.Contains(string(got), "OLD_TOKEN") {
		t.Errorf("the block's old line survived:\n%s", got)
	}
}

// A member whose own marker block stamps a secret the template already
// passes above the marker (a Node executor stamped CCR_ROUTINE_TOKEN there)
// gets that name once: GitHub refuses a workflow whose env repeats a key.
func TestExpectedDropsAStampedNameTheTemplatePasses(t *testing.T) {
	have := "env:\n          " + SecretsMarker + "\n" +
		"          CCR_ROUTINE_TOKEN: ${{ secrets.CCR_ROUTINE_TOKEN }}\n" +
		"          SCRAPER_API_KEY: ${{ secrets.SCRAPER_API_KEY }}\n"
	got := string(Expected("claudinite-executor.yml", []byte(have)))
	if n := strings.Count(got, "CCR_ROUTINE_TOKEN: ${{ secrets.CCR_ROUTINE_TOKEN }}"); n != 1 {
		t.Errorf("CCR_ROUTINE_TOKEN passed %d times:\n%s", n, got)
	}
	if !reflect.DeepEqual(StampedSecrets([]byte(got)), []string{"SCRAPER_API_KEY"}) {
		t.Errorf("stamped %v, want [SCRAPER_API_KEY]", StampedSecrets([]byte(got)))
	}
}
