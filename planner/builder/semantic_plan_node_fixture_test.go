package builder

import (
	"reflect"
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/extension"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func TestSemanticPlanNodeGraphOwnsCanonicalEvidence(t *testing.T) {
	grain := query.TimeGrain("month")
	groups := []semanticplan.GroupBy{{Name: "order_month", Dataset: "orders", Grain: &grain}}
	predicate := semanticplan.Predicate{Dataset: "orders", Filter: query.Filter{Field: "status", Operator: query.FilterEQ, Value: "paid"}}
	post := semanticplan.PostEvaluationPredicate{Name: "revenue_copy", Filter: query.Filter{Field: "revenue_copy", Operator: query.FilterGT, Value: 10}}
	scale := extension.MetricScaleEvidence{
		Identity: extension.Identity{Namespace: "metis.semantic", Kind: "metric_scale", Scope: "metric"},
		Metric:   "revenue",
		Version:  "1",
		Factor:   2,
	}
	shared := &semanticplan.MetricSharedGrainEvidence{
		Metric:            "revenue",
		RootDataset:       "orders",
		RootDatasets:      []string{"orders"},
		GrainKey:          "orders|order_month|month|",
		RelationshipPaths: [][]string{{"orders"}},
		FanoutSafe:        true,
	}
	plan := semanticPlanForTest(semanticplan.SemanticPlan{Requested: []string{"revenue_copy"},
		Output: semanticplan.SemanticOutputContract{Grain: groups, Predicates: []semanticplan.PostEvaluationPredicate{post}}}, []semanticNodeFixture{
		semanticTestNodeFixture(semanticNodeFixture{
			ID:               "revenue",
			Kind:             semanticplan.SemanticPlanNodeSourceAggregate,
			Boundary:         semanticplan.SemanticPlanNodeBoundarySourceAggregate,
			OutputGrain:      groups,
			SourceRoots:      []string{"orders"},
			RequiredDatasets: []string{"orders"},
			Root:             semanticplan.DatasetRef{Name: "orders", Source: "analytics.orders"},
			Predicates: []semanticNodePredicateFixture{{
				Scope: semanticplan.SemanticPredicatePreAggregation, OwnerNodeID: "revenue", Proof: semanticplan.SemanticPredicateProofSourceOwnership, Predicate: &predicate,
			}},
			ExtensionEvidence:   []extension.Evidence{scale},
			SharedGrainEvidence: shared,
			Node: semanticplan.SourceAggregateNode{
				Expression: expression.NewResolvedExpression("ANSI_SQL", "SUM(orders.amount)").WithExtensionEvidence(scale),
			},
		}),
		semanticTestNodeFixture(semanticNodeFixture{
			ID:          "revenue_copy",
			Kind:        semanticplan.SemanticPlanNodePostAggregate,
			Boundary:    semanticplan.SemanticPlanNodeBoundaryPostAggregate,
			Inputs:      []semanticNodeInputFixture{{NodeID: "revenue", Grain: groups}},
			OutputGrain: groups,
			Node:        semanticplan.PostAggregateNode{},
		}),
	})

	if err := semanticplan.ValidateSemanticPlanDAG(plan); err != nil {
		t.Fatal(err)
	}
	stages, err := semanticNodeFixturesFromPlanNodes(plan.Nodes)
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) != 2 {
		t.Fatalf("stage count = %d, want 2", len(stages))
	}
	source := stages[0]
	if got, want := source.Dimensions, []string(nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("dimensions = %#v, want %#v", got, want)
	}
	if len(source.Predicates) != 1 || source.Predicates[0].Scope != semanticplan.SemanticPredicatePreAggregation {
		t.Fatalf("source predicates = %#v", source.Predicates)
	}
	if source.SharedGrainEvidence == nil || !source.SharedGrainEvidence.FanoutSafe {
		t.Fatalf("shared grain evidence = %#v", source.SharedGrainEvidence)
	}
	if evidence := source.ExtensionEvidence; len(evidence) != 1 {
		t.Fatalf("extension evidence = %#v", evidence)
	} else if _, ok := evidence[0].(extension.MetricScaleEvidence); !ok {
		t.Fatalf("extension evidence type = %T", evidence[0])
	}
	if got := stages[1].Inputs[0].NodeID; got != "revenue" {
		t.Fatalf("derived input = %q, want revenue", got)
	}
	if len(plan.Output.Predicates) != 1 || plan.Output.Predicates[0].Name != "revenue_copy" {
		t.Fatalf("post predicates = %#v", plan.Output.Predicates)
	}

	mutateSemanticPlanNodeFixturesForTest(plan, func(stages []semanticNodeFixture) {
		stages[0].SourceRoots[0] = "mutated"
		stages[0].OutputGrain[0].Name = "mutated"
		stages[0].SharedGrainEvidence.RootDatasets[0] = "mutated"
	})
	if source.SourceRoots[0] != "orders" || source.OutputGrain[0].Name != "order_month" || source.SharedGrainEvidence.RootDatasets[0] != "orders" {
		t.Fatalf("the stage planner aliases plan-owned slices: %#v", source)
	}
}

