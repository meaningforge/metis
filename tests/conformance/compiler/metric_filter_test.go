package compiler_test

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func semanticGraphNode(plan *semanticplan.SemanticPlan, name string) semanticplan.SemanticPlanNode {
	if plan == nil || len(plan.Nodes) == 0 {
		return nil
	}
	for _, node := range plan.Nodes {
		if node.NodeBase().ID == name {
			return node
		}
	}
	return nil
}

func semanticGraphPostPredicateNames(plan *semanticplan.SemanticPlan) []string {
	if plan == nil {
		return nil
	}
	var out []string
	for _, predicate := range plan.Output.Predicates {
		out = append(out, predicate.Name)
	}
	return out
}

func TestSourceMetricFilterIsPostEvaluation(t *testing.T) {
	q := query.SemanticQuery{
		Metrics:    []query.MetricRef{{Name: "revenue"}},
		Dimensions: []query.DimensionRef{{Name: "status"}},
		Filters:    []query.Filter{{Field: "revenue", Operator: query.FilterGT, Value: 1000}},
	}
	plan, sqlQuery, err := compile(t, q, "DORIS")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Nodes) == 0 {
		t.Fatal("plan owns no nodes")
	}
	if got := semanticGraphPostPredicateNames(plan); len(got) != 1 || got[0] != "revenue" {
		t.Fatalf("post predicates = %#v", got)
	}
	for _, node := range plan.Nodes {
		if node.Kind() != semanticplan.SemanticPlanNodeSourceAggregate {
			continue
		}
		base := node.NodeBase()
		for _, predicate := range base.Predicates {
			if predicate.Scope == semanticplan.SemanticPredicatePreAggregation {
				t.Fatalf("source predicates = %#v, metric filter must not be pushed down", base.Predicates)
			}
		}
	}
	if !strings.Contains(sqlQuery.SQL, "WHERE") || len(sqlQuery.Parameters) != 1 {
		t.Fatalf("SQL/params missing final metric filter:\n%s\nparams=%#v", sqlQuery.SQL, sqlQuery.Parameters)
	}
}

func TestFilterOnlyMetricIsEvaluatedButNotProjected(t *testing.T) {
	q := query.SemanticQuery{
		Metrics:    []query.MetricRef{{Name: "orders_count"}},
		Dimensions: []query.DimensionRef{{Name: "status"}},
		Filters:    []query.Filter{{Field: "revenue", Operator: query.FilterGTE, Value: 500}},
	}
	plan, sqlQuery, err := compile(t, q, "DORIS")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Projections) != 2 || plan.Projections[1].Name != "orders_count" {
		t.Fatalf("projections = %#v, filter-only revenue must stay hidden", plan.Projections)
	}
	if semanticGraphNode(plan, "revenue") == nil {
		t.Fatalf("semantic nodes = %#v, hidden filter metric revenue was pruned", plan.Nodes)
	}
	if !strings.Contains(sqlQuery.SQL, "revenue") || len(sqlQuery.Parameters) != 1 {
		t.Fatalf("SQL missing hidden filter metric evaluation:\n%s", sqlQuery.SQL)
	}
}

func TestDerivedMetricFilterRunsAfterDerivedEvaluation(t *testing.T) {
	q := query.SemanticQuery{
		Metrics:    []query.MetricRef{{Name: "revenue"}},
		Dimensions: []query.DimensionRef{{Name: "status"}},
		Filters:    []query.Filter{{Field: "contribution_margin", Operator: query.FilterBetween, Value: []int{100, 10000}}},
	}
	plan, sqlQuery, err := compile(t, q, "DORIS")
	if err != nil {
		t.Fatal(err)
	}
	node := semanticGraphNode(plan, "contribution_margin")
	if node == nil || node.Kind() != semanticplan.SemanticPlanNodePostAggregate {
		t.Fatalf("semantic nodes = %#v, derived filter metric not evaluated", plan.Nodes)
	}
	if got := semanticGraphPostPredicateNames(plan); len(got) != 1 || got[0] != "contribution_margin" {
		t.Fatalf("post predicates = %#v", got)
	}
	if len(sqlQuery.Parameters) != 2 || !strings.Contains(sqlQuery.SQL, "BETWEEN") {
		t.Fatalf("derived metric filter SQL mismatch:\n%s params=%#v", sqlQuery.SQL, sqlQuery.Parameters)
	}
	if !strings.Contains(sqlQuery.SQL, "`metric_003_contribution_margin`.`contribution_margin` BETWEEN") {
		t.Fatalf("derived metric filter is not bound to its materialized relation:\n%s", sqlQuery.SQL)
	}
	if !strings.Contains(sqlQuery.SQL, "JOIN `metric_003_contribution_margin`") || strings.Contains(sqlQuery.SQL, "FULL OUTER JOIN `metric_003_contribution_margin`") {
		t.Fatalf("filter-only derived metric must constrain rather than expand the visible result domain:\n%s", sqlQuery.SQL)
	}
}

