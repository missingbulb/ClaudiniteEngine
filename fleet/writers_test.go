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

// writers is every function under fleet/ that calls a GH with a method
// other than GET, by package-relative name, and whom it writes to.
var writers = map[string]string{
	"fleet.FireScheduler":     "member: a workflow dispatch on its own scheduler",
	"fleet.EnsureLabel":       "manager",
	"roster.ConvergeAdoption": "manager",
	"update.Force":            "member: through FireScheduler",
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
				if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					m, _ := strconv.Unquote(lit.Value)
					if slices.Contains([]string{"POST", "PUT", "PATCH", "DELETE"}, m) {
						found[pkg+"."+fn.Name.Name] = true
					}
				}
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "FireScheduler" {
					found[pkg+"."+fn.Name.Name] = true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "FireScheduler" {
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
