package fleet_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// writers is every function under fleet/ that calls a GH (or Expect)
// with a method other than GET, dispatches a scheduler or puts a file,
// by package-relative name, and whom it writes to.
var writers = map[string]string{
	"fleet.FireScheduler":        "member: a workflow dispatch on its own scheduler",
	"fleet.EnsureLabel":          "manager, or a member's work-list label",
	"fleet.PutFile":              "member: the one write into its tree, a sha-guarded Contents PUT",
	"roster.ConvergeAdoption":    "manager",
	"update.Force":               "member: through FireScheduler",
	"addpacks.Remark":            "member: its work-list issue's mark, body and status labels",
	"addpacks.openIssue":         "member: a marked work-list issue",
	"addpacks.CloseSatisfied":    "member: closes its satisfied requested list",
	"addpacks.ConvergeSuspected": "member: closes its suspected list when fitted",
	"addpacks.Run":               "member: the nudge, through FireScheduler",
	"seeds.member":               "member: its settings file, through PutFile",
	"mirror.commit":              "manager: its own vendored branch, the shelf's mirror",
}

func TestEveryWriterIsListed(t *testing.T) {
	found := map[string]bool{}
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		pkg := f.Name.Name
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				for _, arg := range call.Args[:min(2, len(call.Args))] {
					if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						m, _ := strconv.Unquote(lit.Value)
						if slices.Contains([]string{"POST", "PUT", "PATCH", "DELETE"}, m) {
							found[pkg+"."+fn.Name.Name] = true
						}
					}
				}
				name := ""
				if id, ok := call.Fun.(*ast.Ident); ok {
					name = id.Name
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
					name = sel.Sel.Name
				}
				if name == "FireScheduler" || name == "PutFile" {
					found[pkg+"."+fn.Name.Name] = true
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 {
		t.Fatal("the scan found no writer at all; it no longer reads the source")
	}
	for w := range found {
		if _, ok := writers[w]; !ok {
			t.Errorf("%s writes and is not listed in writers", w)
		}
	}
	for w := range writers {
		if !found[w] {
			t.Errorf("writers lists %s, which writes nothing", w)
		}
	}
}
