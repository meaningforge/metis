package conversion

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPackageDependencyBoundary(t *testing.T) {
	forbidden := map[string]struct{}{
		"github.com/meaningforge/metis/compiler":            {},
		"github.com/meaningforge/metis/planner":             {},
		"github.com/meaningforge/metis/planner/evaluation":  {},
		"github.com/meaningforge/metis/planner/builder":     {},
		"github.com/meaningforge/metis/planner/optimizer":   {},
		"github.com/meaningforge/metis/planner/attribution": {},
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, imported := range parsed.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf("parse import in %s: %v", file, err)
			}
			if _, blocked := forbidden[path]; blocked {
				t.Fatalf("%s imports forbidden package %s", file, imported.Path.Value)
			}
			if strings.HasPrefix(path, "github.com/meaningforge/metis/renderer/") {
				t.Fatalf("%s imports concrete renderer package %s", file, imported.Path.Value)
			}
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch selector.Sel.Name {
			case "NewRegistry", "Resolve", "Render":
				t.Errorf("%s calls forbidden renderer authority method %s", file, selector.Sel.Name)
			}
			return true
		})
	}
}
