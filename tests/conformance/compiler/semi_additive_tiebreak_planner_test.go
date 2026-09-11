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

func TestSemiAdditiveTieBreakPreservesHiddenSecondaryInputGrain(t *testing.T) {
	doc, err := ossie.NewLoader().Load(fixtures.CommerceModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	model := &doc.SemanticModel[0]

	fieldAdded := false
	for i := range model.Datasets {
		dataset := &model.Datasets[i]
		for _, field := range dataset.Fields {
			if field.Name != "snapshot_date" {
				continue
			}
			dataset.Fields = append(dataset.Fields, ossie.Field{
				Name: "snapshot_sequence", Datatype: ossie.DataTypeInteger,
				Expression: ossie.Expression{Dialects: []ossie.DialectExpression{{Dialect: ossie.DialectANSISQL, Expression: dataset.Name + ".snapshot_sequence"}}},
				Dimension:  &ossie.Dimension{},
			})
			fieldAdded = true
			break
		}
	}
	if !fieldAdded {
		t.Fatal("snapshot_date field not found")
	}

	found := false
	for i := range model.Metrics {
		if model.Metrics[i].Name != "inventory_balance" {
			continue
		}
		for j := range model.Metrics[i].CustomExtensions {
			ext := &model.Metrics[i].CustomExtensions[j]
			if ext.VendorName != ossie.MetisExtensionVendor {
				continue
			}
			ext.Data = `{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"last","tie_break_dimension":"snapshot_sequence"}`
			found = true
			break
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
	q := query.SemanticQuery{Project: projectName, Model: modelName, Metrics: []query.MetricRef{{Name: "inventory_balance"}}, Dimensions: []query.DimensionRef{{Name: "warehouse"}}}
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
		t.Fatalf("semantic graph = %#v", plan.Nodes)
	}
	derived := semanticGraphNodeByID(t, plan.Nodes, "inventory_balance")
	derivedBase := derived.NodeBase()
	if len(derivedBase.Inputs) != 1 {
		t.Fatalf("semi-additive inputs = %#v", derivedBase.Inputs)
	}
	base := semanticGraphNodeByID(t, plan.Nodes, derivedBase.Inputs[0].NodeID)
	if got := groupNames(base.NodeBase().OutputGrain); !reflect.DeepEqual(got, []string{"warehouse", "snapshot_date", "snapshot_sequence"}) {
		t.Fatalf("base input grain = %v, want warehouse + hidden snapshot_date + snapshot_sequence", got)
	}
}

func TestSemiAdditiveWindowGroupingPreservesHiddenEvaluationGrain(t *testing.T) {
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
			if ext.VendorName != ossie.MetisExtensionVendor {
				continue
			}
			ext.Data = `{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"last","window_groupings":["warehouse"]}`
			found = true
			break
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
	q := query.SemanticQuery{Project: projectName, Model: modelName, Metrics: []query.MetricRef{{Name: "inventory_balance"}}}
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
		t.Fatalf("semantic graph = %#v", plan.Nodes)
	}

	derivedNode := semanticGraphNodeByID(t, plan.Nodes, "inventory_balance")
	derived, ok := derivedNode.(semanticplan.SemiAdditiveNode)
	if !ok {
		t.Fatalf("inventory_balance node = %T, want SemiAdditiveNode", derivedNode)
	}
	derivedBase := derived.NodeBase()
	if len(derivedBase.Inputs) != 1 {
		t.Fatalf("semi-additive inputs = %#v", derivedBase.Inputs)
	}
	base := semanticGraphNodeByID(t, plan.Nodes, derivedBase.Inputs[0].NodeID)
	if got := groupNames(base.NodeBase().OutputGrain); !reflect.DeepEqual(got, []string{"snapshot_date", "warehouse"}) {
		t.Fatalf("base input grain = %v, want hidden snapshot_date + warehouse", got)
	}
	if len(derivedBase.OutputGrain) != 0 {
		t.Fatalf("derived output grain = %v, window grouping must stay hidden", groupNames(derivedBase.OutputGrain))
	}
	if !reflect.DeepEqual(derived.Spec.WindowGroupings, []string{"warehouse"}) {
		t.Fatalf("semi-additive spec = %#v", derived.Spec)
	}
}
