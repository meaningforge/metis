package harness

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/compiler"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

// CompileScenarioForRenderer returns the complete production compilation
// artifact for one canonical scenario. Runtime-backed execution tests pass the
// exact Renderer selected with their DataSource Backend so there is no second
// dialect or Renderer authority inside the harness.
func CompileScenarioForRenderer(t *testing.T, scenario scenarios.Scenario, selected renderer.Renderer) *artifact.CompiledQuery {
	t.Helper()
	return compileScenario(t, scenario, selected, true)
}

func compileScenario(t *testing.T, scenario scenarios.Scenario, selected renderer.Renderer, optimize bool) *artifact.CompiledQuery {
	t.Helper()
	definition, ok := fixtures.Lookup(scenario.Fixture)
	if !ok {
		t.Fatalf("unknown conformance fixture %q", scenario.Fixture)
	}
	doc, err := ossie.NewLoader().Load(definition.Document)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest(definition.Project, doc)
	if err != nil {
		t.Fatal(err)
	}
	semanticQuery := scenario.Query
	semanticQuery.Project = definition.Project
	semanticQuery.Model = definition.Model
	if selected == nil {
		t.Fatal("selected Renderer is required")
	}
	resolved, err := resolver.New(manifest.NewStore(snapshot)).ResolveForRenderer(context.Background(), semanticQuery, selected)
	if err != nil {
		t.Fatal(err)
	}

	var plan *semanticplan.SemanticPlan
	if optimize {
		plan, err = planner.New().Plan(context.Background(), resolved, selected)
	} else {
		plan, err = planner.New(nil).Plan(context.Background(), resolved, selected)
	}
	if err != nil {
		t.Fatal(err)
	}

	compiled, err := compiler.CompileWithRenderer(context.Background(), plan, selected)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func mustRenderer(t *testing.T, dialect string) renderer.Renderer {
	t.Helper()
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := registry.Resolve(sql.SQLDialect(dialect))
	if err != nil {
		t.Fatal(err)
	}
	return renderer
}

func compilePlan(ctx context.Context, plan *semanticplan.SemanticPlan, renderer renderer.Renderer) (*artifact.CompiledQuery, error) {
	compiled, err := compiler.CompileWithRenderer(ctx, plan, renderer)
	if err != nil {
		return nil, err
	}
	return compiled, nil
}
