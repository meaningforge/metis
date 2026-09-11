package semantic

import (
	"context"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestOptimizedAndUnoptimizedPlansPreserveAgentExplanation(t *testing.T) {
	renderer := mustRenderer(t, "DORIS")
	revenue := &ossie.Metric{Name: "revenue", Datatype: ossie.DataTypeDecimal}
	cost := &ossie.Metric{Name: "total_cost", Datatype: ossie.DataTypeDecimal}
	root := semanticplan.DatasetRef{Name: "orders", Source: "sales.orders"}
	plan := &semanticplan.SemanticPlan{
		Model: semanticplan.ModelRef{Project: "analytics", Name: "sales"},
		Root:  root,
		Projections: []semanticplan.Projection{
			{Name: "revenue", Kind: semanticplan.ProjectionMetric, Metric: revenue, Expression: expression.NewResolvedExpression("ANSI_SQL", "SUM(orders.amount)")},
			{Name: "total_cost", Kind: semanticplan.ProjectionMetric, Metric: cost, Expression: expression.NewResolvedExpression("ANSI_SQL", "SUM(orders.cost)")},
		},
		Requested: []string{"revenue", "total_cost"},
		Nodes: []semanticplan.SemanticPlanNode{
			semanticplan.SourceAggregateNode{
				Base: semanticplan.SemanticPlanNodeBase{
					ID:       "revenue",
					Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate,
					PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{
						Movement: semanticplan.SemanticPredicateBoundaryAllowedWithProof,
						Proof:    semanticplan.SemanticPredicateProofDatasetReachability,
					},
				},
				Source:      semanticplan.SemanticSourceState{Root: root, RequiredDatasets: []string{"orders"}, SourceRoots: []string{"orders"}},
				MetricState: semanticplan.SemanticMetricState{Metrics: []string{"revenue"}},
				Metric:      revenue,
				Expression:  expression.NewResolvedExpression("ANSI_SQL", "SUM(orders.amount)"),
			},
			semanticplan.SourceAggregateNode{
				Base: semanticplan.SemanticPlanNodeBase{
					ID:       "total_cost",
					Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate,
					PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{
						Movement: semanticplan.SemanticPredicateBoundaryAllowedWithProof,
						Proof:    semanticplan.SemanticPredicateProofDatasetReachability,
					},
				},
				Source:      semanticplan.SemanticSourceState{Root: root, RequiredDatasets: []string{"orders"}, SourceRoots: []string{"orders"}},
				MetricState: semanticplan.SemanticMetricState{Metrics: []string{"total_cost"}},
				Metric:      cost,
				Expression:  expression.NewResolvedExpression("ANSI_SQL", "SUM(orders.cost)"),
			},
		},
	}
	margin := &ossie.Metric{Name: "margin", Datatype: ossie.DataTypeDecimal}
	plan.Projections = append(plan.Projections, semanticplan.Projection{
		Name:       "margin",
		Kind:       semanticplan.ProjectionMetric,
		Metric:     margin,
		Expression: expression.NewResolvedExpression("ANSI_SQL", "revenue - total_cost"),
	})
	plan.Requested = []string{"margin"}
	plan.Nodes = append(plan.Nodes, semanticplan.PostAggregateNode{
		Base: semanticplan.SemanticPlanNodeBase{
			ID:       "margin",
			Boundary: semanticplan.SemanticPlanNodeBoundaryPostAggregate,
			PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{
				Movement: semanticplan.SemanticPredicateBoundaryBlocked,
				Proof:    semanticplan.SemanticPredicateProofNodeSemantics,
			},
			Inputs: []semanticplan.SemanticPlanNodeInput{{NodeID: "revenue"}, {NodeID: "total_cost"}},
		},
		MetricState: semanticplan.SemanticMetricState{Metrics: []string{"margin"}},
		Metric:      margin,
		Expression:  expression.NewResolvedExpression("ANSI_SQL", "revenue - total_cost"),
	})

	unoptimizedExplanation, err := buildQueryExplanation(plan)
	if err != nil {
		t.Fatal(err)
	}
	optimized, err := optimizer.Default().Optimize(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	optimizedExplanation, err := buildQueryExplanation(optimized)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(optimizedExplanation, unoptimizedExplanation) {
		t.Fatalf("Agent-facing explanation changed after optimization:\nunoptimized=%#v\noptimized=%#v", unoptimizedExplanation, optimizedExplanation)
	}

	unoptimizedPlan, err := conversion.BuildSQLPlan(plan, renderer)
	if err != nil {
		t.Fatal(err)
	}
	optimizedPlan, err := conversion.BuildSQLPlan(optimized, renderer)
	if err != nil {
		t.Fatal(err)
	}
	if len(optimizedPlan.Blocks) >= len(unoptimizedPlan.Blocks) {
		t.Fatalf("expected a structural reduction while preserving explanation: unoptimized=%d optimized=%d", len(unoptimizedPlan.Blocks), len(optimizedPlan.Blocks))
	}
}
