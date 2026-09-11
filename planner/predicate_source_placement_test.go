package planner_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

const stagedSourcePredicateModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: sales.orders
        fields:
          - name: amount
            datatype: Decimal
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.amount}]}
          - name: cost
            datatype: Decimal
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.cost}]}
          - name: status
            datatype: String
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.status}]}
    metrics:
      - name: revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount)"}]}
      - name: total_cost
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.cost)"}]}
      - name: margin
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "revenue - total_cost"}]}
`

func TestPlannerOwnsStagedSourcePredicatePlacementBeforeOptimization(t *testing.T) {
	resolved := resolveMetricEvaluation(t, stagedSourcePredicateModel, query.SemanticQuery{Model: "sales", Metrics: []query.MetricRef{{Name: "margin"}}, Filters: []query.Filter{{Field: "status", Operator: query.FilterEQ, Value: "paid"}}})

	unoptimized, err := planner.New(nil).Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	if len(semanticPlanNodeFixturesForTest(unoptimized)) == 0 {
		t.Fatal("canonical semantic graph is nil")
	}
	if len(unoptimized.Predicates) != 1 {
		t.Fatalf("top-level predicates = %#v, want planner-owned staged copy", unoptimized.Predicates)
	}
	predicate := unoptimized.Predicates[0]
	for _, stage := range semanticPlanNodeFixturesForTest(unoptimized) {
		switch stage.Kind {
		case semanticplan.SemanticPlanNodeSourceAggregate:
			if len(stage.Predicates) != 1 || stage.Predicates[0].Predicate == nil || !reflect.DeepEqual(*stage.Predicates[0].Predicate, predicate) {
				t.Fatalf("source metric %q predicates = %#v, want planner placement proof", stage.ID, stage.Predicates)
			}
		default:
			if len(stage.Predicates) != 0 {
				t.Fatalf("derived metric %q received source predicate: %#v", stage.ID, stage.Predicates)
			}
		}
	}

	optimized, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	if len(optimized.Predicates) != 0 {
		t.Fatalf("optimized top-level predicates = %#v, want consumed redundant copy", optimized.Predicates)
	}
	for _, stage := range semanticPlanNodeFixturesForTest(optimized) {
		if stage.Kind != semanticplan.SemanticPlanNodeSourceAggregate {
			continue
		}
		if len(stage.Predicates) != 1 || stage.Predicates[0].Predicate == nil || !reflect.DeepEqual(*stage.Predicates[0].Predicate, predicate) {
			t.Fatalf("optimized source metric %q predicates = %#v, want unchanged planner placement", stage.ID, stage.Predicates)
		}
	}
}
