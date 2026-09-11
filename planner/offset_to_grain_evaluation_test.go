package planner_test

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

const offsetToGrainEvaluationModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: commerce
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - name: order_date
            datatype: Date
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.order_date}]
            dimension: {is_time: true}
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.amount}]
    metrics:
      - name: revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount)"}]
      - name: revenue_at_start_of_year
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: revenue}]
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"offset_to_grain","base_metric":"revenue","time_dimension":"order_date","grain":"year"}'
`

func TestSemanticPlanDAGPlansOffsetToGrainAfterBaseMetric(t *testing.T) {
	resolved := resolveMetricEvaluation(t, offsetToGrainEvaluationModel, query.SemanticQuery{
		Model:      "commerce",
		Metrics:    []query.MetricRef{{Name: "revenue_at_start_of_year"}},
		Dimensions: []query.DimensionRef{{Name: "order_date", Grain: grainPtr(query.TimeGrainMonth)}},
	})
	plan, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	stages := semanticPlanNodeFixturesForTest(plan)
	if len(stages) != 2 {
		t.Fatalf("stages = %#v", stages)
	}
	base := stages[0]
	offset := stages[1]
	if base.ID != "revenue" || base.Kind != semanticplan.SemanticPlanNodeSourceAggregate {
		t.Fatalf("base = %#v", base)
	}
	if offset.ID != "revenue_at_start_of_year" || offset.Kind != semanticplan.SemanticPlanNodeOffsetToGrain {
		t.Fatalf("offset = %#v", offset)
	}
	if len(offset.Inputs) != 1 || offset.Inputs[0].NodeID != "revenue" {
		t.Fatalf("inputs = %#v", offset.Inputs)
	}
	offsetNode, ok := offset.Node.(semanticplan.OffsetToGrainNode)
	if !ok || offsetNode.OffsetPlan == nil {
		t.Fatalf("offset node = %#v", offset.Node)
	}
	boundary := offsetNode.OffsetPlan
	if boundary.QueryGrain != query.TimeGrainMonth || boundary.BoundaryGrain != query.TimeGrainYear {
		t.Fatalf("boundary = %#v", boundary)
	}
}

func TestSemanticPlanDAGRejectsOffsetToGrainDependencyMismatch(t *testing.T) {
	broken := `
version: "0.2.0.dev0"
semantic_model:
  - name: commerce
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - name: order_date
            datatype: Date
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.order_date}]}
            dimension: {is_time: true}
          - name: amount
            datatype: Decimal
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.amount}]}
    metrics:
      - name: revenue
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount)"}]}
      - name: other
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount)"}]}
      - name: bad_offset
        expression: {dialects: [{dialect: ANSI_SQL, expression: other}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"offset_to_grain","base_metric":"revenue","time_dimension":"order_date","grain":"year"}'
`
	resolved := resolveMetricEvaluation(t, broken, query.SemanticQuery{
		Model:      "commerce",
		Metrics:    []query.MetricRef{{Name: "bad_offset"}},
		Dimensions: []query.DimensionRef{{Name: "order_date", Grain: grainPtr(query.TimeGrainMonth)}},
	})
	if _, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB")); err == nil {
		t.Fatal("expected dependency mismatch planning error")
	}
}

func grainPtr(grain query.TimeGrain) *query.TimeGrain { return &grain }
