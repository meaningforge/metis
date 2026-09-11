package planner_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/serrors"
)

const metricEvaluationModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: finance
    datasets:
      - name: orders
        source: finance.orders
        fields:
          - name: day_id
            datatype: Date
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.day_id}]
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.amount}]
          - name: discount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.discount}]
      - name: costs
        source: finance.costs
        fields:
          - name: day_id
            datatype: Date
            expression:
              dialects: [{dialect: ANSI_SQL, expression: costs.day_id}]
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: costs.amount}]
      - name: calendar
        source: finance.calendar
        primary_key: [day_id]
        fields:
          - name: day_id
            datatype: Date
            expression:
              dialects: [{dialect: ANSI_SQL, expression: calendar.day_id}]
            dimension: {is_time: true}
    relationships:
      - name: orders_to_calendar
        from: orders
        to: calendar
        from_columns: [day_id]
        to_columns: [day_id]
      - name: costs_to_calendar
        from: costs
        to: calendar
        from_columns: [day_id]
        to_columns: [day_id]
    metrics:
      - name: revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount)"}]
      - name: discounts
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.discount)"}]
      - name: cost
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(costs.amount)"}]
      - name: contribution_margin
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "revenue - discounts"}]
      - name: gross_margin
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "revenue - cost"}]
`

func resolveMetricEvaluation(t *testing.T, modelText string, query query.SemanticQuery) *resolver.ResolvedSemanticQuery {
	t.Helper()
	doc, err := ossie.NewLoader().Load([]byte(modelText))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("test", doc)
	if err != nil {
		t.Fatal(err)
	}
	query.Project = "test"
	resolved, err := resolver.New(manifest.NewStore(snapshot)).ResolveForRenderer(context.Background(), query, mustRenderer(t, "DORIS"))
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestSemanticPlanDAGBuildsSameSourcePostAggregation(t *testing.T) {
	resolved := resolveMetricEvaluation(t, metricEvaluationModel, query.SemanticQuery{
		Model: "finance", Metrics: []query.MetricRef{{Name: "contribution_margin"}},
	})
	plan, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	wantNames := []string{"discounts", "revenue", "contribution_margin"}
	if !reflect.DeepEqual(semanticNodeFixtureNames(semanticPlanNodeFixturesForTest(plan)), wantNames) {
		t.Fatalf("evaluation order = %#v, want %#v", semanticNodeFixtureNames(semanticPlanNodeFixturesForTest(plan)), wantNames)
	}
	derived := semanticPlanNodeFixturesForTest(plan)[2]
	if derived.Kind != semanticplan.SemanticPlanNodePostAggregate || !reflect.DeepEqual(derived.SourceRoots, []string{"orders"}) {
		t.Fatalf("derived stage = %#v", derived)
	}
	groups := sharedSourceMetricGroups(semanticPlanNodeFixturesForTest(plan))
	if len(groups) != 1 || !reflect.DeepEqual(groups[0], []string{"discounts", "revenue"}) {
		t.Fatalf("canonical fused source groups = %#v", groups)
	}
	stmt, err := conversion.BuildSQLPlan(plan, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stmt.Blocks) != 3 || len(stmt.Blocks[0].Projections) != 2 {
		t.Fatalf("fused staged SQLPlan = %#v", stmt)
	}
}

func TestSemanticPlanDAGBuildsMultiSourceGrainComposition(t *testing.T) {
	grain := query.TimeGrainMonth
	resolved := resolveMetricEvaluation(t, metricEvaluationModel, query.SemanticQuery{
		Model:      "finance",
		Metrics:    []query.MetricRef{{Name: "gross_margin"}},
		Dimensions: []query.DimensionRef{{Name: "calendar.day_id", Grain: &grain}},
		Filters:    []query.Filter{{Field: "calendar.day_id", Operator: query.FilterGTE, Value: "2026-01-01"}},
	})
	plan, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	stages := semanticPlanNodeFixturesForTest(plan)
	if len(stages) != 3 {
		t.Fatalf("semantic plan = %#v", plan)
	}
	if groups := sharedSourceMetricGroups(stages); len(groups) != 0 {
		t.Fatalf("incompatible source contexts were fused: %#v", groups)
	}
	for _, stage := range stages {
		if !semanticNodeFixtureNodeExpressionResolved(stage) {
			t.Fatalf("metric %q has no target-resolved node expression", stage.ID)
		}
	}
	derived := stages[2]
	if derived.Kind != semanticplan.SemanticPlanNodeJoinAggregates {
		t.Fatalf("composition kind = %q, want %q", derived.Kind, semanticplan.SemanticPlanNodeJoinAggregates)
	}
	if !reflect.DeepEqual(derived.SourceRoots, []string{"costs", "orders"}) {
		t.Fatalf("source roots = %#v", derived.SourceRoots)
	}
	for _, leaf := range stages[:2] {
		if len(leaf.Joins) != 1 || len(leaf.OutputGrain) != 1 || len(leaf.Predicates) != 1 {
			t.Fatalf("leaf placement = %#v", leaf)
		}
		predicate := leaf.Predicates[0]
		if predicate.Scope != semanticplan.SemanticPredicatePreAggregation || predicate.Predicate == nil {
			t.Fatalf("leaf predicate placement = %#v", predicate)
		}
	}

	stmt, err := conversion.BuildSQLPlan(plan, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stmt.Blocks) != 4 || len(stmt.Blocks[2].Joins) != 1 {
		t.Fatalf("staged SQLPlan = %#v", stmt)
	}
	if got := stmt.Blocks[2].Joins[0].Kind; got != "full_outer" {
		t.Fatalf("composition join kind = %q", got)
	}
}

func TestSemanticPlanDAGStagesIndependentMultiSourceMetrics(t *testing.T) {
	grain := query.TimeGrainMonth
	resolved := resolveMetricEvaluation(t, metricEvaluationModel, query.SemanticQuery{
		Model:      "finance",
		Metrics:    []query.MetricRef{{Name: "revenue"}, {Name: "cost"}},
		Dimensions: []query.DimensionRef{{Name: "calendar.day_id", Grain: &grain}},
	})
	plan, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	// Independent source roots are constructed as separate stages. That used to
	// be asserted against a resolver flag; the stages are the evidence now.
	stages := semanticPlanNodeFixturesForTest(plan)
	if len(stages) != 2 {
		t.Fatalf("semantic stages = %#v", stages)
	}
	if got := semanticNodeFixtureNames(stages); !reflect.DeepEqual(got, []string{"revenue", "cost"}) {
		t.Fatalf("evaluation order = %#v", got)
	}
	stmt, err := conversion.BuildSQLPlan(plan, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	root := stmt.Blocks[len(stmt.Blocks)-1]
	if len(stmt.Blocks) != 3 || len(root.Joins) != 1 || root.Joins[0].Kind != "full_outer" {
		t.Fatalf("independent source composition SQLPlan = %#v", stmt)
	}
}

func TestSemanticPlanDAGUsesCrossJoinForUngroupedSources(t *testing.T) {
	resolved := resolveMetricEvaluation(t, metricEvaluationModel, query.SemanticQuery{
		Model: "finance", Metrics: []query.MetricRef{{Name: "gross_margin"}},
	})
	plan, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	stages := semanticPlanNodeFixturesForTest(plan)
	if len(stages) != 3 {
		t.Fatalf("semantic plan = %#v", plan)
	}
	if got := stages[2].Kind; got != semanticplan.SemanticPlanNodeCrossJoinAggregates {
		t.Fatalf("composition kind = %q, want %q", got, semanticplan.SemanticPlanNodeCrossJoinAggregates)
	}
}

const disconnectedMetricEvaluationModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: finance
    datasets:
      - name: orders
        source: finance.orders
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.amount}]
          - name: status
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.status}]
            dimension: {}
      - name: costs
        source: finance.costs
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: costs.amount}]
    metrics:
      - name: revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount)"}]
      - name: cost
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(costs.amount)"}]
      - name: gross_margin
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "revenue - cost"}]
`

