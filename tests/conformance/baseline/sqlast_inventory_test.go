package baseline_test

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// This test guards the deleted physical IR boundary. Tests
// are scanned too, so a compatibility path cannot survive under test cover.
func TestLegacySQLASTArchitectureIsDeleted(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	files := repositoryGoFiles(t, root)
	assertExactInventory(t, "repository sqlast imports", findSQLASTImports(t, root, files), nil)
	assertExactInventory(t, "repository legacy SQL AST builder calls", findLegacySQLASTBuilderCalls(t, root, files), nil)
	if _, err := fs.Stat(os.DirFS(root), "sqlast"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("legacy sqlast package exists or cannot be checked: %v", err)
	}
}

func repositoryGoFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "vendor") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk repository Go files: %v", err)
	}
	sort.Strings(files)
	return files
}

func findSQLASTImports(t *testing.T, root string, files []string) []string {
	t.Helper()
	var found []string
	for _, path := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imported := range parsed.Imports {
			value, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf("unquote import in %s: %v", path, err)
			}
			if value == "github.com/meaningforge/metis/sqlast" {
				found = append(found, slashRelative(t, root, path))
			}
		}
	}
	return found
}

func findLegacySQLASTBuilderCalls(t *testing.T, root string, files []string) []string {
	t.Helper()
	set := map[string]bool{}
	for _, path := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch function := call.Fun.(type) {
			case *ast.Ident:
				if function.Name == "Build"+"SQLAST" {
					set[slashRelative(t, root, path)] = true
				}
			case *ast.SelectorExpr:
				if function.Sel.Name == "Build"+"SQLAST" {
					set[slashRelative(t, root, path)] = true
				}
			}
			return true
		})
	}
	var found []string
	for path := range set {
		found = append(found, path)
	}
	sort.Strings(found)
	return found
}

func slashRelative(t *testing.T, root, path string) string {
	t.Helper()
	relative, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatalf("make %s relative to %s: %v", path, root, err)
	}
	return filepath.ToSlash(relative)
}

func assertExactInventory(t *testing.T, name string, got, want []string) {
	t.Helper()
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("%s changed\n got:\n  %s\nwant:\n  %s", name, strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}