func TestSemanticPlanNodeDAGPreservesDependencyOrder(t *testing.T) {
	stages := []semanticNodeFixture{
		semanticTestNodeFixture(semanticNodeFixture{ID: "a", Kind: semanticplan.SemanticPlanNodeSourceAggregate, Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate}),
		semanticTestNodeFixture(semanticNodeFixture{ID: "b", Kind: semanticplan.SemanticPlanNodeSourceAggregate, Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate}),
		semanticTestNodeFixture(semanticNodeFixture{ID: "ratio", Kind: semanticplan.SemanticPlanNodePostAggregate, Boundary: semanticplan.SemanticPlanNodeBoundaryPostAggregate, Inputs: []semanticNodeInputFixture{{NodeID: "b"}, {NodeID: "a"}}}),
	}
	got := []string{stages[2].Inputs[0].NodeID, stages[2].Inputs[1].NodeID}
	want := []string{"b", "a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("input order = %#v, want %#v", got, want)
	}
}

func TestBuildOwnedSemanticNodesUsesCanonicalConstructionInputs(t *testing.T) {
	grain := query.TimeGrain("month")
	groups := []semanticplan.GroupBy{{Name: "order_month", Dataset: "orders", Grain: &grain}}
	input := ConstructionInput{
		Requested: []string{"revenue"},
		Nodes: []semanticplan.SemanticPlanNode{semanticplan.SourceAggregateNode{
			Base:        semanticplan.SemanticPlanNodeBase{ID: "revenue", OutputGrain: groups},
			Source:      semanticplan.SemanticSourceState{SourceRoots: []string{"orders"}},
			MetricState: semanticplan.SemanticMetricState{Metrics: []string{"revenue"}},
		}},
		OutputGrain: groups,
	}
	shared := &semanticplan.SharedGrainResolution{Metrics: []semanticplan.MetricSharedGrainEvidence{{
		Metric:       "revenue",
		RootDatasets: []string{"orders"},
		GrainKey:     "orders|order_month|month|",
		FanoutSafe:   true,
	}}}

	nodes, err := semanticplan.OwnNodes(input.Nodes, shared)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].NodeBase().ID != "revenue" {
		t.Fatalf("nodes = %#v", nodes)
	}
	owned := nodes[0].(semanticplan.SourceAggregateNode)
	if owned.MetricState.SharedGrainEvidence == nil || !owned.MetricState.SharedGrainEvidence.FanoutSafe {
		t.Fatalf("shared grain evidence = %#v", owned.MetricState.SharedGrainEvidence)
	}

	inputNode := input.Nodes[0].(semanticplan.SourceAggregateNode)
	inputNode.Source.SourceRoots[0] = "stale_orders"
	shared.Metrics[0].RootDatasets[0] = "stale_orders"
	if got := owned.Source.SourceRoots; !reflect.DeepEqual(got, []string{"orders"}) {
		t.Fatalf("nodes alias canonical construction input: %#v", got)
	}
	if got := owned.MetricState.SharedGrainEvidence.RootDatasets; !reflect.DeepEqual(got, []string{"orders"}) {
		t.Fatalf("nodes alias shared-grain input: %#v", got)
	}
}