func TestSemanticPlanDAGRejectsPartiallyApplicableFilter(t *testing.T) {
	resolved := resolveMetricEvaluation(t, disconnectedMetricEvaluationModel, query.SemanticQuery{
		Model:   "finance",
		Metrics: []query.MetricRef{{Name: "gross_margin"}},
		Filters: []query.Filter{{Field: "orders.status", Operator: query.FilterEQ, Value: "paid"}},
	})
	_, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err == nil {
		t.Fatal("expected metric evaluation planning error")
	}
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrMetricSourceUnreachable {
		t.Fatalf("error = %#v, want %s", err, serrors.ErrMetricSourceUnreachable)
	}
}

func semanticNodeFixtureNames(stages []semanticNodeFixture) []string {
	out := make([]string, len(stages))
	for i := range stages {
		out[i] = stages[i].ID
	}
	return out
}

func sharedSourceMetricGroups(stages []semanticNodeFixture) [][]string {
	byGroup := map[string][]string{}
	order := make([]string, 0)
	for _, stage := range stages {
		if stage.Kind != semanticplan.SemanticPlanNodeSourceAggregate || stage.ShareGroup == "" {
			continue
		}
		if _, ok := byGroup[stage.ShareGroup]; !ok {
			order = append(order, stage.ShareGroup)
		}
		byGroup[stage.ShareGroup] = append(byGroup[stage.ShareGroup], stage.ID)
	}
	out := make([][]string, 0, len(order))
	for _, group := range order {
		out = append(out, byGroup[group])
	}
	return out
}

func semanticNodeFixtureNodeExpressionResolved(stage semanticNodeFixture) bool {
	switch node := stage.Node.(type) {
	case semanticplan.SourceAggregateNode:
		return node.Expression.IsResolved()
	case semanticplan.PostAggregateNode:
		return node.Expression.IsResolved()
	case semanticplan.JoinAggregatesNode:
		return node.Expression.IsResolved()
	case semanticplan.CrossJoinAggregatesNode:
		return node.Expression.IsResolved()
	case semanticplan.CumulativeWindowNode:
		return node.Expression.IsResolved()
	case semanticplan.TimeOffsetNode:
		return node.Expression.IsResolved()
	case semanticplan.OffsetToGrainNode:
		return node.Expression.IsResolved()
	case semanticplan.ConversionNode:
		return node.Expression.IsResolved()
	case semanticplan.SemiAdditiveNode:
		return node.Expression.IsResolved()
	default:
		return false
	}
}
