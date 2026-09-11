package semanticplan

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
)

const rootPlannerImport = "github.com/meaningforge/metis/planner"

func TestPackageDoesNotImportRootPlanner(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if filepath.Ext(name) != ".go" || filepath.Base(name) == "dependency_test.go" {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imported := range parsed.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf("unquote import in %s: %v", name, err)
			}
			if path == rootPlannerImport {
				t.Fatalf("%s imports the root planner package; semanticplan must remain an IR boundary", name)
			}
		}
	}
}
