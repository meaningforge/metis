package planner

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestRootPlannerContainsOnlyOrchestrationAndObservation(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	got := append([]string(nil), pkg.GoFiles...)
	sort.Strings(got)
	want := []string{"doc.go", "planner.go", "runtime_observer.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("root planner production files = %v, want orchestration boundary %v", got, want)
	}
}

func TestHelperSizedPlannerPackagesDoNotReappear(t *testing.T) {
	for _, name := range []string{"join", "source_scan"} {
		if _, err := os.Stat(name); err == nil {
			t.Errorf("planner/%s exists; keep relationship proof in builder and source-scan fusion in optimizer", name)
		} else if !os.IsNotExist(err) {
			t.Fatalf("inspect planner/%s: %v", name, err)
		}
	}
}

func TestRootExternalTestsExerciseOrchestrationOrCrossDomainIntegration(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if parsed.Name.Name != "planner_test" || !containsTestFunction(parsed) {
			continue
		}

		importsRoot := false
		domains := map[string]struct{}{}
		for _, imported := range parsed.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf("unquote import in %s: %v", name, err)
			}
			if path == "github.com/meaningforge/metis/planner" {
				importsRoot = true
				break
			}
			const prefix = "github.com/meaningforge/metis/planner/"
			if strings.HasPrefix(path, prefix) {
				domain := strings.Split(strings.TrimPrefix(path, prefix), "/")[0]
				domains[domain] = struct{}{}
			}
		}
		if !importsRoot && len(domains) < 2 {
			t.Errorf("%s is a root external test but exercises neither planner orchestration nor a cross-domain integration; move it to its owning subpackage", name)
		}
	}
}

func containsTestFunction(file *ast.File) bool {
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Recv == nil && strings.HasPrefix(function.Name.Name, "Test") {
			return true
		}
	}
	return false
}
