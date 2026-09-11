package compiler_test

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
)

func TestCumulativeMetricPlanningContract(t *testing.T) {
	doc, err := ossie.NewLoader().Load(fixtures.CommerceModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest(projectName, doc)
	if err != nil {
		t.Fatal(err)
	}
	grain := query.TimeGrainMonth
	query := query.SemanticQuery{
		Project:    projectName,
		Model:      modelName,
		Metrics:    []query.MetricRef{{Name: "cumulative_revenue"}},
		Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}},
	}
	renderer := mustRenderer(t, "DORIS")
	resolved, err := resolver.New(manifest.NewStore(snapshot)).ResolveForRenderer(context.Background(), query, renderer)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planner.New().Plan(context.Background(), resolved, renderer)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Nodes) == 0 {
		t.Fatal("plan owns no nodes")
	}
	if got, want := len(plan.Nodes), 2; got != want {
		t.Fatalf("semantic nodes = %d, want %d", got, want)
	}
	base, ok := plan.Nodes[0].(semanticplan.SourceAggregateNode)
	if !ok || base.NodeBase().ID != "revenue" {
		t.Fatalf("base node = %#v, want revenue source aggregate", plan.Nodes[0])
	}
	cumulative, ok := plan.Nodes[1].(semanticplan.CumulativeWindowNode)
	if !ok || cumulative.NodeBase().ID != "cumulative_revenue" {
		t.Fatalf("cumulative node = %#v", plan.Nodes[1])
	}
	spec := cumulative.Spec
	if spec.BaseMetric != "revenue" || spec.TimeDimension != "order_date" || spec.Window.Type != "unbounded" {
		t.Fatalf("cumulative spec = %#v", spec)
	}
	inputs := cumulative.NodeBase().Inputs
	if len(inputs) != 1 || inputs[0].NodeID != "revenue" {
		t.Fatalf("cumulative inputs = %#v, want revenue", inputs)
	}
}
