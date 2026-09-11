package compiler_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
)

func TestSemiAdditiveFirstBuildsEarliestSnapshotSemanticNode(t *testing.T) {
	doc, err := ossie.NewLoader().Load(fixtures.CommerceModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	model := &doc.SemanticModel[0]
	found := false
	for i := range model.Metrics {
		if model.Metrics[i].Name != "inventory_balance" {
			continue
		}
		for j := range model.Metrics[i].CustomExtensions {
			ext := &model.Metrics[i].CustomExtensions[j]
			if ext.VendorName == ossie.MetisExtensionVendor {
				ext.Data = `{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"first"}`
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("inventory_balance semi-additive extension not found")
	}
	if err := ossie.ValidateDocument(doc); err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest(projectName, doc)
	if err != nil {
		t.Fatal(err)
	}
	q := query.SemanticQuery{
		Project:    projectName,
		Model:      modelName,
		Metrics:    []query.MetricRef{{Name: "inventory_balance"}},
		Dimensions: []query.DimensionRef{{Name: "warehouse"}},
	}
	renderer := mustRenderer(t, "DORIS")
	resolved, err := resolver.New(manifest.NewStore(snapshot)).ResolveForRenderer(context.Background(), q, renderer)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planner.New().Plan(context.Background(), resolved, renderer)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Nodes) != 2 {
		t.Fatalf("semantic nodes = %#v, want inventory_quantity + inventory_balance", plan.Nodes)
	}
	base, ok := plan.Nodes[0].(semanticplan.SourceAggregateNode)
	if !ok {
		t.Fatalf("base node = %T, want SourceAggregateNode", plan.Nodes[0])
	}
	first, ok := plan.Nodes[1].(semanticplan.SemiAdditiveNode)
	if !ok {
		t.Fatalf("semi-additive node = %T, want SemiAdditiveNode", plan.Nodes[1])
	}
	if first.NodeBase().ID != "inventory_balance" || first.Kind() != semanticplan.SemanticPlanNodeSemiAdditiveFirst {
		t.Fatalf("semi-additive node = %#v", first)
	}
	if first.Spec.Aggregation != "first" {
		t.Fatalf("semi-additive spec = %#v", first.Spec)
	}
	if got := groupNames(base.NodeBase().OutputGrain); !reflect.DeepEqual(got, []string{"warehouse", "snapshot_date"}) {
		t.Fatalf("base input grain = %v, want warehouse + hidden snapshot_date", got)
	}
	if got := groupNames(first.NodeBase().OutputGrain); !reflect.DeepEqual(got, []string{"warehouse"}) {
		t.Fatalf("semi-additive output grain = %v, want user-visible warehouse only", got)
	}
	inputs := first.NodeBase().Inputs
	if len(inputs) != 1 || inputs[0].NodeID != "inventory_quantity" {
		t.Fatalf("semi-additive inputs = %#v, want inventory_quantity", inputs)
	}
}
