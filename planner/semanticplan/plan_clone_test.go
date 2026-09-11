package semanticplan

import (
	"reflect"
	"testing"

	"github.com/meaningforge/metis/query"
)

func TestClonePlanOwnsMutablePlanStateAndDropsTrace(t *testing.T) {
	limit := 10
	plan := &SemanticPlan{
		Projections:       []Projection{{Name: "revenue", Datasets: []string{"orders", "customers"}}},
		Requested:         []string{"revenue"},
		Limit:             &limit,
		OptimizationTrace: []OptimizationStep{{Rule: "source_scan_fusion", Changed: true}},
		Nodes: []SemanticPlanNode{SourceAggregateNode{
			Base: SemanticPlanNodeBase{ID: "revenue", Dimensions: []string{"day"}},
			Source: SemanticSourceState{
				SourceRoots:      []string{"orders"},
				RequiredDatasets: []string{"orders", "customers"},
			},
			MetricState: SemanticMetricState{Metrics: []string{"revenue"}},
		}},
		DenseCalendar: &DenseCalendarPlan{OutputPredicates: []Predicate{{Dataset: "calendar"}}},
		SharedGrain:   &SharedGrainResolution{Metrics: []MetricSharedGrainEvidence{{RootDatasets: []string{"orders"}}}},
	}

	clone := ClonePlan(plan)
	if clone == plan || clone.Limit == plan.Limit || clone.OptimizationTrace != nil {
		t.Fatalf("clone did not establish plan snapshot ownership: %#v", clone)
	}
	clone.Projections[0].Datasets[0] = "changed"
	clone.Requested[0] = "changed"
	*clone.Limit = 5
	clone.DenseCalendar.OutputPredicates[0].Dataset = "changed"
	clone.SharedGrain.Metrics[0].RootDatasets[0] = "changed"
	node := clone.Nodes[0].(SourceAggregateNode)
	node.Source.SourceRoots[0] = "changed"
	clone.Nodes[0] = node

	if got, want := plan.Projections[0].Datasets, []string{"orders", "customers"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("projection datasets mutated through clone: got %v, want %v", got, want)
	}
	if got, want := plan.Requested, []string{"revenue"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("requested mutated through clone: got %v, want %v", got, want)
	}
	if *plan.Limit != 10 || plan.DenseCalendar.OutputPredicates[0].Dataset != "calendar" || plan.SharedGrain.Metrics[0].RootDatasets[0] != "orders" {
		t.Fatalf("plan-owned nested state mutated through clone: %#v", plan)
	}
	if got := plan.Nodes[0].(SourceAggregateNode).Source.SourceRoots[0]; got != "orders" {
		t.Fatalf("node source roots mutated through clone: %q", got)
	}
}

func TestCanonicalizeFingerprintSetsPreservesSequences(t *testing.T) {
	plan := &SemanticPlan{
		Projections: []Projection{{Datasets: []string{"customers", "orders", "customers"}}},
		Nodes: []SemanticPlanNode{SourceAggregateNode{
			Base: SemanticPlanNodeBase{ID: "revenue", Dimensions: []string{"day", "", "day", "month"}},
			Source: SemanticSourceState{
				SourceRoots:      []string{"orders", "", "customers", "orders"},
				RequiredDatasets: []string{"customers", "orders", "customers"},
			},
			MetricState: SemanticMetricState{Metrics: []string{"revenue", "", "revenue", "profit"}},
		}},
		Groups: []GroupBy{{Name: "day", Grain: timeGrain(query.TimeGrainDay)}, {Name: "month", Grain: timeGrain(query.TimeGrainMonth)}},
	}

	CanonicalizeFingerprintSets(plan)
	if got, want := plan.Projections[0].Datasets, []string{"customers", "orders"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("projection datasets = %v, want %v", got, want)
	}
	node := plan.Nodes[0].(SourceAggregateNode)
	if got, want := node.Base.Dimensions, []string{"day", "month"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dimensions = %v, want %v", got, want)
	}
	if got, want := node.Source.SourceRoots, []string{"customers", "orders"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("source roots = %v, want %v", got, want)
	}
	if got, want := node.MetricState.Metrics, []string{"profit", "revenue"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("metrics = %v, want %v", got, want)
	}
	if got, want := []string{plan.Groups[0].Name, plan.Groups[1].Name}, []string{"day", "month"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sequence-valued groups changed: got %v, want %v", got, want)
	}
}

func timeGrain(value query.TimeGrain) *query.TimeGrain { return &value }
