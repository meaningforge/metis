package optimizer

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
)

func TestPackageDoesNotImportForbiddenPlannerDomains(t *testing.T) {
	forbidden := map[string]struct{}{
		"github.com/meaningforge/metis/planner":            {},
		"github.com/meaningforge/metis/planner/builder":    {},
		"github.com/meaningforge/metis/planner/conversion": {},
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if filepath.Base(name) == "dependency_test.go" {
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
			if _, blocked := forbidden[path]; blocked {
				t.Fatalf("%s imports forbidden planner domain %q", name, path)
			}
		}
	}
}
