package semanticplan_test

import (
	"reflect"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/extension"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func init() {
	seeders[reflect.TypeOf((*semanticplan.SemanticPlanNode)(nil)).Elem()] = func(seed string) reflect.Value {
		resolved := expression.NewResolvedExpression("DUCKDB", seed).
			WithAnalysis(expression.BoundExpression{Bindings: map[expression.Reference]expression.BoundSymbol{}}, expression.TypedExpression{Deterministic: true}).
			WithExtensionEvidence(extension.MetricScaleEvidence{Identity: extension.Identity{}, Metric: seed, Version: "v1", Factor: 7})
		return reflect.ValueOf(semanticplan.SemanticPlanNode(semanticplan.SourceAggregateNode{
			Base: semanticplan.SemanticPlanNodeBase{
				ID:         seed,
				Dimensions: []string{seed},
				Inputs:     []semanticplan.SemanticPlanNodeInput{{NodeID: seed}},
			},
			Source:      semanticplan.SemanticSourceState{SourceRoots: []string{seed}, RequiredDatasets: []string{seed}},
			MetricState: semanticplan.SemanticMetricState{Metrics: []string{seed}},
			Expression:  resolved,
		}))
	}
}
