package artifact

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPackageDependencyBoundary(t *testing.T) {
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
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(path, "github.com/meaningforge/metis/") && path != "github.com/meaningforge/metis/renderer/sql" && path != "github.com/meaningforge/metis/ossie" && path != "github.com/meaningforge/metis/query" {
				t.Fatalf("%s imports forbidden Metis package %s", file, path)
			}
		}
	}
}
