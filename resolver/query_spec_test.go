package resolver_test

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/resolver"
)

func TestResolveForRendererReturnsSemanticQuerySpecConsumedByPlanner(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(semanticModel))
	if err != nil {
		t.Fatal(err)
	}
	semanticManifest, err := manifest.BuildProjectManifest(testProject, doc)
	if err != nil {
		t.Fatal(err)
	}
	renderer := mustRenderer(t, "DORIS")
	var spec *resolver.SemanticQuerySpec
	spec, err = resolver.New(manifest.NewStore(semanticManifest)).ResolveForRenderer(context.Background(), query.SemanticQuery{
		Project: testProject,
		Model:   "sales",
		Metrics: []query.MetricRef{{Name: "total_revenue"}},
	}, renderer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := planner.New().Plan(context.Background(), spec, renderer); err != nil {
		t.Fatal(err)
	}
}
