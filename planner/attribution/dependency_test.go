package attribution

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestPackageDependencyBoundary(t *testing.T) {
	forbidden := map[string]struct{}{
		"github.com/meaningforge/metis/planner":            {},
		"github.com/meaningforge/metis/planner/builder":    {},
		"github.com/meaningforge/metis/planner/optimizer":  {},
		"github.com/meaningforge/metis/planner/conversion": {},
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, imported := range parsed.Imports {
			if _, blocked := forbidden[imported.Path.Value[1:len(imported.Path.Value)-1]]; blocked {
				t.Fatalf("%s imports forbidden package %s", file, imported.Path.Value)
			}
		}
	}
}
