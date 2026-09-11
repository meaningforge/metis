package builder

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMetricEvaluationAuthorityBoundary(t *testing.T) {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve planner source directory")
	}
	dir := filepath.Dir(current)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == "EvaluationMetrics" {
				t.Errorf("%s consumes ResolvedSemanticQuery.EvaluationMetrics instead of the supplied MetricEvaluationPlan", name)
			}
			if owner, ok := selector.X.(*ast.Ident); ok && owner.Name == "evaluation" && selector.Sel.Name == "BuildMetricEvaluationPlan" {
				t.Errorf("%s builds a second MetricEvaluationPlan outside root Planner orchestration", name)
			}
			return true
		})
	}
}
