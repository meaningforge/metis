package resolver_test

import (
	"context"
	"strings"
	"testing"

	"github.com/meaningforge/metis/query"
)

func resolveDistinctValues(t *testing.T, q query.SemanticQuery) error {
	t.Helper()
	r := newResolver(t)
	q.Project = testProject
	_, err := r.inner.ResolveForRenderer(context.Background(), q, mustRenderer(t, "DUCKDB"))
	return err
}

func TestDistinctValuesIntentAcceptsOneDimension(t *testing.T) {
	err := resolveDistinctValues(t, query.SemanticQuery{
		Model:      "sales",
		Intent:     query.QueryIntentDistinctValues,
		Dimensions: []query.DimensionRef{{Name: "region"}},
		Filters:    []query.Filter{{Field: "region", Operator: query.FilterNEQ, Value: "unknown"}},
		OrderBy:    []query.OrderBy{{Field: "region", Direction: query.SortAsc}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDistinctValuesIntentRejectsMetrics(t *testing.T) {
	err := resolveDistinctValues(t, query.SemanticQuery{
		Model:      "sales",
		Intent:     query.QueryIntentDistinctValues,
		Metrics:    []query.MetricRef{{Name: "total_revenue"}},
		Dimensions: []query.DimensionRef{{Name: "region"}},
	})
	if err == nil || !strings.Contains(err.Error(), "does not accept metrics") {
		t.Fatalf("error = %v", err)
	}
}

func TestDistinctValuesIntentRequiresExactlyOneDimension(t *testing.T) {
	err := resolveDistinctValues(t, query.SemanticQuery{Model: "sales", Intent: query.QueryIntentDistinctValues})
	if err == nil || !strings.Contains(err.Error(), "exactly one dimension") {
		t.Fatalf("error = %v", err)
	}

	err = resolveDistinctValues(t, query.SemanticQuery{
		Model: "sales", Intent: query.QueryIntentDistinctValues,
		Dimensions: []query.DimensionRef{{Name: "region"}, {Name: "order_date"}},
	})
	if err == nil || !strings.Contains(err.Error(), "exactly one dimension") {
		t.Fatalf("error = %v", err)
	}
}

func TestDistinctValuesIntentRejectsMetricFiltersAndOrdering(t *testing.T) {
	metricFilter := query.SemanticQuery{
		Model: "sales", Intent: query.QueryIntentDistinctValues,
		Dimensions: []query.DimensionRef{{Name: "region"}},
		Filters:    []query.Filter{{Field: "total_revenue", Operator: query.FilterGT, Value: 100}},
	}
	if err := resolveDistinctValues(t, metricFilter); err == nil || !strings.Contains(err.Error(), "metric filters") {
		t.Fatalf("metric filter error = %v", err)
	}

	metricOrder := query.SemanticQuery{
		Model: "sales", Intent: query.QueryIntentDistinctValues,
		Dimensions: []query.DimensionRef{{Name: "region"}},
		OrderBy:    []query.OrderBy{{Field: "total_revenue", Direction: query.SortDesc}},
	}
	if err := resolveDistinctValues(t, metricOrder); err == nil || !strings.Contains(err.Error(), "order_by") {
		t.Fatalf("metric order error = %v", err)
	}
}

func TestRejectUnknownQueryIntent(t *testing.T) {
	err := resolveDistinctValues(t, query.SemanticQuery{
		Model: "sales", Intent: query.QueryIntent("mystery"),
		Dimensions: []query.DimensionRef{{Name: "region"}},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported query intent") {
		t.Fatalf("error = %v", err)
	}
}
