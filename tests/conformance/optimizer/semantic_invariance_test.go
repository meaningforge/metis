package optimizer_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/compiler"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/tests/conformance/evidence"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func TestOptimizerDifferentialCorePreservesSemanticExplanation(t *testing.T) {
	for _, target := range evidence.CompilerTargets() {
		target := target
		t.Run(target.Dialect, func(t *testing.T) {
			for _, scenario := range scenarios.OptimizerDifferentialCore() {
				scenario := scenario
				t.Run(scenario.Name, func(t *testing.T) {
					optimized, unoptimized, renderer := planScenario(t, scenario, target.Dialect)

					if (len(optimized.Nodes) == 0) != (len(unoptimized.Nodes) == 0) {
						t.Fatalf("optimizer changed canonical graph ownership: optimized=%t unoptimized=%t", len(optimized.Nodes) != 0, len(unoptimized.Nodes) != 0)
					}
					if len(optimized.Nodes) == 0 {
						assertGraphlessCompileInvariant(t, optimized, unoptimized, renderer)
					}

					optimizedExplain, err := semanticplan.Explain(optimized)
					if err != nil {
						t.Fatalf("explain optimized plan: %v", err)
					}
					unoptimizedExplain, err := semanticplan.Explain(unoptimized)
					if err != nil {
						t.Fatalf("explain unoptimized plan: %v", err)
					}
					if !reflect.DeepEqual(optimizedExplain, unoptimizedExplain) {
						t.Fatalf("optimizer changed semantic explanation:\noptimized=%#v\nunoptimized=%#v", optimizedExplain, unoptimizedExplain)
					}
				})
			}
		})
	}
}

func assertGraphlessCompileInvariant(t *testing.T, optimized, unoptimized *semanticplan.SemanticPlan, renderer renderer.Renderer) {
	t.Helper()
	optimizedPhysical, err := compiler.CompileWithRenderer(context.Background(), optimized, renderer)
	if err != nil {
		t.Fatalf("compile optimized graphless plan: %v", err)
	}
	unoptimizedPhysical, err := compiler.CompileWithRenderer(context.Background(), unoptimized, renderer)
	if err != nil {
		t.Fatalf("compile unoptimized graphless plan: %v", err)
	}
	if !reflect.DeepEqual(optimizedPhysical, unoptimizedPhysical) {
		t.Fatalf("optimizer changed graphless physical contract:\noptimized=%#v\nunoptimized=%#v", optimizedPhysical, unoptimizedPhysical)
	}
}

func planScenario(t *testing.T, scenario scenarios.Scenario, dialect string) (*semanticplan.SemanticPlan, *semanticplan.SemanticPlan, renderer.Renderer) {
	t.Helper()
	definition, ok := fixtures.Lookup(scenario.Fixture)
	if !ok {
		t.Fatalf("unknown conformance fixture %q", scenario.Fixture)
	}
	document, err := ossie.NewLoader().Load(definition.Document)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest(definition.Project, document)
	if err != nil {
		t.Fatal(err)
	}
	semanticQuery := scenario.Query
	semanticQuery.Project = definition.Project
	semanticQuery.Model = definition.Model
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := registry.Resolve(sql.SQLDialect(dialect))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolver.New(manifest.NewStore(snapshot)).ResolveForRenderer(context.Background(), semanticQuery, renderer)
	if err != nil {
		t.Fatal(err)
	}
	optimized, err := planner.New().Plan(context.Background(), resolved, renderer)
	if err != nil {
		t.Fatal(err)
	}
	unoptimized, err := planner.New(nil).Plan(context.Background(), resolved, renderer)
	if err != nil {
		t.Fatal(err)
	}
	return optimized, unoptimized, renderer
}
