package logger

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// untraced lists the functions allowed to start without the trace call.
// "*" covers every file.
var untraced = map[string]map[string]bool{
	// Error and LogMessage are called while a line is being logged; init and
	// Execute run before the log is open.
	"*": set("init", "Execute", "Error", "LogMessage", "String"),
	// Called per character or word, or (line) logged by scanShell instead.
	"pkg/targets/structure.go": set("top", "push", "pushSubst", "pop", "inString", "depth", "keyword", "line"),
}

// Packages the logger depends on (and the logger itself) cannot call it.
var untracedDirs = []string{"pkg/logger", "pkg/paths", "pkg/version"}

// With --verbose the log shows every call, so every function must trace itself.
func TestEveryFunctionIsTraced(t *testing.T) {
	root := filepath.Join("..", "..")
	checked := 0
	for _, top := range []string{"cmd", "pkg"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			for _, dir := range untracedDirs {
				if filepath.Dir(rel) == dir {
					return nil
				}
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil || untraced["*"][fn.Name.Name] || untraced[rel][fn.Name.Name] {
					continue
				}
				checked++
				var first bytes.Buffer
				if len(fn.Body.List) > 0 {
					printer.Fprint(&first, fset, fn.Body.List[0])
				}
				if first.String() != "defer logger.Enter()()" {
					t.Errorf("%s:%d: %s must start with `defer logger.Enter()()` (or be listed in untraced)",
						rel, fset.Position(fn.Pos()).Line, fn.Name.Name)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if checked < 100 {
		t.Fatalf("only %d functions checked: is the source tree where the test expects it?", checked)
	}
}
