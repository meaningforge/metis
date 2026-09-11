package builder

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const rootPlannerImport = "github.com/meaningforge/metis/planner"

func TestBuilderDoesNotImportRootPlanner(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read builder package: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		filename := filepath.Join(".", entry.Name())
		parsed, err := parser.ParseFile(token.NewFileSet(), filename, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", filename, err)
		}
		for _, imported := range parsed.Imports {
			if strings.Trim(imported.Path.Value, "\"") == rootPlannerImport {
				t.Errorf("%s imports the root planner package", filename)
			}
		}
	}
}