func TestTimeOffsetMetricFilterRunsAfterOffsetEvaluation(t *testing.T) {
	grain := query.TimeGrainMonth
	q := query.SemanticQuery{
		Metrics:    []query.MetricRef{{Name: "revenue"}},
		Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}, {Name: "region"}},
		Filters:    []query.Filter{{Field: "previous_month_revenue", Operator: query.FilterGT, Value: 0}},
	}
	plan, sqlQuery, err := compile(t, q, "DORIS")
	if err != nil {
		t.Fatal(err)
	}
	node := semanticGraphNode(plan, "previous_month_revenue")
	if node == nil || node.Kind() != semanticplan.SemanticPlanNodeTimeOffset {
		t.Fatalf("semantic nodes = %#v, time-offset filter metric not evaluated", plan.Nodes)
	}
	if got := semanticGraphPostPredicateNames(plan); len(got) != 1 || got[0] != "previous_month_revenue" {
		t.Fatalf("post predicates = %#v", got)
	}
	if len(sqlQuery.Parameters) != 1 || !strings.Contains(sqlQuery.SQL, "previous_month_revenue") {
		t.Fatalf("offset metric filter SQL mismatch:\n%s params=%#v", sqlQuery.SQL, sqlQuery.Parameters)
	}
	if !strings.Contains(sqlQuery.SQL, "`metric_002_previous_month_revenue`.`previous_month_revenue` >") {
		t.Fatalf("time-offset metric filter is not bound to its materialized relation:\n%s", sqlQuery.SQL)
	}
	if !strings.Contains(sqlQuery.SQL, "JOIN `metric_002_previous_month_revenue`") || strings.Contains(sqlQuery.SQL, "FULL OUTER JOIN `metric_002_previous_month_revenue`") {
		t.Fatalf("filter-only time-offset metric must constrain rather than expand the visible result domain:\n%s", sqlQuery.SQL)
	}
}

func TestMetricAndDimensionFiltersKeepSeparatePlacement(t *testing.T) {
	q := query.SemanticQuery{
		Metrics:    []query.MetricRef{{Name: "revenue"}},
		Dimensions: []query.DimensionRef{{Name: "status"}},
		Filters: []query.Filter{
			{Field: "status", Operator: query.FilterEQ, Value: "paid"},
			{Field: "revenue", Operator: query.FilterGT, Value: 1000},
		},
	}
	plan, sqlQuery, err := compile(t, q, "DORIS")
	if err != nil {
		t.Fatal(err)
	}
	if got := semanticGraphPostPredicateNames(plan); len(got) != 1 || got[0] != "revenue" {
		t.Fatalf("post predicates = %#v, want only revenue", got)
	}
	foundLeafStatus := false
	for _, node := range plan.Nodes {
		if node.Kind() != semanticplan.SemanticPlanNodeSourceAggregate {
			continue
		}
		for _, owned := range node.NodeBase().Predicates {
			if owned.Scope != semanticplan.SemanticPredicatePreAggregation || owned.Predicate == nil {
				continue
			}
			predicate := owned.Predicate
			if predicate.Filter.Field == "status" {
				foundLeafStatus = true
			}
			if predicate.Filter.Field == "revenue" {
				t.Fatalf("metric predicate pushed into leaf: %#v", predicate)
			}
		}
	}
	if !foundLeafStatus {
		t.Fatalf("semantic nodes = %#v, status filter was not pushed to leaf", plan.Nodes)
	}
	if got := len(sqlQuery.Parameters); got != 2 {
		t.Fatalf("parameters = %d, want dimension + metric filters", got)
	}
	if strings.Count(sqlQuery.SQL, "WHERE") < 2 {
		t.Fatalf("expected leaf and final WHERE clauses:\n%s", sqlQuery.SQL)
	}
}

func TestCumulativeMetricFilterRunsAfterWindowEvaluation(t *testing.T) {
	grain := query.TimeGrainMonth
	q := query.SemanticQuery{
		Metrics:    []query.MetricRef{{Name: "cumulative_revenue"}},
		Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}},
		Filters:    []query.Filter{{Field: "cumulative_revenue", Operator: query.FilterGT, Value: 5000}},
	}
	plan, sqlQuery, err := compile(t, q, "DORIS")
	if err != nil {
		t.Fatal(err)
	}
	node := semanticGraphNode(plan, "cumulative_revenue")
	if node == nil || node.Kind() != semanticplan.SemanticPlanNodeCumulativeWindow {
		t.Fatalf("semantic nodes = %#v, cumulative window node missing", plan.Nodes)
	}
	for _, source := range plan.Nodes {
		if source.Kind() != semanticplan.SemanticPlanNodeSourceAggregate {
			continue
		}
		for _, owned := range source.NodeBase().Predicates {
			if owned.Predicate != nil && owned.Predicate.Filter.Field == "cumulative_revenue" {
				t.Fatalf("cumulative metric filter pushed below window: %#v", owned.Predicate)
			}
		}
	}
	if got := semanticGraphPostPredicateNames(plan); len(got) != 1 || got[0] != "cumulative_revenue" {
		t.Fatalf("post predicates = %#v", got)
	}
	wherePos := strings.LastIndex(sqlQuery.SQL, "WHERE")
	overPos := strings.Index(sqlQuery.SQL, "OVER (")
	if overPos < 0 || wherePos < 0 || wherePos < overPos {
		t.Fatalf("cumulative filter must render after window evaluation:\n%s", sqlQuery.SQL)
	}
	if len(sqlQuery.Parameters) != 1 {
		t.Fatalf("parameters = %#v, want cumulative metric threshold", sqlQuery.Parameters)
	}
}

func TestMetricFilterRejectsUnsupportedEqualityInV1(t *testing.T) {
	q := query.SemanticQuery{Metrics: []query.MetricRef{{Name: "revenue"}}, Filters: []query.Filter{{Field: "revenue", Operator: query.FilterEQ, Value: 100}}}
	_, _, err := compile(t, q, "DORIS")
	if err == nil || !strings.Contains(err.Error(), "metric filters v1") {
		t.Fatalf("error = %v, want explicit metric-filter v1 operator error", err)
	}
}
