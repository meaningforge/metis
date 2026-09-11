package resolver_test

import (
	"context"
	"errors"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/serrors"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
)

func TestOrdinaryMetricMayUseNonCanonicalTimeDimension(t *testing.T) {
	r := fixtureResolver(t)
	grain := query.TimeGrainMonth
	q := query.SemanticQuery{Project: "metricflow-reference", Model: "commerce", Metrics: []query.MetricRef{{Name: "revenue"}}, Dimensions: []query.DimensionRef{{Name: "ship_date", Grain: &grain}}}
	resolved, err := r.Resolve(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Metrics) != 1 || resolved.Metrics[0].TimeBinding == nil || resolved.Metrics[0].TimeBinding.TimeDimension != "order_date" {
		t.Fatalf("resolved metric binding = %#v", resolved.Metrics)
	}
	if len(resolved.Dimensions) != 1 || resolved.Dimensions[0].Name != "ship_date" {
		t.Fatalf("resolved dimensions = %#v", resolved.Dimensions)
	}
}

func TestTimeRelativeMetricRejectsNonCanonicalTimeDimension(t *testing.T) {
	r := fixtureResolver(t)
	grain := query.TimeGrainMonth
	for _, metric := range []string{"cumulative_revenue", "previous_month_revenue"} {
		t.Run(metric, func(t *testing.T) {
			q := query.SemanticQuery{Project: "metricflow-reference", Model: "commerce", Metrics: []query.MetricRef{{Name: metric}}, Dimensions: []query.DimensionRef{{Name: "ship_date", Grain: &grain}}}
			_, err := r.Resolve(context.Background(), q)
			var apiErr *serrors.Error
			if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrInvalidQuery {
				t.Fatalf("error = %#v, want INVALID_QUERY", err)
			}
			if got := apiErr.Details["canonical_time_dimension"]; got != "order_date" {
				t.Fatalf("canonical_time_dimension = %v", got)
			}
			if got := apiErr.Details["query_time_dimension"]; got != "ship_date" {
				t.Fatalf("query_time_dimension = %v", got)
			}
		})
	}
}

func TestTimeRelativeMetricAllowsCanonicalTimeDimension(t *testing.T) {
	r := fixtureResolver(t)
	grain := query.TimeGrainMonth
	for _, metric := range []string{"cumulative_revenue", "previous_month_revenue"} {
		q := query.SemanticQuery{Project: "metricflow-reference", Model: "commerce", Metrics: []query.MetricRef{{Name: metric}}, Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}}}
		if _, err := r.Resolve(context.Background(), q); err != nil {
			t.Fatalf("%s resolve: %v", metric, err)
		}
	}
}

func TestDerivedMetricInheritsCanonicalTimeBinding(t *testing.T) {
	r := fixtureResolver(t)
	grain := query.TimeGrainMonth
	q := query.SemanticQuery{Project: "metricflow-reference", Model: "commerce", Metrics: []query.MetricRef{{Name: "revenue_growth_rate"}}, Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}}}
	resolved, err := r.Resolve(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Metrics) != 1 || resolved.Metrics[0].TimeBinding == nil || resolved.Metrics[0].TimeBinding.TimeDimension != "order_date" {
		t.Fatalf("derived metric binding = %#v, want inherited order_date", resolved.Metrics)
	}
	assertEvaluationMetricBinding(t, resolved, "previous_month_revenue", "order_date")
	assertEvaluationMetricBinding(t, resolved, "revenue_growth_rate", "order_date")
}

func TestFilterOnlyDerivedMetricInheritsCanonicalTimeBinding(t *testing.T) {
	r := fixtureResolver(t)
	grain := query.TimeGrainMonth
	q := query.SemanticQuery{
		Project: "metricflow-reference", Model: "commerce",
		Metrics:    []query.MetricRef{{Name: "revenue"}},
		Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}},
		Filters:    []query.Filter{{Field: "revenue_growth_rate", Operator: query.FilterGT, Value: 0}},
	}
	resolved, err := r.Resolve(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	assertEvaluationMetricBinding(t, resolved, "previous_month_revenue", "order_date")
	assertEvaluationMetricBinding(t, resolved, "revenue_growth_rate", "order_date")
}

func TestOrderOnlyDerivedMetricInheritsCanonicalTimeBinding(t *testing.T) {
	r := fixtureResolver(t)
	grain := query.TimeGrainMonth
	q := query.SemanticQuery{
		Project: "metricflow-reference", Model: "commerce",
		Metrics:    []query.MetricRef{{Name: "revenue"}},
		Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}},
		OrderBy:    []query.OrderBy{{Field: "revenue_growth_rate", Direction: query.SortDesc}},
	}
	resolved, err := r.Resolve(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	assertEvaluationMetricBinding(t, resolved, "previous_month_revenue", "order_date")
	assertEvaluationMetricBinding(t, resolved, "revenue_growth_rate", "order_date")
}

func TestDerivedMetricRejectsConflictingCanonicalTimeBindings(t *testing.T) {
	const modelYAML = `version: "0.2.0.dev0"
semantic_model:
  - name: multi_time
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - name: order_date
            datatype: Date
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.order_date}]}
            dimension: {is_time: true}
          - name: ship_date
            datatype: Date
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.ship_date}]}
            dimension: {is_time: true}
          - {name: amount, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: orders.amount}]}}
    metrics:
      - name: ordered_revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount)"}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"time_binding","time_dimension":"order_date"}'
      - name: shipped_revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount)"}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"time_binding","time_dimension":"ship_date"}'
      - name: blended_revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "ordered_revenue + shipped_revenue"}]}
`
	doc, err := ossie.NewLoader().Load([]byte(modelYAML))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("p", doc)
	if err != nil {
		t.Fatal(err)
	}
	r := resolver.New(manifest.NewStore(snapshot))
	q := query.SemanticQuery{Project: "p", Model: "multi_time", Metrics: []query.MetricRef{{Name: "blended_revenue"}}}
	_, err = r.Resolve(context.Background(), q)
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrInvalidQuery {
		t.Fatalf("error = %#v, want INVALID_QUERY", err)
	}
	if apiErr.Message != "derived metric dependencies have incompatible canonical time bindings" {
		t.Fatalf("message = %q", apiErr.Message)
	}
}

func assertEvaluationMetricBinding(t *testing.T, resolved *resolver.ResolvedSemanticQuery, metricName, timeDimension string) {
	t.Helper()
	for _, metric := range resolved.EvaluationMetrics {
		if metric.Name == metricName {
			if metric.TimeBinding == nil || metric.TimeBinding.TimeDimension != timeDimension {
				t.Fatalf("%s binding = %#v, want %s", metricName, metric.TimeBinding, timeDimension)
			}
			return
		}
	}
	t.Fatalf("evaluation metric %q not found", metricName)
}

func fixtureResolver(t *testing.T) *resolver.Resolver {
	t.Helper()
	doc, err := ossie.NewLoader().Load(fixtures.CommerceModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("metricflow-reference", doc)
	if err != nil {
		t.Fatal(err)
	}
	return resolver.New(manifest.NewStore(snapshot))
}
