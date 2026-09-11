package planner_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/query"
)

const optimizerDifferentialModel = `
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

func TestOptimizedAndUnoptimizedPlansPreserveCanonicalOutputContract(t *testing.T) {
	resolved := resolveMetricEvaluation(t, optimizerDifferentialModel, query.SemanticQuery{
		Model:   "sales",
		Metrics: []query.MetricRef{{Name: "margin"}},
	})

	unoptimized, err := planner.New(nil).Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	optimized, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}

	unoptimizedSchema, err := conversion.BuildOutputSchema(unoptimized)
	if err != nil {
		t.Fatal(err)
	}
	optimizedSchema, err := conversion.BuildOutputSchema(optimized)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(optimizedSchema, unoptimizedSchema) {
		t.Fatalf("output schema changed after optimization:\nunoptimized=%#v\noptimized=%#v", unoptimizedSchema, optimizedSchema)
	}
	if !reflect.DeepEqual(optimized.Projections, unoptimized.Projections) ||
		!reflect.DeepEqual(optimized.Groups, unoptimized.Groups) ||
		!reflect.DeepEqual(optimized.Sorts, unoptimized.Sorts) ||
		!reflect.DeepEqual(optimized.Limit, unoptimized.Limit) {
		t.Fatal("public query shaping contract changed after optimization")
	}

	unoptimizedPlan, err := conversion.BuildSQLPlan(unoptimized, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	optimizedPlan, err := conversion.BuildSQLPlan(optimized, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	if len(optimizedPlan.Blocks) >= len(unoptimizedPlan.Blocks) {
		t.Fatalf("optimizer did not reduce staged block shape: unoptimized=%d optimized=%d", len(unoptimizedPlan.Blocks), len(optimizedPlan.Blocks))
	}
	optimizedGroups := sharedSourceMetricGroups(semanticPlanNodeFixturesForTest(optimized))
	if len(optimizedGroups) != 1 || !reflect.DeepEqual(optimizedGroups[0], []string{"revenue", "total_cost"}) {
		t.Fatalf("optimized canonical shared source groups = %#v, want one", optimizedGroups)
	}
	unoptimizedGroups := sharedSourceMetricGroups(semanticPlanNodeFixturesForTest(unoptimized))
	if len(unoptimizedGroups) != 0 {
		t.Fatalf("unoptimized canonical shared source groups = %#v, want none", unoptimizedGroups)
	}
}
