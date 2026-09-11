package planner_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

func TestOffsetToGrainWidensLowerBoundToContainingBoundary(t *testing.T) {
	resolved := resolveMetricEvaluation(t, offsetToGrainEvaluationModel, query.SemanticQuery{
		Model:      "commerce",
		Metrics:    []query.MetricRef{{Name: "revenue_at_start_of_year"}},
		Dimensions: []query.DimensionRef{{Name: "order_date", Grain: grainPtr(query.TimeGrainMonth)}},
		Filters:    []query.Filter{{Field: "order_date", Operator: query.FilterGTE, Value: "2026-03-01"}},
	})
	plan, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	graph := plan
	if len(semanticPlanNodeFixturesForTest(graph)) != 2 {
		t.Fatalf("plan stages = %#v", semanticPlanNodeFixturesForTest(graph))
	}
	source := sourceStagePredicates(t, semanticPlanNodeFixturesForTest(graph))
	if len(source) != 1 {
		t.Fatalf("source predicates = %#v", source)
	}
	got := source[0].Filter
	if got.Operator != query.FilterGTE || got.Value != "2026-01-01" {
		t.Fatalf("source filter = %#v", got)
	}
	visible := finalOutputPredicates(t, graph)
	if len(visible) != 1 {
		t.Fatalf("post predicates = %#v", visible)
	}
	if visible[0].Filter.Operator != query.FilterGTE || visible[0].Filter.Value != "2026-03-01" {
		t.Fatalf("visible filter = %#v", visible[0].Filter)
	}
	if len(plan.Predicates) != 0 {
		t.Fatalf("visible predicate leaked into source plan = %#v", plan.Predicates)
	}
}

func TestOffsetToGrainWidensBetweenLowerBoundOnly(t *testing.T) {
	resolved := resolveMetricEvaluation(t, offsetToGrainEvaluationModel, query.SemanticQuery{
		Model:      "commerce",
		Metrics:    []query.MetricRef{{Name: "revenue_at_start_of_year"}},
		Dimensions: []query.DimensionRef{{Name: "order_date", Grain: grainPtr(query.TimeGrainMonth)}},
		Filters:    []query.Filter{{Field: "order_date", Operator: query.FilterBetween, Value: []string{"2026-03-01", "2026-05-31"}}},
	})
	plan, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	if len(semanticPlanNodeFixturesForTest(plan)) == 0 {
		t.Fatal("plan owns no stages")
	}
	source := sourceStagePredicates(t, semanticPlanNodeFixturesForTest(plan))
	if len(source) != 1 {
		t.Fatalf("source predicates = %#v", source)
	}
	got, ok := source[0].Filter.Value.([]string)
	if !ok || !reflect.DeepEqual(got, []string{"2026-01-01", "2026-05-31"}) {
		t.Fatalf("source range = %#v", source[0].Filter.Value)
	}
	visiblePredicates := finalOutputPredicates(t, plan)
	if len(visiblePredicates) != 1 {
		t.Fatalf("post predicates = %#v", visiblePredicates)
	}
	visible, ok := visiblePredicates[0].Filter.Value.([]string)
	if !ok || !reflect.DeepEqual(visible, []string{"2026-03-01", "2026-05-31"}) {
		t.Fatalf("visible range = %#v", visiblePredicates[0].Filter.Value)
	}
}

func TestOffsetToGrainRejectsNonRangeTimeFilter(t *testing.T) {
	resolved := resolveMetricEvaluation(t, offsetToGrainEvaluationModel, query.SemanticQuery{
		Model:      "commerce",
		Metrics:    []query.MetricRef{{Name: "revenue_at_start_of_year"}},
		Dimensions: []query.DimensionRef{{Name: "order_date", Grain: grainPtr(query.TimeGrainMonth)}},
		Filters:    []query.Filter{{Field: "order_date", Operator: query.FilterEQ, Value: "2026-03-01"}},
	})
	_, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrUnsupportedTimeFilter {
		t.Fatalf("error = %#v", err)
	}
}

func sourceStagePredicates(t *testing.T, stages []semanticNodeFixture) []semanticplan.Predicate {
	t.Helper()
	var out []semanticplan.Predicate
	for _, stage := range stages {
		if stage.Kind != semanticplan.SemanticPlanNodeSourceAggregate {
			continue
		}
		for _, owned := range stage.Predicates {
			if owned.Predicate != nil {
				out = append(out, *owned.Predicate)
			}
		}
	}
	return out
}

func finalOutputPredicates(t *testing.T, plan *semanticplan.SemanticPlan) []semanticplan.PostEvaluationPredicate {
	t.Helper()
	return append([]semanticplan.PostEvaluationPredicate(nil), plan.Output.Predicates...)
}
