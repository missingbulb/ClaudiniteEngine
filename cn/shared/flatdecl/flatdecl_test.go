package flatdecl

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/jsjson"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packset"
)

func put(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestContentAndWrite(t *testing.T) {
	repo := t.TempDir()
	put(t, repo, map[string]string{
		".claudinite/shared/packs/acme-pack/tasks/b/task.json":  `{"id": "b", "n": 1e3, "z": {}, "a": []}`,
		".claudinite/shared/packs/acme-pack/tasks/a/task.yaml":  "id: a\nn: 2\n",
		".claudinite/shared/packs/acme-pack/dashboard.json":     "{not json",
		".claudinite/local/packs/mine/tasks/c/task.json":        `{"id": "c"}`,
		".claudinite/temp/packs/current_user/tasks/d/task.json": `{"id": "d"}`,
		Retired[0]:             "{}",
		LegacyPath(Retired[0]): "{}",
	})
	packs := []packset.Pack{
		{ID: "acme-pack", Kind: packset.Canon, Dir: filepath.Join(repo, ".claudinite/shared/packs/acme-pack"), Rel: ".claudinite/shared/packs/acme-pack"},
		{ID: "mine", Kind: packset.Local, Dir: filepath.Join(repo, ".claudinite/local/packs/mine"), Rel: ".claudinite/local/packs/mine"},
		{ID: "current_user", Kind: packset.Temp, Dir: filepath.Join(repo, ".claudinite/temp/packs/current_user"), Rel: ".claudinite/temp/packs/current_user"},
	}
	content, err := Content(repo, packs)
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "version": 1,
  "tasks": {
    "acme-pack/a": {
      "path": ".claudinite/shared/packs/acme-pack/tasks/a/task.yaml",
      "declaration": {
        "id": "a",
        "n": 2
      }
    },
    "acme-pack/b": {
      "path": ".claudinite/shared/packs/acme-pack/tasks/b/task.json",
      "declaration": {
        "id": "b",
        "n": 1000,
        "z": {},
        "a": []
      }
    },
    "local/mine/c": {
      "path": ".claudinite/local/packs/mine/tasks/c/task.json",
      "declaration": {
        "id": "c"
      }
    }
  }
}
`
	if content[TasksFile] != want {
		t.Errorf("tasks file:\n%s\nwant:\n%s", content[TasksFile], want)
	}
	if _, ok := content[Retired[0]]; ok {
		t.Errorf("content renders the retired %s", Retired[0])
	}
	written, err := Write(repo, packs)
	if err != nil || !reflect.DeepEqual(written, []string{TasksFile, Retired[0], LegacyPath(Retired[0])}) {
		t.Fatalf("first write %v %v: want the task file written and the retired file deleted where held", written, err)
	}
	for _, f := range []string{Retired[0], LegacyPath(Retired[0])} {
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(f))); !os.IsNotExist(err) {
			t.Errorf("%s is still there", f)
		}
	}
	if written, err := Write(repo, packs); err != nil || len(written) != 0 {
		t.Errorf("second write %v %v, want nothing", written, err)
	}
	if c, err := Content(repo, nil); c != nil || err != nil {
		t.Errorf("no packs: %v %v, want nothing to write", c, err)
	}
}

// A source that does not parse is carried as its text.
func TestEntryCarriesUnparsedText(t *testing.T) {
	if got := Entry("x/task.json", []byte("{not json")); !strings.Contains(jsjson.Stringify(got), `"text":"{not json"`) {
		t.Errorf("%s", jsjson.Stringify(got))
	}
}
